package ios

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// nlist is one 64-bit symbol table entry, reduced to what the fixture needs.
type nlist struct {
	name  string
	typ   uint8
	sect  uint8
	value uint64
}

// machoSpec is the shape buildMachO assembles.
type machoSpec struct {
	syms    []nlist
	bind    []byte   // LC_DYLD_INFO bind opcode stream; nil for no command
	lazy    []byte   // lazy bind stream, same command
	chained []byte   // LC_DYLD_CHAINED_FIXUPS payload
	dylibs  []string // LC_LOAD_DYLIB names
}

// buildMachO assembles a minimal but well-formed arm64 dylib.
//
// Hand-rolling the file is the point: a fixture produced by a real toolchain
// always carries LC_SYMTAB undefined entries as well, so it cannot tell
// "parsed the symbol table" apart from "parsed the bind stream". These tests
// put the imports in exactly one place and see whether runvm finds them.
func buildMachO(t *testing.T, spec machoSpec) []byte {
	t.Helper()
	le := binary.LittleEndian
	pad := func(n int) int { return (n + 7) &^ 7 }

	strtab := []byte{0}
	nameOff := make([]uint32, len(spec.syms))
	for i, s := range spec.syms {
		nameOff[i] = uint32(len(strtab))
		strtab = append(strtab, s.name...)
		strtab = append(strtab, 0)
	}
	var syms []byte
	for i, s := range spec.syms {
		var e [16]byte
		le.PutUint32(e[0:], nameOff[i])
		e[4], e[5] = s.typ, s.sect
		le.PutUint64(e[8:], s.value)
		syms = append(syms, e[:]...)
	}

	var cmds []byte
	ncmds := 0
	// addCmd appends a zeroed load command and returns its offset in cmds.
	addCmd := func(size int) int {
		at := len(cmds)
		cmds = append(cmds, make([]byte, size)...)
		ncmds++
		return at
	}
	put := func(at, rel int, v uint32) { le.PutUint32(cmds[at+rel:], v) }

	for _, lib := range spec.dylibs {
		size := pad(24 + len(lib) + 1)
		at := addCmd(size)
		put(at, 0, 0xc) // LC_LOAD_DYLIB
		put(at, 4, uint32(size))
		put(at, 8, 24) // lc_str offset, relative to the command
		copy(cmds[at+24:], lib)
	}

	symtab := addCmd(24)
	put(symtab, 0, 0x2) // LC_SYMTAB
	put(symtab, 4, 24)
	put(symtab, 12, uint32(len(spec.syms)))

	dyldInfo := -1
	if spec.bind != nil || spec.lazy != nil {
		dyldInfo = addCmd(48)
		put(dyldInfo, 0, 0x80000022) // LC_DYLD_INFO_ONLY
		put(dyldInfo, 4, 48)
	}

	chainedCmd := -1
	if spec.chained != nil {
		chainedCmd = addCmd(16)
		put(chainedCmd, 0, 0x80000034) // LC_DYLD_CHAINED_FIXUPS
		put(chainedCmd, 4, 16)
	}

	data := pad(32 + len(cmds))
	bindOff := data + len(strtab) + len(syms)
	lazyOff := bindOff + len(spec.bind)
	chainedOff := lazyOff + len(spec.lazy)

	put(symtab, 8, uint32(data+len(strtab))) // symoff
	put(symtab, 16, uint32(data))            // stroff
	put(symtab, 20, uint32(len(strtab)))     // strsize
	if dyldInfo >= 0 {
		put(dyldInfo, 16, uint32(bindOff))
		put(dyldInfo, 20, uint32(len(spec.bind)))
		put(dyldInfo, 32, uint32(lazyOff))
		put(dyldInfo, 36, uint32(len(spec.lazy)))
	}
	if chainedCmd >= 0 {
		put(chainedCmd, 8, uint32(chainedOff))
		put(chainedCmd, 12, uint32(len(spec.chained)))
	}

	out := make([]byte, 32, data+len(strtab)+len(syms)+len(spec.bind)+len(spec.lazy)+len(spec.chained))
	le.PutUint32(out[0:], 0xfeedfacf)     // MH_MAGIC_64
	le.PutUint32(out[4:], 0x0100000c)     // CPU_TYPE_ARM64
	le.PutUint32(out[12:], 6)             // MH_DYLIB
	le.PutUint32(out[16:], uint32(ncmds)) // ncmds
	le.PutUint32(out[20:], uint32(len(cmds)))
	out = append(out, cmds...)
	out = append(out, make([]byte, data-len(out))...)
	out = append(out, strtab...)
	out = append(out, syms...)
	out = append(out, spec.bind...)
	out = append(out, spec.lazy...)
	out = append(out, spec.chained...)
	return out
}

