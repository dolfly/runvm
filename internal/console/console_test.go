package console

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/mcp"
	"github.com/dolfly/runvm/internal/resources"
	"github.com/dolfly/runvm/internal/tools"
)

func newConsole(t *testing.T, user, pass string, maxSize int64) (*httptest.Server, *tools.Set, *resources.Store) {
	t.Helper()
	srv, set := tools.New("test", tools.Limits{})
	store, err := resources.New(filepath.Join(t.TempDir(), "root"), maxSize)
	if err != nil {
		t.Fatalf("resources.New: %v", err)
	}
	set.Resources = store

	h := New(Options{
		MCP:      srv.Handler(mcp.HTTPOptions{}),
		Server:   srv,
		Store:    store,
		Status:   func() (map[string]any, error) { return set.Status() },
		Username: user,
		Password: pass,
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, set, store
}

func login(t *testing.T, base, user, pass string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	resp, err := http.Post(base+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			if !c.HttpOnly {
				t.Error("session cookie must be HttpOnly")
			}
			if c.SameSite == http.SameSiteDefaultMode {
				t.Error("session cookie must set SameSite")
			}
			return c.Value
		}
	}
	t.Fatal("no session cookie returned")
	return ""
}

func doReq(t *testing.T, method, url, cookie string, body io.Reader, contentType string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
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

func TestConsoleRequiresLogin(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "s3cret", resources.DefaultMaxSize)

	resp, _ := doReq(t, http.MethodGet, ts.URL+"/api/status", "", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/status = %d, want 401", resp.StatusCode)
	}

	// The SPA shell and its assets are public, otherwise there is no login page.
	resp, _ = doReq(t, http.MethodGet, ts.URL+"/", "", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
}

func TestConsoleLoginFlow(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "s3cret", resources.DefaultMaxSize)

	// Wrong password stays out.
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	resp, err := http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", resp.StatusCode)
	}

	cookie := login(t, ts.URL, "admin", "s3cret")

	// The cookie now unlocks the data endpoints.
	resp, status := doReq(t, http.MethodGet, ts.URL+"/api/status", cookie, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated /api/status = %d, want 200", resp.StatusCode)
	}
	if _, ok := status["android"].(map[string]any); !ok {
		t.Fatalf("status has no android backend: %v", status)
	}

	// Logout revokes the session.
	doReq(t, http.MethodPost, ts.URL+"/api/logout", cookie, nil, "")
	// Reuse the old cookie value via a fresh request: it must now be invalid.
	resp, _ = doReq(t, http.MethodGet, ts.URL+"/api/status", cookie, nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout /api/status = %d, want 401", resp.StatusCode)
	}
}

func TestConsoleUploadThenListAndRead(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "p", resources.DefaultMaxSize)
	cookie := login(t, ts.URL, "admin", "p")

	src := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(src, []byte("hello console"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	file, err := mw.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("hello console")); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("target", "docs/hello.txt"); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	resp, meta := doReq(t, http.MethodPost, ts.URL+"/api/upload", cookie, &buf, mw.FormDataContentType())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d: %v", resp.StatusCode, meta)
	}
	if meta["uri"] != "runvm://files/docs/hello.txt" {
		t.Fatalf("upload uri = %v", meta["uri"])
	}

	_, list := doReq(t, http.MethodGet, ts.URL+"/api/resources", cookie, nil, "")
	found := false
	for _, item := range list["resources"].([]any) {
		entry := item.(map[string]any)
		if entry["uri"] == "runvm://files/docs/hello.txt" {
			found = true
			if entry["writable"] != true {
				t.Errorf("writable flag = %v", entry["writable"])
			}
		}
	}
	if !found {
		t.Fatalf("uploaded resource missing from list: %v", list)
	}

	_, read := doReq(t, http.MethodGet, ts.URL+"/api/resources/read?uri=runvm%3A%2F%2Ffiles%2Fdocs%2Fhello.txt", cookie, nil, "")
	if read["text"] != "hello console" {
		t.Fatalf("read text = %q", read["text"])
	}
}

