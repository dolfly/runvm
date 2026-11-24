package h5

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeSession builds a Session whose host side is a pipe the test can read.
func fakeSession(t *testing.T) (*Session, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	s := &Session{
		stdin:   w,
		pending: map[int]*waiter{},
		done:    make(chan struct{}),
	}
	return s, r
}

func TestDeliverCarriesPDF(t *testing.T) {
	s, _ := fakeSession(t)
	w := &waiter{op: "printpdf", ch: make(chan evalReply, 1)}
	s.pending[4] = w

	ok := true
	s.deliver(hostMsg{ID: 4, OK: &ok, PDF: []byte("%PDF-1.3")})

	r := <-w.ch
	if r.err != nil {
		t.Fatalf("deliver: %v", r.err)
	}
	if string(r.pdf) != "%PDF-1.3" {
		t.Fatalf("pdf = %q", r.pdf)
	}
	if _, still := s.pending[4]; still {
		t.Error("a delivered reply must clear its pending entry")
	}
}

// A backend that cannot print must be distinguishable from a backend that
// failed, because only the first one is allowed to fall back to a raster.
func TestDeliverMapsUnsupportedPerCommand(t *testing.T) {
	for _, tc := range []struct {
		op   string
		want error
	}{
		{"printpdf", errPrintUnsupported},
		{"snapshot", errSnapshotUnsupported},
	} {
		s, _ := fakeSession(t)
		w := &waiter{op: tc.op, ch: make(chan evalReply, 1)}
		s.pending[1] = w

		ok := false
		s.deliver(hostMsg{ID: 1, OK: &ok, Error: "unsupported: no such selector"})

		err := (<-w.ch).err
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.op, err, tc.want)
		}
		if !strings.Contains(err.Error(), "no such selector") {
			t.Errorf("%s: the host's own message must survive: %v", tc.op, err)
		}
	}
}

func TestRequestWritesTheOpAndReturnsTheReply(t *testing.T) {
	s, r := fakeSession(t)

	got := make(chan evalReply, 1)
	go func() {
		reply, err := s.request("printpdf", 5*time.Second)
		if err != nil {
			t.Errorf("request: %v", err)
		}
		got <- reply
	}()

	// The command has to reach the host as one JSON line naming the op.
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("read command: %v", err)
	}
	line := string(buf[:n])
	if !strings.Contains(line, `"op":"printpdf"`) || !strings.HasSuffix(line, "\n") {
		t.Fatalf("command = %q, want one JSON line with op printpdf", line)
	}

	// Answer it the way a host would.
	var id int
	if _, err := fmt.Sscanf(line, `{"id":%d,`, &id); err != nil {
		t.Fatalf("parse id from %q: %v", line, err)
	}
	ok := true
	s.deliver(hostMsg{ID: id, OK: &ok, PDF: []byte("%PDF-1.7")})

	if reply := <-got; string(reply.pdf) != "%PDF-1.7" {
		t.Fatalf("reply = %+v", reply)
	}
}

// A host that never answers must not leak the pending entry.
func TestRequestTimesOutAndDropsPending(t *testing.T) {
	s, _ := fakeSession(t)

	if _, err := s.request("printpdf", 50*time.Millisecond); err == nil {
		t.Fatal("want a timeout error")
	} else if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	s.mu.Lock()
	left := len(s.pending)
	s.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d pending entries left behind after a timeout", left)
	}
}

func TestNewDetachedAliveAndDead(t *testing.T) {
	alive := NewDetached("https://example.com", true)
	if !alive.Alive() {
		t.Error("a detached-alive session must report Alive")
	}
	if alive.Target() != "https://example.com" {
		t.Errorf("Target = %q", alive.Target())
	}

	dead := NewDetached("https://example.com", false)
	if dead.Alive() {
		t.Error("a detached-dead session must not report Alive")
	}
	select {
	case <-dead.Done():
	default:
		t.Error("a detached-dead session's Done channel must be closed")
	}

	// Closing a detached session has no host process to signal or reap.
	if err := dead.Close(); err != nil {
		t.Errorf("Close on detached dead: %v", err)
	}
	if err := alive.Close(); err != nil {
		t.Errorf("Close on detached alive: %v", err)
	}
}

func TestConsoleTouchesLastUsed(t *testing.T) {
	s, _ := fakeSession(t)
	before := s.LastUsed()
	time.Sleep(time.Millisecond)
	s.Console(false)
	if !s.LastUsed().After(before) {
		t.Error("Console must touch last_used")
	}
}

func TestRequestTouchesLastUsed(t *testing.T) {
	s, _ := fakeSession(t)
	before := s.LastUsed()
	// request touches before writing, so a call that then times out still
	// counts as use.
	_, _ = s.request("snapshot", 20*time.Millisecond)
	if !s.LastUsed().After(before) {
		t.Error("request must touch last_used")
	}
}

func TestProfileNameFollowsEphemeral(t *testing.T) {
	// The flag and the profile must never disagree: an ephemeral page has
	// nothing on disk, so it must not claim a profile, and a persistent page
	// must not be reported as in-memory.
	cases := []struct {
		name      string
		opts      OpenOptions
		ephemeral bool
		want      string
	}{
		{"default is ephemeral", OpenOptions{}, true, ""},
		{"profile dropped when ephemeral", OpenOptions{Profile: "shop"}, true, ""},
		{"profile kept when persistent", OpenOptions{Profile: "shop"}, false, "shop"},
		{"persistent without a profile", OpenOptions{}, false, ""},
	}
	for _, tc := range cases {
		if got := profileName(tc.opts, tc.ephemeral); got != tc.want {
			t.Errorf("%s: profileName = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDetachedSessionReportsNoProfile(t *testing.T) {
	s := NewDetached("https://example.com", true)
	if s.Ephemeral() {
		t.Error("a detached session is not an ephemeral browser page")
	}
	if s.Profile() != "" {
		t.Errorf("Profile = %q, want empty", s.Profile())
	}
	if s.CreatedAt().IsZero() {
		t.Error("CreatedAt must be set")
	}
}