// bindStream encodes one classic bind stream that binds names to dylib 1.
func bindStream(names ...string) []byte {
	var s []byte
	for _, n := range names {
		s = append(s, 0x11, 0x01) // BIND_OPCODE_SET_DYLIB_ORDINAL_IMM 1
		s = append(s, 0x40|byte(len(n)+2))
		s = append(s, '_')
		s = append(s, n...)
		s = append(s, 0)
		s = append(s, 0x90) // BIND_OPCODE_DO
	}
	return append(s, 0x00) // BIND_OPCODE_DONE
}

// lazyStream is the same names with no ordinal prefix, the shape a lazy bind
// stream has.
func lazyStream(names ...string) []byte {
	var s []byte
	for _, n := range names {
		s = append(s, 0x40|byte(len(n)+2), '_')
		s = append(s, n...)
		s = append(s, 0, 0x90)
	}
	return append(s, 0x00)
}

// chainedFixups builds an LC_DYLD_CHAINED_FIXUPS payload whose imports table
// names the given symbols. Only the imports table and the string pool are
// real; the starts array is empty, which is enough for reporting imports.
func chainedFixups(t *testing.T, names ...string) []byte {
	t.Helper()
	le := binary.LittleEndian
	pool := []byte{0}
	offsets := make([]uint32, len(names))
	for i, n := range names {
		offsets[i] = uint32(len(pool))
		pool = append(pool, n...)
		pool = append(pool, 0)
	}
	const header = 28
	importsOff := header
	symbolsOff := importsOff + 4*len(names)
	payload := make([]byte, symbolsOff+len(pool))
	le.PutUint32(payload[0:], 0)                  // fixups_version
	le.PutUint32(payload[4:], uint32(symbolsOff)) // starts_offset
	le.PutUint32(payload[8:], uint32(importsOff))
	le.PutUint32(payload[12:], uint32(symbolsOff))
	le.PutUint32(payload[16:], uint32(len(names)))
	le.PutUint32(payload[20:], 1) // DYLD_CHAINED_IMPORT
	le.PutUint32(payload[24:], 0) // uncompressed
	for i, off := range offsets {
		// lib_ordinal 1 (libSystem), not weak, name offset in the pool.
		le.PutUint32(payload[importsOff+4*i:], off<<9|1)
	}
	copy(payload[symbolsOff:], pool)
	return payload
}

// readDeps writes raw to a temp file and parses it, failing on any error.
func readDeps(t *testing.T, raw []byte) Dependencies {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.dylib")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ReadDependencies(path)
	if err != nil {
		t.Fatalf("ReadDependencies: %v", err)
	}
	return deps
}

func TestReadDependenciesBindStream(t *testing.T) {
	// The symbol table knows _add and nothing else; the imports live in the
	// bind stream, which is where debug/macho's ImportedSymbols() never looks.
	deps := readDeps(t, buildMachO(t, machoSpec{
		syms:   []nlist{{name: "_add", typ: 0x0f, sect: 1, value: 0x1000}},
		bind:   bindStream("malloc", "memcpy"),
		dylibs: []string{"/usr/lib/libSystem.B.dylib"},
	}))
	if want := []string{"malloc", "memcpy"}; !reflect.DeepEqual(deps.Imports, want) {
		t.Errorf("imports = %v, want %v", deps.Imports, want)
	}
	if want := []string{"/usr/lib/libSystem.B.dylib"}; !reflect.DeepEqual(deps.SystemLibs, want) {
		t.Errorf("system_libs = %v, want %v", deps.SystemLibs, want)
	}
}

func TestReadDependenciesLazyBindStream(t *testing.T) {
	// A lazy bind stream names its symbols the same way; the only difference is
	// that the ordinal comes from the previous binding, not from an opcode.
	deps := readDeps(t, buildMachO(t, machoSpec{
		syms:   []nlist{{name: "_add", typ: 0x0f, sect: 1, value: 0x1000}},
		lazy:   lazyStream("strlen"),
		dylibs: []string{"/usr/lib/libSystem.B.dylib"},
	}))
	if want := []string{"strlen"}; !reflect.DeepEqual(deps.Imports, want) {
		t.Errorf("imports = %v, want %v", deps.Imports, want)
	}
}

func TestReadDependenciesChainedFixups(t *testing.T) {
	// Current toolchains emit LC_DYLD_CHAINED_FIXUPS instead of bind opcodes.
	// The symbol table below still has _add but no undefined entries at all.
	deps := readDeps(t, buildMachO(t, machoSpec{
		syms:    []nlist{{name: "_add", typ: 0x0f, sect: 1, value: 0x1000}},
		chained: chainedFixups(t, "malloc", "free", "strlen"),
		dylibs:  []string{"/usr/lib/libSystem.B.dylib"},
	}))
	want := []string{"free", "malloc", "strlen"}
	if !reflect.DeepEqual(deps.Imports, want) {
		t.Errorf("imports = %v, want %v", deps.Imports, want)
	}
}

