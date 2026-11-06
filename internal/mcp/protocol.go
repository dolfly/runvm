// Package mcp implements the subset of the Model Context Protocol a
// tool-only server needs: JSON-RPC 2.0 over stdio with initialize,
// tools/list and tools/call.
//
// The transport is newline-delimited JSON on stdin/stdout, as the MCP stdio
// binding specifies. Nothing else may write to stdout while a server runs.
package mcp

import (
	"encoding/json"
	"fmt"
)

const (
	// JSONRPCVersion is the only protocol version this server speaks.
	JSONRPCVersion = "2.0"

	// ProtocolVersion is the newest MCP revision the server implements.
	ProtocolVersion = "2025-06-18"
)

// supportedVersions is ordered newest first; initialize answers with the
// client's version when it is known, so older clients keep working.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Request is one JSON-RPC call or notification. A request without an ID is a
// notification and gets no response.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the peer expects no reply.
func (r *Request) IsNotification() bool { return len(r.ID) == 0 }

// Response is one JSON-RPC reply. Exactly one of Result or Error is set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// JSON-RPC 2.0 error codes, plus the MCP-recommended ones in use here.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

func rpcErrorf(code int, format string, args ...any) *RPCError {
	return &RPCError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// toolInfo is the wire shape of one entry in tools/list.
type toolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"inputSchema"`
}

// textContent is the only content type this server emits: tool results are
// JSON rendered as text, which every client can display.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// imageContent is one base64 image in a tool result.
type imageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// Image is what a handler returns when the result is a picture: the model gets
// the pixels plus an optional one-line caption.
type Image struct {
	MimeType string
	Data     []byte
	Caption  string
}

type callResult struct {
	Content []any `json:"content"`
	IsError bool  `json:"isError,omitempty"`
}

func negotiateVersion(client string) string {
	for _, v := range supportedVersions {
		if v == client {
			return v
		}
	}
	return ProtocolVersion
}
