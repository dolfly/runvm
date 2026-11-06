package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func catalogServer() *Server {
	srv := NewServer("test", "0")
	srv.AddResource(Resource{
		URI: "runvm://doc", Name: "doc", MimeType: "text/plain",
		Read: func(context.Context) (any, error) { return "hello", nil },
	})
	srv.AddResource(Resource{
		URI: "runvm://blob", Name: "blob", MimeType: "image/png",
		Read: func(context.Context) (any, error) { return []byte{1, 2, 3}, nil },
	})
	srv.AddResourceLister(func() []Resource {
		return []Resource{{URI: "runvm://live", Name: "live",
			Read: func(context.Context) (any, error) { return map[string]int{"n": 1}, nil }}}
	})
	srv.AddPrompt(Prompt{
		Name: "greet", Description: "say hi",
		Arguments: []PromptArgument{{Name: "who", Required: true}},
		Build: func(args map[string]string) (string, error) {
			who, err := RequireArg(args, "who")
			if err != nil {
				return "", err
			}
			return "hello " + who, nil
		},
	})
	return srv
}

func TestResourcesListAndRead(t *testing.T) {
	srv := catalogServer()

	got := serve(t, srv, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	res, _ := got[0]["result"].(map[string]any)
	list, _ := res["resources"].([]any)
	if len(list) != 3 {
		t.Fatalf("resources = %d, want 3 (2 fixed + 1 live)", len(list))
	}

	got = serve(t, srv, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"runvm://doc"}}`)
	result, _ := got[0]["result"].(map[string]any)
	contents := result["contents"].([]any)[0].(map[string]any)
	if contents["text"] != "hello" {
		t.Errorf("text resource = %v", contents)
	}

	got = serve(t, srv, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"runvm://blob"}}`)
	result, _ = got[0]["result"].(map[string]any)
	contents = result["contents"].([]any)[0].(map[string]any)
	if contents["blob"] != "AQID" {
		t.Errorf("blob = %v, want base64 AQID", contents["blob"])
	}

	got = serve(t, srv, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"runvm://nope"}}`)
	if e, ok := got[0]["error"].(map[string]any); !ok || !strings.Contains(e["message"].(string), "unknown uri") {
		t.Errorf("unknown uri should be a protocol error: %v", got[0])
	}
}

func TestPromptsListAndGet(t *testing.T) {
	srv := catalogServer()

	got := serve(t, srv, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`)
	result, _ := got[0]["result"].(map[string]any)
	list, _ := result["prompts"].([]any)
	if len(list) != 1 {
		t.Fatalf("prompts = %d", len(list))
	}

	got = serve(t, srv, `{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"greet","arguments":{"who":"you"}}}`)
	result, _ = got[0]["result"].(map[string]any)
	msgs, _ := result["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(map[string]any)
	if content["text"] != "hello you" {
		t.Errorf("message = %v", content)
	}

	got = serve(t, srv, `{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"greet","arguments":{}}}`)
	e, ok := got[0]["error"].(map[string]any)
	if !ok || !strings.Contains(e["message"].(string), "who") {
		t.Errorf("missing argument should name it: %v", got[0])
	}
}

func TestInitializeAdvertisesCatalog(t *testing.T) {
	srv := catalogServer()
	got := serve(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	result := got[0]["result"].(map[string]any)
	caps := result["capabilities"].(map[string]any)
	for _, key := range []string{"tools", "resources", "prompts"} {
		if _, ok := caps[key]; !ok {
			t.Errorf("capabilities missing %q: %v", key, caps)
		}
	}
}

func TestEmptyCatalogNotAdvertised(t *testing.T) {
	srv := NewServer("test", "0")
	got := serve(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	caps := got[0]["result"].(map[string]any)["capabilities"].(map[string]any)
	if _, ok := caps["resources"]; ok {
		t.Error("resources should not be advertised when empty")
	}
}

var _ = json.Marshal
