// Command runvm starts throwaway test environments for Android native
// libraries and web pages, and exposes them to a model over the Model Context
// Protocol.
//
// Two subcommands matter:
//
//	runvm          MCP server on stdin/stdout
//	runvm doctor   probe what this machine can actually run
//
// "runvm web-host" is internal: it is the child process that owns one page,
// because webframe's API requires the webview to own the main goroutine.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/dolfly/runvm/internal/console"
	"github.com/dolfly/runvm/internal/doctor"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/mcp"
	"github.com/dolfly/runvm/internal/persist"
	"github.com/dolfly/runvm/internal/resources"
	"github.com/dolfly/runvm/internal/tools"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "web-host":
			// webframe requires the webview on the main OS thread; this is
			// that thread, so nothing else may be scheduled onto it.
			runtime.LockOSThread()
			target := "about:blank"
			if len(os.Args) > 2 {
				target = os.Args[2]
			}
			env, ephemeral := h5.EnvFromHost()
			os.Exit(h5.RunHost(target, env, ephemeral))
		case "selftest":
			selftest()
			return
		case "doctor":
			report := doctor.Run(version)
			report.Print(os.Stdout)
			if !report.OK() {
				os.Exit(1)
			}
			return
		case "version", "-v", "--version":
			fmt.Printf("runvm %s (go %s, %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return
		case "http":
			serveHTTP(os.Args[2:])
			return
		case "help", "-h", "--help":
			usage()
			return
		case "mcp", "serve":
			// fall through to the default server below
		default:
			fmt.Fprintf(os.Stderr, "runvm: unknown command %q\n\n", os.Args[1])
			usage()
			os.Exit(2)
		}
	}
	serve()
}

func serve() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, set := tools.New(version, limitsFromEnv())
	res, err := resourcesFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "runvm:", err)
		os.Exit(1)
	}
	set.Resources = res
	openPersistence(set)
	set.StartReaper(ctx)
	// Sessions hold emulators and child processes; a clean shutdown must not
	// leave either behind. It keeps the ledger, so the next process can report
	// what was open when this one stopped.
	defer set.Shutdown()

	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "runvm:", err)
		os.Exit(1)
	}
}

// serveHTTP exposes the tool surface over MCP's Streamable HTTP transport, for
// clients that cannot spawn a process. It refuses to listen on a non-loopback
// address without a token: an unauthenticated port on this server would let
// anyone on the network run code in a native emulator.
func serveHTTP(args []string) {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	addr := fs.String("addr", envOr("RUNVM_HTTP_ADDR", "127.0.0.1:8765"), "listen address")
	token := fs.String("token", os.Getenv("RUNVM_TOKEN"), "require Authorization: Bearer <token> (env RUNVM_TOKEN)")
	_ = fs.Parse(args)

	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runvm: bad -addr %q: %v\n", *addr, err)
		os.Exit(2)
	}
	loopback := host == "" || host == "127.0.0.1" || host == "::1" || host == "localhost"
	if !loopback && *token == "" {
		fmt.Fprintf(os.Stderr, "runvm: refusing to listen on %q without a token; \n"+
			"       set -token or RUNVM_TOKEN, or bind 127.0.0.1 and use a tunnel\n", *addr)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, set := tools.New(version, limitsFromEnv())
	res, err := resourcesFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "runvm:", err)
		os.Exit(2)
	}
	set.Resources = res
	openPersistence(set)
	set.StartReaper(ctx)
	defer set.Shutdown()

	mcpHandler := server.Handler(mcp.HTTPOptions{Token: *token})

	// Console credentials come from the environment. A missing password is
	// generated and printed to stderr; there is deliberately no baked-in
	// default password, so an out-of-the-box run is never guessable.
	consoleUser := envOr("RUNVM_CONSOLE_USER", "admin")
	consolePassword := os.Getenv("RUNVM_CONSOLE_PASSWORD")
	if consolePassword == "" {
		consolePassword = randomHex(16)
		fmt.Fprintf(os.Stderr, "runvm: console login user %q generated password %q\n"+
			"       (set RUNVM_CONSOLE_USER / RUNVM_CONSOLE_PASSWORD to choose)\n",
			consoleUser, consolePassword)
	}

	handler := console.New(console.Options{
		MCP:      mcpHandler,
		Server:   server,
		Store:    res,
		Status:   func() (map[string]any, error) { return set.Status() },
		Username: consoleUser,
		Password: consolePassword,
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	auth := "off"
	if *token != "" {
		auth = "bearer token"
	}
	fmt.Fprintf(os.Stderr, "runvm: MCP over HTTP on http://%s/mcp (auth: %s)\n", *addr, auth)
	fmt.Fprintf(os.Stderr, "runvm: console on http://%s/ (login: %s)\n", *addr, consoleUser)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "runvm:", err)
		os.Exit(1)
	}
}

