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
// The probe deliberately calls libSystem: add is pure computation, while slen
// and via_malloc make it a dylib with imports. A fixture that imported nothing
// is how the import table went unreported for so long — there was nothing to
// report, so every assertion about imports passed.
package iosprobe

import (
	"debug/macho"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Fixture is the prebuilt probe dylib, compiled from FixtureSource.
//
//go:embed probe_darwin_arm64.dylib
var Fixture []byte

// FixtureSource is the C the dylib was built from, embedded so the blob stays
// reviewable and reproducible. Build it (macOS with Xcode's command line
// tools) with:
//
//	clang -arch arm64 -target arm64-apple-macos11 -dynamiclib -O2 -fno-builtin \
//	      -Wl,-bind_at_load -o probe_darwin_arm64.dylib probe_darwin_arm64.c
//
// -fno-builtin keeps clang from inlining strlen/malloc, which would leave the
// import table empty again. -Wl,-bind_at_load matters for the same reason:
// the default link emits lazy bind opcodes, and golem refuses those with
// "carries lazy/weak bind streams (unsupported)". A bind-at-load dylib gets
// its imports resolved when the image is mapped, which is also when runvm can
// still see them.
//
//go:embed probe_darwin_arm64.c
var FixtureSource string

// Write materializes the fixture in a fresh temp directory, returning its
// path and a cleanup func. A caller that only needs the bytes should use
// Fixture directly.
func Write() (path string, cleanup func(), err error) {
	return write("probe_darwin_arm64.dylib", Fixture)
}

// Probe is what a machine must produce to prove the iOS path is live: the
// return value of add(3, 4).
const Probe = 7

// LibcImports are the libSystem symbols the fixture imports. They are the
// reason a "pure computation" fixture was not enough: with them present, the
// import table has something to report, and ios_call has something to refuse.
var LibcImports = []string{"free", "malloc", "strlen"}

// SystemLib is the library the fixture declares it links against
// (LC_LOAD_DYLIB). libSystem is a claim about the file, so it must be read
// from the file rather than assumed.
const SystemLib = "/usr/lib/libSystem.B.dylib"

func write(name string, data []byte) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "runvm-ios-probe-")
	if err != nil {
		return "", func() {}, err
	}
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, err
	}
	return path, func() { _ = os.RemoveAll(dir) }, nil
}

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
