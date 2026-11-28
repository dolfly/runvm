package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/android"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/ios"
	"github.com/dolfly/runvm/internal/persist"
)

// newPersistedSet is a set with a real data directory, so the ledger and the
// profile paths are the ones a server would use. No browser is started.
func newPersistedSet(t *testing.T) *Set {
	t.Helper()
	return newPersistedSetIn(t, t.TempDir())
}

// newPersistedSetIn is newPersistedSet with a caller-chosen data directory, so
// a test can start a second process against the directory the first one left.
func newPersistedSetIn(t *testing.T, dir string) *Set {
	t.Helper()
	_, set := New("test", Limits{})
	store, err := persist.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	set.Persist = store
	set.Restore()
	return set
}

func TestLedgerFollowsLiveSet(t *testing.T) {
	set := newPersistedSet(t)
	set.H5.Add(h5.NewDetached("https://a.example", true))
	set.H5.Add(h5.NewDetached("https://b.example", true))

	entries := set.Persist.Entries()
	if len(entries) != 2 {
		t.Fatalf("ledger has %d entries, want 2", len(entries))
	}
	if entries[0].ID != "h5-1" || entries[0].Backend != "h5" || entries[0].Target != "https://a.example" {
		t.Errorf("entry = %+v, want the first h5 session", entries[0])
	}
	if !entries[0].Ephemeral {
		t.Error("a session with no profile must be recorded as ephemeral")
	}
	if entries[0].CreatedAt == "" || entries[0].LastUsed == "" {
		t.Errorf("h5 entry is missing timestamps: %+v", entries[0])
	}

	set.H5.Delete("h5-1")
	if entries := set.Persist.Entries(); len(entries) != 1 || entries[0].ID != "h5-2" {
		t.Errorf("ledger after delete = %+v, want only h5-2", entries)
	}

	set.CloseAll()
	if entries := set.Persist.Entries(); len(entries) != 0 {
		t.Errorf("ledger after close_all = %+v, want empty", entries)
	}
}

// A graceful shutdown (SIGTERM, docker stop, systemd restart) must keep the
// ledger. The old code saved an empty snapshot while closing the sessions, so
// the most common restart path erased every record and handed the ids out
// again — while a kill -9 preserved them.
func TestShutdownKeepsTheLastLedgerSnapshot(t *testing.T) {
	dir := t.TempDir()
	set := newPersistedSetIn(t, dir)
	set.H5.Add(h5.NewDetached("https://shop.example", true))
	set.H5.Add(h5.NewDetached("https://news.example", true))

	set.Shutdown()
	if set.H5.Len() != 0 {
		t.Fatalf("shutdown must close every session: h5=%d", set.H5.Len())
	}

	// The file on disk, not just the in-memory copy: a shutdown that rewrote
	// the ledger would leave {"sessions": []} here.
	raw, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "h5-1") || !strings.Contains(string(raw), "h5-2") {
		t.Errorf("shutdown rewrote the ledger: %s", raw)
	}

	// A late change racing the shutdown must not wipe it either.
	set.persistChange()
	if entries := set.Persist.Entries(); len(entries) != 2 {
		t.Errorf("ledger after shutdown = %+v, want both records kept", entries)
	}

	// Restart against the same directory: the records come back stale with a
	// reason, and the ids continue instead of being reused.
	again := newPersistedSetIn(t, dir)
	details := statusRoot(t, again)["persistence"].(map[string]any)["sessions"].([]map[string]any)
	if len(details) != 2 {
		t.Fatalf("persisted details after restart = %+v, want two", details)
	}
	stale := map[string]bool{}
	for _, d := range details {
		id, _ := d["session"].(string)
		stale[id] = d["state"] == "stale" && d["reason"] != nil
	}
	if !stale["h5-1"] || !stale["h5-2"] {
		t.Errorf("every record must come back stale with a reason: %+v", details)
	}
	if id := again.H5.Add(h5.NewDetached("https://other.example", true)); id != "h5-3" {
		t.Errorf("first h5 id after a graceful restart = %q, want h5-3 (no reuse)", id)
	}
}

func TestStatusDistinguishesRestoredFromStale(t *testing.T) {
	set := newPersistedSet(t)
	// A profile that really exists: the stale reason must be "reopen it", not
	// "its directory is gone".
	if _, err := persist.ProfileDir(set.Persist.Dir(), "shop"); err != nil {
		t.Fatal(err)
	}
	record := persist.Entry{
		ID: "h5-4", Backend: "h5", Target: "https://shop.example", Profile: "shop",
		CreatedAt: "2026-11-24T00:00:00Z", LastUsed: "2026-11-24T00:00:01Z",
		Reopen: &persist.Reopen{Target: "https://shop.example", Profile: "shop"},
	}
	if err := set.Persist.Save([]persist.Entry{record}); err != nil {
		t.Fatal(err)
	}

	root := statusRoot(t, set)
	p := root["persistence"].(map[string]any)
	if p["enabled"] != true {
		t.Fatalf("persistence = %+v, want enabled", p)
	}
	if p["data_dir"] == "" || p["profiles_dir"] == "" {
		t.Errorf("persistence must name its directories: %+v", p)
	}
	details := p["sessions"].([]map[string]any)
	if len(details) != 1 {
		t.Fatalf("persisted details = %+v, want one", details)
	}
	d := details[0]
	if d["state"] != "stale" {
		t.Errorf("state = %v, want stale", d["state"])
	}
	if reason, _ := d["reason"].(string); !strings.Contains(reason, "h5_open") {
		t.Errorf("stale reason must say how to reopen, got %q", reason)
	}
	if d["profile"] != "shop" {
		t.Errorf("profile = %v, want shop", d["profile"])
	}

	// The same id, live again: restored, with no reason left to explain.
	set.H5.AddID("h5-4", h5.NewDetached("https://shop.example", true))
	details = statusRoot(t, set)["persistence"].(map[string]any)["sessions"].([]map[string]any)
	if details[0]["state"] != "restored" {
		t.Errorf("state = %v, want restored", details[0]["state"])
	}
	if _, ok := details[0]["reason"]; ok {
		t.Errorf("a restored session must not carry a stale reason: %+v", details[0])
	}
}

