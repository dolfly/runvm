package android

import (
	"debug/elf"
	"fmt"
	"os"
	"sort"
)

// Exports lists the function symbols a library exports.
//
// This reads the ELF directly instead of asking the emulator, so it works
// before a session exists, without a CPU backend and even on a library that
// would fail to boot. That matters for discovery: a model usually wants to
// know what it can call before spending a second on emulator startup.
func Exports(path string) ([]string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read exports: %w", err)
	}
	defer f.Close()

	syms, err := f.DynamicSymbols()
	if err != nil {
		return nil, fmt.Errorf("read dynamic symbols: %w", err)
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) != elf.STT_FUNC || s.Section == elf.SHN_UNDEF {
			continue
		}
		if b := elf.ST_BIND(s.Info); b != elf.STB_GLOBAL && b != elf.STB_WEAK {
			continue
		}
		if s.Name == "" {
			continue
		}
		if _, dup := seen[s.Name]; dup {
			continue
		}
		seen[s.Name] = struct{}{}
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names, nil
}

// Info is a cheap, non-booting description of a library.
type Info struct {
	Arch  string `json:"arch"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes"`
}

// Describe reports the ELF class and machine without loading anything.
func Describe(path string) (Info, error) {
	f, err := elf.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	st, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Arch:  f.Machine.String(),
		Kind:  fmt.Sprintf("%s %s", f.Class, f.Type),
		Bytes: st.Size(),
	}, nil
}
