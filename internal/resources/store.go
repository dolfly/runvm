// Package resources implements the writable half of runvm's resource space:
// the on-disk store behind the runvm://files/<relative-path> URIs.
//
// The store is deliberately small and strict. Every path is validated before
// it touches the filesystem, and everything that can fail does so with an
// explicit error instead of a silent truncation or a write outside the root.
// This is the layer the HTTP console (stage 2) reuses for its edit/upload
// controls, so its safety properties belong here rather than in one transport.
package resources

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultDir is the resources root when RUNVM_RESOURCES_DIR is unset: a
// runvm-resources directory under the process working directory.
const DefaultDir = "runvm-resources"

// DefaultMaxSize caps a single stored resource at 64 MiB unless overridden.
const DefaultMaxSize = 64 << 20

// Sentinel errors let callers (and the MCP tools) tell "the caller asked for
// something out of bounds" apart from "the disk failed".
var (
	// ErrTraversal reports a path that leaves the resources root: an absolute
	// path, a ".." component, or a symlink that resolves outside it.
	ErrTraversal = errors.New("resource path escapes the resources root")
	// ErrTooLarge reports a resource over the configured single-file limit.
	ErrTooLarge = errors.New("resource exceeds the size limit")
)

// Meta describes one stored resource. It is the result of upload/write and one
// row of a listing.
type Meta struct {
	URI      string    `json:"uri"`
	Name     string    `json:"name"` // relative path within the root, /-separated
	Size     int64     `json:"size"`
	Sha256   string    `json:"sha256,omitempty"`
	ModTime  time.Time `json:"mod_time"`
	MimeType string    `json:"mime_type,omitempty"`
}

// Store is a writable resource root. It owns a real (symlink-resolved)
// directory and enforces a per-file size cap.
type Store struct {
	root    string
	maxSize int64
}

// New creates the store. dir is created with 0700 if missing, then resolved
// through symlinks so every later containment check compares real paths.
// maxSize <= 0 means DefaultMaxSize.
func New(dir string, maxSize int64) (*Store, error) {
	if dir == "" {
		dir = DefaultDir
	}
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("resources: create root: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resources: resolve root: %w", err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, fmt.Errorf("resources: abs root: %w", err)
	}
	return &Store{root: real, maxSize: maxSize}, nil
}

// Root returns the resolved root directory.
func (s *Store) Root() string { return s.root }

// MaxSize returns the per-file limit in bytes.
func (s *Store) MaxSize() int64 { return s.maxSize }

// ParseSize turns a human size like "64MiB" or "67108864" into bytes. It is
// the parser behind RUNVM_RESOURCES_MAX_SIZE.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	n, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	unit := strings.ToUpper(strings.TrimSpace(s[i:]))
	var mult int64 = 1
	switch unit {
	case "", "B":
	case "K", "KB", "KIB":
		mult = 1 << 10
	case "M", "MB", "MIB":
		mult = 1 << 20
	case "G", "GB", "GIB":
		mult = 1 << 30
	default:
		return 0, fmt.Errorf("invalid size unit %q", unit)
	}
	return n * mult, nil
}

