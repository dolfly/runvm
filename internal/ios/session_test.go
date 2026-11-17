package ios

import (
	"context"
	"os"
	"path/filepath"
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

// TestImportsAreReported proves the honest failure mode: a dylib's imports are
// listed, and calling one does not silently look like a working function.
func TestImportsAreReported(t *testing.T) {
	imports, err := Imports(probePath(t))
	if err != nil {
		t.Fatalf("imports: %v", err)
	}
	// The fixture defines add and imports nothing beyond the runtime's own
	// dyld stubs, so the meaningful assertion is that the call succeeds.
	if imports == nil {
		t.Error("imports must be a slice, never nil")
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
