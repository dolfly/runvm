package android

import (
	"testing"
)

func TestParseArgs(t *testing.T) {
	got, err := ParseArgs([]any{float64(2), "hi", map[string]any{"buffer": float64(16)}})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d args", len(got))
	}
	if got[0].Kind != ArgWord || got[0].Word != 2 {
		t.Errorf("arg0 = %+v", got[0])
	}
	if got[1].Kind != ArgString || got[1].Str != "hi" {
		t.Errorf("arg1 = %+v", got[1])
	}
	if got[2].Kind != ArgBuffer || got[2].Size != 16 {
		t.Errorf("arg2 = %+v", got[2])
	}
}

func TestParseArgsRejects(t *testing.T) {
	cases := []struct {
		name string
		in   []any
	}{
		{"negative", []any{float64(-1)}},
		{"fractional", []any{1.5}},
		{"object without buffer", []any{map[string]any{"size": float64(1)}}},
		{"zero buffer", []any{map[string]any{"buffer": float64(0)}}},
		{"unsupported type", []any{true}},
	}
	for _, c := range cases {
		if _, err := ParseArgs(c.in); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

func TestSymbolFilter(t *testing.T) {
	names := []string{"Java_com_x_sign", "sign", "encryptBuffer"}
	if got := SymbolFilter(names, "SIGN"); len(got) != 2 {
		t.Errorf("filter SIGN = %v, want 2 matches", got)
	}
	if got := SymbolFilter(names, ""); len(got) != len(names) {
		t.Errorf("empty filter dropped entries: %v", got)
	}
}
