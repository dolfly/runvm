package mcp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// HTTPOptions configures the Streamable HTTP transport.
type HTTPOptions struct {
	// Token, when set, requires "Authorization: Bearer <token>". Binding to a
	// non-loopback address without one is refused by the caller, not here.
	Token string
}

const maxBody = 16 << 20 // a tools/call may carry a whole JS file

// Handler exposes the same tools over MCP's Streamable HTTP transport.
//
// One endpoint carries everything: POST /mcp takes a JSON-RPC message (or a
// batch) and answers with JSON; GET /mcp is refused because this server never
// pushes unsolicited messages, which is exactly what a client should expect
// when it probes for the optional server->client stream.
func (s *Server) Handler(opt HTTPOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		s.serveMCP(w, r, opt)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request, opt HTTPOptions) {
	if opt.Token != "" && !validBearer(r.Header.Get("Authorization"), opt.Token) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="runvm"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.httpPost(w, r, body)

	case http.MethodGet:
		// No server-initiated stream: a client that opened one would wait
		// forever, so say so instead of holding the connection.
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "this server does not push server-initiated messages; use POST /mcp", http.StatusMethodNotAllowed)

	case http.MethodDelete:
		// Session termination is a no-op: sessions live in the tool layer and
		// are closed explicitly with h5_close / android_close.
		w.WriteHeader(http.StatusOK)

	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) httpPost(w http.ResponseWriter, r *http.Request, body []byte) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	if trimmed[0] == '[' { // JSON-RPC batch
		var batch []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &batch); err != nil {
			writeJSONRPCError(w, CodeParseError, "invalid JSON: "+err.Error())
			return
		}
		out := make([]*Response, 0, len(batch))
		for _, msg := range batch {
			if resp := s.handle(r.Context(), msg); resp != nil {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	resp := s.handle(r.Context(), []byte(trimmed))
	if resp == nil {
		// Notifications get no body, per the transport spec.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	// Give the client a session handle to echo back; this server does not
	// require it, but clients expect to see one after initialize.
	if result, ok := resp.Result.(map[string]any); ok {
		if _, isInit := result["serverInfo"]; isInit {
			w.Header().Set("Mcp-Session-Id", newSessionID())
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONRPCError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, http.StatusOK, &Response{
		JSONRPC: JSONRPCVersion,
		Error:   &RPCError{Code: code, Message: msg},
	})
}

// validBearer compares in constant time so a token cannot be guessed by
// timing the 401s.
func validBearer(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "runvm"
	}
	return hex.EncodeToString(b[:])
}
