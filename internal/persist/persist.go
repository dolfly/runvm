// Package persist keeps what must survive one runvm process: the session
// ledger and one directory per persistent browser profile.
//
// Everything lives as plain files under a single data directory
// (RUNVM_DATA_DIR; /work/runvm in a container, the user config directory
// otherwise). The directory and every profile directory are 0700 and the
// ledger is 0600, because a browser profile is a live login: whoever can read
// it can be the user on every site they are signed in to.
//
// The ledger is a record, not a resume mechanism: it says which sessions
// existed, what they were, and which profile could bring one back. Nothing
// here boots a browser or an emulator.
package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// ContainerDir is where the image keeps application data (/data/apps/runvm
	// on the host). When it exists the default data directory lands under it,
	// so rebuilding the container keeps the ledger and the profiles.
	ContainerDir = "/work"

	// ProfilesSubdir holds one directory per persistent browser profile.
	ProfilesSubdir = "profiles"

	// ledgerName is the session ledger inside the data directory.
	ledgerName = "sessions.json"

	// ledgerVersion is the schema this build writes and understands.
	ledgerVersion = 1
)

// Store is the on-disk state behind the session set: the data directory, the
// session ledger and the browser profiles. It is safe for concurrent use.
type Store struct {
	dir string

	mu       sync.Mutex
	entries  []Entry
	loadErr  error
	readOnly bool // the ledger came from a newer schema: never overwrite it
	writeErr error
}

// Open creates dir (0700) if needed and loads the session ledger from it.
//
// A corrupt ledger is not fatal: it is renamed aside and reported through
// LoadErr, so the server starts with an empty ledger instead of dying or
// silently overwriting evidence.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("persist: empty data directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("persist: data directory %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("persist: create data directory %q: %w", abs, err)
	}
	// MkdirAll can only clear bits through the umask; set the mode explicitly
	// because the directory holds live logins.
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("persist: mode of data directory %q: %w", abs, err)
	}
	s := &Store{dir: abs}
	s.entries, s.readOnly, s.loadErr = load(abs)
	return s, nil
}

// FromEnv opens the data directory named by RUNVM_DATA_DIR, or the default.
func FromEnv() (*Store, error) { return Open(EnvDir()) }

// EnvDir is the configured data directory: RUNVM_DATA_DIR when set, the
// default otherwise.
func EnvDir() string {
	if v := strings.TrimSpace(os.Getenv("RUNVM_DATA_DIR")); v != "" {
		return v
	}
	return DefaultDir()
}

// DefaultDir picks the data directory when RUNVM_DATA_DIR is unset: /work/runvm
// when /work exists (the container's writable application directory), else
// <user config>/runvm.
func DefaultDir() string {
	if st, err := os.Stat(ContainerDir); err == nil && st.IsDir() {
		return filepath.Join(ContainerDir, "runvm")
	}
	if cfg, err := os.UserConfigDir(); err == nil && cfg != "" {
		return filepath.Join(cfg, "runvm")
	}
	return filepath.Join(os.TempDir(), "runvm")
}

// Dir is the data directory.
func (s *Store) Dir() string { return s.dir }

// LoadErr reports why the ledger could not be read at startup, if it could not.
func (s *Store) LoadErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// WriteErr reports the most recent failed ledger write, if any.
func (s *Store) WriteErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeErr
}

// Entries returns the recorded sessions, oldest first.
func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Save writes entries as the whole ledger: temp file + rename, so a concurrent
// reader sees either the old file or the new one, never a half-written one.
//
// Callers must serialize their snapshots (the session set does): the ledger is
// a full snapshot, so the last write wins.
func (s *Store) Save(entries []Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly {
		s.writeErr = errors.New("persist: ledger was written by a newer runvm; refusing to overwrite it")
		return s.writeErr
	}
	if entries == nil {
		entries = []Entry{}
	}
	doc := ledgerFile{
		Version:   ledgerVersion,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Sessions:  entries,
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		s.writeErr = fmt.Errorf("persist: encode ledger: %w", err)
		return s.writeErr
	}
	buf = append(buf, '\n')

	path := filepath.Join(s.dir, ledgerName)
	tmp, err := os.CreateTemp(s.dir, ".sessions-*.tmp")
	if err != nil {
		s.writeErr = fmt.Errorf("persist: temp ledger: %w", err)
		return s.writeErr
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		s.writeErr = fmt.Errorf("persist: mode of temp ledger: %w", err)
		return s.writeErr
	}
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		s.writeErr = fmt.Errorf("persist: write temp ledger: %w", err)
		return s.writeErr
	}
	// Sync before the rename: a container that is killed right after the
	// rename must not leave a zero-length ledger behind.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		s.writeErr = fmt.Errorf("persist: sync temp ledger: %w", err)
		return s.writeErr
	}
	if err := tmp.Close(); err != nil {
		s.writeErr = fmt.Errorf("persist: close temp ledger: %w", err)
		return s.writeErr
	}
	if err := os.Rename(tmpName, path); err != nil {
		s.writeErr = fmt.Errorf("persist: replace %s: %w", path, err)
		return s.writeErr
	}
	s.entries = entries
	s.writeErr = nil
	return nil
}

// ProfileDir returns <data dir>/profiles/<name>, creating it 0700. The name is
// a single path element: a profile name must never be able to climb out of the
// profiles directory.
func (s *Store) ProfileDir(name string) (string, error) { return ProfileDir(s.dir, name) }

// ResumeID returns the recorded session id that used to hold profile, so
// reopening the same profile can address the same id again. It returns "" when
// the profile was never recorded.
func (s *Store) ResumeID(profile string) string {
	if profile == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if e.Backend == "h5" && e.Profile == profile {
			return e.ID
		}
	}
	return ""
}

// ledgerFile is the on-disk shape of the ledger.
type ledgerFile struct {
	Version   int     `json:"version"`
	UpdatedAt string  `json:"updated_at"`
	Sessions  []Entry `json:"sessions"`
}

// load reads the ledger. readOnly is true when the file was written by a newer
// schema, in which case it is left untouched.
func load(dir string) (entries []Entry, readOnly bool, err error) {
	path := filepath.Join(dir, ledgerName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("persist: read %s: %w", path, err)
	}
	var doc ledgerFile
	if err := json.Unmarshal(data, &doc); err != nil {
		backup := fmt.Sprintf("%s.bad-%d", path, time.Now().Unix())
		if rerr := os.Rename(path, backup); rerr != nil {
			return nil, false, fmt.Errorf("persist: %s is corrupt (%v) and could not be kept (%v)", path, err, rerr)
		}
		return nil, false, fmt.Errorf("persist: %s is corrupt (%v); kept as %s", path, err, backup)
	}
	if doc.Version > ledgerVersion {
		return nil, true, fmt.Errorf("persist: %s has ledger version %d, this build understands %d", path, doc.Version, ledgerVersion)
	}
	out := make([]Entry, 0, len(doc.Sessions))
	for _, e := range doc.Sessions {
		if e.ID == "" || e.Backend == "" {
			continue // a hand-edited line must not become a phantom session
		}
		out = append(out, e)
	}
	return out, false, nil
}
