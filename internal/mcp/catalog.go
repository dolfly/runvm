package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Resources and prompts: the read-only half of the surface. A resource is
// something the client can fetch by URI (status, tool list, a screenshot); a
// prompt is a parameterised instruction template. Both are optional in MCP
// and only advertised when something is registered.

// Resource is one fetchable document.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	// Read returns either a string (text resource) or []byte (blob).
	Read func(ctx context.Context) (any, error)
}

type resourceEntry struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// PromptArgument is one named parameter of a prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Prompt is one instruction template the client can instantiate.
type Prompt struct {
	Name        string
	Description string
	Arguments   []PromptArgument
	// Build renders the prompt. args holds the client-supplied values.
	Build func(args map[string]string) (string, error)
}

// AddResource registers a fixed resource.
func (s *Server) AddResource(r Resource) {
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	s.resources = append(s.resources, r)
}

// AddResourceLister registers a source of resources that depend on live state
// (a screenshot only exists once someone took one). It is consulted on every
// resources/list, so the catalog never goes stale.
func (s *Server) AddResourceLister(f func() []Resource) {
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	s.resourceListers = append(s.resourceListers, f)
}

// AddPrompt registers a prompt template.
func (s *Server) AddPrompt(p Prompt) {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	s.prompts = append(s.prompts, p)
}

func (s *Server) allResources() []Resource {
	s.resourceMu.Lock()
	out := append([]Resource(nil), s.resources...)
	listers := append([]func() []Resource(nil), s.resourceListers...)
	s.resourceMu.Unlock()
	for _, f := range listers {
		out = append(out, f()...)
	}
	return out
}

func (s *Server) listResources() any {
	entries := make([]resourceEntry, 0)
	for _, r := range s.allResources() {
		entries = append(entries, resourceEntry{URI: r.URI, Name: r.Name, Description: r.Description, MimeType: r.MimeType})
	}
	return map[string]any{"resources": entries}
}

func (s *Server) readResource(ctx context.Context, params json.RawMessage) (any, *RPCError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErrorf(CodeInvalidParams, "resources/read: %v", err)
	}
	for _, r := range s.allResources() {
		if r.URI != p.URI {
			continue
		}
		if r.Read == nil {
			break
		}
		value, err := r.Read(ctx)
		if err != nil {
			return nil, rpcErrorf(CodeInternalError, "resources/read %s: %v", p.URI, err)
		}
		mime := r.MimeType
		if mime == "" {
			mime = "text/plain"
		}
		switch v := value.(type) {
		case []byte:
			return map[string]any{"contents": []any{map[string]any{
				"uri": p.URI, "mimeType": mime, "blob": base64.StdEncoding.EncodeToString(v)}}}, nil
		case string:
			return map[string]any{"contents": []any{map[string]any{
				"uri": p.URI, "mimeType": mime, "text": v}}}, nil
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, rpcErrorf(CodeInternalError, "resources/read %s: %v", p.URI, err)
			}
			return map[string]any{"contents": []any{map[string]any{
				"uri": p.URI, "mimeType": mime, "text": string(b)}}}, nil
		}
	}
	return nil, rpcErrorf(CodeInvalidParams, "resources/read: unknown uri %q", p.URI)
}

func (s *Server) promptByName(name string) (Prompt, bool) {
	s.promptMu.Lock()
	defer s.promptMu.Unlock()
	for _, p := range s.prompts {
		if p.Name == name {
			return p, true
		}
	}
	return Prompt{}, false
}

func (s *Server) listPrompts() any {
	entries := make([]map[string]any, 0)
	for _, p := range s.prompts {
		entry := map[string]any{"name": p.Name, "description": p.Description}
		if len(p.Arguments) > 0 {
			entry["arguments"] = p.Arguments
		}
		entries = append(entries, entry)
	}
	return map[string]any{"prompts": entries}
}

func (s *Server) getPrompt(params json.RawMessage) (any, *RPCError) {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErrorf(CodeInvalidParams, "prompts/get: %v", err)
	}
	prompt, ok := s.promptByName(p.Name)
	if !ok {
		return nil, rpcErrorf(CodeInvalidParams, "prompts/get: unknown prompt %q", p.Name)
	}
	text, err := prompt.Build(p.Arguments)
	if err != nil {
		return nil, rpcErrorf(CodeInvalidParams, "prompts/get: %v", err)
	}
	return map[string]any{
		"description": prompt.Description,
		"messages": []any{map[string]any{
			"role":    "user",
			"content": map[string]any{"type": "text", "text": text},
		}},
	}, nil
}

// RequireArg pulls a required prompt argument, with a message that names it.
func RequireArg(args map[string]string, name string) (string, error) {
	if v := args[name]; v != "" {
		return v, nil
	}
	return "", fmt.Errorf("missing required argument %q", name)
}
