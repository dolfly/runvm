package tools

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"slices"
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
		skipWithoutEngine(t, reply.text(t))
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

// skipWithoutEngine recognises the failure that means "this machine has no CPU
// engine", which is a property of the host, not of the code.
func skipWithoutEngine(t *testing.T, msg string) {
	t.Helper()
	for _, hint := range []string{"engine", "libunicorn", "unicorn", "backend"} {
		if strings.Contains(msg, hint) {
			t.Skipf("no CPU engine on this machine: %s", msg)
		}
	}
}

// TestIOSImportBoundaryIsHonest covers the three outcomes a caller can see,
// because the tool descriptions promise them one by one:
//
//	ios_imports lists the imports; calling one directly reports it; and a
//	function defined in the dylib that calls one gets golem's stub value 0.
func TestIOSImportBoundaryIsHonest(t *testing.T) {
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	srv, _ := New("test", Limits{})
	open := rpcCall(t, srv, "ios_open", map[string]any{"dylib": path})
	if open.Result.IsError {
		skipWithoutEngine(t, open.text(t))
		t.Fatalf("ios_open: %s", open.text(t))
	}
	var opened map[string]any
	if err := json.Unmarshal([]byte(open.text(t)), &opened); err != nil {
		t.Fatalf("open result is not JSON: %v\n%s", err, open.text(t))
	}
	id, _ := opened["session"].(string)
	if id == "" {
		t.Fatalf("open result has no session: %v", opened)
	}

	imp := rpcCall(t, srv, "ios_imports", map[string]any{"session": id})
	var imports struct {
		Imports []string `json:"imports"`
		Count   int      `json:"count"`
	}
	if err := json.Unmarshal([]byte(imp.text(t)), &imports); err != nil {
		t.Fatalf("imports result is not JSON: %v\n%s", err, imp.text(t))
	}
	if !slices.Contains(imports.Imports, "strlen") {
		t.Errorf("imports = %v, want strlen (the fixture calls it and cannot have it)", imports.Imports)
	}

	// Calling the imported name must be a reported failure, not a 0.
	bad := rpcCall(t, srv, "ios_call", map[string]any{
		"session": id, "symbol": "strlen", "args": []any{"abcd"},
	})
	if !bad.Result.IsError || !strings.Contains(bad.text(t), "has no implementation") {
		t.Errorf("calling an imported name: %q (isError=%v)", bad.text(t), bad.Result.IsError)
	}

	// A defined function still runs.
	good := rpcCall(t, srv, "ios_call", map[string]any{
		"session": id, "symbol": "add", "args": []any{3, 4},
	})
	if !strings.Contains(good.text(t), "\"value\": 7") {
		t.Errorf("add(3,4) = %q, want value 7", good.text(t))
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

// brokenBindStream returns probe bytes whose LC_DYLD_INFO(_ONLY) bind stream
// points past the end of the file. It is the same shape as the case in
// internal/ios: a file that parses, boots, and whose import table cannot be
// read.
func brokenBindStream(t *testing.T, raw []byte) []byte {
	t.Helper()
	const lcDyldInfoOnly = 0x8000_0022
	out := append([]byte(nil), raw...)
	ncmds := binary.LittleEndian.Uint32(out[16:])
	off := uint32(32)
	for i := uint32(0); i < ncmds; i++ {
		if int(off)+8 > len(out) {
			break
		}
		cmd := binary.LittleEndian.Uint32(out[off:])
		size := binary.LittleEndian.Uint32(out[off+4:])
		if cmd == lcDyldInfoOnly {
			binary.LittleEndian.PutUint32(out[off+16:], 0x00ff_ff00) // bind_off
			return out
		}
		off += size
	}
	t.Fatalf("probe has no LC_DYLD_INFO_ONLY")
	return nil
}

// TestIOSSymbolsReportsImportErrors pins the response shape, not just the
// error: ios_symbols used to drop the parse error and return "imports": null,
// which reads like a dylib with no imports. The file is broken after the
// session opens, because golem refuses a dylib whose bind stream is out of
// bounds — a file it will not boot never reaches the tool at all.
func TestIOSSymbolsReportsImportErrors(t *testing.T) {
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	srv, _ := New("test", Limits{})
	open := rpcCall(t, srv, "ios_open", map[string]any{"dylib": path})
	if open.Result.IsError {
		skipWithoutEngine(t, open.text(t))
		t.Fatalf("ios_open: %s", open.text(t))
	}
	var opened map[string]any
	if err := json.Unmarshal([]byte(open.text(t)), &opened); err != nil {
		t.Fatalf("open result is not JSON: %v\n%s", err, open.text(t))
	}
	id, _ := opened["session"].(string)
	if id == "" {
		t.Fatalf("open result has no session: %v", opened)
	}

	if err := os.WriteFile(path, brokenBindStream(t, iosprobe.Fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	syms := rpcCall(t, srv, "ios_symbols", map[string]any{"session": id})
	if syms.Result.IsError {
		t.Fatalf("ios_symbols: %s", syms.text(t))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(syms.text(t)), &out); err != nil {
		t.Fatalf("ios_symbols result is not JSON: %v\n%s", err, syms.text(t))
	}
	msg, _ := out["imports_error"].(string)
	if msg == "" {
		t.Errorf("imports_error is missing: the caller cannot tell a broken import table from an empty one\n%s", syms.text(t))
	}
	if !strings.Contains(msg, "bind stream") {
		t.Errorf("imports_error = %q, want the stream that could not be read", msg)
	}
	// The exports half is unaffected: the failure is scoped to the read it
	// belongs to, and the rest of the answer is still usable.
	if exports, _ := out["exports"].([]any); len(exports) == 0 {
		t.Errorf("exports = %v, want the intact export list", out["exports"])
	}
}
