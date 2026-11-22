package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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
	// Counting is all reserved does, so a placeholder entry is enough here; a
	// real session needs an emulator or a browser.
	set.H5.Add(nil)
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
		"runvm_status",
		"runvm_upload", "runvm_write",
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
