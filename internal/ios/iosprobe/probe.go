// Package iosprobe owns the tiny Mach-O arm64 dylib runvm uses to prove the
// iOS path works on this machine.
//
// A probe has to be a real dylib that boots through golem's Darwin
// personality; the only honest check is "call it and see what comes back".
// Building one needs a macOS toolchain and arm64 targeting, which CI's Linux
// and Windows runners do not have, so the dylib is a committed test fixture:
// every platform can run the same probe, and doctor/selftest report whether
// it produced the value it must.
//
// The fixture deliberately imports one symbol (strlen) and defines one
// function that calls it (length). That is what makes the backend's boundary
// observable: add runs, length returns the optimistic stub's 0. A probe with
// only pure-compute functions would pass while proving nothing about the
// honest-failure story the tool descriptions promise.
package iosprobe

import (
	"debug/macho"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Fixture is the prebuilt probe dylib, compiled from FixtureSource below.
//
//go:embed probe_darwin_arm64.dylib
var Fixture []byte

// FixtureSource is the C the dylib was built from; kept here so the blob is
// reviewable and reproducible:
//
//	#include <string.h>
//
//	long add(long a, long b) { return a + b; }
//
//	// calls strlen, which this dylib imports: runvm's stub answers 0
//	long length(const char *s) { return (long)strlen(s); }
//
// Build (macOS with Xcode's command line tools; -bind_at_load is required,
// because runvm does not emulate dyld's lazy stub binder):
//
//	clang -arch arm64 -target arm64-apple-macos11 -dynamiclib -O2 \
//	      -Wl,-bind_at_load -o probe_darwin_arm64.dylib probe.c
const FixtureSource = "#include <string.h>\n" +
	"\n" +
	"long add(long a, long b) { return a + b; }\n" +
	"\n" +
	"/* calls strlen, which this dylib imports: the boundary the iOS backend\n" +
	" * cannot cross. golem's stub answers 0, so this returns 0, not 4. */\n" +
	"long length(const char *s) { return (long)strlen(s); }\n"

// FixtureImport is the symbol the fixture imports and cannot have resolved.
// Calling it directly must report "imported but has no implementation".
const FixtureImport = "strlen"

// Write materializes the fixture in a fresh temp directory, returning its
// path and a cleanup func. A caller that only needs the bytes should use
// Fixture directly.
func Write() (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "runvm-ios-probe-")
	if err != nil {
		return "", func() {}, err
	}
	path = filepath.Join(dir, "probe_darwin_arm64.dylib")
	if err := os.WriteFile(path, Fixture, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, err
	}
	return path, func() { _ = os.RemoveAll(dir) }, nil
}

// Probe is what a machine must produce to prove the iOS path is live: the
// return value of add(3, 4).
const Probe = 7

// Describe reports the fixture's identity from its header, so a failure can
// say whether the bytes are what they claim to be.
func Describe(path string) string {
	f, err := macho.Open(path)
	if err != nil {
		return fmt.Sprintf("not a readable Mach-O: %v", err)
	}
	defer f.Close()
	return fmt.Sprintf("Mach-O %s %s", f.Type, f.Cpu)
}