// Upload copies a local file into the store. target is the relative path/name
// it should live under; empty means the source's base name.
func (s *Store) Upload(localPath, target string) (Meta, error) {
	if target == "" {
		target = filepath.Base(localPath)
	}
	clean, err := cleanRel(target)
	if err != nil {
		return Meta{}, err
	}

	src, err := os.Open(localPath)
	if err != nil {
		return Meta{}, err
	}
	defer src.Close()

	full, err := s.prepareWrite(clean)
	if err != nil {
		return Meta{}, err
	}

	// Stream into a temp file in the destination directory, then rename so a
	// failed upload never leaves a half-written resource behind.
	tmp, err := os.CreateTemp(filepath.Dir(full), tempPrefix+"*")
	if err != nil {
		return Meta{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	h := sha256.New()
	var head [512]byte
	nHead, err := io.ReadFull(src, head[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		tmp.Close()
		return Meta{}, err
	}
	if nHead > 0 {
		if _, err := tmp.Write(head[:nHead]); err != nil {
			tmp.Close()
			return Meta{}, err
		}
		if _, err := h.Write(head[:nHead]); err != nil {
			tmp.Close()
			return Meta{}, err
		}
	}

	rest, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, s.maxSize+1))
	if err != nil {
		tmp.Close()
		return Meta{}, err
	}
	total := int64(nHead) + rest
	if total > s.maxSize {
		tmp.Close()
		return Meta{}, fmt.Errorf("%w: %d bytes exceeds limit %d bytes", ErrTooLarge, total, s.maxSize)
	}
	if err := tmp.Close(); err != nil {
		return Meta{}, err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return Meta{}, err
	}
	if err := os.Rename(tmpName, full); err != nil {
		return Meta{}, err
	}

	return s.meta(clean, total, h.Sum(nil), detectMime(clean, head[:nHead]))
}

// Write stores data under name, creating or overwriting it. This is the
// internal write interface the HTTP console (stage 2) calls for "edit save";
// it is also wrapped as the runvm_write MCP tool.
func (s *Store) Write(name string, data []byte) (Meta, error) {
	if int64(len(data)) > s.maxSize {
		return Meta{}, fmt.Errorf("%w: %d bytes exceeds limit %d bytes", ErrTooLarge, len(data), s.maxSize)
	}
	clean, err := cleanRel(name)
	if err != nil {
		return Meta{}, err
	}
	full, err := s.prepareWrite(clean)
	if err != nil {
		return Meta{}, err
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return Meta{}, err
	}
	if err := os.Chmod(full, 0o600); err != nil {
		return Meta{}, err
	}
	sum := sha256.Sum256(data)
	return s.meta(clean, int64(len(data)), sum[:], detectMime(clean, data))
}

// Read returns the raw bytes of a resource.
func (s *Store) Read(name string) ([]byte, error) {
	clean, err := cleanRel(name)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(s.root, clean)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, err
	}
	if !s.inside(real) {
		return nil, fmt.Errorf("%w: %q resolves outside the resources root", ErrTraversal, name)
	}
	return os.ReadFile(real)
}

// tempPrefix marks in-flight upload temp files, which are renamed into place
// atomically and must never surface in a listing.
const tempPrefix = ".runvm-upload-"

// List returns every regular file under the root, sorted by name. Symlinks are
// skipped: upload/write never create them, and a symlink can point outside the
// root, which must not leak into the catalog. In-flight upload temp files are
// skipped too.
func (s *Store) List() ([]Meta, error) {
	out := make([]Meta, 0)
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == s.root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), tempPrefix) {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		head, err := headOf(path)
		if err != nil {
			return err
		}
		out = append(out, Meta{
			URI:      "runvm://files/" + filepath.ToSlash(rel),
			Name:     filepath.ToSlash(rel),
			Size:     fi.Size(),
			ModTime:  fi.ModTime().UTC(),
			MimeType: detectMime(rel, head),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// IsText reports whether data should be served as a text resource rather than
// a binary blob: valid UTF-8 with no NUL bytes.
func IsText(data []byte) bool {
	return utf8.Valid(data) && !bytes.Contains(data, []byte{0})
}

// cleanRel validates a client-supplied relative path: no absolute paths, no
// ".." components. It returns the cleaned form.
func cleanRel(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty resource path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: absolute path %q is not allowed", ErrTraversal, rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q escapes the resources root", ErrTraversal, rel)
	}
	return clean, nil
}

// prepareWrite validates the target and its parent directory before any bytes
// are written: the target itself must not be a symlink, and the resolved
// parent must stay inside the root.
func (s *Store) prepareWrite(clean string) (string, error) {
	full := filepath.Join(s.root, clean)
	if !s.inside(full) {
		return "", fmt.Errorf("%w: %q escapes the resources root", ErrTraversal, clean)
	}
	if fi, err := os.Lstat(full); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %q is a symlink", ErrTraversal, clean)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !s.inside(realDir) {
		return "", fmt.Errorf("%w: %q resolves outside the resources root", ErrTraversal, clean)
	}
	return full, nil
}

func (s *Store) inside(path string) bool {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// meta builds the result entry for a freshly written file.
func (s *Store) meta(name string, size int64, sum []byte, mimeType string) (Meta, error) {
	full := filepath.Join(s.root, name)
	fi, err := os.Stat(full)
	if err != nil {
		return Meta{}, err
	}
	return Meta{
		URI:      "runvm://files/" + filepath.ToSlash(name),
		Name:     filepath.ToSlash(name),
		Size:     size,
		Sha256:   hex.EncodeToString(sum),
		ModTime:  fi.ModTime().UTC(),
		MimeType: mimeType,
	}, nil
}

// headOf reads up to 512 bytes of a file, enough for content sniffing without
// paying for the whole file on every listing.
func headOf(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

// detectMime sniffs a content type from the first bytes, falling back to the
// file extension and finally octet-stream.
func detectMime(name string, head []byte) string {
	mt := http.DetectContentType(head)
	if mt != "application/octet-stream" {
		return mt
	}
	if ext := filepath.Ext(name); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return mt
}
