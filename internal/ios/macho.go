package ios

import (
	"bytes"
	"compress/zlib"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Mach-O import bookkeeping.
//
// debug/macho's ImportedSymbols reads the undefined entries of LC_SYMTAB. That
// is the view nm shows; it is not what the loader reads. dyld resolves imports
// through the bind streams of LC_DYLD_INFO(_ONLY) or the import table of
// LC_DYLD_CHAINED_FIXUPS, where the symbol strings may even be zlib-compressed.
// The two usually agree, but LC_SYMTAB can be absent or partial, and reading
// only it can therefore answer "no such symbol" for a name the loader would
// resolve — which reads like a typo in the caller's request rather than a
// boundary. Both sources are read here and merged.
//
// The formats are the ones in mach-o/fixup-chains.h and mach-o/loader.h; the
// payload offsets are absolute file offsets, so the file is read a second time
// rather than reconstructed from the parsed load commands.
const (
	lcReqDyld           = 0x8000_0000
	lcDyldInfo          = 0x22
	lcDyldChainedFixups = 0x34
	lcLoadDylib         = 0xc
	lcLoadWeakDylib     = 0x18
	lcReexportDylib     = 0x1f
	lcLoadUpwardDylib   = 0x23
)

// imageMeta is the import-side view of a Mach-O file: what it needs from
// outside and which images it links against.
type imageMeta struct {
	// Imports holds every name the image imports, leading underscore trimmed,
	// deduplicated and sorted.
	Imports []string
	// Libraries holds the install names of the dylibs it links against, in
	// load order. This is what LC_LOAD_DYLIB says, not a guess from the
	// imported symbol names.
	Libraries []string
}

func readImageMeta(path string) (imageMeta, error) {
	f, err := macho.Open(path)
	if err != nil {
		return imageMeta{}, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	var m imageMeta
	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimPrefix(name, "_")
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		m.Imports = append(m.Imports, name)
	}

	// Coarse source first: the undefined symbol table. It is what golem itself
	// considers an import, and it is the only source for a handful of older
	// linkers that emit nothing else.
	if ims, err := f.ImportedSymbols(); err == nil {
		for _, name := range ims {
			add(name)
		}
	}

	fh, err := os.Open(path)
	if err != nil {
		return imageMeta{}, fmt.Errorf("read %s: %w", path, err)
	}
	defer fh.Close()

	for _, l := range f.Loads {
		switch lc := l.(type) {
		case *macho.Dylib:
			switch binary.LittleEndian.Uint32(lc.LoadBytes) &^ lcReqDyld {
			case lcLoadDylib, lcLoadWeakDylib, lcReexportDylib, lcLoadUpwardDylib:
				m.Libraries = append(m.Libraries, lc.Name)
			}
		case macho.LoadBytes:
			switch binary.LittleEndian.Uint32(lc) &^ lcReqDyld {
			case lcDyldInfo:
				if len(lc) < 48 {
					continue
				}
				bo := binary.LittleEndian
				// dyld_info_command: ten uint32 pairs from offset 8; the bind
				// streams sit at 16..40.
				for _, s := range [...][2]uint32{
					{bo.Uint32(lc[16:]), bo.Uint32(lc[20:])}, // bind
					{bo.Uint32(lc[24:]), bo.Uint32(lc[28:])}, // weak bind
					{bo.Uint32(lc[32:]), bo.Uint32(lc[36:])}, // lazy bind
				} {
					raw, err := readAt(fh, s[0], s[1])
					if err != nil || len(raw) == 0 {
						continue
					}
					for _, name := range bindOpcodes(raw) {
						add(name)
					}
				}
			case lcDyldChainedFixups:
				if len(lc) < 16 {
					continue
				}
				raw, err := readAt(fh, binary.LittleEndian.Uint32(lc[8:]), binary.LittleEndian.Uint32(lc[12:]))
				if err != nil {
					continue
				}
				for _, name := range chainedImports(raw) {
					add(name)
				}
			}
		}
	}

	sort.Strings(m.Imports)
	return m, nil
}

func readAt(f *os.File, off, size uint32) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, int64(off)); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// bind opcodes (mach-o/loader.h), masked with BIND_OPCODE_MASK 0xf0; the low
// nibble is the immediate.
const (
	bindOpcodeDone                        = 0x00
	bindOpcodeSetDylibOrdinalIMMU         = 0x10
	bindOpcodeSetDylibOrdinalULEB         = 0x20
	bindOpcodeSetDylibSpecialIMMU         = 0x30
	bindOpcodeSetSymbolTrailingFlags      = 0x40
	bindOpcodeSetTypeIMMU                 = 0x50
	bindOpcodeSetAddendSLEB               = 0x60
	bindOpcodeSetSegmentAndOffsetULEB     = 0x70
	bindOpcodeAddAddrULEB                 = 0x80
	bindOpcodeDoBind                      = 0x90
	bindOpcodeDoBindAddAddrULEB           = 0xa0
	bindOpcodeDoBindAddAddrIMMScaled      = 0xb0
	bindOpcodeDoBindULEBTimesSkippingULEB = 0xc0
)

