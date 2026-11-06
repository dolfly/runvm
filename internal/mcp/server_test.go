package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// idOf reports the numeric id of a response, or 0 when it has none (a parse
// error answers with a null id).
func idOf(m map[string]any) float64 {
	if v, ok := m["id"].(float64); ok {
		return v
	}
	return 0
}

func newTestServer() *Server {
	srv := NewServer("test", "0.1.0")
	srv.Add(Tool{
		Name:        "echo",
		Description: "echo back",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			return "echo:" + string(args), nil
		},
	})
	srv.Add(Tool{
		Name:        "boom",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return nil, context.DeadlineExceeded
		},
	})
	return srv
}

func serve(t *testing.T, srv *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var got []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("response is not JSON: %q", line)
		}
		got = append(got, m)
	}
	// Concurrent dispatch means replies may arrive in any order; JSON-RPC
	// keys them by id, so the tests do too.
	sort.Slice(got, func(i, j int) bool { return idOf(got[i]) < idOf(got[j]) })
	return got
}

func TestHandshakeAndToolsList(t *testing.T) {
	srv := newTestServer()
	got := serve(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	// The notification must not produce a response.
	if len(got) != 2 {
		t.Fatalf("got %d responses, want 2", len(got))
	}
	init, _ := got[0]["result"].(map[string]any)
	if init["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v", init["protocolVersion"])
	}
	if _, ok := init["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("capabilities.tools missing")
	}

	list, _ := got[1]["result"].(map[string]any)
	tools, _ := list["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	first, _ := tools[0].(map[string]any)
	if first["name"] != "echo" {
		t.Errorf("first tool = %v, want echo (registration order)", first["name"])
	}
	if _, ok := first["inputSchema"]; !ok {
		t.Error("tool is missing inputSchema")
	}
}

func TestCallToolAndErrors(t *testing.T) {
	srv := newTestServer()
	got := serve(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/nope"}`,
	)
	if len(got) != 4 {
		t.Fatalf("got %d responses, want 4", len(got))
	}

	ok := got[0]["result"].(map[string]any)
	content := ok["content"].([]any)[0].(map[string]any)
	if text, _ := content["text"].(string); !strings.Contains(text, `{"x":1}`) {
		t.Errorf("echo text = %q", text)
	}

	// A failing handler is a tool result with isError, not a JSON-RPC error,
	// so the model sees it and can react.
	failed := got[1]["result"].(map[string]any)
	if failed["isError"] != true {
		t.Errorf("boom result = %v, want isError", failed)
	}

	// Unknown tool and unknown method are protocol errors.
	for _, i := range []int{2, 3} {
		e, ok := got[i]["error"].(map[string]any)
		if !ok {
			t.Fatalf("response %d should be an error: %v", i, got[i])
		}
		if i == 3 && e["code"] != float64(CodeMethodNotFound) {
			t.Errorf("code = %v, want %d", e["code"], CodeMethodNotFound)
		}
	}
}

func TestParseErrorKeepsGoing(t *testing.T) {
	srv := newTestServer()
	got := serve(t, srv,
		`not json`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(got) != 2 {
		t.Fatalf("got %d responses, want 2", len(got))
	}
	if e, ok := got[0]["error"].(map[string]any); !ok || e["code"] != float64(CodeParseError) {
		t.Errorf("first response = %v, want parse error", got[0])
	}
}

func TestToJSONPassesStringsThrough(t *testing.T) {
	if got := toJSON("hello"); got != "hello" {
		t.Errorf("toJSON(string) = %q, want raw", got)
	}
	if got := toJSON(map[string]int{"a": 1}); !strings.Contains(got, `"a": 1`) {
		t.Errorf("toJSON(map) = %q", got)
	}
}
