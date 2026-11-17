package ios

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// What a dylib needs from outside itself, and why this file exists.
//
// debug/macho's ImportedSymbols() reads LC_SYMTAB's undefined entries only.
// That is one of the three places an import can live, and on a real arm64
// dylib it is often the empty one: calls through __got go through the
// LC_DYLD_INFO bind opcode stream, and current toolchains emit
// LC_DYLD_CHAINED_FIXUPS instead. Reading the symtab alone therefore reports
// "no imports" for a library that plainly calls malloc — the failure this
// package had until the other two encodings were parsed as well.
//
// The dylib dependencies (LC_LOAD_DYLIB and friends) are read from the load
// commands for the same reason: an empty system_libs field says "this image
// needs nothing", which is a claim, not an absence of evidence.

// Load commands read here, from mach-o/loader.h. LC_REQ_DYLD is masked off
// before comparing, the way dyld itself does.
const (
	lcReqDyld           = 0x80000000
	lcDyldInfo          = 0x22 // LC_DYLD_INFO and LC_DYLD_INFO_ONLY
	lcDyldChainedFixups = 0x34 // LC_DYLD_CHAINED_FIXUPS
	lcLoadDylib         = 0xc
	lcLoadWeakDylib     = 0x18
	lcLazyLoadDylib     = 0x20
	lcReexportDylib     = 0x1f
	lcLoadUpwardDylib   = 0x23
)

// Classic dyld bind opcodes (mach-o/loader.h). Only the stream structure is
// decoded: an import is named by BIND_OPCODE_SET_SYMBOL_TRAILING_IMM, and
// where it would be written into the image is the loader's business.
const (
	bindOpDone                 = 0x00
	bindOpSetDylibOrdinalImm   = 0x10
	bindOpSetDylibOrdinalUleb  = 0x20
	bindOpSetDylibSpecialImm   = 0x30
	bindOpSetSymbolTrailingImm = 0x40
	bindOpSetTypeImm           = 0x50
	bindOpSetAddendSleb        = 0x60
	bindOpSetSegOffsetUleb     = 0x70
	bindOpAddAddrUleb          = 0x80
	bindOpDo                   = 0x90
	bindOpDoAddAddrUleb        = 0xa0
	bindOpDoAddAddrImmScaled   = 0xb0
	bindOpDoUlebTimesSkipUleb  = 0xc0
)

// Chained-fixups imports table formats (mach-o/fixup-chains.h).
const (
	chainedImport         = 1
	chainedImportAddend   = 2
	chainedImportAddend64 = 3
)

// Dependencies is what a dylib asks the outside world for.
type Dependencies struct {
	// Imports are the symbol names libSystem / Foundation / Objective-C would
	// have to provide. Sorted, unique, leading underscore stripped.
	Imports []string `json:"imports"`
	// SystemLibs are the libraries the image declares it links against
	// (LC_LOAD_DYLIB and its weak/reexport/lazy/upward variants).
	SystemLibs []string `json:"system_libs"`
}

