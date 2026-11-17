package ios

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dolfly/runvm/internal/ios/iosprobe"
)

// The byte sequences below are captured from real clang output on macOS
// arm64. They are asserted verbatim because the parser's job is to agree with
// dyld's, not with a convenient reading of the file.

// bindOf builds a non-lazy bind stream that binds one symbol per DO_BIND.
func bindOf(names ...string) []byte {
	var b []byte
	b = append(b, 0x11) // BIND_OPCODE_SET_DYLIB_ORDINAL_IMM, ordinal 1
	for _, n := range names {
		b = append(b, 0x40) // SET_SYMBOL_TRAILING_FLAGS
		b = append(b, '_')
		b = append(b, n...)
		b = append(b, 0)
		b = append(b, 0x51)       // SET_TYPE_IMM, pointer
		b = append(b, 0x70, 0x00) // SET_SEGMENT_AND_OFFSET_ULEB, segment 1 offset 0
		b = append(b, 0x90)       // DO_BIND
	}
	b = append(b, 0x00) // DONE
	return b
}

func TestBindOpcodes(t *testing.T) {
	// Exactly the stream clang emitted for a dylib importing strlen:
	// 11 40 "_strlen\0" 51 71 00 90 00.
	real := []byte{0x11, 0x40, '_', 's', 't', 'r', 'l', 'e', 'n', 0x00, 0x51, 0x71, 0x00, 0x90, 0x00}
	// Names come back exactly as the stream stores them, underscore included;
	// readImageMeta trims it, the way golem does.
	if got := bindOpcodes(real); !slices.Equal(got, []string{"_strlen"}) {
		t.Errorf("bindOpcodes(real clang stream) = %v, want [_strlen]", got)
	}
	if got := bindOpcodes(bindOf("strlen", "memcpy")); !slices.Equal(got, []string{"_strlen", "_memcpy"}) {
		t.Errorf("two-symbol stream = %v", got)
	}
	// An unknown opcode ends the walk rather than inventing a name.
	if got := bindOpcodes([]byte{0xe0, 0x40, 'x', 0x00}); len(got) != 0 {
		t.Errorf("unknown opcode must stop the walk, got %v", got)
	}
	// A trailing cstring with no NUL must not panic or produce garbage.
	if got := bindOpcodes([]byte{0x40, 'x', 'y'}); len(got) != 0 {
		t.Errorf("unterminated symbol = %v, want nothing", got)
	}
}

func TestBindOpcodesULEBTimes(t *testing.T) {
	// DO_BIND_ULEB_TIMES_SKIPPING_ULEB repeats one symbol count times.
	stream := append(bindOf("strlen")[:0], 0x11, 0x40)
	stream = append(stream, "_strlen\x00"...)
	stream = append(stream, 0x51, 0x70, 0x00, 0xc2, 0x02, 0x00, 0x08, 0x00)
	got := bindOpcodes(stream)
	if !slices.Equal(got, []string{"_strlen", "_strlen"}) {
		t.Errorf("times-stream = %v, want [_strlen _strlen]", got)
	}
}

func TestReadULEB(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want uint64
		n    int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x7f}, 0x7f, 1},
		{[]byte{0x80, 0x01}, 0x80, 2},
		{[]byte{0xac, 0x02}, 0x2c + 0x100, 2},
	} {
		got, n := readULEB(tc.in)
		if got != tc.want || n != tc.n {
			t.Errorf("readULEB(% x) = (%d,%d), want (%d,%d)", tc.in, got, n, tc.want, tc.n)
		}
	}
	// Truncated input consumes what is there instead of reading past the end.
	if got, n := readULEB([]byte{0x80}); got != 0 || n != 1 {
		t.Errorf("readULEB(truncated) = (%d,%d)", got, n)
	}
}

