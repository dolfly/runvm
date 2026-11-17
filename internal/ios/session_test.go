package ios

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/ios/iosprobe"
)

// probePath writes the embedded Mach-O fixture and returns its path.
func probePath(t *testing.T) string {
	t.Helper()
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		t.Fatalf("write probe: %v", err)
	}
	t.Cleanup(cleanup)
	return path
}

// engineAvailable asks golem for the engine it would build rather than
// guessing from build tags: the unicorn tag only compiles the backend in, it
// does not put libunicorn on the loader's path.
func engineAvailable(t *testing.T) bool {
	t.Helper()
	path, cleanup, err := iosprobe.Write()
	if err != nil {
		return false
	}
	defer cleanup()
	sess, err := Open(path, OpenOptions{Pool: 1})
	if err != nil {
		return false
	}
	_ = sess.Close()
	return true
}

func TestIsMachO(t *testing.T) {
	dir := t.TempDir()
	elfish := filepath.Join(dir, "not-a-dylib.bin")
	if err := os.WriteFile(elfish, []byte{0x7f, 'E', 'L', 'F', 1, 2, 3, 4}, 0o644); err != nil {
		t.Fatal(err)
	}
	if IsMachO(probePath(t)) != true {
		t.Error("Mach-O fixture must be detected")
	}
	if IsMachO(elfish) {
		t.Error("an ELF header must not be reported as Mach-O")
	}
	if IsMachO(filepath.Join(dir, "missing")) {
		t.Error("a missing file is not Mach-O")
	}
}

func TestDescribeAndExports(t *testing.T) {
	path := probePath(t)
	info, err := Describe(path)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if info.Bytes == 0 || !strings.Contains(strings.ToLower(info.Arch), "arm64") {
		t.Errorf("unexpected info: %+v", info)
	}
	names, err := Exports(path)
	if err != nil {
		t.Fatalf("exports: %v", err)
	}
	if len(names) == 0 || names[0] != "add" {
		t.Errorf("exports = %v, want the first entry to be add", names)
	}
}