func TestConsoleEditReadsBack(t *testing.T) {
	ts, _, store := newConsole(t, "admin", "p", resources.DefaultMaxSize)
	cookie := login(t, ts.URL, "admin", "p")

	if _, err := store.Write("note.txt", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"name": "note.txt", "content": "v2 edited"})
	resp, meta := doReq(t, http.MethodPut, ts.URL+"/api/resources/file", cookie, bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit = %d: %v", resp.StatusCode, meta)
	}

	// MCP resources/read must reflect the edit immediately (the console saves
	// through the same store the MCP tool wraps).
	data, err := store.Read("note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2 edited" {
		t.Fatalf("store read = %q", data)
	}
}

func TestConsoleRejectsTraversalAndOversize(t *testing.T) {
	ts, _, store := newConsole(t, "admin", "p", 4) // 4-byte cap makes oversize easy to hit
	cookie := login(t, ts.URL, "admin", "p")

	for _, name := range []string{"../evil", "/abs/path", "a/../../evil"} {
		body, _ := json.Marshal(map[string]string{"name": name, "content": "x"})
		resp, _ := doReq(t, http.MethodPut, ts.URL+"/api/resources/file", cookie, bytes.NewReader(body), "application/json")
		if resp.StatusCode == http.StatusOK {
			t.Errorf("write %q should be rejected", name)
		}
	}

	body, _ := json.Marshal(map[string]string{"name": "big.txt", "content": "12345"})
	resp, _ := doReq(t, http.MethodPut, ts.URL+"/api/resources/file", cookie, bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize write = %d, want 413", resp.StatusCode)
	}

	// The store itself is untouched by the rejected writes.
	metas, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 0 {
		t.Fatalf("store has %d entries after rejected writes", len(metas))
	}
}

func TestDebugPageIsReadOnly(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "p", resources.DefaultMaxSize)
	cookie := login(t, ts.URL, "admin", "p")

	// The write tools and the state-changing tools have no call entry, including
	// runvm_close_all (added after the console first landed).
	for _, name := range []string{"runvm_write", "runvm_upload", "h5_open", "android_open", "h5_eval", "runvm_close_all"} {
		req, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": map[string]any{}},
		})
		resp, out := doReq(t, http.MethodPost, ts.URL+"/api/debug", cookie, bytes.NewReader(req), "application/json")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("debug proxy status = %d", resp.StatusCode)
		}
		response := out["response"].(map[string]any)
		if response["error"] == nil {
			t.Errorf("tools/call %s should be refused by the debug page", name)
		}
	}

	// A read-only tool still works.
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "runvm_status", "arguments": map[string]any{}},
	})
	resp, out := doReq(t, http.MethodPost, ts.URL+"/api/debug", cookie, bytes.NewReader(req), "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("debug proxy status = %d", resp.StatusCode)
	}
	response := out["response"].(map[string]any)
	if response["error"] != nil {
		t.Fatalf("runvm_status should succeed on the debug page: %v", response["error"])
	}
	if response["result"] == nil {
		t.Fatal("runvm_status returned no result")
	}

	// An unknown method is refused too.
	req, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "resources/templates/list"})
	_, out = doReq(t, http.MethodPost, ts.URL+"/api/debug", cookie, bytes.NewReader(req), "application/json")
	response = out["response"].(map[string]any)
	if response["error"] == nil {
		t.Error("unknown method should be refused by the debug page")
	}
}

