//go:build !linux

package h5

// ensureDisplay is a no-op off Linux: macOS and Windows always have a window
// server, and the webview needs no display bootstrap.
func ensureDisplay() error { return nil }
