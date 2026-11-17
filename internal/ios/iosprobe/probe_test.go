package iosprobe

import (
	"debug/macho"
	"os"
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
	found := false
	for _, s := range f.Symtab.Syms {
		if s.Name == "_add" {
			found = true
			break
		}
	}
	if !found {
		t.Error(`fixture does not export _add; rebuild it from FixtureSource`)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() != int64(len(Fixture)) {
		t.Errorf("materialized size = %d, embedded %d", st.Size(), len(Fixture))
	}
}
