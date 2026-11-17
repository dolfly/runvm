// Package ios loads Darwin/Mach-O shared libraries (arm64) and calls their
// exported symbols.
//
// It is deliberately a sibling of internal/android, not a mode of it: the
// container format decides the personality. A Mach-O image gets no bionic
// assets, no JNI table and no Java stubs — golem picks the Darwin personality
// from the magic itself (loader.Sniff), and runvm's job is to stop feeding it
// Android state.
//
// The honest scope is "pure computation": symbols defined by the dylib itself
// resolve and run. Anything imported from libSystem / Objective-C / Swift
// (malloc, memcpy, objc_msgSend, ...) has no guest implementation and no
// host replacement — Darwin's ReplaceFns lives behind golem's internal tree,
// so an external module cannot register one. Two different things follow, and
// both are true at once: calling such a name directly with ios_call fails with
// an explicit error, while the same call made from inside the dylib is not an
// error at all — the import reads as golem's optimistic stub, which returns 0.
// Imports() names the first case and ReadDependencies() lists the whole table;
// a dylib that links libSystem and "works" is usually in the second one.
package ios

import (
	"context"
	"debug/macho"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/isesword/golem/emulator"
)

// maxPool bounds the engine pool, same reasoning as android: each engine is a
// full guest process (address space + JIT buffer), so this is a memory
// decision before it is a concurrency one.
const maxPool = 16

// OpenOptions configures a session at creation.
type OpenOptions struct {
	Engine  string
	Pool    int   // engines to warm; 1 = one call at a time
	MaxUses int64 // recycle an engine after this many calls; 0 = never
}

// Session is one dylib plus a pool of emulators that have it loaded.
//
// golem's Emulator is not goroutine-safe, and the pool gives that invariant a
// home: an engine is lent to one caller at a time. Pool size 1 therefore means
// exactly "one call at a time", the same semantics a single mutex-guarded
// emulator would have.
type Session struct {
	path string
	cfg  emulator.Config
	opts OpenOptions
	pool *emulator.Pool[*emulator.Emulator]

	closeOnce sync.Once
	created   atomic.Int64
	lastUsed  atomic.Int64 // unix nanos; the reaper reads this
}

// Open boots a pool of emulators with the dylib loaded. A non-empty path is
// required: unlike the Android side there is no bootable "empty" Darwin image
// to probe, and an empty AssetRoot is the point — Darwin must never look for
// the bionic sysroot.
func Open(path string, opts OpenOptions) (*Session, error) {
	if path == "" {
		return nil, errors.New("ios_open: path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("ios_open: %q: %w", path, err)
	}
	if opts.Pool <= 0 {
		opts.Pool = 1
	}
	if opts.Pool > maxPool {
		return nil, fmt.Errorf("ios_open: pool %d exceeds the maximum of %d", opts.Pool, maxPool)
	}

	s := &Session{
		path: path,
		opts: opts,
		// No AssetRoot: the Darwin personality has no sysroot to map, and
		// leaving it empty is what keeps an iOS session off the bionic path.
		cfg: emulator.Config{
			SOPath:      path,
			Engine:      opts.Engine,
			ProcessName: "runvm",
		},
	}

	pool, err := emulator.NewPool[*emulator.Emulator](opts.Pool, opts.MaxUses,
		func() (*emulator.Emulator, error) {
			s.created.Add(1)
			emu, err := emulator.New(s.cfg)
			if err != nil {
				return nil, err
			}
			return emu, nil
		},
		func(emu *emulator.Emulator) { _ = emu.Close() },
	)
	if err != nil {
		return nil, fmt.Errorf("ios_open: %s: %w (run `runvm doctor` if this mentions the engine or libunicorn)%s",
			path, err, linkHint(err))
	}
	s.pool = pool
	s.touch()
	return s, nil
}

func (s *Session) touch() { s.lastUsed.Store(time.Now().UnixNano()) }

// Path is the loaded dylib.
func (s *Session) Path() string { return s.path }

// Engine reports the backend golem selected. It borrows an engine to ask,
// because the backend is only known once one is constructed.
func (s *Session) Engine() string {
	var engine string
	ctx := context.Background()
	if err := s.pool.Do(ctx, func(emu *emulator.Emulator) error {
		engine = emu.Engine()
		return nil
	}); err != nil {
		return "unknown"
	}
	return engine
}

// PoolSize is the configured engine count.
func (s *Session) PoolSize() int { return s.opts.Pool }

// EnginesCreated counts every engine ever constructed, including pool warmup
// and post-panic rebuilds. Growth past the configured size is the signal that
// something is recycling or crashing.
func (s *Session) EnginesCreated() int64 { return s.created.Load() }

// Stats reports pool occupancy, so a long test run can be watched instead of
// guessed at.
func (s *Session) Stats() emulator.PoolStats { return s.pool.Stats() }

// LastUsed is when this session was last touched, for the idle reaper.
func (s *Session) LastUsed() time.Time { return time.Unix(0, s.lastUsed.Load()) }

// Close releases every engine in the pool. Idempotent.
func (s *Session) Close() error {
	s.closeOnce.Do(func() { s.pool.CloseAll() })
	return nil
}

// Info is a cheap, non-booting description of a dylib.
type Info struct {
	Arch  string `json:"arch"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes"`
}

// Describe reports the container format without loading anything, so a caller
// can tell a Mach-O dylib from an ELF .so before it reaches android.Open.
func Describe(path string) (Info, error) {
	f, err := macho.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	st, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Arch:  f.Cpu.String(),
		Kind:  fmt.Sprintf("Mach-O %s %s", f.Type, "dylib"),
		Bytes: st.Size(),
	}, nil
}

