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
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/dolfly/runvm/internal/doctor"
	"github.com/dolfly/runvm/internal/h5"
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

	server, set := tools.New(version)
	// Sessions hold emulators and child processes; a clean shutdown must not
	// leave either behind.
	defer set.CloseAll()

	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "runvm:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `runvm — test environments as MCP tools

usage:
  runvm [mcp]           serve the MCP server on stdin/stdout
  runvm doctor          probe this machine's backends
  runvm version         print the version
  runvm web-host <url>  internal: host one page (spawned by h5_open)

environment:
  RUNVM_ASSETS   path to golem's bionic sysroot (assets/android/sdk23)
  RUNVM_ENGINE   default CPU backend, e.g. unicorn

The server speaks JSON-RPC 2.0 over stdio. Point an MCP client at the binary:

  {"mcpServers": {"runvm": {"command": "runvm", "args": []}}}
`)
}
