package iosprobe

import (
	"debug/macho"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestFixtureIsWhatItClaims guards the one thing the doctor and selftest
// checks depend on: the embedded bytes must still be an arm64 Mach-O dylib
// that exports add. A truncated or re-encoded blob would otherwise fail much
// later as "add not found", which reads like a runvm bug.
func TestFixtureIsWhatItClaims(t *testing.T) {
	path, cleanup, err := Write()
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	defer cleanup()

	f, err := macho.Open(path)
	if err != nil {
		t.Fatalf("the fixture is not a readable Mach-O: %v", err)
	}
	defer f.Close()

	if f.Type != macho.TypeDylib {
		t.Errorf("fixture type = %s, want a dylib", f.Type)
	}
	if f.Cpu != macho.CpuArm64 {
		t.Errorf("fixture cpu = %s, want arm64 (golem would refuse to boot it)", f.Cpu)
	}
	if f.Symtab == nil {
		t.Fatal("fixture has no symbol table; rebuild it from FixtureSource")
	}
	var defined, undefined []string
	for _, s := range f.Symtab.Syms {
		name := strings.TrimPrefix(s.Name, "_")
		if s.Sect == 0 {
			undefined = append(undefined, name)
			continue
		}
		defined = append(defined, name)
	}
	for _, want := range []string{"add", "length"} {
		if !slices.Contains(defined, want) {
			t.Errorf("fixture does not export %s (exports: %v); rebuild it from FixtureSource", want, defined)
		}
	}
	// The import is what makes the boundary testable: without it, "imported but
	// unimplemented" can never be observed and a regression would be invisible.
	if !slices.Contains(undefined, FixtureImport) {
		t.Errorf("fixture does not import %s (imports: %v); rebuild it with -Wl,-bind_at_load",
			FixtureImport, undefined)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() != int64(len(Fixture)) {
		t.Errorf("materialized size = %d, embedded %d", st.Size(), len(Fixture))
	}
}
