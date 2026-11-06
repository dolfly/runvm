package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/isesword/golem/emulator"
)

// maxPool bounds the engine pool. Each engine is a full guest process
// (address space + JIT buffer), so this is a memory decision as much as a
// concurrency one.
const maxPool = 16

// OpenOptions configures a session at creation.
type OpenOptions struct {
	Engine  string
	DEX     string
	Pool    int   // engines to warm; 1 = one call at a time
	MaxUses int64 // recycle an engine after this many calls; 0 = never
}

// Session is one library plus a pool of emulators that have it loaded.
//
// golem's Emulator is not goroutine-safe: it owns CPU and memory state. The
// Pool gives that invariant a home — an engine is lent to one caller at a
// time, and concurrency scales by engine count instead of by locks. Pool size
// 1 therefore means exactly "one call at a time", the same semantics a single
// mutex-guarded emulator would have.
type Session struct {
	path string
	cfg  emulator.Config
	opts OpenOptions
	pool *emulator.Pool[*emulator.Emulator]
	jni  *jniHandler

	closeOnce sync.Once
	created   atomic.Int64
	lastUsed  atomic.Int64 // unix nanos; the reaper reads this

	// hooks are applied lazily: an engine learns about them the first time it
	// is used after they were registered, so registering a hook never costs a
	// reboot and never disturbs another engine's state.
	hmu     sync.Mutex
	hooks   map[string]uint64
	version int
	applied map[*emulator.Emulator]int // engine -> hook version it has applied
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

// Open boots a pool of emulators with the library loaded and its initializers
// run. An empty path boots bionic alone (used by doctor to separate "no
// engine" from "bad library").
func Open(path string, opts OpenOptions) (*Session, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("android_open: %q: %w", path, err)
		}
	}
	assets := AssetsDir()
	if !AssetsPresent(assets) {
		return nil, fmt.Errorf("android_open: bionic sysroot not found at %q: set RUNVM_ASSETS to golem's assets directory", assets)
	}
	if opts.Pool <= 0 {
		opts.Pool = 1
	}
	if opts.Pool > maxPool {
		return nil, fmt.Errorf("android_open: pool %d exceeds the maximum of %d", opts.Pool, maxPool)
	}

	jni := newJNI()
	s := &Session{
		path:    path,
		opts:    opts,
		hooks:   map[string]uint64{},
		applied: map[*emulator.Emulator]int{},
		jni:     jni,
		cfg: emulator.Config{
			SOPath:      path,
			AssetRoot:   assets,
			Engine:      opts.Engine,
			ProcessName: "runvm",
			// Config.Android is deprecated in favour of a typed platform
			// config, but that constructor lives in golem's internal tree, so
			// the legacy field is the only way in from another module. The
			// shim converts it once, at options normalization.
			Android: emulator.AndroidConfig{JNI: jni},
		},
	}

	pool, err := emulator.NewPool[*emulator.Emulator](opts.Pool, opts.MaxUses,
		func() (*emulator.Emulator, error) {
			s.created.Add(1)
			emu, err := emulator.New(s.cfg)
			if err != nil {
				return nil, err
			}
			if opts.DEX != "" {
				if _, err := emu.LoadDex(opts.DEX); err != nil {
					_ = emu.Close()
					return nil, fmt.Errorf("load dex %q: %w", opts.DEX, err)
				}
			}
			return emu, nil
		},
		func(emu *emulator.Emulator) { _ = emu.Close() },
	)
	if err != nil {
		return nil, fmt.Errorf("android_open: %s: %w (run `runvm doctor` if this mentions the engine or libunicorn)", path, err)
	}
	s.pool = pool
	s.touch()
	return s, nil
}

// Path is the loaded library.
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

// DEX is the optional classes.dex used for JNI metadata lookups.
func (s *Session) DEX() string { return s.opts.DEX }

// PoolSize is the configured engine count.
func (s *Session) PoolSize() int { return s.opts.Pool }

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

