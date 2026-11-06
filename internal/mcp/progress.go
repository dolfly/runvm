package mcp

import "context"

// Progress reporting, the let-the-model-watch half of a long tool call.
//
// A client opts in by putting a progressToken in the request's _meta; the
// server then streams notifications/progress for that token. Both transports
// support it: stdio interleaves the notifications with replies, HTTP upgrades
// the POST response to an SSE stream. A caller that does not opt in sees the
// same behaviour as before, which is why every call site works with or
// without a token.

type notifierKey struct{}

// notifier is what a transport installs to carry progress for one request.
type notifier func(progress float64, message string)

// withNotifier returns a context carrying the sink for one request.
func withNotifier(ctx context.Context, n notifier) context.Context {
	return context.WithValue(ctx, notifierKey{}, n)
}

// Notify reports progress for the request in ctx, if the client asked for it.
// Calls from tools are always safe: with no token they are no-ops.
func Notify(ctx context.Context, progress float64, message string) {
	if n, ok := ctx.Value(notifierKey{}).(notifier); ok && n != nil {
		n(progress, message)
	}
}

// HasProgress reports whether anyone is listening, so a tool can skip work
// whose only purpose is a progress message.
func HasProgress(ctx context.Context) bool {
	n, ok := ctx.Value(notifierKey{}).(notifier)
	return ok && n != nil
}

// progressParams is the shape of notifications/progress.
type progressParams struct {
	ProgressToken any     `json:"progressToken"`
	Progress      float64 `json:"progress"`
	Total         float64 `json:"total,omitempty"`
	Message       string  `json:"message,omitempty"`
}
