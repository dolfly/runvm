// Package console serves the runvm HTTP control panel: a single-page console
// behind session-cookie login, plus the read-only MCP debug page.
//
// The console is an HTTP-side addition and never replaces the MCP transport.
// Its debug page dispatches against the in-process MCP server (not the /mcp
// HTTP endpoint), and it is deliberately read-only: the write tools and the
// state-changing tools are listed but have no call entry, so there is exactly
// one place that can change runvm state — the console's own edit/upload UI and
// the MCP transport.
package console

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dolfly/runvm/internal/mcp"
	"github.com/dolfly/runvm/internal/resources"
)

//go:embed static
var staticFS embed.FS

// cookieName is the session cookie the console hands out on login.
const cookieName = "runvm_console"

// sessionTTL bounds how long a login stays valid without re-authentication.
const sessionTTL = 24 * time.Hour

// maxDebugBody bounds a debug-page request; a tools/call may carry a whole JS
// file, so it matches the MCP transport's own limit.
const maxDebugBody = 16 << 20

// Options configures the console handler.
type Options struct {
	// MCP is the already-built /mcp and /healthz handler (with its Bearer
	// token auth, if any). The console forwards those paths untouched.
	MCP http.Handler
	// Server is the in-process MCP server the debug page dispatches against.
	Server *mcp.Server
	// Store is the writable resource store behind runvm://files/. It may be
	// nil, in which case edit/upload answer with a clear error.
	Store *resources.Store
	// Status returns the live backend/session snapshot (runvm_status data).
	Status func() (map[string]any, error)
	// Username and Password are the console login credentials. The caller is
	// responsible for generating a password when none is configured; no
	// default password may be baked in.
	Username string
	Password string
}

// Handler is the console. It owns the cookie sessions and the read-only debug
// gate.
type Handler struct {
	mcpHandler http.Handler
	server     *mcp.Server
	store      *resources.Store
	status     func() (map[string]any, error)
	username   string
	password   string
	sessions   *sessionStore
}

// New builds the console handler.
func New(o Options) http.Handler {
	h := &Handler{
		mcpHandler: o.MCP,
		server:     o.Server,
		store:      o.Store,
		status:     o.Status,
		username:   o.Username,
		password:   o.Password,
		sessions:   newSessionStore(),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp", "/healthz":
			// The MCP transport and its health check keep their existing
			// behavior, including Bearer-token auth.
			if h.mcpHandler != nil {
				h.mcpHandler.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}
		h.serve(w, r)
	})
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/login":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleLogin(w, r)
	case "/api/logout":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleLogout(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !h.authorized(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h.handleAPI(w, r)
			return
		}
		// Anything else is the SPA: the built dist tree, with index.html as
		// the fallback for the per-page routes (§6.3) so a refresh or a shared
		// deep link does not land on a 404.
		h.serveSPA(w, r)
	}
}

// serveSPA serves the embedded Vite build. The embed filesystem has no parent
// directory and no symlinks, so nothing can traverse out of it; the path is
// still cleaned and traversal is refused explicitly rather than left to
// ReadFile's error. Requests that resolve to a real file are served with their
// content type; extension-less paths (the SPA routes) fall back to index.html.
func (h *Handler) serveSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name, ok := cleanStaticPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if data, err := staticFS.ReadFile("static/dist/" + name); err == nil {
		writeStatic(w, r, name, data)
		return
	}
	// Not a real file. A missing hashed asset (it has an extension) is a real
	// 404; an extension-less path is an SPA route, so serve the shell.
	if strings.Contains(path.Base(name), ".") {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/dist/index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeStatic(w, r, "index.html", data)
}

// cleanStaticPath maps a request path to a name inside the embedded dist tree.
// It rejects anything that could escape the tree: absolute paths, "..",
// backslashes and NUL bytes.
func cleanStaticPath(p string) (string, bool) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "index.html", true
	}
	if strings.ContainsAny(p, "\\\x00") {
		return "", false
	}
	clean := path.Clean(p)
	if clean != p || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false
	}
	return clean, true
}

