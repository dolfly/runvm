package android

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/isesword/golem/emulator"
)

// Session is one loaded library plus its emulator instance.
//
// golem's emulator is not safe for concurrent calls (it owns CPU and memory
// state), so every entry point takes the mutex. Concurrency comes from having
// several sessions, which is also how golem's own engine pool works.
type Session struct {
	mu     sync.Mutex
	emu    *emulator.Emulator
	path   string
	engine string
	dex    string
}

// AssetsDir resolves the bionic sysroot directory. RUNVM_ASSETS wins so a
// packaged binary can be pointed at a sysroot it did not ship with.
func AssetsDir() string {
	if v := os.Getenv("RUNVM_ASSETS"); v != "" {
		return v
	}
	return emulator.AssetsDir()
}

// AssetsPresent reports whether the sysroot looks usable.
func AssetsPresent(dir string) bool {
	if dir == "" {
		return false
	}
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

// Open boots an emulator with the library loaded and its initializers run.
// engine selects the CPU backend ("" = golem's default).
func Open(path, engine, dex string) (*Session, error) {
	// An empty path boots bionic alone: no main library, which is what
	// doctor probes with to separate "no engine" from "bad library".
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("android_open: %q: %w", path, err)
		}
	}
	assets := AssetsDir()
	if !AssetsPresent(assets) {
		return nil, fmt.Errorf("android_open: bionic sysroot not found at %q: set RUNVM_ASSETS to golem's assets directory", assets)
	}
	emu, err := emulator.New(emulator.Config{
		SOPath:      path,
		AssetRoot:   assets,
		Engine:      engine,
		ProcessName: "runvm",
	})
	if err != nil {
		return nil, fmt.Errorf("android_open: %s: %w (run `runvm doctor` if this mentions the engine or libunicorn)", path, err)
	}
	s := &Session{emu: emu, path: path, engine: emu.Engine(), dex: dex}
	if dex != "" {
		if _, err := emu.LoadDex(dex); err != nil {
			_ = emu.Close()
			return nil, fmt.Errorf("android_open: load dex %q: %w", dex, err)
		}
	}
	return s, nil
}

// Path is the loaded library.
func (s *Session) Path() string { return s.path }

// Engine is the backend golem selected.
func (s *Session) Engine() string { return s.engine }

// DEX is the optional classes.dex used for JNI metadata lookups.
func (s *Session) DEX() string { return s.dex }

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
			if x != float64(uint64(x)) {
				return nil, fmt.Errorf("argument %d: %v is not an integer", i, x)
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

// Call invokes an exported symbol. golem supports up to 8 integer arguments
// per call, which is also the practical ABI limit for this interface.
func (s *Session) Call(symbol string, args []Arg) (Result, error) {
	var res Result
	if symbol == "" {
		return res, errors.New("android_call: empty symbol")
	}
	if len(args) > 8 {
		return res, fmt.Errorf("android_call: %d arguments, at most 8 are supported", len(args))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return res, errors.New("android_call: session is closed")
	}

	values := make([]emulator.Value, 0, len(args))
	var buffers []struct {
		addr uint64
		size int
	}
	for _, a := range args {
		switch a.Kind {
		case ArgString:
			values = append(values, emulator.Ptr(s.emu.WriteCStringAlloc(a.Str)))
		case ArgBuffer:
			if a.Size > maxBuffer {
				return res, fmt.Errorf("android_call: buffer of %d bytes exceeds the %d byte cap", a.Size, maxBuffer)
			}
			addr, err := s.emu.Malloc(uint64(a.Size))
			if err != nil {
				return res, fmt.Errorf("android_call: allocate %d byte buffer: %w", a.Size, err)
			}
			values = append(values, emulator.Ptr(addr))
			buffers = append(buffers, struct {
				addr uint64
				size int
			}{addr, a.Size})
		default:
			values = append(values, emulator.Words(a.Word)...)
		}
	}

	v, err := s.emu.CallSymbol(symbol, values...)
	if err != nil {
		return res, fmt.Errorf("android_call: %s: %w", symbol, err)
	}
	res.Value = v
	for _, b := range buffers {
		raw, err := s.emu.ReadBytes(b.addr, uint64(b.size))
		if err != nil {
			return res, fmt.Errorf("android_call: read back buffer: %w", err)
		}
		res.Buffers = append(res.Buffers, Buffer{
			Size:  b.size,
			Hex:   hexBytes(raw),
			ASCII: printable(raw),
		})
	}
	return res, nil
}

// Close tears the emulator down. Safe to call twice.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return nil
	}
	err := s.emu.Close()
	s.emu = nil
	return err
}

func hexBytes(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		sb.WriteString(strconv.FormatUint(uint64(c)>>4, 16))
		sb.WriteString(strconv.FormatUint(uint64(c)&0xf, 16))
	}
	return sb.String()
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
func Hex(v uint64) string { return "0x" + strconv.FormatUint(v, 16) }

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
