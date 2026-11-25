package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dolfly/runvm/internal/android"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/ios"
	"github.com/dolfly/runvm/internal/persist"
	"github.com/dolfly/runvm/internal/session"
)

// wirePersistence points the three session stores at the ledger. The stores do
// not know what persistence is: they call back after every add and remove, and
// the ledger is rewritten from the live set.
func (s *Set) wirePersistence() {
	s.Android.SetHooks(session.Hooks[*android.Session]{
		Added:   func(string, *android.Session) { s.persistChange() },
		Removed: func(string, *android.Session) { s.persistChange() },
	})
	s.IOS.SetHooks(session.Hooks[*ios.Session]{
		Added:   func(string, *ios.Session) { s.persistChange() },
		Removed: func(string, *ios.Session) { s.persistChange() },
	})
	s.H5.SetHooks(session.Hooks[*h5.Session]{
		Added:   func(string, *h5.Session) { s.persistChange() },
		Removed: func(string, *h5.Session) { s.persistChange() },
	})
}

// Restore prepares the set for a fresh process: it moves each id counter past
// the ids the ledger already recorded, so a session opened after a restart can
// never be handed an id the ledger still uses.
//
// It deliberately opens nothing. Reopening pages at startup would boot one
// browser per recorded session before the first request, and a stale record is
// not a reason to spend that. runvm_status shows what could come back and how.
func (s *Set) Restore() {
	if s.Persist == nil {
		return
	}
	var android, ios, h5 int
	for _, e := range s.Persist.Entries() {
		n := trailingID(e.ID)
		switch e.Backend {
		case "android":
			if n > android {
				android = n
			}
		case "ios":
			if n > ios {
				ios = n
			}
		case "h5":
			if n > h5 {
				h5 = n
			}
		}
	}
	s.Android.Seed(android)
	s.IOS.Seed(ios)
	s.H5.Seed(h5)
}

// trailingID returns the numeric suffix of a session id ("h5-12" -> 12), or 0
// when the id does not carry one. Ids are ours, but a hand-edited ledger must
// not turn into a panic or a nonsense seed.
func trailingID(id string) int {
	i := strings.LastIndexByte(id, '-')
	if i < 0 || i == len(id)-1 {
		return 0
	}
	n := 0
	for _, c := range id[i+1:] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0
		}
	}
	return n
}