func TestReadDependenciesSymbolTableOnly(t *testing.T) {
	// The classic encoding still has to work on its own.
	deps := readDeps(t, buildMachO(t, machoSpec{
		syms: []nlist{
			{name: "_add", typ: 0x0f, sect: 1, value: 0x1000},
			{name: "_strlen", typ: 0x01, sect: 0},
		},
		dylibs: []string{"/usr/lib/libSystem.B.dylib"},
	}))
	if want := []string{"strlen"}; !reflect.DeepEqual(deps.Imports, want) {
		t.Errorf("imports = %v, want %v", deps.Imports, want)
	}
}

func TestReadDependenciesMergesAndDeduplicates(t *testing.T) {
	// All three encodings at once, with malloc in both the symtab and the bind
	// stream: the union, sorted, without repeats.
	deps := readDeps(t, buildMachO(t, machoSpec{
		syms: []nlist{
			{name: "_add", typ: 0x0f, sect: 1, value: 0x1000},
			{name: "_malloc", typ: 0x01, sect: 0},
			{name: "_strlen", typ: 0x01, sect: 0},
		},
		bind:    bindStream("malloc", "memcpy"),
		lazy:    lazyStream("free"),
		chained: chainedFixups(t, "strlen", "objc_msgSend"),
		dylibs: []string{
			"/usr/lib/libSystem.B.dylib",
			"/usr/lib/libobjc.A.dylib",
		},
	}))
	want := []string{"free", "malloc", "memcpy", "objc_msgSend", "strlen"}
	if !reflect.DeepEqual(deps.Imports, want) {
		t.Errorf("imports = %v, want %v", deps.Imports, want)
	}
	if want := []string{"/usr/lib/libSystem.B.dylib", "/usr/lib/libobjc.A.dylib"}; !reflect.DeepEqual(deps.SystemLibs, want) {
		t.Errorf("system_libs = %v, want %v", deps.SystemLibs, want)
	}
}

// TestReadDependenciesErrors pins the failure mode of a malformed file: an
// error that names the encoding, never a panic and never a silent "no imports".
func TestReadDependenciesErrors(t *testing.T) {
	good := buildMachO(t, machoSpec{
		syms:    []nlist{{name: "_add", typ: 0x0f, sect: 1, value: 0x1000}},
		bind:    bindStream("malloc"),
		chained: chainedFixups(t, "free"),
		dylibs:  []string{"/usr/lib/libSystem.B.dylib"},
	})
	chainedAt := len(good) - len(chainedFixups(t, "free"))

	cases := []struct {
		name string
		raw  []byte
		want string
	}{
		{"unsupported bind opcode", withByte(good, bindOpcodeAt(t, good), 0xd0), "bind opcode"},
		{"unknown fixups version", withUint32(good, chainedAt, 1), "fixups version"},
		{"unknown imports format", withUint32(good, chainedAt+20, 9), "imports format"},
		{"compressed symbol pool", withUint32(good, chainedAt+24, 1), "compressed"},
		{"imports table out of bounds", withUint32(good, chainedAt+8, 1<<20), "out of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.dylib")
			if err := os.WriteFile(path, tc.raw, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := ReadDependencies(path)
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// bindOpcodeAt finds the BIND_OPCODE_SET_SYMBOL_TRAILING_IMM of the synthetic
// bind stream so a test can corrupt that opcode byte in place.
func bindOpcodeAt(t *testing.T, raw []byte) int {
	t.Helper()
	head := []byte{0x11, 0x01} // BIND_OPCODE_SET_DYLIB_ORDINAL_IMM, ordinal 1
	for i := 0; i+len(head) <= len(raw); i++ {
		if bytes.Equal(raw[i:i+len(head)], head) {
			return i + 2
		}
	}
	t.Fatal("no bind stream in the synthetic file")
	return -1
}

func withByte(raw []byte, at int, v byte) []byte {
	out := append([]byte(nil), raw...)
	out[at] = v
	return out
}

func withUint32(raw []byte, at int, v uint32) []byte {
	out := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint32(out[at:], v)
	return out
}

// TestLinkHint pins the hint attached to the one actionable loader rejection.
func TestLinkHint(t *testing.T) {
	got := linkHint(errors.New("loader: image carries lazy/weak bind streams (unsupported for arm64)"))
	if !strings.Contains(got, "-Wl,-bind_at_load") || !strings.Contains(got, "-Wl,-fixup_chains") {
		t.Errorf("linkHint = %q, want it to name both working link flags", got)
	}
	if got := linkHint(errors.New("no CPU backend compiled in")); got != "" {
		t.Errorf("linkHint = %q, want empty for an unrelated error", got)
	}
	if got := linkHint(nil); got != "" {
		t.Errorf("linkHint(nil) = %q, want empty", got)
	}
}