// limitsFromEnv reads the session guardrails. Idle reaping is on by default:
// a forgotten page or emulator should cost a timer, not a leak.
func limitsFromEnv() tools.Limits {
	return tools.Limits{
		MaxSessions: envInt("RUNVM_MAX_SESSIONS", 8),
		IdleTimeout: envDuration("RUNVM_IDLE_TIMEOUT", 30*time.Minute),
	}
}

// resourcesFromEnv builds the writable resource store behind runvm://files/.
func resourcesFromEnv() (*resources.Store, error) {
	max, err := resources.ParseSize(envOr("RUNVM_RESOURCES_MAX_SIZE", "64MiB"))
	if err != nil {
		return nil, fmt.Errorf("bad RUNVM_RESOURCES_MAX_SIZE: %w", err)
	}
	return resources.New(envOr("RUNVM_RESOURCES_DIR", resources.DefaultDir), max)
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// randomHex returns n random bytes as hex (2n characters). It is the console
// password generator when RUNVM_CONSOLE_PASSWORD is unset.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand is not expected to fail
	}
	return hex.EncodeToString(b)
}

// openPersistence attaches the on-disk state (session ledger + browser
// profiles) to the set. A failure is not fatal — the server still serves, and
// runvm_status reports why persistence is off — because a read-only data
// directory must not stop the MCP endpoint from working.
func openPersistence(set *tools.Set) {
	store, err := persist.FromEnv()
	set.Persist, set.PersistErr = store, err
	if err != nil {
		fmt.Fprintln(os.Stderr, "runvm: persistence disabled:", err)
		return
	}
	// Move the id counters past the recorded ids, so a session opened after a
	// restart cannot collide with one the ledger still names. Nothing is
	// reopened: runvm_status shows what could come back.
	set.Restore()
}

func usage() {
	fmt.Fprint(os.Stderr, `runvm — test environments as MCP tools

usage:
  runvm [mcp]           serve the MCP server on stdin/stdout
  runvm http [flags]    serve the same tools over MCP Streamable HTTP
  runvm doctor          probe this machine's backends
  runvm selftest        open a page, screenshot it, and check it painted
  runvm version         print the version
  runvm web-host <url>  internal: host one page (spawned by h5_open)

environment:
  RUNVM_HTTP_ADDR   default listen address for runvm http (127.0.0.1:8765)
  RUNVM_MAX_SESSIONS cap on live sessions together (default 8, 0 = unlimited)
  RUNVM_IDLE_TIMEOUT close sessions unused this long, e.g. 30m (default 30m, 0 = off)
  RUNVM_TOKEN      bearer token required by runvm http
  RUNVM_CONSOLE_USER      console login username (default admin)
  RUNVM_CONSOLE_PASSWORD  console login password (generated and printed if unset)
  RUNVM_ASSETS     path to golem's bionic sysroot (assets/android/sdk23)
  RUNVM_ENGINE   default CPU backend, e.g. unicorn
  RUNVM_RESOURCES_DIR   writable resource root (default ./runvm-resources)
  RUNVM_RESOURCES_MAX_SIZE  per-file upload/write cap, e.g. 64MiB (default 64MiB)
  RUNVM_DATA_DIR   session ledger + browser profiles (default /work/runvm, else <config>/runvm)
  RUNVM_H5_PERSIST turn on a default browser profile for ephemeral:false opens (default off)

The server speaks JSON-RPC 2.0 over stdio. Point an MCP client at the binary:

  {"mcpServers": {"runvm": {"command": "runvm", "args": []}}}
`)
}