// chainedBlob builds an LC_DYLD_CHAINED_FIXUPS payload with one import, using
// the layout from mach-o/fixup-chains.h.
func chainedBlob(t *testing.T, format uint32, entry []byte, symbols []byte) []byte {
	t.Helper()
	bo := binary.LittleEndian
	const importsOff = 28
	b := make([]byte, importsOff, importsOff+len(entry)+len(symbols))
	bo.PutUint32(b[0:], 0)                              // fixups_version
	bo.PutUint32(b[4:], 0)                              // starts_offset (unused here)
	bo.PutUint32(b[8:], importsOff)                     // imports_offset
	bo.PutUint32(b[12:], uint32(importsOff+len(entry))) // symbols_offset
	bo.PutUint32(b[16:], 1)                             // imports_count
	bo.PutUint32(b[20:], format)                        // imports_format
	bo.PutUint32(b[24:], 0)                             // symbols_format: uncompressed
	b = append(b, entry...)
	return append(b, symbols...)
}

func TestChainedImports(t *testing.T) {
	// dyld_chained_import: ordinal 1, weak 0, name_offset 1 (one leading NUL
	// in the string blob). This is the 01 02 00 00 ld64 emitted in a real
	// binary: 1 | 0<<8 | 1<<9.
	entry := []byte{0x01, 0x02, 0x00, 0x00}
	blob := chainedBlob(t, 1, entry, []byte{0x00, '_', 's', 't', 'r', 'l', 'e', 'n', 0x00})
	if got := chainedImports(blob); !slices.Equal(got, []string{"_strlen"}) {
		t.Errorf("format 1 imports = %v, want [_strlen]", got)
	}

	// dyld_chained_import_addend: same bitfield, then int32 addend.
	entry2 := make([]byte, 8)
	copy(entry2, entry)
	blob = chainedBlob(t, 2, entry2, []byte{0x00, '_', 'm', 'e', 'm', 'c', 'p', 'y', 0x00})
	if got := chainedImports(blob); !slices.Equal(got, []string{"_memcpy"}) {
		t.Errorf("format 2 imports = %v, want [_memcpy]", got)
	}

	// dyld_chained_import_addend64: ordinal:16, name_offset:32, then int64.
	entry3 := make([]byte, 16)
	binary.LittleEndian.PutUint64(entry3, 1|1<<32)
	blob = chainedBlob(t, 3, entry3, []byte{0x00, '_', 's', 't', 'r', 'l', 'e', 'n', 0x00})
	if got := chainedImports(blob); !slices.Equal(got, []string{"_strlen"}) {
		t.Errorf("format 3 imports = %v, want [_strlen]", got)
	}

	// Out-of-range name offsets are skipped, not read out of bounds.
	entry4 := []byte{0xff, 0xff, 0xff, 0x7f}
	blob = chainedBlob(t, 1, entry4, []byte("_strlen\x00"))
	if got := chainedImports(blob); len(got) != 0 {
		t.Errorf("out-of-range name offset = %v, want nothing", got)
	}
}

func TestChainedImportsZlib(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	// leading NUL: ld64 reserves offset 0, so the import sits at 1
	_, _ = zw.Write([]byte("\x00_strlen\x00"))
	_ = zw.Close()

	bo := binary.LittleEndian
	entry := []byte{0x01, 0x02, 0x00, 0x00}
	b := make([]byte, 28)
	bo.PutUint32(b[8:], 28)
	bo.PutUint32(b[12:], 32)
	bo.PutUint32(b[16:], 1)
	bo.PutUint32(b[20:], 1)
	bo.PutUint32(b[24:], 1) // symbols_format: zlib compressed
	b = append(b, entry...)
	b = append(b, z.Bytes()...)

	if got := chainedImports(b); !slices.Equal(got, []string{"_strlen"}) {
		t.Errorf("zlib-compressed symbols = %v, want [_strlen]", got)
	}
}

