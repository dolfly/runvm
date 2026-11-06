// Package h5 drives a real WebKit page through webframe.
//
// webframe's Frame must be created on the main goroutine and Run blocks it
// for the life of the page, so one page per process is the only shape its
// API allows. runvm therefore hosts each page in a child process (the
// "web-host" subcommand) and talks to it over a line-delimited JSON pipe:
// the MCP server stays a normal program while the page gets the main thread
// and event loop it needs.
package h5

// hostCmd is a command from the MCP server to a page host.
type hostCmd struct {
	ID int    `json:"id,omitempty"`
	Op string `json:"op"`
	JS string `json:"js,omitempty"`
}

// hostMsg is anything a page host reports back: an event, or a reply to a
// command when ID and OK are set.
type hostMsg struct {
	Event string `json:"event,omitempty"`
	Level string `json:"level,omitempty"`
	Text  string `json:"text,omitempty"`
	Code  int    `json:"code,omitempty"`

	ID     int    `json:"id,omitempty"`
	OK     *bool  `json:"ok,omitempty"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Line is one console line or page exception kept for h5_console.
type Line struct {
	Level string `json:"level,omitempty"`
	Text  string `json:"text"`
}
