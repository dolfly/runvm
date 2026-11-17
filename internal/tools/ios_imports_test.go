package tools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/ios/iosprobe"
)

// TestIOSOpenReportsImportsAndRefusesImportedCalls is the tool-level half of
// the import contract. The probe is built to call libSystem (strlen, malloc,
// free), so an answer of "no imports" is wrong here, and a call to an imported
// symbol has to say so instead of handing back a 0 that looks like a result.
//
// It needs a CPU engine: without one, ios_open cannot boot, so the test skips
// rather than pretending the path works.
func TestIOSOpenReportsImportsAndRefusesImportedCalls(t *testing.T) {
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	srv, _ := New("test", Limits{})
	open := rpcCall(t, srv, "ios_open", map[string]any{"dylib": path})
	if open.Result.IsError {
		if strings.Contains(open.text(t), "engine") || strings.Contains(open.text(t), "libunicorn") {
			t.Skipf("no CPU engine on this machine: %s", open.text(t))
		}
		t.Fatalf("ios_open: %s", open.text(t))
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(open.text(t)), &body); err != nil {
		t.Fatalf("ios_open result is not JSON: %v\n%s", err, open.text(t))
	}
	imports := jsonStrings(t, body["imports"])
	if len(imports) == 0 {
		t.Fatalf("ios_open reported no imports, but the probe calls %s", iosprobe.SystemLib)
	}
	for _, want := range iosprobe.LibcImports {
		if !slices.Contains(imports, want) {
			t.Errorf("imports = %v, missing %q", imports, want)
		}
	}
	if libs := jsonStrings(t, body["system_libs"]); !slices.Contains(libs, iosprobe.SystemLib) {
		t.Errorf("system_libs = %v, missing %q", libs, iosprobe.SystemLib)
	}

	session, _ := body["session"].(string)
	if session == "" {
		t.Fatalf("ios_open returned no session: %s", open.text(t))
	}

	listed := rpcCall(t, srv, "ios_imports", map[string]any{"session": session})
	if listed.Result.IsError {
		t.Fatalf("ios_imports: %s", listed.text(t))
	}
	var impBody map[string]any
	if err := json.Unmarshal([]byte(listed.text(t)), &impBody); err != nil {
		t.Fatalf("ios_imports result is not JSON: %v", err)
	}
	if got := jsonStrings(t, impBody["imports"]); !slices.Equal(got, imports) {
		t.Errorf("ios_imports = %v, ios_open said %v", got, imports)
	}

	called := rpcCall(t, srv, "ios_call", map[string]any{
		"session": session, "symbol": "strlen", "args": []any{"hello"},
	})
	if !called.Result.IsError {
		t.Fatalf("calling an imported symbol must fail, got %s", called.text(t))
	}
	if msg := called.text(t); !strings.Contains(msg, "imported by the dylib") {
		t.Errorf("error = %q, want it to name the symbol as imported", msg)
	}
}

// jsonStrings converts a decoded JSON array into []string, failing on anything
// else so a missing or misspelled field shows up as a type error here rather
// than as an empty list.
func jsonStrings(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("want a JSON array, got %#v", v)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("want an array of strings, got %#v", list)
		}
		out = append(out, s)
	}
	return out
}