func TestDebugCatalogAndSchemaLint(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "p", resources.DefaultMaxSize)
	cookie := login(t, ts.URL, "admin", "p")

	_, catalog := doReq(t, http.MethodGet, ts.URL+"/api/debug/catalog", cookie, nil, "")
	toolsList := catalog["tools"].([]any)
	if len(toolsList) != 24 {
		t.Fatalf("tools = %d, want 24", len(toolsList))
	}
	seenCloseAll := false
	for _, item := range toolsList {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		callable := entry["callable"].(bool)
		if name == "runvm_close_all" {
			seenCloseAll = true
		}
		// The catalog's callable flag must match the allow-list exactly: an
		// allow-listed tool is callable, everything else is read-only.
		if callable != debugCallableTools[name] {
			t.Errorf("tool %s callable = %v, want %v", name, callable, debugCallableTools[name])
		}
	}
	if !seenCloseAll {
		t.Error("runvm_close_all missing from the debug catalog")
	}
	if debugCallableTools["runvm_close_all"] {
		t.Error("runvm_close_all must not be in the debug allow-list")
	}
	if kinds := catalog["resource_kinds"].([]any); len(kinds) != 5 {
		t.Fatalf("resource_kinds = %d, want 5", len(kinds))
	}
	if prompts := catalog["prompts"].([]any); len(prompts) != 3 {
		t.Fatalf("prompts = %d, want 3", len(prompts))
	}

	_, lint := doReq(t, http.MethodGet, ts.URL+"/api/debug/lint", cookie, nil, "")
	if lint["errors"] != float64(0) {
		t.Errorf("lint errors = %v, want 0", lint["errors"])
	}
	if lint["warnings"] != float64(3) {
		t.Errorf("lint warnings = %v, want 3", lint["warnings"])
	}

	got := map[string]bool{}
	for _, item := range lint["issues"].([]any) {
		issue := item.(map[string]any)
		got[issue["path"].(string)] = true
	}
	for _, want := range []string{
		"ios_call.properties.args.items",
		"android_call.properties.args.items",
		"android_java_stub.properties.returns",
	} {
		if !got[want] {
			t.Errorf("missing lint warning at %s (have %v)", want, got)
		}
	}
}

func TestMCPEndpointsPassThrough(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "p", resources.DefaultMaxSize)

	// /mcp keeps working without a console cookie, and /healthz stays up.
	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp = %d, want 200", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", resp.StatusCode)
	}
}

func TestResourceListCarriesSha256AndModTime(t *testing.T) {
	ts, _, store := newConsole(t, "admin", "p", resources.DefaultMaxSize)
	cookie := login(t, ts.URL, "admin", "p")

	if _, err := store.Write("docs/report.json", []byte(`{"ok": true}`)); err != nil {
		t.Fatal(err)
	}

	_, list := doReq(t, http.MethodGet, ts.URL+"/api/resources", cookie, nil, "")
	found := false
	for _, item := range list["resources"].([]any) {
		entry := item.(map[string]any)
		if entry["uri"] != "runvm://files/docs/report.json" {
			continue
		}
		found = true
		if entry["writable"] != true {
			t.Errorf("writable = %v", entry["writable"])
		}
		sha, _ := entry["sha256"].(string)
		if len(sha) != 64 {
			t.Errorf("sha256 = %q, want 64 hex chars", sha)
		}
		if mt, _ := entry["mod_time"].(string); mt == "" {
			t.Errorf("mod_time empty")
		}
	}
	if !found {
		t.Fatalf("file entry missing from list: %v", list)
	}
}

func TestSPADeepLinksServeShell(t *testing.T) {
	ts, _, _ := newConsole(t, "admin", "p", resources.DefaultMaxSize)

	// The per-page routes must render the shell, not a Go 404, so a refresh or
	// a shared deep link works without any server-side routing table.
	for _, path := range []string{"/", "/index.html", "/resources", "/resources/tools", "/resources/screenshots", "/resources/files", "/debug"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		if !strings.Contains(string(body), "<div id=\"app\">") {
			t.Errorf("GET %s did not return the SPA shell", path)
		}
	}

	// A missing hashed asset is a real 404 (it has an extension), not a shell.
	resp, _ := doReq(t, http.MethodGet, ts.URL+"/assets/missing-abc123.js", "", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /assets/missing-abc123.js = %d, want 404", resp.StatusCode)
	}

	// Traversal is refused even though the embed FS has no parent directory.
	resp, _ = doReq(t, http.MethodGet, ts.URL+"/../internal/console/console.go", "", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /../... = %d, want 404", resp.StatusCode)
	}
}