func TestParseArgs(t *testing.T) {
	got, err := ParseArgs([]any{float64(3), "hi", map[string]any{"buffer": float64(8)}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 3 || got[0].Kind != ArgWord || got[0].Word != 3 ||
		got[1].Kind != ArgString || got[1].Str != "hi" ||
		got[2].Kind != ArgBuffer || got[2].Size != 8 {
		t.Errorf("parsed args = %+v", got)
	}

	for name, in := range map[string][]any{
		"negative":        {float64(-1)},
		"fractional":      {1.5},
		"no buffer field": {map[string]any{"size": float64(4)}},
		"zero buffer":     {map[string]any{"buffer": float64(0)}},
		"bool":            {true},
	} {
		if _, err := ParseArgs(in); err == nil {
			t.Errorf("%s: want an error, got none", name)
		}
	}
}

func TestOpenRequiresMachO(t *testing.T) {
	if _, err := Open("", OpenOptions{}); err == nil {
		t.Error("empty path must fail: there is no bootable empty Darwin image")
	}
	dir := t.TempDir()
	notMachO := filepath.Join(dir, "libfake.so")
	if err := os.WriteFile(notMachO, []byte{0x7f, 'E', 'L', 'F'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(notMachO, OpenOptions{}); err == nil {
		t.Error("an ELF file must be rejected by ios.Open")
	}
	if _, err := Open(filepath.Join(dir, "nope"), OpenOptions{}); err == nil {
		t.Error("a missing file must fail")
	}
}

// TestCallAdd is the contract the doctor probe also checks: a pure-compute
// Mach-O export runs through the Darwin personality with no bionic involved.
// It needs a CPU engine, so it skips when none is compiled/loadable.
func TestCallAdd(t *testing.T) {
	if !engineAvailable(t) {
		t.Skip("no CPU engine: build with -tags unicorn and point GOLEM_UNICORN at libunicorn")
	}
	path := probePath(t)
	sess, err := Open(path, OpenOptions{Pool: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	res, err := sess.Call(context.Background(), "add", []Arg{
		{Kind: ArgWord, Word: 3},
		{Kind: ArgWord, Word: 4},
	})
	if err != nil {
		t.Fatalf("call add: %v", err)
	}
	if res.Value != 7 {
		t.Errorf("add(3,4) = %d (%s), want 7", res.Value, Hex(res.Value))
	}

	if _, err := sess.Call(context.Background(), "", nil); err == nil {
		t.Error("empty symbol must fail")
	}
	if _, err := sess.Call(context.Background(), "no_such_symbol", nil); err == nil {
		t.Error("an unknown symbol must fail, not return 0")
	}
}

// TestImportsAreReported is the honest half of the contract, on a dylib that
// actually calls libSystem. The previous version only asserted that the slice
// was non-nil, which a fixture importing nothing satisfied — so the test
// could not fail whatever the import table said.
func TestImportsAreReported(t *testing.T) {
	imports, err := Imports(probePath(t))
	if err != nil {
		t.Fatalf("imports: %v", err)
	}
	if len(imports) == 0 {
		t.Fatalf("the probe imports nothing: it is supposed to call %s", iosprobe.SystemLib)
	}
	for _, want := range iosprobe.LibcImports {
		if !slices.Contains(imports, want) {
			t.Errorf("imports = %v, missing %q", imports, want)
		}
	}
}

// TestCallImportedSymbolIsExplicit pins the split the documentation promises:
// calling an imported symbol directly is an error that says so, while the same
// call made from inside the dylib is not an error at all — it reads golem's
// optimistic stub, which returns 0. Needs a CPU engine, so it skips without
// one.
func TestCallImportedSymbolIsExplicit(t *testing.T) {
	if !engineAvailable(t) {
		t.Skip("no CPU engine: build with -tags unicorn and point GOLEM_UNICORN at libunicorn")
	}
	sess, err := Open(probePath(t), OpenOptions{Pool: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	ctx := context.Background()
	if _, err := sess.Call(ctx, "malloc", nil); err == nil {
		t.Fatal("calling an imported symbol must fail, not return 0")
	} else if !strings.Contains(err.Error(), "imported by the dylib") {
		t.Errorf("error = %q, want it to name the symbol as imported", err)
	}

	// The same call from inside the dylib is the documented optimistic path:
	// golem binds the import to a stub that returns 0, and runvm does not turn
	// that into an error.
	res, err := sess.Call(ctx, "slen", []Arg{{Kind: ArgString, Str: "hello"}})
	if err != nil {
		t.Fatalf("slen: %v", err)
	}
	if res.Value != 0 {
		t.Errorf("slen = %d, want 0 from golem's stub", res.Value)
	}
	if res, err := sess.Call(ctx, "via_malloc", []Arg{{Kind: ArgWord, Word: 5}}); err != nil {
		t.Fatalf("via_malloc: %v", err)
	} else if res.Value != 5 {
		t.Errorf("via_malloc(5) = %d, want 5", res.Value)
	}
}

func TestSymbolFilter(t *testing.T) {
	in := []string{"Add", "sub", "add_impl"}
	if got := SymbolFilter(in, "ADD"); len(got) != 2 || got[0] != "Add" || got[1] != "add_impl" {
		t.Errorf("filter = %v", got)
	}
	if got := SymbolFilter(in, ""); len(got) != 3 {
		t.Errorf("empty filter must keep everything, got %v", got)
	}
}

func TestHexAndPrintable(t *testing.T) {
	if Hex(0) != "0x0" || Hex(255) != "0xff" {
		t.Errorf("Hex is wrong: %q %q", Hex(0), Hex(255))
	}
	if printable([]byte("ab\x00cd")) != "ab" {
		t.Errorf("printable must stop at the first NUL")
	}
	if hexBytes([]byte{0xde, 0xad}) != "dead" {
		t.Errorf("hexBytes = %q", hexBytes([]byte{0xde, 0xad}))
	}
}