// writeStatic writes one embedded asset with a content type derived from its
// extension, falling back to octet-stream for anything unknown.
func writeStatic(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	// The shell must never be cached stale (the login page and the live data
	// are behind it); hashed assets are immutable by filename but keeping a
	// single no-cache policy is simpler and correct for a local console.
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

func (h *Handler) handleAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/status" && r.Method == http.MethodGet:
		h.handleStatus(w, r)
	case r.URL.Path == "/api/resources" && r.Method == http.MethodGet:
		h.handleResourcesList(w, r)
	case r.URL.Path == "/api/resources/read" && r.Method == http.MethodGet:
		h.handleResourceRead(w, r)
	case r.URL.Path == "/api/resources/file" && r.Method == http.MethodPut:
		h.handleResourceWrite(w, r)
	case r.URL.Path == "/api/upload" && r.Method == http.MethodPost:
		h.handleUpload(w, r)
	case r.URL.Path == "/api/debug/catalog" && r.Method == http.MethodGet:
		h.handleDebugCatalog(w, r)
	case r.URL.Path == "/api/debug/lint" && r.Method == http.MethodGet:
		h.handleDebugLint(w, r)
	case r.URL.Path == "/api/debug" && r.Method == http.MethodPost:
		h.handleDebug(w, r)
	default:
		http.NotFound(w, r)
	}
}

// --- auth -----------------------------------------------------------------

func (h *Handler) authorized(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	return h.sessions.valid(c.Value)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(req.Username), []byte(h.username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(req.Password), []byte(h.password)) == 1
	if !userOK || !passOK {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token := h.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		h.sessions.revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{tokens: map[string]time.Time{}}
}

func (s *sessionStore) create() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand is not expected to fail
	}
	t := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.tokens[t] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return t
}

func (s *sessionStore) valid(t string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[t]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, t)
		return false
	}
	return true
}

func (s *sessionStore) revoke(t string) {
	s.mu.Lock()
	delete(s.tokens, t)
	s.mu.Unlock()
}

// --- status / resources ----------------------------------------------------

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if h.status == nil {
		http.Error(w, "status unavailable", http.StatusServiceUnavailable)
		return
	}
	st, err := h.status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleResourcesList lists every resource the MCP server exposes, marking the
