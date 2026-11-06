module github.com/dolfly/runvm

go 1.27.3

require (
	github.com/isesword/golem v0.2.1
	tractor.dev/webframe v0.0.0-20261030030822-25c24cb61aad
)

require (
	github.com/crgimenes/glaze v0.0.0-00010101000000-000000000000 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// webframe pins its webview binding to progrium's glaze fork. Replace
// directives inside dependencies are ignored by Go, so the host module has to
// repeat it or the build fails with "unknown revision 000000000000".
replace github.com/crgimenes/glaze => github.com/progrium/glaze v0.0.0-20261029191355-bc7899dbb235