// ReadDependencies parses a Mach-O file's imports and linked libraries
// without booting anything, so it also answers for a dylib golem would refuse
// to load.
func ReadDependencies(path string) (Dependencies, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Dependencies{}, fmt.Errorf("read dependencies: %w", err)
	}
	f, err := macho.NewFile(bytes.NewReader(raw))
	if err != nil {
		return Dependencies{}, fmt.Errorf("read dependencies: %w", err)
	}
	defer func() { _ = f.Close() }()

	imports := map[string]struct{}{}
	systemLibs := map[string]struct{}{}
	addImport := func(name string) {
		imports[strings.TrimPrefix(name, "_")] = struct{}{}
	}

	// 1. LC_SYMTAB undefined entries: the classic encoding.
	if f.Symtab != nil {
		for _, s := range f.Symtab.Syms {
			if s.Sect == 0 && s.Name != "" {
				addImport(s.Name)
			}
		}
	}

	// 2. Load commands: dyld bind opcode streams, the chained-fixups imports
	//    table, and the libraries the image links against. The table is walked
	//    by count and by sizeofcmds, never past its end: the bytes that follow
	//    are symbol and string data that also looks like load commands.
	loadOff := loadsOffset(raw)
	if loadOff < 0 {
		return Dependencies{}, fmt.Errorf("read dependencies: truncated Mach-O header")
	}
	ncmds := int(binary.LittleEndian.Uint32(raw[16:]))
	end := loadOff + int(binary.LittleEndian.Uint32(raw[20:]))
	if ncmds < 0 || end < loadOff || end > len(raw) {
		return Dependencies{}, fmt.Errorf("read dependencies: load commands overrun the file")
	}
	off := loadOff
	for i := 0; i < ncmds; i++ {
		if off+8 > end {
			return Dependencies{}, fmt.Errorf("read dependencies: load command %d runs past the command table", i)
		}
		cmd := binary.LittleEndian.Uint32(raw[off:])
		size := int(binary.LittleEndian.Uint32(raw[off+4:]))
		if size < 8 || off+size > end {
			return Dependencies{}, fmt.Errorf(
				"read dependencies: load command at %#x claims %d bytes, %d available", off, size, end-off)
		}
		switch cmd &^ lcReqDyld {
		case lcDyldInfo:
			names, err := dyldInfoImports(raw, off, size)
			if err != nil {
				return Dependencies{}, err
			}
			for _, n := range names {
				addImport(n)
			}
		case lcDyldChainedFixups:
			names, err := chainedImportNames(raw, off, size)
			if err != nil {
				return Dependencies{}, err
			}
			for _, n := range names {
				addImport(n)
			}
		case lcLoadDylib, lcLoadWeakDylib, lcLazyLoadDylib, lcReexportDylib, lcLoadUpwardDylib:
			name, err := dylibName(raw, off, size)
			if err != nil {
				return Dependencies{}, err
			}
			systemLibs[name] = struct{}{}
		}
		off += size
	}

	return Dependencies{Imports: sortedSet(imports), SystemLibs: sortedSet(systemLibs)}, nil
}

// linkHint explains the one loader rejection a caller can act on. golem
// refuses lazy and weak bind streams, which is exactly what a default
// `clang -arch arm64 -dynamiclib` link produces; the fix is a link flag, not
// a different tool.
func linkHint(err error) string {
	if err != nil && strings.Contains(err.Error(), "lazy/weak bind streams") {
		return " — golem rejects lazy binds; relink with -Wl,-bind_at_load (non-lazy) or -Wl,-fixup_chains (chained fixups)"
	}
	return ""
}

// Imports lists the symbols a dylib needs from outside itself. A non-empty
// list is the honest explanation for a call that cannot work.
func Imports(path string) ([]string, error) {
	deps, err := ReadDependencies(path)
	if err != nil {
		return nil, err
	}
	return deps.Imports, nil
}

// SystemLibs lists the libraries a dylib declares it links against.
func SystemLibs(path string) ([]string, error) {
	deps, err := ReadDependencies(path)
	if err != nil {
		return nil, err
	}
	return deps.SystemLibs, nil
}

// loadsOffset is where the load commands start: right after the mach header,
// 32 bytes for 64-bit and 28 for 32-bit.
func loadsOffset(raw []byte) int {
	if len(raw) < 4 {
		return -1
	}
	switch binary.LittleEndian.Uint32(raw) {
	case 0xfeedfacf: // MH_MAGIC_64
		return 32
	case 0xfeedface: // MH_MAGIC
		return 28
	default:
		// Not a Mach-O we can walk; macho.NewFile has already rejected the
		// byte-swapped and unknown magics.
		return -1
	}
}