func TestStatusReportsMissingProfileAndCorruptLedger(t *testing.T) {
	set := newPersistedSet(t)
	// A profile whose directory was removed by hand: reopening would start
	// logged out, and the status has to say that instead of "just reopen it".
	if err := set.Persist.Save([]persist.Entry{{
		ID: "h5-1", Backend: "h5", Target: "https://gone.example", Profile: "gone",
	}}); err != nil {
		t.Fatal(err)
	}
	d := statusRoot(t, set)["persistence"].(map[string]any)["sessions"].([]map[string]any)[0]
	if reason, _ := d["reason"].(string); !strings.Contains(reason, "logged out") {
		t.Errorf("reason = %q, want the logged-out warning", reason)
	}

	// A ledger that is not JSON: reported, kept, and never fatal.
	corrupt := newPersistedSet(t)
	if err := os.WriteFile(filepath.Join(corrupt.Persist.Dir(), "sessions.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := persist.Open(corrupt.Persist.Dir())
	if err != nil {
		t.Fatal(err)
	}
	corrupt.Persist = reopened
	p := statusRoot(t, corrupt)["persistence"].(map[string]any)
	if p["ledger_error"] == nil {
		t.Error("a corrupt ledger must be reported in runvm_status")
	}
}

func TestRestoreSeedsIdsAndOpensNothing(t *testing.T) {
	set := newPersistedSet(t)
	if err := set.Persist.Save([]persist.Entry{
		{ID: "h5-7", Backend: "h5"},
		{ID: "android-3", Backend: "android"},
		{ID: "ios-2", Backend: "ios"},
	}); err != nil {
		t.Fatal(err)
	}
	set.Restore()

	if id := set.H5.Add(h5.NewDetached("https://a.example", true)); id != "h5-8" {
		t.Errorf("first h5 id after restore = %q, want h5-8", id)
	}
	if id := set.Android.Add(&android.Session{}); id != "android-4" {
		t.Errorf("first android id after restore = %q, want android-4", id)
	}
	if id := set.IOS.Add(&ios.Session{}); id != "ios-3" {
		t.Errorf("first ios id after restore = %q, want ios-3", id)
	}
	// Restore is not a resume: no session came back on its own.
	if set.H5.Len() != 1 || set.Android.Len() != 1 || set.IOS.Len() != 1 {
		t.Errorf("restore must not open sessions: h5=%d android=%d ios=%d",
			set.H5.Len(), set.Android.Len(), set.IOS.Len())
	}
}

func TestH5OpenRejectsBadProfileBeforeBooting(t *testing.T) {
	set := newPersistedSet(t)
	_, err := set.h5Open(context.Background(), json.RawMessage(`{"target":"about:blank","ephemeral":false,"profile":"../escape"}`))
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("err = %v, want a profile validation error", err)
	}
	if set.H5.Len() != 0 {
		t.Error("a rejected profile must not leave a session behind")
	}
}

func TestH5OpenProfileNeedsPersistence(t *testing.T) {
	_, set := New("test", Limits{}) // no data directory
	_, err := set.h5Open(context.Background(), json.RawMessage(`{"target":"about:blank","ephemeral":false,"profile":"shop"}`))
	if err == nil || !strings.Contains(err.Error(), "persistent storage") {
		t.Fatalf("err = %v, want a persistence-unavailable error", err)
	}
	if p := set.persistenceStatus(); p["enabled"] != false || p["reason"] == nil {
		t.Errorf("status must say persistence is off and why: %+v", p)
	}
}

func TestDefaultProfileIsOffUnlessOptedIn(t *testing.T) {
	set := &Set{}
	if got := set.defaultProfile(); got != "" {
		t.Errorf("defaultProfile = %q, want empty by default", got)
	}
	t.Setenv("RUNVM_H5_PERSIST", "1")
	if got := set.defaultProfile(); got != "default" {
		t.Errorf("defaultProfile with RUNVM_H5_PERSIST=1 = %q, want default", got)
	}
	t.Setenv("RUNVM_H5_PERSIST", "shop")
	if got := set.defaultProfile(); got != "shop" {
		t.Errorf("defaultProfile with a name = %q, want shop", got)
	}
	t.Setenv("RUNVM_H5_PERSIST", "0")
	if got := set.defaultProfile(); got != "" {
		t.Errorf("defaultProfile with 0 = %q, want empty", got)
	}
}

func statusRoot(t *testing.T, set *Set) map[string]any {
	t.Helper()
	st, err := set.status(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return st.(map[string]any)
}
