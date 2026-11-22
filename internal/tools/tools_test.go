package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dolfly/runvm/internal/android"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/ios"
)

func TestUnwrapJSONString(t *testing.T) {
	// webframe formats a returned string as JSON, so the poll envelope arrives
	// quoted and escaped; the unwrapper has to undo exactly that.
	cases := map[string]string{
		`"{\"ok\":true}"`: `{"ok":true}`,
		`{"ok":true}`:     `{"ok":true}`,
		`"plain"`:         `plain`,
		`"`:               `"`,
	}
	for in, want := range cases {
		if got := unwrapJSONString(in); got != want {
			t.Errorf("unwrapJSONString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReservedRespectsLimit(t *testing.T) {
	_, set := New("test", Limits{MaxSessions: 0})
	if err := set.reserved(); err != nil {
		t.Errorf("unlimited: %v", err)
	}

	_, set = New("test", Limits{MaxSessions: 1})
	if err := set.reserved(); err != nil {
		t.Errorf("empty store should allow one more: %v", err)
	}
	// Counting is all reserved does, so a detached placeholder session is
	// enough here; a real session needs an emulator or a browser.
	set.H5.Add(h5.NewDetached("https://example.com", true))
	if err := set.reserved(); err == nil {
		t.Error("limit should be enforced once full")
	}
}

func TestStartReaperDisabledWithoutTimeout(t *testing.T) {
	_, set := New("test", Limits{})
	set.StartReaper(context.Background()) // must be a no-op, not a goroutine
	if n := set.reap(time.Now()); n != 0 {
		t.Errorf("reap with no timeout closed %d sessions", n)
	}
}

func TestWebbrainToolUnknownAction(t *testing.T) {
	_, set := New("test", Limits{})
	id := set.H5.Add(nil) // a real session needs a browser; validation is pre-browser
	raw := json.RawMessage(`{"session":"` + id + `","action":"nope"}`)
	if _, err := set.webbrainTool(context.Background(), raw); err == nil {
		t.Error("unknown action should error")
	}
	raw = json.RawMessage(`{"session":"` + id + `"}`)
	if _, err := set.webbrainTool(context.Background(), raw); err == nil {
		t.Error("missing action should error")
	}
	raw = json.RawMessage(`{"session":"h5-404","action":"tree"}`)
	if _, err := set.webbrainTool(context.Background(), raw); err == nil {
		t.Error("unknown session should error")
	}
}

func TestToolsRegistered(t *testing.T) {
	srv, _ := New("test", Limits{})
	want := []string{
		"runvm_status", "runvm_upload", "runvm_write", "runvm_close_all",
		"android_open", "android_symbols", "android_hook", "android_java_stub", "android_java_stubs", "android_call", "android_close",
		"ios_open", "ios_symbols", "ios_imports", "ios_call", "ios_close",
		"h5_open", "h5_eval", "h5_wait_for", "h5_console", "h5_screenshot", "h5_pdf", "h5_close",
		"webbrain",
	}
	have := map[string]bool{}
	for _, tool := range srv.Tools() {
		have[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("%s is not registered", name)
		}
	}
	if len(have) != len(want) {
		t.Errorf("tool count = %d, want %d: %v", len(have), len(want), have)
	}
}

func TestSweepDeadRemovesDeadH5Sessions(t *testing.T) {
	_, set := New("test", Limits{})
	set.H5.Add(h5.NewDetached("https://dead.example", false))
	set.H5.Add(h5.NewDetached("https://alive.example", true))

	if n := set.sweepDead(); n != 1 {
		t.Fatalf("sweepDead removed %d sessions, want 1", n)
	}
	if set.H5.Len() != 1 {
		t.Errorf("h5 store should hold 1 session after sweep, got %d", set.H5.Len())
	}
	reclaimed := set.reclaimedSnapshot()
	if len(reclaimed) != 1 || reclaimed[0].Reason != "dead" || reclaimed[0].Backend != "h5" {
		t.Errorf("reclaimed = %+v, want one dead h5 entry", reclaimed)
	}
}

func TestReservedSweepsDeadBeforeCounting(t *testing.T) {
	_, set := New("test", Limits{MaxSessions: 1})
	// A dead session still holds its slot until something sweeps it; reserved
	// must sweep first so a new h5_open can succeed without an explicit close.
	set.H5.Add(h5.NewDetached("https://dead.example", false))
	if err := set.reserved(); err != nil {
		t.Errorf("a dead session must not block a new open: %v", err)
	}
	if set.H5.Len() != 0 {
		t.Errorf("dead session should have been swept, got %d", set.H5.Len())
	}
}

func TestStatusH5DetailsAndReclaimed(t *testing.T) {
	_, set := New("test", Limits{})
	set.H5.Add(h5.NewDetached("https://alive.example", true))
	set.H5.Add(h5.NewDetached("https://dead.example", false))

	st, err := set.status(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	root := st.(map[string]any)
	sessions := root["sessions"].(map[string]any)
	if _, ok := sessions["android"]; !ok {
		t.Error("sessions.android key is missing")
	}
	if _, ok := sessions["ios"]; !ok {
		t.Error("sessions.ios key is missing")
	}
	h5s := sessions["h5"].([]map[string]any)
	if len(h5s) != 2 {
		t.Fatalf("h5 details = %d entries, want 2", len(h5s))
	}
	for _, d := range h5s {
		target, _ := d["target"].(string)
		want := "alive"
		if target == "https://dead.example" {
			want = "dead"
		}
		if d["state"] != want {
			t.Errorf("session %v (target %q) state = %v, want %v", d["session"], target, d["state"], want)
		}
		if d["last_used"] == "" {
			t.Errorf("session %v has no last_used", d["session"])
		}
	}
	if _, ok := root["reclaimed"]; !ok {
		t.Error("reclaimed key is missing from runvm_status")
	}
}

func TestCloseAllTool(t *testing.T) {
	_, set := New("test", Limits{})
	set.H5.Add(h5.NewDetached("https://a.example", true))
	set.H5.Add(h5.NewDetached("https://b.example", true))

	res, err := set.closeAll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]any)
	if out["count"].(int) != 2 {
		t.Errorf("count = %v, want 2", out["count"])
	}
	if set.H5.Len() != 0 {
		t.Error("close_all must empty the h5 store")
	}
	if len(set.reclaimedSnapshot()) != 2 {
		t.Errorf("close_all must record each release, got %d", len(set.reclaimedSnapshot()))
	}

	set.H5.Add(h5.NewDetached("https://c.example", true))
	res, err = set.closeAll(context.Background(), json.RawMessage(`{"backends":["h5"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["count"].(int) != 1 {
		t.Errorf("h5-only close count = %v, want 1", res.(map[string]any)["count"])
	}

	if _, err := set.closeAll(context.Background(), json.RawMessage(`{"backends":["nope"]}`)); err == nil {
		t.Error("unknown backend must error")
	}
}

func TestCloseH5Idempotent(t *testing.T) {
	_, set := New("test", Limits{})
	id := set.H5.Add(h5.NewDetached("https://example.com", true))
	gone, err := set.closeH5(id)
	if !gone || err != nil {
		t.Fatalf("first close: gone=%v err=%v", gone, err)
	}
	gone, err = set.closeH5(id)
	if gone || err != nil {
		t.Errorf("second close: gone=%v err=%v, want (false, nil)", gone, err)
	}
}

func TestReservedErrorNamesSessions(t *testing.T) {
	_, set := New("test", Limits{MaxSessions: 1})
	set.H5.Add(h5.NewDetached("https://example.com", true))
	err := set.reserved()
	if err == nil {
		t.Fatal("want a quota error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "h5-1 (https://example.com)") {
		t.Errorf("error must name the live session and its target, got %q", msg)
	}
	if !strings.Contains(msg, "runvm_close_all") {
		t.Errorf("error must point at runvm_close_all, got %q", msg)
	}
}

func TestReapRecordsReclaimed(t *testing.T) {
	_, set := New("test", Limits{IdleTimeout: time.Minute})
	id := set.H5.Add(h5.NewDetached("https://example.com", true))
	// NewDetached never touches, so last_used reads as epoch and is well past
	// any cutoff.
	if n := set.reap(time.Now().Add(2 * time.Minute)); n != 1 {
		t.Fatalf("reap closed %d sessions, want 1", n)
	}
	if set.H5.Len() != 0 {
		t.Error("reap must empty the h5 store")
	}
	reclaimed := set.reclaimedSnapshot()
	if len(reclaimed) != 1 || reclaimed[0].Session != id || reclaimed[0].Reason != "idle" || reclaimed[0].Backend != "h5" {
		t.Errorf("reclaimed = %+v, want one idle h5 entry for %s", reclaimed, id)
	}
}

// TestReadPathsTouchLastUsed pins that the read-only tool entries (which do not
// go through Call/Eval) still mark the session as used, so the idle reaper does
// not collect a session mid-inspection. Empty-path fixtures make the disk read
// fail, but touch happens before the read.
func TestReadPathsTouchLastUsed(t *testing.T) {
	_, set := New("test", Limits{})

	asess := &android.Session{}
	aid := set.Android.Add(asess)
	_, _ = set.androidSymbols(context.Background(), json.RawMessage(`{"session":"`+aid+`"}`))
	if asess.LastUsed().IsZero() {
		t.Error("android_symbols must touch the session's last_used")
	}

	isess := &ios.Session{}
	iid := set.IOS.Add(isess)
	_, _ = set.iosSymbols(context.Background(), json.RawMessage(`{"session":"`+iid+`"}`))
	if isess.LastUsed().IsZero() {
		t.Error("ios_symbols must touch the session's last_used")
	}

	isess2 := &ios.Session{}
	iid2 := set.IOS.Add(isess2)
	_, _ = set.iosImports(context.Background(), json.RawMessage(`{"session":"`+iid2+`"}`))
	if isess2.LastUsed().IsZero() {
		t.Error("ios_imports must touch the session's last_used")
	}
}
