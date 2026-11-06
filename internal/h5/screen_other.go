//go:build !linux

package h5

import "errors"

// capturePNG is Linux-only: the cross-platform WebView APIs expose no
// snapshot call, so there is nothing honest to return here yet.
func capturePNG(string) ([]byte, error) {
	return nil, errors.New("screenshot: only supported on Linux (private Xvfb + xwd); see docs/ROADMAP.md")
}
