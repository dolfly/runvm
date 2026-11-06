package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// maxInFlight bounds concurrent requests. A model that fires fifty tool calls
// at once gets backpressure here rather than fifty child processes.
const maxInFlight = 32

// DefaultToolTimeout bounds a single tools/call so a stuck backend cannot
// wedge the session. Tools that need longer (a page load, a slow emulator
// boot) still answer within this budget, because the work they wait on is
// themselves bounded.
const DefaultToolTimeout = 120 * time.Second

// Handler runs one tool. Returning an error surfaces as a tool result with
// isError set, not as a JSON-RPC error: the model should see and react to it.
type Handler func(ctx context.Context, args json.RawMessage) (any, error)

// Tool is one callable capability.
type Tool struct {
	Name        string
	Description string
	InputSchema any
	Handler     Handler
}

// Server is a tool-only MCP server over a stream pair.
type Server struct {
	name    string
	version string
	tools   map[string]*Tool
	order   []string

	// cancels maps a request id to the cancel function of the call running
	// under it, so notifications/cancelled can actually stop work instead of
	// being a polite no-op.
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

// NewServer creates an empty server. Tools are added before Serve.
func NewServer(name, version string) *Server {
	return &Server{name: name, version: version, tools: map[string]*Tool{}, cancels: map[string]context.CancelFunc{}}
}

// Add registers a tool. Re-adding a name replaces it in place, which keeps
// tools/list stable if a caller builds the registry twice.
func (s *Server) Add(t Tool) {
	if _, ok := s.tools[t.Name]; !ok {
		s.order = append(s.order, t.Name)
	}
	tool := t
	s.tools[t.Name] = &tool
}

// Tools returns the registered tools in registration order.
func (s *Server) Tools() []*Tool {
	out := make([]*Tool, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.tools[n])
	}
	return out
}

// Serve reads newline-delimited JSON-RPC from in and writes replies to out
// until the stream closes or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	// MCP arguments (a whole JS file, a symbol list) exceed the 64 KiB
	// default; 16 MiB is far past anything a model sends in one call.
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)

	enc := json.NewEncoder(out)
	var (
		mu       sync.Mutex // guards enc and writeErr
		writeErr error
		wg       sync.WaitGroup
	)
	slots := make(chan struct{}, maxInFlight)

	// Requests run concurrently and answers may come back out of order, which
	// JSON-RPC allows: a slow h5_open must not block a call against a session
	// that is already open. Ordering that matters (a page's own script calls,
	// one engine's calls) is enforced by the backend, not here.
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			break
		}
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		buf := append([]byte(nil), line...)

		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			resp := s.handle(ctx, buf)
			if resp == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if err := enc.Encode(resp); err != nil && writeErr == nil {
				writeErr = err
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	err := writeErr
	mu.Unlock()
	if err != nil {
		return fmt.Errorf("mcp: write response: %w", err)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("mcp: read stdin: %w", err)
	}
	return nil
}

// handle decodes and dispatches one message, returning nil for notifications.
func (s *Server) handle(ctx context.Context, line []byte) *Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return &Response{JSONRPC: JSONRPCVersion, Error: rpcErrorf(CodeParseError, "invalid JSON: %v", err)}
	}

	// Cancellation arrives as a notification naming the request to stop.
	if req.Method == "notifications/cancelled" {
		s.cancelRequest(req.Params)
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !req.IsNotification() {
		s.registerCancel(req.ID, cancel)
		defer s.unregisterCancel(req.ID)
	}

	resp := &Response{JSONRPC: JSONRPCVersion, ID: req.ID}
	result, rerr := s.dispatch(ctx, &req)
	if req.IsNotification() {
		return nil
	}
	if rerr != nil {
		resp.Error = rerr
		return resp
	}
	resp.Result = result
	return resp
}

func (s *Server) dispatch(ctx context.Context, req *Request) (any, *RPCError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params), nil
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return s.listTools(), nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	case "resources/list", "prompts/list":
		// Declared absent in capabilities; answering empty beats an error
		// for clients that probe unconditionally.
		return map[string]any{"resources": []any{}, "prompts": []any{}}, nil
	default:
		return nil, rpcErrorf(CodeMethodNotFound, "method %q is not supported", req.Method)
	}
}

func (s *Server) initialize(params json.RawMessage) any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	return map[string]any{
		"protocolVersion": negotiateVersion(p.ProtocolVersion),
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{"name": s.name, "version": s.version},
		"instructions": "runvm exposes throwaway test environments: android_* drives " +
			"a native Android .so inside golem's emulator, h5_* drives a real WebKit page " +
			"through webframe. Open a session, call it, then close it.",
	}
}

func (s *Server) listTools() any {
	infos := make([]toolInfo, 0, len(s.order))
	for _, t := range s.Tools() {
		infos = append(infos, toolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return map[string]any{"tools": infos}
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *RPCError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErrorf(CodeInvalidParams, "tools/call params: %v", err)
	}
	tool, ok := s.tools[p.Name]
	if !ok {
		return nil, rpcErrorf(CodeInvalidParams, "unknown tool %q", p.Name)
	}
	if len(p.Arguments) == 0 {
		p.Arguments = json.RawMessage("{}")
	}

	cctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
	defer cancel()

	value, err := tool.Handler(cctx, p.Arguments)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return callResult{IsError: true, Content: []any{textContent{Type: "text",
				Text: fmt.Sprintf("%s timed out after %s", p.Name, DefaultToolTimeout)}}}, nil
		}
		return callResult{IsError: true, Content: []any{textContent{Type: "text",
			Text: err.Error()}}}, nil
	}
	if img, ok := value.(Image); ok {
		content := make([]any, 0, 2)
		if img.Caption != "" {
			content = append(content, textContent{Type: "text", Text: img.Caption})
		}
		content = append(content, imageContent{Type: "image", Data: base64.StdEncoding.EncodeToString(img.Data), MimeType: img.MimeType})
		return callResult{Content: content}, nil
	}
	return callResult{Content: []any{textContent{Type: "text", Text: toJSON(value)}}}, nil
}

// toJSON renders a tool result. Strings pass through unchanged so a JS
// evaluation result reads as itself rather than as a quoted JSON string.
func toJSON(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func (s *Server) registerCancel(id json.RawMessage, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancels[string(id)] = cancel
}

func (s *Server) unregisterCancel(id json.RawMessage) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.cancels, string(id))
}

// cancelRequest stops the call the client named. An unknown id is ignored:
// the call may have finished between the cancel being sent and arriving.
func (s *Server) cancelRequest(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := json.Unmarshal(params, &p); err != nil || len(p.RequestID) == 0 {
		return
	}
	s.cancelMu.Lock()
	cancel := s.cancels[string(p.RequestID)]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
