package h5

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// defaultReadyTimeout covers a cold page load: spawning the host, starting
// Xvfb on a display-less Linux box, booting WebKit and running the hooks.
var defaultReadyTimeout = 30 * time.Second

// maxConsoleLines bounds the console backlog so a page in a loop cannot grow
// the server's memory without bound.
const maxConsoleLines = 500

// Session is one page in a child process.
type Session struct {
	target string
	cmd    *exec.Cmd
	stdin  io.WriteCloser

	mu      sync.Mutex
	wmu     sync.Mutex // serializes writes to stdin: one line per JSON message
	evalMu  sync.Mutex // one evaluation at a time: statement order is part of the API
	seq     int
	pending map[int]chan evalReply
	lines   []Line
	closed  bool

	ready chan struct{}
	done  chan struct{}
	exit  int

	readyOnce sync.Once
}

type evalReply struct {
	result string
	err    error
}

// Open starts a page host for target (a URL, a file path, or about:blank) and
// waits until its hooks are installed and the page is evaluable.
func Open(target string, env map[string]string, ephemeral bool, readyTimeout time.Duration) (*Session, error) {
	if target == "" {
		target = "about:blank"
	}
	if readyTimeout <= 0 {
		readyTimeout = defaultReadyTimeout
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("h5_open: locate runvm binary: %w", err)
	}

	cmd := exec.Command(exe, "web-host", target)
	cmd.Env = append(os.Environ(), "RUNVM_HOST=1")
	if ephemeral {
		cmd.Env = append(cmd.Env, "RUNVM_EPHEMERAL=1")
	}
	if len(env) > 0 {
		blob, err := json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("h5_open: env: %w", err)
		}
		cmd.Env = append(cmd.Env, "RUNVM_HOST_ENV="+string(blob))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("h5_open: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("h5_open: stdout pipe: %w", err)
	}
	// The host's stderr carries WebKit/GTK noise and is not part of the
	// protocol, so it goes straight to our stderr.
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("h5_open: start host: %w", err)
	}

	s := &Session{
		target:  target,
		cmd:     cmd,
		stdin:   stdin,
		pending: map[int]chan evalReply{},
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
	}
	go s.readLoop(stdout)

	select {
	case <-s.ready:
		return s, nil
	case <-s.done:
		if lines := s.Console(false); len(lines) > 0 {
			last := lines[len(lines)-1]
			return nil, fmt.Errorf("h5_open: page did not become ready (%s): %s", target, last.Text)
		}
		return nil, fmt.Errorf("h5_open: page did not become ready (%s)", target)
	case <-time.After(readyTimeout):
		_ = s.Close()
		return nil, fmt.Errorf("h5_open: timed out after %s waiting for %s", readyTimeout, target)
	}
}

func (s *Session) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var msg hostMsg
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		switch msg.Event {
		case "":
			s.deliver(msg)
		case "ready":
			s.readyOnce.Do(func() { close(s.ready) })
		case "console", "exception":
			s.appendLine(Line{Level: msg.Level, Text: msg.Text})
		case "loaded":
			s.appendLine(Line{Level: "load", Text: s.target})
		case "exit":
			s.mu.Lock()
			s.exit = msg.Code
			s.mu.Unlock()
		}
	}
	close(s.done)
}

func (s *Session) deliver(msg hostMsg) {
	s.mu.Lock()
	ch, ok := s.pending[msg.ID]
	delete(s.pending, msg.ID)
	s.mu.Unlock()
	if !ok {
		return
	}
	if msg.OK != nil && *msg.OK {
		ch <- evalReply{result: msg.Result}
		return
	}
	ch <- evalReply{err: errors.New(msg.Error)}
}

func (s *Session) appendLine(l Line) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, l)
	if len(s.lines) > maxConsoleLines {
		s.lines = s.lines[len(s.lines)-maxConsoleLines:]
	}
}

// Eval runs js in the page and returns webframe's formatted result. The
// statement may be an expression or a block; webframe awaits promises.
func (s *Session) Eval(js string, timeout time.Duration) (string, error) {
	if js == "" {
		return "", errors.New("h5_eval: empty script")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// A page is single-threaded; two interleaved evaluations would make the
	// order of side effects unpredictable, so callers queue here.
	s.evalMu.Lock()
	defer s.evalMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", errors.New("h5_eval: session is closed")
	}
	s.seq++
	id := s.seq
	ch := make(chan evalReply, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	payload, err := json.Marshal(hostCmd{ID: id, Op: "eval", JS: js})
	if err != nil {
		return "", fmt.Errorf("h5_eval: %w", err)
	}
	s.wmu.Lock()
	_, werr := s.stdin.Write(append(payload, '\n'))
	s.wmu.Unlock()
	if werr != nil {
		s.dropPending(id)
		return "", fmt.Errorf("h5_eval: write to host: %w", werr)
	}

	select {
	case r := <-ch:
		return r.result, r.err
	case <-s.done:
		s.dropPending(id)
		return "", errors.New("h5_eval: page closed before the result arrived")
	case <-time.After(timeout):
		s.dropPending(id)
		return "", fmt.Errorf("h5_eval: timed out after %s", timeout)
	}
}

func (s *Session) dropPending(id int) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

// Console returns buffered console lines, optionally clearing them.
func (s *Session) Console(clear bool) []Line {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Line, len(s.lines))
	copy(out, s.lines)
	if clear {
		s.lines = nil
	}
	return out
}

// Target is the page this session opened.
func (s *Session) Target() string { return s.target }

// Close stops the page host. Safe to call twice.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	select {
	case <-s.done:
	default:
		s.wmu.Lock()
		_, _ = s.stdin.Write([]byte("{\"op\":\"close\"}\n"))
		s.wmu.Unlock()
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			_ = s.cmd.Process.Kill()
		}
	}
	_ = s.stdin.Close()
	return s.cmd.Wait()
}

// EnvFromHost reads the env blob the parent passed on the command line.
func EnvFromHost() (map[string]string, bool) {
	blob := os.Getenv("RUNVM_HOST_ENV")
	ephemeral := os.Getenv("RUNVM_EPHEMERAL") == "1"
	if blob == "" {
		return map[string]string{}, ephemeral
	}
	var env map[string]string
	if err := json.Unmarshal([]byte(blob), &env); err != nil {
		return map[string]string{}, ephemeral
	}
	return env, ephemeral
}