// dyldInfoImports reads the bind, weak-bind and lazy-bind opcode streams of an
// LC_DYLD_INFO(_ONLY) command. Their offsets are absolute file offsets.
func dyldInfoImports(raw []byte, off, size int) ([]string, error) {
	const header = 8 + 40 // cmd/cmdsize plus ten uint32 offset/size pairs
	if size < header {
		return nil, fmt.Errorf("LC_DYLD_INFO at %#x is %d bytes, want at least %d", off, size, header)
	}
	u32 := func(rel int) uint32 { return binary.LittleEndian.Uint32(raw[off+rel:]) }
	var names []string
	for _, stream := range []struct{ off, size uint32 }{
		{u32(16), u32(20)}, // bind
		{u32(24), u32(28)}, // weak bind
		{u32(32), u32(36)}, // lazy bind
	} {
		if stream.size == 0 {
			continue
		}
		start, end := int(stream.off), int(stream.off)+int(stream.size)
		if start < 0 || end > len(raw) {
			return nil, fmt.Errorf("LC_DYLD_INFO at %#x: opcode stream %#x+%#x out of file bounds (%#x)",
				off, stream.off, stream.size, len(raw))
		}
		got, err := bindSymbolNames(raw[start:end])
		if err != nil {
			return nil, fmt.Errorf("LC_DYLD_INFO at %#x: %w", off, err)
		}
		names = append(names, got...)
	}
	return names, nil
}

// bindSymbolNames walks one classic bind opcode stream and returns the symbol
// names it binds, in order. Addresses, dylib ordinals and addends are skipped:
// only the names matter for reporting imports.
func bindSymbolNames(stream []byte) ([]string, error) {
	var names []string
	for i := 0; i < len(stream); {
		op, imm := stream[i]&0xf0, stream[i]&0x0f
		i++
		switch op {
		case bindOpDone:
			if imm == 0 {
				return names, nil
			}
			// imm > 0 is alignment padding, not a name.
		case bindOpSetDylibOrdinalImm, bindOpSetDylibSpecialImm, bindOpSetTypeImm:
			// the value is the immediate
		case bindOpSetDylibOrdinalUleb, bindOpSetSegOffsetUleb:
			_, i = uleb128(stream, i)
		case bindOpSetAddendSleb:
			_, i = sleb128(stream, i)
		case bindOpSetSymbolTrailingImm:
			start := i
			for i < len(stream) && stream[i] != 0 {
				i++
			}
			if i == len(stream) {
				return nil, errors.New("bind stream ends inside a symbol name")
			}
			name := strings.TrimPrefix(string(stream[start:i]), "_")
			i++
			if name == "" {
				return nil, errors.New("bind stream carries an empty symbol name")
			}
			names = append(names, name)
		case bindOpAddAddrUleb:
			_, i = uleb128(stream, i)
		case bindOpDo:
			// binds the pending symbol
		case bindOpDoAddAddrUleb:
			_, i = uleb128(stream, i)
		case bindOpDoAddAddrImmScaled:
			// the offset is the immediate
		case bindOpDoUlebTimesSkipUleb:
			_, i = uleb128(stream, i)
			_, i = uleb128(stream, i)
		default:
			return nil, fmt.Errorf("unsupported bind opcode %#x", op)
		}
	}
	return names, nil
}