// persistChange writes the ledger from the live set. The save is serialized
// here because the ledger is a full snapshot: the last write must be the newest
// state, not whichever goroutine got there first.
func (s *Set) persistChange() {
	if s.Persist == nil {
		return
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if err := s.Persist.Save(s.ledgerEntries()); err != nil {
		s.persistErr = err
		return
	}
	s.persistErr = nil
}

// ledgerEntries snapshots every live session. The h5 entries carry what a
// restore needs; the emulators carry a record only, because their state is a
// process that dies with the server.
func (s *Set) ledgerEntries() []persist.Entry {
	out := make([]persist.Entry, 0, s.Android.Len()+s.IOS.Len()+s.H5.Len())
	for _, id := range s.Android.IDs() {
		sess, ok := s.Android.Get(id)
		if !ok {
			continue
		}
		out = append(out, persist.Entry{ID: id, Backend: "android", LastUsed: rfc3339(sess.LastUsed())})
	}
	for _, id := range s.IOS.IDs() {
		sess, ok := s.IOS.Get(id)
		if !ok {
			continue
		}
		out = append(out, persist.Entry{ID: id, Backend: "ios", LastUsed: rfc3339(sess.LastUsed())})
	}
	for _, id := range s.H5.IDs() {
		sess, ok := s.H5.Get(id)
		if !ok {
			continue
		}
		e := persist.Entry{
			ID:        id,
			Backend:   "h5",
			Target:    sess.Target(),
			CreatedAt: rfc3339(sess.CreatedAt()),
			LastUsed:  rfc3339(sess.LastUsed()),
			Ephemeral: sess.Profile() == "", // nothing on disk to restore
			Profile:   sess.Profile(),
			Reopen:    reopenHint(sess),
		}
		out = append(out, e)
	}
	return out
}

// reopenHint is what a caller needs to bring a recorded h5 session back.
func reopenHint(sess *h5.Session) *persist.Reopen {
	profile := sess.Profile()
	if profile == "" {
		return nil // in-memory page: there is nothing to restore
	}
	return &persist.Reopen{Target: sess.Target(), Profile: profile, Visible: sess.Visible()}
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// persistReason explains why persistence is off, for a tool error.
func (s *Set) persistReason() string {
	if s.Persist != nil {
		return "the data directory is available"
	}
	if s.PersistErr != nil {
		return s.PersistErr.Error()
	}
	return "no data directory is configured (RUNVM_DATA_DIR)"
}

// persistenceStatus is the runvm_status section about state that outlives the
// process: whether it is on, where it is, and which recorded sessions could
// come back. It never claims a session is restorable when it is not.
func (s *Set) persistenceStatus() map[string]any {
	out := map[string]any{"enabled": s.Persist != nil}
	if s.Persist == nil {
		out["reason"] = s.persistReason()
		return out
	}
	out["data_dir"] = s.Persist.Dir()
	out["profiles_dir"] = filepath.Join(s.Persist.Dir(), persist.ProfilesSubdir)
	if err := s.Persist.LoadErr(); err != nil {
		out["ledger_error"] = err.Error()
	} else {
		out["ledger"] = "ok"
	}
	s.persistMu.Lock()
	writeErr := s.persistErr
	s.persistMu.Unlock()
	if writeErr != nil {
		out["last_write_error"] = writeErr.Error()
	}
	out["sessions"] = s.persistedDetails()
	return out
}

// persistedDetails marks each recorded session restored (live again, addressable
// under the same id) or stale (a record with a reason). The reason is the part
// that keeps the status honest: a stale entry never reads like a live one.
func (s *Set) persistedDetails() []map[string]any {
	entries := s.Persist.Entries()
	out := make([]map[string]any, 0, len(entries))
	profilesDir := filepath.Join(s.Persist.Dir(), persist.ProfilesSubdir)
	for _, e := range entries {
		detail := map[string]any{
			"session": e.ID,
			"backend": e.Backend,
			"state":   "stale",
			"reason":  staleReason(e, profilesDir),
		}
		if e.CreatedAt != "" {
			detail["created_at"] = e.CreatedAt
		}
		if e.LastUsed != "" {
			detail["last_used"] = e.LastUsed
		}
		if e.Profile != "" {
			detail["profile"] = e.Profile
		}
		if e.Reopen != nil {
			detail["reopen"] = e.Reopen
		}
		if s.recordedSessionIsLive(e) {
			detail["state"] = "restored"
			delete(detail, "reason")
		}
		out = append(out, detail)
	}
	return out
}

func (s *Set) recordedSessionIsLive(e persist.Entry) bool {
	switch e.Backend {
	case "h5":
		_, ok := s.H5.Get(e.ID)
		return ok
	case "android":
		_, ok := s.Android.Get(e.ID)
		return ok
	case "ios":
		_, ok := s.IOS.Get(e.ID)
		return ok
	}
	return false
}

func staleReason(e persist.Entry, profilesDir string) string {
	switch e.Backend {
	case "h5":
		switch {
		case e.Profile == "":
			return "opened without a persistent profile; the record is all that survives a restart"
		default:
			if _, err := os.Stat(filepath.Join(profilesDir, e.Profile)); err != nil {
				return fmt.Sprintf("profile directory for %q is gone; reopening would start logged out", e.Profile)
			}
			return "page host exited; call h5_open with the recorded target and profile to bring the session back"
		}
	case "android", "ios":
		return "emulator state is never persisted; only the record survives a restart"
	default:
		return "unknown backend in the ledger; the record is ignored"
	}
}

// defaultProfile is the server-side default profile name, used when a caller
// asks for a persistent page (ephemeral:false) without naming a profile.
// RUNVM_H5_PERSIST turns it on; unset, the default page stays in-memory, so
// flipping the default is a deployment decision rather than a code change.
func (s *Set) defaultProfile() string {
	v := strings.TrimSpace(os.Getenv("RUNVM_H5_PERSIST"))
	switch {
	case v == "", v == "0", strings.EqualFold(v, "false"), strings.EqualFold(v, "no"):
		return ""
	case strings.EqualFold(v, "1"), strings.EqualFold(v, "true"), strings.EqualFold(v, "yes"), strings.EqualFold(v, "on"):
		return "default"
	default:
		return v
	}
}
