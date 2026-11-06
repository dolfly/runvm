module github.com/dolfly/runvm

go 1.27.3

require (
	github.com/isesword/golem v0.2.1
	golang.org/x/sys v0.48.0
	tractor.dev/webframe v0.0.0-20261030030822-25c24cb61aad
)

require (
	github.com/dolfly/glaze v0.2.1 // indirect
	github.com/ebitengine/purego v0.11.3 // indirect
)

// webframe is the one fork runvm carries: it exposes Frame.Snapshot(), which
// reaches the WKWebView snapshot in github.com/dolfly/glaze. That module is
// required by its own path, so there is no glaze replace here.
replace tractor.dev/webframe => github.com/dolfly/webframe-fork v0.2.0
