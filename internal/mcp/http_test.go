package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func httpServer(t *testing.T, opt HTTPOptions) *httptest.Server {
	t.Helper()
	srv := newTestServer()
	srv.Add(Tool{
		Name:        "slow",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return "finished", nil
			}
		},
	})
	ts := httptest.NewServer(srv.Handler(opt))
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, url, body string, header map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestHTTPInitializeAndList(t *testing.T) {
	ts := httpServer(t, HTTPOptions{})

	resp, body := post(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Mcp-Session-Id") == "" {
		t.Error("initialize should hand back a session id")
	}
	result, _ := body["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}

	_, body = post(t, ts.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	list, _ := body["result"].(map[string]any)
	if tools, _ := list["tools"].([]any); len(tools) != 3 {
		t.Errorf("tools = %d, want 3", len(tools))
	}
}

func TestHTTPNotificationIsAccepted(t *testing.T) {
	ts := httpServer(t, HTTPOptions{})
	resp, _ := post(t, ts.URL, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202 with no body", resp.StatusCode)
	}
}

func TestHTTPBatch(t *testing.T) {
	ts := httpServer(t, HTTPOptions{})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(
		`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var batch []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("batch response has %d entries", len(batch))
	}
}

func TestHTTPRequiresToken(t *testing.T) {
	ts := httpServer(t, HTTPOptions{Token: "s3cret"})

	resp, _ := post(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Error("401 should advertise the scheme")
	}

	resp, _ = post(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Authorization": "Bearer wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", resp.StatusCode)
	}

	resp, _ = post(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Authorization": "Bearer s3cret"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("right token: status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPGetIsRefused(t *testing.T) {
	ts := httpServer(t, HTTPOptions{})
	resp, err := http.Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "POST") {
		t.Errorf("Allow = %q", allow)
	}
}

func TestHTTPCancelStopsRunningCall(t *testing.T) {
	ts := httpServer(t, HTTPOptions{})

	done := make(chan map[string]any, 1)
	go func() {
		_, body := post(t, ts.URL, `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"slow","arguments":{}}}`, nil)
		done <- body
	}()

	// Give the call a moment to register its cancel function.
	time.Sleep(150 * time.Millisecond)
	resp, _ := post(t, ts.URL, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":42}}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d", resp.StatusCode)
	}

	select {
	case body := <-done:
		result, _ := body["result"].(map[string]any)
		if result["isError"] != true {
			t.Errorf("cancelled call should come back as isError: %v", body)
		}
		content, _ := result["content"].([]any)
		if len(content) == 0 {
			t.Fatal("cancelled call has no content")
		}
		text, _ := content[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "context canceled") {
			t.Errorf("text = %q, want a cancellation", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop the call")
	}
}

func TestHealthz(t *testing.T) {
	ts := httpServer(t, HTTPOptions{})
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