// bindOpcodes walks a bind stream and returns the symbol names it binds.
//
// An unrecognised opcode ends the walk instead of resynchronising: a bind
// stream is a linear script, and guessing past an unknown byte would invent
// imports rather than miss them.
func bindOpcodes(b []byte) []string {
	var (
		out []string
		sym string
	)
	for i := 0; i < len(b); {
		op, imm := b[i]&0xf0, b[i]&0x0f
		i++
		switch op {
		case bindOpcodeDone:
			return out
		case bindOpcodeSetSymbolTrailingFlags:
			end := bytes.IndexByte(b[i:], 0)
			if end < 0 {
				return out
			}
			sym = string(b[i : i+end])
			i += end + 1
		case bindOpcodeSetDylibOrdinalULEB, bindOpcodeSetSegmentAndOffsetULEB,
			bindOpcodeAddAddrULEB, bindOpcodeDoBindAddAddrULEB:
			_, n := readULEB(b[i:])
			i += n
		case bindOpcodeSetAddendSLEB:
			_, n := readULEB(b[i:]) // SLEB payload: the length is what matters here
			i += n
		case bindOpcodeDoBind:
			out = append(out, sym)
		case bindOpcodeDoBindAddAddrIMMScaled:
			out = append(out, sym)
		case bindOpcodeDoBindULEBTimesSkippingULEB:
			count, n1 := readULEB(b[i:])
			i += n1
			_, n2 := readULEB(b[i:])
			i += n2
			for k := uint64(0); k < count; k++ {
				out = append(out, sym)
			}
		case bindOpcodeSetDylibOrdinalIMMU, bindOpcodeSetDylibSpecialIMMU, bindOpcodeSetTypeIMMU:
			_ = imm // the immediate carries the value; nothing to consume
		default:
			return out
		}
	}
	return out
}

// readULEB returns a ULEB128 value and how many bytes it occupied. Truncated
// input yields the partial value and the remaining length, so the caller's
// cursor still lands on the end of the buffer.
func readULEB(b []byte) (uint64, int) {
	var (
		v uint64
		s uint
	)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < 0x80 {
			if i == 9 && c > 1 {
				return v, len(b) // overflow; stop where the stream stops
			}
			return v | uint64(c)<<s, i + 1
		}
		v |= uint64(c&0x7f) << s
		s += 7
		if i == 9 {
			break
		}
	}
	return v, len(b)
}

// chainedImports decodes the import table of an LC_DYLD_CHAINED_FIXUPS
// payload. Only the symbol names are needed here — the fixup chains themselves
// are golem's job — but the table is where an image says which external
// symbols it binds.
func chainedImports(b []byte) []string {
	if len(b) < 28 {
		return nil
	}
	bo := binary.LittleEndian
	importsOff, symbolsOff := bo.Uint32(b[8:]), bo.Uint32(b[12:])
	count, format, symbolsFormat := bo.Uint32(b[16:]), bo.Uint32(b[20:]), bo.Uint32(b[24:])

	symbols := b[min(uint64(symbolsOff), uint64(len(b))):]
	if symbolsFormat != 0 {
		// symbols_format 1 = the string blob is zlib compressed.
		zr, err := zlib.NewReader(bytes.NewReader(symbols))
		if err != nil {
			return nil
		}
		defer zr.Close()
		if out, err := io.ReadAll(zr); err == nil {
			symbols = out
		}
	}

	var out []string
	for i := uint32(0); i < count; i++ {
		var nameOff uint64
		switch format {
		case 1: // dyld_chained_import: ordinal:8 weak:1 name_offset:23
			e := entry(b, importsOff, i, 4)
			if e == nil {
				return out
			}
			nameOff = uint64(bo.Uint32(e)) >> 9
		case 2: // dyld_chained_import_addend: same bits, then int32 addend
			e := entry(b, importsOff, i, 8)
			if e == nil {
				return out
			}
			nameOff = uint64(bo.Uint32(e)) >> 9
		case 3: // dyld_chained_import_addend64: ordinal:16 weak:1 reserved:15 name_offset:32
			e := entry(b, importsOff, i, 16)
			if e == nil {
				return out
			}
			nameOff = uint64(bo.Uint64(e)) >> 32
		default:
			return out
		}
		if nameOff >= uint64(len(symbols)) {
			continue
		}
		out = append(out, cString(symbols[nameOff:]))
	}
	return out
}

func entry(b []byte, base, i uint32, size uint32) []byte {
	off := uint64(base) + uint64(i)*uint64(size)
	if off+uint64(size) > uint64(len(b)) {
		return nil
	}
	return b[off : off+uint64(size)]
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