// writable runvm://files/ entries and attaching the structured sha256 and
// modification time the file-info view and status bar need. The MCP catalog
// only carries uri/name/description/mimeType/size, so those two fields come
// straight from the store's own listing.
//
// For runvm://files/* entries the `name` field is rewritten to the relative
// path under the resources root. The MCP catalog's name is a display label
// ("Resource <path>"), and the console's file browser treats `name` as the
// path — a display label there would make tree/save/select all target the
// wrong location (DO-143 QA regression).
func (h *Handler) handleResourcesList(w http.ResponseWriter, r *http.Request) {
	result, rerr := h.dispatch(r.Context(), "resources/list", nil)
	if rerr != nil {
		http.Error(w, rerr.Message, http.StatusInternalServerError)
		return
	}

	// Index the store's files by name once, so the console can fill in sha256
	// and mod_time without hashing per entry.
	fileMeta := map[string]resources.Meta{}
	if h.store != nil {
		if metas, err := h.store.List(); err == nil {
			for _, m := range metas {
				fileMeta[m.Name] = m
			}
		}
	}

	resourcesRaw, _ := result["resources"].([]any)
	out := make([]map[string]any, 0, len(resourcesRaw))
	for _, item := range resourcesRaw {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		uri, _ := entry["uri"].(string)
		writable := strings.HasPrefix(uri, "runvm://files/")
		entry["writable"] = writable
		if writable {
			name := strings.TrimPrefix(uri, "runvm://files/")
			entry["name"] = name
			if m, ok := fileMeta[name]; ok {
				entry["sha256"] = m.Sha256
				entry["mod_time"] = m.ModTime.UTC().Format(time.RFC3339)
			}
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": out})
}

// handleResourceRead fetches one resource's contents by URI, reusing the MCP
// resources/read semantics: text resources come back as text, binaries as a
// base64 blob.
func (h *Handler) handleResourceRead(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		http.Error(w, "missing uri", http.StatusBadRequest)
		return
	}
	result, rerr := h.dispatch(r.Context(), "resources/read", map[string]any{"uri": uri})
	if rerr != nil {
		http.Error(w, rerr.Message, http.StatusInternalServerError)
		return
	}
	contents, _ := result["contents"].([]any)
	if len(contents) == 0 {
		http.Error(w, "resource has no content", http.StatusInternalServerError)
		return
	}
	entry, _ := contents[0].(map[string]any)
	out := map[string]any{"uri": uri, "mimeType": entry["mimeType"]}
	if text, ok := entry["text"].(string); ok {
		out["text"] = text
	}
	if blob, ok := entry["blob"].(string); ok {
		out["blob"] = blob
	}
	writeJSON(w, http.StatusOK, out)
}

// handleResourceWrite saves edited text into a writable resource. This is the
// same internal write path runvm_write wraps, so a resources/read right after
// returns the new content.
func (h *Handler) handleResourceWrite(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.Error(w, "resource store is not configured; set RUNVM_RESOURCES_DIR and restart", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, h.limit()+1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	meta, err := h.store.Write(req.Name, []byte(req.Content))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, resources.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// handleUpload accepts a multipart file upload and stores it through the same
// resources.Store.Upload path the runvm_upload tool uses, so the validation
// and size cap are identical.
func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.Error(w, "resource store is not configured; set RUNVM_RESOURCES_DIR and restart", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseMultipartForm(h.limit() + (1 << 20)); err != nil {
		http.Error(w, "bad upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()
	target := r.FormValue("target")
	if target == "" {
		target = header.Filename
	}

	// Stage the upload to a temp file so the store's own streaming copy,
	// sha256 and size-cap logic runs exactly as it does for runvm_upload.
	tmp, err := os.CreateTemp("", "runvm-console-upload-*")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, io.LimitReader(file, h.limit()+1)); err != nil {
		tmp.Close()
		http.Error(w, "read upload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmp.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	meta, err := h.store.Upload(tmpName, target)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, resources.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// limit returns the store's per-file cap for request-body bounding.
func (h *Handler) limit() int64 {
	if h.store == nil {
		return resources.DefaultMaxSize
	}
	if m := h.store.MaxSize(); m > 0 {
		return m
	}
	return resources.DefaultMaxSize
}

// --- debug page ------------------------------------------------------------

// debugCallableTools is the allow-list of tools the debug page may call: the
// read-only, information-gathering ones. Everything else — the two write tools,
// every open/close/hook/stub/eval/action tool, and runvm_close_all — is listed
// in the catalog but has no call entry. An allow-list is deliberate: a deny-
// list silently grants every future tool a call entry, so adding a tool later
// could open a state-changing hole without touching this file.
var debugCallableTools = map[string]bool{
	"runvm_status":       true,
	"ios_symbols":        true,
	"ios_imports":        true,
	"android_symbols":    true,
	"android_java_stubs": true,
	"h5_console":         true,
}

// debugMethods are the JSON-RPC methods the read-only debug page may send.
var debugMethods = map[string]bool{
	"initialize":     true,
	"ping":           true,
	"tools/list":     true,
	"tools/call":     true,
	"resources/list": true,
	"resources/read": true,
	"prompts/list":   true,
	"prompts/get":    true,
}

// resourceKinds are the runvm:// resource URI families the debug page explains.
// There are five: two fixed virtual resources, the writable file store, and the
// two per-session artifact families.
var resourceKinds = []map[string]any{
	{"uri": "runvm://status", "writable": false, "description": "后端、限制、活跃会话 (JSON)"},
	{"uri": "runvm://tools", "writable": false, "description": "工具目录与 input schema (JSON)"},
	{"uri": "runvm://files/<path>", "writable": true, "description": "可写资源：文本走 text、二进制走 blob，上传或写入后才出现"},
	{"uri": "runvm://screenshot/<session>", "writable": false, "description": "最近一次截图 (PNG)，截过才存在"},
	{"uri": "runvm://pdf/<session>", "writable": false, "description": "最近一次 PDF 导出，导出过才存在"},
}

func (h *Handler) handleDebugCatalog(w http.ResponseWriter, r *http.Request) {
	if h.server == nil {
		http.Error(w, "MCP server unavailable", http.StatusServiceUnavailable)
		return
	}
	tools := make([]map[string]any, 0, len(h.server.Tools()))
	for _, t := range h.server.Tools() {
		tools = append(tools, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
			"callable":    debugCallableTools[t.Name],
		})
	}

	prompts, rerr := h.dispatch(r.Context(), "prompts/list", nil)
	if rerr != nil {
		http.Error(w, rerr.Message, http.StatusInternalServerError)
		return
	}
	resourcesList, rerr := h.dispatch(r.Context(), "resources/list", nil)
	if rerr != nil {
		http.Error(w, rerr.Message, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":          tools,
		"resources":      resourcesList["resources"],
		"resource_kinds": resourceKindsWithPresence(resourcesList["resources"]),
		"prompts":        prompts["prompts"],
	})
}

// resourceKindsWithPresence marks each of the five URI families with whether
// the live catalog currently has an entry of that kind, so the debug page can
// show "5 resource kinds" without pretending empty kinds have entries.
func resourceKindsWithPresence(live any) []map[string]any {
	uris := map[string]bool{}
	if list, ok := live.([]any); ok {
		for _, item := range list {
			if entry, ok := item.(map[string]any); ok {
				if uri, ok := entry["uri"].(string); ok {
					uris[uri] = true
				}
			}
		}
	}
	out := make([]map[string]any, 0, len(resourceKinds))
	for _, kind := range resourceKinds {
		entry := make(map[string]any, len(kind)+1)
		for k, v := range kind {
			entry[k] = v
		}
		entry["present"] = kindPresent(kind["uri"].(string), uris)
		out = append(out, entry)
	}
	return out
}

func kindPresent(template string, uris map[string]bool) bool {
	if i := strings.IndexByte(template, '<'); i >= 0 {
		// Parameterised kinds (runvm://files/<path>, .../screenshot/<session>)
		// are present when any live URI carries that prefix.
		prefix := template[:i]
		for uri := range uris {
			if strings.HasPrefix(uri, prefix) {
				return true
			}
		}
		return false
	}
	return uris[template]
}

func (h *Handler) handleDebugLint(w http.ResponseWriter, r *http.Request) {
	if h.server == nil {
		http.Error(w, "MCP server unavailable", http.StatusServiceUnavailable)
		return
	}
	infos := make([]ToolInfo, 0, len(h.server.Tools()))
	for _, t := range h.server.Tools() {
		infos = append(infos, ToolInfo{Name: t.Name, InputSchema: t.InputSchema})
	}
	issues := LintTools(infos)
	if issues == nil {
		issues = []SchemaLintIssue{}
	}
	errorsCount, warningsCount := 0, 0
	for _, i := range issues {
		if i.Severity == "error" {
			errorsCount++
		} else {
			warningsCount++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues":   issues,
		"errors":   errorsCount,
		"warnings": warningsCount,
	})
}

// handleDebug proxies one JSON-RPC request to the in-process server and echoes
// both the raw request and the raw response, which is the debug page's whole
// value: the exact wire bytes, not a summary.
func (h *Handler) handleDebug(w http.ResponseWriter, r *http.Request) {
	if h.server == nil {
		http.Error(w, "MCP server unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDebugBody))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	raw := json.RawMessage(body)
	if !json.Valid(raw) {
		writeDebug(w, raw, &mcp.Response{
			JSONRPC: mcp.JSONRPCVersion,
			Error:   &mcp.RPCError{Code: mcp.CodeParseError, Message: "invalid JSON"},
		})
		return
	}

	var req mcp.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeDebug(w, raw, &mcp.Response{
			JSONRPC: mcp.JSONRPCVersion,
			Error:   &mcp.RPCError{Code: mcp.CodeInvalidRequest, Message: err.Error()},
		})
		return
	}
	if rerr := h.checkDebug(&req); rerr != nil {
		writeDebug(w, raw, &mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: req.ID, Error: rerr})
		return
	}
	writeDebug(w, raw, h.server.Call(r.Context(), raw))
}

// checkDebug enforces the read-only contract: only the safe methods are
// accepted, and tools/call refuses the write and state-changing tools.
func (h *Handler) checkDebug(req *mcp.Request) *mcp.RPCError {
	if !debugMethods[req.Method] {
		return &mcp.RPCError{
			Code:    mcp.CodeMethodNotFound,
			Message: fmt.Sprintf("debug page is read-only: method %q is not allowed", req.Method),
		}
	}
	if req.Method != "tools/call" {
		return nil
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return &mcp.RPCError{Code: mcp.CodeInvalidParams, Message: "tools/call params: " + err.Error()}
	}
	if !debugCallableTools[p.Name] {
		return &mcp.RPCError{
			Code:    mcp.CodeInvalidParams,
			Message: fmt.Sprintf("debug page is read-only: tool %q has no call entry here", p.Name),
		}
	}
	return nil
}

func writeDebug(w http.ResponseWriter, req json.RawMessage, resp *mcp.Response) {
	writeJSON(w, http.StatusOK, map[string]any{"request": req, "response": resp})
}

// --- helpers ---------------------------------------------------------------

// dispatch sends one JSON-RPC request to the in-process server and returns the
// normalized result object (or a protocol error).
func (h *Handler) dispatch(ctx context.Context, method string, params any) (map[string]any, *mcp.RPCError) {
	if h.server == nil {
		return nil, &mcp.RPCError{Code: mcp.CodeInternalError, Message: "MCP server unavailable"}
	}
	msg, err := json.Marshal(map[string]any{"jsonrpc": mcp.JSONRPCVersion, "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, &mcp.RPCError{Code: mcp.CodeInternalError, Message: err.Error()}
	}
	resp := h.server.Call(ctx, msg)
	if resp.Error != nil {
		return nil, resp.Error
	}
	result, err := normalize(resp.Result)
	if err != nil {
		return nil, &mcp.RPCError{Code: mcp.CodeInternalError, Message: err.Error()}
	}
	return result, nil
}

// normalize round-trips a dispatch result through JSON so callers always see
// map[string]any and []any rather than the concrete catalog types.
func normalize(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