// Exports lists the symbols a dylib exports, by reading the Mach-O symbol
// table directly instead of asking a live emulator, so it works before a
// session exists, without a CPU engine, and on a library that would not boot —
// the same reasoning as android.Exports.
//
// golem's Mach-O parser keeps defined, non-undefined, non-debug symbols and
// strips the leading underscore before publishing them, so this deliberately
// mirrors that rule rather than debug/macho's stab filtering: the names here
// are exactly the names ios_call accepts.
func Exports(path string) ([]string, error) {
	f, err := macho.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read exports: %w", err)
	}
	defer f.Close()
	if f.Symtab == nil {
		return nil, fmt.Errorf("read symbols: no symbol table (stripped?)")
	}
	syms := f.Symtab.Syms
	seen := map[string]struct{}{}
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		if s.Sect == 0 { // undefined: an import, not something to call
			continue
		}
		name := strings.TrimPrefix(s.Name, "_")
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// IsMachO reports whether path is a Mach-O object file. Only the header is
// read, so this is safe on any input and cheap enough to run per open: it is
// what decides whether android.Open hands the file to the Darwin personality.
func IsMachO(path string) bool {
	f, err := macho.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// ArgKind distinguishes the three things a call argument can be.
type ArgKind uint8

const (
	// ArgWord is a plain machine word (an integer parameter).
	ArgWord ArgKind = iota
	// ArgString is a NUL-terminated UTF-8 string copied into guest memory.
	ArgString
	// ArgBuffer is guest memory of a given size passed as a pointer, then
	// read back after the call — how an out-parameter becomes observable.
	ArgBuffer
)

// Arg is one call argument.
type Arg struct {
	Kind ArgKind
	Word uint64
	Str  string
	Size int
}

// ParseArgs accepts the JSON shape MCP clients send:
//
//	2                 -> word
//	"hello"           -> char* (copied, NUL-terminated)
//	{"buffer": 64}    -> pointer to 64 writable bytes, read back after the call
//
// Numbers must be non-negative integers; pass the two's-complement uint64 for
// a negative int32 parameter.
func ParseArgs(raw []any) ([]Arg, error) {
	out := make([]Arg, 0, len(raw))
	for i, v := range raw {
		switch x := v.(type) {
		case float64:
			if x < 0 {
				return nil, fmt.Errorf("argument %d: negative numbers are not supported, pass the two's-complement uint64", i)
			}
			if x != float64(uint64(x)) || x >= 1<<53 {
				return nil, fmt.Errorf("argument %d: %v is not a non-negative integer (use a string to pass a full 64-bit value)", i, x)
			}
			out = append(out, Arg{Kind: ArgWord, Word: uint64(x)})
		case string:
			out = append(out, Arg{Kind: ArgString, Str: x})
		case map[string]any:
			n, ok := x["buffer"]
			if !ok {
				return nil, fmt.Errorf(`argument %d: object needs a "buffer" size`, i)
			}
			f, ok := n.(float64)
			if !ok || f <= 0 || f != float64(int(f)) {
				return nil, fmt.Errorf(`argument %d: "buffer" must be a positive integer size`, i)
			}
			out = append(out, Arg{Kind: ArgBuffer, Size: int(f)})
		default:
			return nil, fmt.Errorf("argument %d: want number, string or {\"buffer\": n}, got %T", i, v)
		}
	}
	return out, nil
}

// Buffer is the contents of an ArgBuffer after the call.
type Buffer struct {
	Size  int    `json:"size"`
	Hex   string `json:"hex"`
	ASCII string `json:"ascii,omitempty"`
}

// Result is a completed call: the return value plus any out-buffers.
type Result struct {
	Value   uint64   `json:"value"`
	Buffers []Buffer `json:"buffers,omitempty"`
}

const maxBuffer = 1 << 20 // sanity cap: no test needs a 1 MiB out-parameter

// Call invokes an exported symbol on a pooled engine.
func (s *Session) Call(ctx context.Context, symbol string, args []Arg) (Result, error) {
	var res Result
	if symbol == "" {
		return res, errors.New("ios_call: empty symbol")
	}
	if len(args) > 8 {
		return res, fmt.Errorf("ios_call: %d arguments, at most 8 are supported", len(args))
	}

	s.touch()
	err := s.pool.Do(ctx, func(emu *emulator.Emulator) error {
		type buf struct {
			addr uint64
			size int
		}
		var buffers []buf
		values := make([]emulator.Value, 0, len(args))
		for _, a := range args {
			switch a.Kind {
			case ArgString:
				values = append(values, emulator.Ptr(emu.WriteCStringAlloc(a.Str)))
			case ArgBuffer:
				if a.Size > maxBuffer {
					return fmt.Errorf("ios_call: buffer of %d bytes exceeds the %d byte cap", a.Size, maxBuffer)
				}
				addr, err := emu.Malloc(uint64(a.Size))
				if err != nil {
					return fmt.Errorf("ios_call: allocate %d byte buffer: %w", a.Size, err)
				}
				values = append(values, emulator.Ptr(addr))
				buffers = append(buffers, buf{addr: addr, size: a.Size})
			default:
				values = append(values, emulator.Words(a.Word)...)
			}
		}

		v, err := emu.CallSymbol(symbol, values...)
		if err != nil {
			// A name the image imports but nothing defines is the expected
			// libSystem case, and "symbol not found" alone reads like a typo.
			if strings.Contains(err.Error(), "not found") {
				if imports, ierr := Imports(s.path); ierr == nil && slices.Contains(imports, symbol) {
					return fmt.Errorf("ios_call: %s is imported by the dylib but has no implementation "+
						"(runvm supplies no libSystem/ObjC symbols; the dylib's own calls to it "+
						"read a stub that returns 0, which is why they look like they work)", symbol)
				}
			}
			return fmt.Errorf("ios_call: %s: %w", symbol, err)
		}
		res.Value = v
		for _, b := range buffers {
			raw, err := emu.ReadBytes(b.addr, uint64(b.size))
			if err != nil {
				return fmt.Errorf("ios_call: read back buffer: %w", err)
			}
			res.Buffers = append(res.Buffers, Buffer{Size: b.size, Hex: hexBytes(raw), ASCII: printable(raw)})
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

func hexBytes(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0xf])
	}
	return string(out)
}

// printable keeps the readable characters of a buffer and drops the rest, so
// a returned char* shows up as text instead of a hex blob.
func printable(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		if c >= 0x20 && c < 0x7f {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Hex formats a guest word the way both humans and models expect to see one.
func Hex(v uint64) string { return "0x" + formatUint(v) }

func formatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = "0123456789abcdef"[v&0xf]
		v >>= 4
	}
	return string(buf[i:])
}

// SymbolFilter keeps names containing sub (case-insensitive); empty keeps all.
func SymbolFilter(names []string, sub string) []string {
	if sub == "" {
		return names
	}
	needle := strings.ToLower(sub)
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), needle) {
			out = append(out, n)
		}
	}
	return out
}
