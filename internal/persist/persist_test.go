package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.LoadErr() != nil {
		t.Fatalf("fresh store should have no load error: %v", s.LoadErr())
	}
	in := []Entry{
		{ID: "h5-1", Backend: "h5", Target: "https://example.com", Profile: "p1",
			CreatedAt: "2026-11-24T00:00:00Z", LastUsed: "2026-11-24T00:00:01Z",
			Reopen: &Reopen{Target: "https://example.com", Profile: "p1", Visible: true}},
		{ID: "android-1", Backend: "android", LastUsed: "2026-11-24T00:00:02Z"},
	}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Entries()
	if len(got) != 2 {
		t.Fatalf("loaded %d entries, want 2", len(got))
	}
	if got[0].Profile != "p1" || got[0].Reopen == nil || !got[0].Reopen.Visible {
		t.Errorf("round trip lost fields: %+v", got[0])
	}
	if id := again.ResumeID("p1"); id != "h5-1" {
		t.Errorf("ResumeID(p1) = %q, want h5-1", id)
	}
	if id := again.ResumeID("nope"); id != "" {
		t.Errorf("ResumeID(nope) = %q, want empty", id)
	}
}

func TestLedgerPermissions(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save([]Entry{{ID: "h5-1", Backend: "h5"}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("ledger mode = %o, want 600", perm)
	}
	dst, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dst.Mode().Perm(); perm != 0o700 {
		t.Errorf("data dir mode = %o, want 700", perm)
	}
}

func TestCorruptLedgerIsKeptAndReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("a corrupt ledger must not stop the server: %v", err)
	}
	if s.LoadErr() == nil || !strings.Contains(s.LoadErr().Error(), "corrupt") {
		t.Fatalf("LoadErr = %v, want a corruption report", s.LoadErr())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sessions.json.bad-") {
			found = true
		}
	}
	if !found {
		t.Error("the corrupt ledger should be kept as sessions.json.bad-<ts>")
	}
	// The server can still write a fresh ledger; the record is not lost silently.
	if err := s.Save([]Entry{{ID: "h5-1", Backend: "h5"}}); err != nil {
		t.Fatalf("save after corruption: %v", err)
	}
	if got := s.Entries(); len(got) != 1 || got[0].ID != "h5-1" {
		t.Errorf("entries after save = %+v", got)
	}
}

func TestNewerLedgerVersionIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	doc := ledgerFile{Version: 99, Sessions: []Entry{{ID: "h5-1", Backend: "h5"}}}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), buf, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.LoadErr() == nil {
		t.Fatal("a newer schema must be reported")
	}
	if err := s.Save(nil); err == nil {
		t.Fatal("a newer schema must not be overwritten")
	}
	after, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(buf) {
		t.Error("the newer ledger file was modified")
	}
}

func TestSaveIsAtomicUnderConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				entries := []Entry{{ID: "h5-" + string(rune('a'+n)), Backend: "h5"}}
				if err := s.Save(entries); err != nil {
					t.Errorf("save: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	// Whatever the interleaving, the file on disk must be one complete document.
	data, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc ledgerFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("ledger is not valid JSON after concurrent saves: %v", err)
	}
	if doc.Version != ledgerVersion {
		t.Errorf("version = %d, want %d", doc.Version, ledgerVersion)
	}
}

func TestSaveReplacesWholeLedger(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save([]Entry{{ID: "h5-1", Backend: "h5"}, {ID: "h5-2", Backend: "h5"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save([]Entry{{ID: "h5-1", Backend: "h5"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Entries(); len(got) != 1 {
		t.Errorf("Save must replace the ledger, got %+v", got)
	}
}

func TestProfileDirRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "with\x00null", strings.Repeat("x", 65)} {
		if _, err := ProfileDir(dir, name); err == nil {
			t.Errorf("ProfileDir(%q) should fail", name)
		}
	}
	got, err := ProfileDir(dir, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "profiles", "shop"); got != want {
		t.Errorf("ProfileDir = %q, want %q", got, want)
	}
	st, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Errorf("profile dir mode = %o, want 700", perm)
	}
	// A name that only differs by a separator must not land in the same place.
	if _, err := ProfileDir(dir, "shop/../../etc"); err == nil {
		t.Error("a traversal in the profile name must be rejected")
	}
}

func TestProfileEnvPerPlatform(t *testing.T) {
	cases := map[string][]string{
		"linux":   {"XDG_DATA_HOME", "XDG_CACHE_HOME"},
		"windows": {"APPDATA"},
		"darwin":  {"HOME"},
	}
	for goos, keys := range cases {
		env := ProfileEnv(goos, "/p")
		if len(env) != len(keys) {
			t.Errorf("%s: env = %v, want %d entries", goos, env, len(keys))
			continue
		}
		for i, kv := range env {
			if !strings.HasPrefix(kv, keys[i]+"=/p") {
				t.Errorf("%s: env[%d] = %q, want %s under /p", goos, i, kv, keys[i])
			}
		}
	}
	if env := ProfileEnv("plan9", "/p"); env != nil {
		t.Errorf("unknown platform should get no redirection, got %v", env)
	}
	if runtime.GOOS == "linux" {
		if env := ProfileEnv(runtime.GOOS, "/p"); len(env) != 2 {
			t.Errorf("linux env = %v", env)
		}
	}
}

func TestDefaultDirAndEnvDir(t *testing.T) {
	t.Setenv("RUNVM_DATA_DIR", "/tmp/runvm-data-test")
	if got := EnvDir(); got != "/tmp/runvm-data-test" {
		t.Errorf("EnvDir = %q, want the override", got)
	}
	if got := DefaultDir(); got == "" {
		t.Error("DefaultDir must not be empty")
	}
}
