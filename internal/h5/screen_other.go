//go:build !linux && !windows

package h5

import "errors"

// capturePNG is Linux-only: the cross-platform WebView APIs expose no
// snapshot call, so there is nothing honest to return here yet.
func capturePNG(string) ([]byte, error) {
	return nil, errors.New("screenshot: not supported on this platform yet; on macOS it needs a native WKWebView snapshot (see docs/ROADMAP.md)")
}

func captureSupported(bool, string) error {
	return errors.New("screenshot: not supported on this platform yet")
}
