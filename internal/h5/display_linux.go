//go:build linux

package h5

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// ensureDisplay gives WebKitGTK an X display to draw on.
//
// webframe's CLI does this for itself, but the library does not: when it is
// embedded the caller owns the display. With DISPLAY or WAYLAND_DISPLAY set
// (a desktop session, or xvfb-run) nothing happens; otherwise a private Xvfb
// is started and shut down with the process.
// When force is set the process always owns its display, even if DISPLAY is
// already set: screenshotting a shared desktop is not what asking for a page
// capture should do.
func ensureDisplay(force bool, width, height int) (bool, error) {
	if !force && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
		return false, nil
	}
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		return false, errors.New("no display: set DISPLAY, install Xvfb, or run under xvfb-run")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return false, err
	}
	defer r.Close()

	// -displayfd has Xvfb pick a free display and write its number to fd 3
	// once it is actually listening, which beats parsing its log.
	if width <= 0 || height <= 0 {
		width, height = 1280, 800
	}
	// The virtual screen is exactly the page viewport, so a root-window grab
	// is the page and nothing else — no crop step, no desktop pixels.
	screen := fmt.Sprintf("%dx%dx24", width, height)
	cmd := exec.Command(xvfb, "-displayfd", "3", "-screen", "0", screen, "-nolisten", "tcp")
	cmd.ExtraFiles = []*os.File{w}
	// Pdeathsig is tied to the thread that created the process — the main
	// thread here, which is where the webview lives too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true}
	if err := cmd.Start(); err != nil {
		w.Close()
		return false, fmt.Errorf("starting Xvfb: %w", err)
	}
	w.Close()

	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		return false, errors.New("Xvfb failed to start")
	}
	return true, os.Setenv("DISPLAY", ":"+strings.TrimSpace(line))
}