// chainedImportNames reads the imports table of an LC_DYLD_CHAINED_FIXUPS
// payload. The table is a flat array of ordinal/weak/name_offset records
// (4, 8 or 16 bytes depending on the format) whose name offsets index into
// the string pool that follows it.
func chainedImportNames(raw []byte, off, size int) ([]string, error) {
	const header = 28 // dyld_chained_fixups_header
	if size < 16 {
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: command is %d bytes, want at least 16", off, size)
	}
	// The payload is not inside the load command: dataoff/datasize point at it.
	dataoff := int(binary.LittleEndian.Uint32(raw[off+8:]))
	datasize := int(binary.LittleEndian.Uint32(raw[off+12:]))
	if dataoff < 0 || datasize < header || dataoff > len(raw) || datasize > len(raw)-dataoff {
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: payload is %d bytes at %#x, want at least %d within the file",
			off, datasize, dataoff, header)
	}
	payload := raw[dataoff : dataoff+datasize]
	u32 := func(rel int) uint32 { return binary.LittleEndian.Uint32(payload[rel:]) }
	if version := u32(0); version != 0 {
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: unsupported fixups version %d (only 0)", off, version)
	}
	importsOff, symbolsOff := u32(8), u32(12)
	count, format, symbolsFormat := u32(16), u32(20), u32(24)
	if symbolsFormat != 0 {
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: compressed symbol strings (format %d) are unsupported",
			off, symbolsFormat)
	}
	var entry int
	switch format {
	case chainedImport:
		entry = 4
	case chainedImportAddend:
		entry = 8
	case chainedImportAddend64:
		entry = 16
	default:
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: unsupported imports format %d (want 1, 2 or 3)",
			off, format)
	}
	if count == 0 {
		return nil, nil
	}
	if uint64(importsOff)+uint64(count)*uint64(entry) > uint64(len(payload)) {
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: imports table %#x+%d*%d out of payload bounds (%#x)",
			off, importsOff, count, entry, len(payload))
	}
	if uint64(symbolsOff) >= uint64(len(payload)) {
		return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: symbol pool at %#x out of payload bounds (%#x)",
			off, symbolsOff, len(payload))
	}
	names := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		var nameOff uint32
		if format == chainedImportAddend64 {
			// lib_ordinal:16, weak_import:1, name_offset:47
			nameOff = symbolsOff + uint32(binary.LittleEndian.Uint64(payload[int(importsOff)+int(i)*entry:])>>17)
		} else {
			// lib_ordinal:8, weak_import:1, name_offset:23
			nameOff = symbolsOff + binary.LittleEndian.Uint32(payload[int(importsOff)+int(i)*entry:])>>9
		}
		if uint64(nameOff) >= uint64(len(payload)) {
			return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: import %d names offset %#x outside the payload (%#x)",
				off, i, nameOff, len(payload))
		}
		name := strings.TrimPrefix(string(payload[nameOff:chainedCStringEnd(payload, nameOff)]), "_")
		if name == "" {
			return nil, fmt.Errorf("LC_DYLD_CHAINED_FIXUPS at %#x: import %d has an empty name", off, i)
		}
		names = append(names, name)
	}
	return names, nil
}

// chainedCStringEnd is the offset of the NUL that ends the string at start.
func chainedCStringEnd(payload []byte, start uint32) int {
	end := int(start)
	for end < len(payload) && payload[end] != 0 {
		end++
	}
	return end
}

// dylibName reads the lc_str name of a dylib load command.
func dylibName(raw []byte, off, size int) (string, error) {
	const header = 24 // cmd/cmdsize/lc_str/timestamp/current_version/compatibility_version
	if size < header {
		return "", fmt.Errorf("dylib load command at %#x is %d bytes, want at least %d", off, size, header)
	}
	nameOff := int(binary.LittleEndian.Uint32(raw[off+8:]))
	if nameOff < header || nameOff >= size {
		return "", fmt.Errorf("dylib name offset %d outside the %d byte load command at %#x", nameOff, size, off)
	}
	start := off + nameOff
	end := start
	for end < off+size && raw[end] != 0 {
		end++
	}
	name := string(raw[start:end])
	if name == "" {
		return "", fmt.Errorf("dylib load command at %#x has an empty name", off)
	}
	return name, nil
}

// uleb128 decodes an ULEB128 at stream[i]; a truncated tail decodes as
// whatever bits were present, the same leniency dyld's own decoder has.
func uleb128(stream []byte, i int) (uint64, int) {
	var v uint64
	var shift uint
	for i < len(stream) {
		b := stream[i]
		i++
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
	}
	return v, i
}

// sleb128 decodes an SLEB128 at stream[i].
func sleb128(stream []byte, i int) (int64, int) {
	var v int64
	var shift uint
	var b byte
	for i < len(stream) {
		b = stream[i]
		i++
		v |= int64(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	if shift < 64 && b&0x40 != 0 {
		v |= -1 << shift
	}
	return v, i
}

// sortedSet returns the set's keys in order; an empty set yields an empty
// slice, never nil, so JSON shows [] rather than null.
func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
