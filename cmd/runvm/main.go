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

	"github.com/dolfly/runvm/internal/doctor"
	"github.com/dolfly/runvm/internal/h5"
	"github.com/dolfly/runvm/internal/mcp"
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
	set.StartReaper(ctx)
	// Sessions hold emulators and child processes; a clean shutdown must not
	// leave either behind.
	defer set.CloseAll()

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
	set.StartReaper(ctx)
	defer set.CloseAll()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(mcp.HTTPOptions{Token: *token}),
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
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "runvm:", err)
		os.Exit(1)
	}
}

// limitsFromEnv reads the session guardrails. Both default to off/8 so a
// forgotten session costs nothing surprising in a short run.
func limitsFromEnv() tools.Limits {
	return tools.Limits{
		MaxSessions: envInt("RUNVM_MAX_SESSIONS", 8),
		IdleTimeout: envDuration("RUNVM_IDLE_TIMEOUT", 0),
	}
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
  RUNVM_IDLE_TIMEOUT close sessions unused this long, e.g. 30m (default off)
  RUNVM_TOKEN      bearer token required by runvm http
  RUNVM_ASSETS     path to golem's bionic sysroot (assets/android/sdk23)
  RUNVM_ENGINE   default CPU backend, e.g. unicorn

The server speaks JSON-RPC 2.0 over stdio. Point an MCP client at the binary:

  {"mcpServers": {"runvm": {"command": "runvm", "args": []}}}
`)
}
