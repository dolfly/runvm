package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/mcp"
	"github.com/dolfly/runvm/internal/resources"
)

// resourcesServer wires a real on-disk store into a real MCP server, the same
// way main does, so the round-trip below exercises tools/call + resources/list
// + resources/read end to end rather than calling the handlers directly.
func resourcesServer(t *testing.T) (*mcp.Server, *resources.Store) {
	t.Helper()
	srv, set := New("test", Limits{})
	store, err := resources.New(filepath.Join(t.TempDir(), "res"), 0)
	if err != nil {
		t.Fatalf("resources.New: %v", err)
	}
	set.Resources = store
	return srv, store
}

// call runs one JSON-RPC request against the server and returns the parsed
// result object.
func call(t *testing.T, srv *mcp.Server, req string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(req+"\n"), &out); err != nil {
		t.Fatalf("serve %s: %v", req, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %q", out.String())
	}
	if e, ok := resp["error"]; ok {
		t.Fatalf("request %s returned error: %v", req, e)
	}
	result, _ := resp["result"].(map[string]any)
	return result
}

func TestUploadWriteReadRoundTrip(t *testing.T) {
	srv, store := resourcesServer(t)

	// tools/list advertises the two new tools.
	list := call(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools, _ := list["tools"].([]any)
	names := map[string]bool{}
	for _, t := range tools {
		names[t.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"runvm_upload", "runvm_write"} {
		if !names[want] {
			t.Errorf("tools/list missing %s", want)
		}
	}

	// Upload a real file.
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("hello from disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	up := call(t, srv, fmt.Sprintf(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"runvm_upload","arguments":{"path":%q,"target":"docs/note.txt"}}}`,
		src))
	content := toolText(t, up)
	var upMeta map[string]any
	if err := json.Unmarshal([]byte(content), &upMeta); err != nil {
		t.Fatalf("upload result not JSON: %q", content)
	}
	if upMeta["uri"] != "runvm://files/docs/note.txt" {
		t.Errorf("upload uri = %v", upMeta["uri"])
	}
	if upMeta["bytes"] != float64(15) {
		t.Errorf("upload bytes = %v, want 15", upMeta["bytes"])
	}
	if upMeta["sha256"] == "" {
		t.Error("upload sha256 empty")
	}

	// resources/list shows it.
	resList := call(t, srv, `{"jsonrpc":"2.0","id":3,"method":"resources/list"}`)
	entries, _ := resList["resources"].([]any)
	if len(entries) == 0 {
		t.Fatal("resources/list returned no entries")
	}
	found := false
	for _, e := range entries {
		if e.(map[string]any)["uri"] == "runvm://files/docs/note.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("resources/list missing runvm://files/docs/note.txt: %v", entries)
	}

	// resources/read returns the identical text.
	read := call(t, srv, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"runvm://files/docs/note.txt"}}`)
	if got := resourceText(t, read); got != "hello from disk" {
		t.Errorf("resources/read = %q, want hello from disk", got)
	}

	// Overwrite via runvm_write; read reflects the new content.
	call(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"runvm_write","arguments":{"target":"docs/note.txt","content":"overwritten"}}}`)
	read2 := call(t, srv, `{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"runvm://files/docs/note.txt"}}`)
	if got := resourceText(t, read2); got != "overwritten" {
		t.Errorf("resources/read after write = %q, want overwritten", got)
	}

	// The store agrees (sanity: the server read from disk, not a cache).
	if _, err := store.Read("docs/note.txt"); err != nil {
		t.Fatalf("store.Read: %v", err)
	}
}

func TestWriteToolEmptyAndMutualExclusion(t *testing.T) {
	srv, _ := resourcesServer(t)

	// content: "" is a valid write of an empty text resource (the key is
	// present, unlike base64), not a missing-argument error.
	call(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"runvm_write","arguments":{"target":"empty.txt","content":""}}}`)
	read := call(t, srv, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"runvm://files/empty.txt"}}`)
	if got := resourceText(t, read); got != "" {
		t.Errorf("read empty = %q, want empty", got)
	}

	// Both content and base64 at once is refused.
	for _, args := range []string{
		`{"target":"x.txt","content":"a","base64":"YQ=="}`,
		`{"target":"x.txt"}`,
	} {
		r := call(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"runvm_write","arguments":`+args+`}}`)
		if r["isError"] != true {
			t.Errorf("write(%s) = %v, want isError", args, r)
		}
	}
}

// toolText extracts the text of a tools/call result.
func toolText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("empty content: %v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}

// resourceText extracts the text of a resources/read result.
func resourceText(t *testing.T, result map[string]any) string {
	t.Helper()
	contents, _ := result["contents"].([]any)
	if len(contents) == 0 {
		t.Fatalf("empty contents: %v", result)
	}
	text, _ := contents[0].(map[string]any)["text"].(string)
	return text
}
