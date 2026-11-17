package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/ios/iosprobe"
	"github.com/dolfly/runvm/internal/mcp"
)

// rpc is one JSON-RPC reply: the tool's text (or the protocol error) and
// whether the tool itself reported isError.
type rpc struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// rpcCall runs one tools/call through a real MCP server over stdio. Going
// through Serve rather than calling the handler directly is deliberate: it
// exercises the timeout, the JSON envelope and the isError path a client sees.
func rpcCall(t *testing.T, srv *mcp.Server, name string, args map[string]any) rpc {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(params),
	})
	var out strings.Builder
	if err := srv.Serve(context.Background(), strings.NewReader(string(line)+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var reply rpc
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &reply); err != nil {
		t.Fatalf("reply is not JSON: %v (%q)", err, out.String())
	}
	return reply
}

func (r rpc) text(t *testing.T) string {
	t.Helper()
	if len(r.Result.Content) == 0 {
		if r.Error != nil {
			return r.Error.Message
		}
		t.Fatalf("reply carries neither content nor error")
	}
	return r.Result.Content[0].Text
}

func TestIOSOpenRejectsNonMachO(t *testing.T) {
	srv, set := New("test", Limits{})
	reply := rpcCall(t, srv, "ios_open", map[string]any{"dylib": "internal/ios/iosprobe/probe.go"})
	if !reply.Result.IsError {
		t.Fatalf("a non-Mach-O input must fail, got %s", reply.text(t))
	}
	if msg := reply.text(t); !strings.Contains(msg, "not a Mach-O") {
		t.Errorf("error should name the format, got %q", msg)
	}
	if set.IOS.Len() != 0 {
		t.Error("a failed open must not leave a session behind")
	}
}

func TestIOSRequiresDylibArgument(t *testing.T) {
	srv, _ := New("test", Limits{})
	reply := rpcCall(t, srv, "ios_open", map[string]any{})
	if !reply.Result.IsError || !strings.Contains(reply.text(t), "required") {
		t.Errorf("want a 'required' error, got %q", reply.text(t))
	}
}

func TestIOSCallRejectsUnknownSession(t *testing.T) {
	srv, _ := New("test", Limits{})
	reply := rpcCall(t, srv, "ios_call", map[string]any{"session": "ios-99", "symbol": "add"})
	if !reply.Result.IsError || !strings.Contains(reply.text(t), "not found") {
		t.Errorf("want a 'not found' error, got %q", reply.text(t))
	}
}

// TestAndroidOpenRoutesMachO pins the dispatch rule: a Mach-O file handed to
// android_open must land on the iOS path. The pool boot needs a CPU engine, so
// this skips where there is none; what it proves when it runs is that no
// android session appears and the id is ios-N.
func TestAndroidOpenRoutesMachO(t *testing.T) {
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	srv, set := New("test", Limits{})
	reply := rpcCall(t, srv, "android_open", map[string]any{"so": path})
	if reply.Result.IsError {
		if strings.Contains(reply.text(t), "engine") || strings.Contains(reply.text(t), "libunicorn") {
			t.Skipf("no CPU engine on this machine: %s", reply.text(t))
		}
		t.Fatalf("android_open on a Mach-O: %s", reply.text(t))
	}

	if set.Android.Len() != 0 {
		t.Error("a Mach-O must not create an android session")
	}
	if set.IOS.Len() != 1 {
		t.Fatalf("want exactly one ios session, got %d (%s)", set.IOS.Len(), reply.text(t))
	}
	for _, id := range set.IOS.IDs() {
		if !strings.HasPrefix(id, "ios-") {
			t.Errorf("session id %q should be ios-N", id)
		}
	}

	text := reply.text(t)
	var body map[string]any
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if body["kind"] != "macho-dylib" {
		t.Errorf("kind = %v, want macho-dylib", body["kind"])
	}
	if present, _ := body["assets_present"].(bool); present {
		t.Error("an iOS session must not report bionic assets")
	}
}

func TestIOSToolsUseIOSSessionStore(t *testing.T) {
	_, set := New("test", Limits{})
	if set.IOS == nil {
		t.Fatal("the ios session store is not wired up")
	}
	// A Mach-O session must count toward the shared session cap, the same way
	// android and h5 sessions do.
	_, limited := New("test", Limits{MaxSessions: 1})
	limited.IOS.Add(nil)
	if err := limited.reserved(); err == nil {
		t.Error("an ios session must count against RUNVM_MAX_SESSIONS")
	}
}