// TestFixtureImportsAndLibraries pins the fixture's identity: one import it
// cannot resolve, and the library it says it links against.
func TestFixtureImportsAndLibraries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.dylib")
	if err := os.WriteFile(path, iosprobe.Fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	imports, err := Imports(path)
	if err != nil {
		t.Fatalf("imports: %v", err)
	}
	if !slices.Contains(imports, "strlen") {
		t.Errorf("imports = %v, want strlen among them (the bind stream is the only source for it)", imports)
	}

	libs, err := Libraries(path)
	if err != nil {
		t.Fatalf("libraries: %v", err)
	}
	if !slices.Contains(libs, "/usr/lib/libSystem.B.dylib") {
		t.Errorf("libraries = %v, want libSystem.B.dylib from LC_LOAD_DYLIB", libs)
	}
}

// TestImportsRejectsNonMachO keeps the reader's failure mode obvious: an ELF
// is not a Darwin image, and the answer should be an error rather than an
// empty import list that reads like "self-contained".
func TestImportsRejectsNonMachO(t *testing.T) {
	dir := t.TempDir()
	elf := filepath.Join(dir, "libfake.so")
	if err := os.WriteFile(elf, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Imports(elf); err == nil {
		t.Error("Imports on an ELF must fail")
	}
	if _, err := Libraries(elf); err == nil {
		t.Error("Libraries on an ELF must fail")
	}
}

// patchBindOffset returns the fixture with its LC_DYLD_INFO(_ONLY) bind stream
// pointing past the end of the file. The bind stream is the fixture's only
// import source besides LC_SYMTAB, so a file like this is exactly where
// "the stream could not be read" must not come back as "the dylib imports
// nothing".
func patchBindOffset(t *testing.T, raw []byte) []byte {
	t.Helper()
	const lcDyldInfoOnly = 0x8000_0022
	out := append([]byte(nil), raw...)
	if len(out) < 32 {
		t.Fatalf("fixture is %d bytes, too short to hold a load command table", len(out))
	}
	ncmds := binary.LittleEndian.Uint32(out[16:])
	off := uint32(32)
	for i := uint32(0); i < ncmds; i++ {
		if int(off)+8 > len(out) {
			break
		}
		cmd := binary.LittleEndian.Uint32(out[off:])
		size := binary.LittleEndian.Uint32(out[off+4:])
		if cmd == lcDyldInfoOnly {
			if size < 24 {
				t.Fatalf("LC_DYLD_INFO_ONLY claims %d bytes, too short to patch", size)
			}
			binary.LittleEndian.PutUint32(out[off+16:], 0x00ff_ff00) // bind_off
			return out
		}
		off += size
	}
	t.Fatalf("fixture has no LC_DYLD_INFO_ONLY; the probe is built with -Wl,-bind_at_load")
	return nil
}

// TestImportsReportsUnreadableBindStream is the regression for the silent
// answer: on the previous code a bind stream pointing outside the file was
// skipped, and the caller got imports: [] — the same shape as a dylib that
// genuinely imports nothing. The reader must say it could not read the table.
func TestImportsReportsUnreadableBindStream(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.dylib")
	if err := os.WriteFile(broken, patchBindOffset(t, iosprobe.Fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Imports(broken); err == nil {
		t.Error("Imports on a dylib whose bind stream is past EOF must fail, not report no imports")
	} else if !strings.Contains(err.Error(), "bind stream") {
		t.Errorf("error = %v, want it to name the bind stream that could not be read", err)
	}

	// The same file must still parse when the stream is intact: the failure
	// above is about the read, not about the format.
	good := filepath.Join(dir, "good.dylib")
	if err := os.WriteFile(good, iosprobe.Fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	imports, err := Imports(good)
	if err != nil {
		t.Fatalf("imports on the intact fixture: %v", err)
	}
	if !slices.Contains(imports, iosprobe.FixtureImport) {
		t.Errorf("imports = %v, want %s", imports, iosprobe.FixtureImport)
	}
}