// Call invokes an exported symbol on a pooled engine.
func (s *Session) Call(ctx context.Context, symbol string, args []Arg) (Result, error) {
	var res Result
	if symbol == "" {
		return res, errors.New("android_call: empty symbol")
	}
	if len(args) > 8 {
		return res, fmt.Errorf("android_call: %d arguments, at most 8 are supported", len(args))
	}

	s.touch()
	err := s.pool.Do(ctx, func(emu *emulator.Emulator) error {
		if err := s.applyHooks(emu); err != nil {
			return err
		}

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
					return fmt.Errorf("android_call: buffer of %d bytes exceeds the %d byte cap", a.Size, maxBuffer)
				}
				addr, err := emu.Malloc(uint64(a.Size))
				if err != nil {
					return fmt.Errorf("android_call: allocate %d byte buffer: %w", a.Size, err)
				}
				values = append(values, emulator.Ptr(addr))
				buffers = append(buffers, buf{addr: addr, size: a.Size})
			default:
				values = append(values, emulator.Words(a.Word)...)
			}
		}

		v, err := emu.CallSymbol(symbol, values...)
		if err != nil {
			return fmt.Errorf("android_call: %s: %w", symbol, err)
		}
		res.Value = v
		for _, b := range buffers {
			raw, err := emu.ReadBytes(b.addr, uint64(b.size))
			if err != nil {
				return fmt.Errorf("android_call: read back buffer: %w", err)
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

// Hook replaces an exported function with one that returns a constant.
//
// This is the cheapest way to take a dependency out of the picture: stub the
// network call, the signature routine, the clock, and test the code around it.
// golem interposes at execution, so the guest's own code is untouched and
// every other symbol still resolves normally.
func (s *Session) Hook(symbol string, returns uint64) error {
	if symbol == "" {
		return errors.New("android_hook: empty symbol")
	}
	s.hmu.Lock()
	defer s.hmu.Unlock()
	if old, ok := s.hooks[symbol]; ok && old != returns {
		return fmt.Errorf("android_hook: %q is already stubbed to %d; open a new session to change it", symbol, old)
	}
	s.touch()
	s.hooks[symbol] = returns
	s.version++
	return nil
}

// applyHooks installs any hooks this engine has not seen yet.
//
// Applying lazily keeps registration free (no reboot, no lost guest state) and
// correct across pool rebuilds: a fresh engine starts at version 0 and catches
// up the first time it is used.
func (s *Session) applyHooks(emu *emulator.Emulator) error {
	s.hmu.Lock()
	want := s.version
	if s.applied[emu] == want {
		s.hmu.Unlock()
		return nil
	}
	hooks := make(map[string]uint64, len(s.hooks))
	for k, v := range s.hooks {
		hooks[k] = v
	}
	s.hmu.Unlock()

	for symbol, returns := range hooks {
		ret := returns
		if err := emu.ReplaceSymbol(symbol, func(*emulator.Hook) uint64 { return ret }); err != nil {
			return fmt.Errorf("android_hook: %s: %w", symbol, err)
		}
	}

	s.hmu.Lock()
	s.applied[emu] = want
	s.hmu.Unlock()
	return nil
}

// Hooks lists the stubbed symbols and their return values.
func (s *Session) Hooks() map[string]uint64 {
	s.hmu.Lock()
	defer s.hmu.Unlock()
	out := make(map[string]uint64, len(s.hooks))
	for k, v := range s.hooks {
		out[k] = v
	}
	return out
}

// touch records use, so an idle session can be reaped.
func (s *Session) touch() { s.lastUsed.Store(time.Now().UnixNano()) }

// LastUsed reports the last time this session did work.
func (s *Session) LastUsed() time.Time { return time.Unix(0, s.lastUsed.Load()) }

// EnginesCreated counts every engine ever constructed, including pool warmup
// and post-panic rebuilds. Growth past the configured size is the signal that
// something is recycling or crashing.
func (s *Session) EnginesCreated() int64 { return s.created.Load() }

// Stats reports pool occupancy, so a long test run can be watched instead of
// guessed at.
func (s *Session) Stats() emulator.PoolStats { return s.pool.Stats() }

// JavaStub answers a Java callback the guest's JNIEnv makes, so a library that
// expects a real Android runtime can be exercised without one. The value is
// coerced at each call site: a string answers object calls, a number answers
// int/long/boolean calls, nil answers "no such object".
//
// Stubs are consulted by every engine in the pool immediately; unlike symbol
// hooks they need no per-engine installation.
func (s *Session) JavaStub(class, method string, v any) (jniStub, error) {
	st, err := stubFromValue(class, method, v)
	if err != nil {
		return st, err
	}
	s.touch()
	if err := s.jni.put(st); err != nil {
		return st, err
	}
	return st, nil
}

// JavaStubs lists the registered Java stubs.
func (s *Session) JavaStubs() []jniStub { return s.jni.list() }

// Close tears the pool down. Safe to call twice.
func (s *Session) Close() error {
	s.closeOnce.Do(func() { s.pool.CloseAll() })
	return nil
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
