package android

import (
	"testing"

	"github.com/isesword/golem/dvm"
)

func TestStubKeyNormalizes(t *testing.T) {
	cases := []struct{ class, method, want string }{
		{"java/lang/System", "currentTimeMillis", "java/lang/System->currentTimeMillis"},
		{"java.lang.System", "currentTimeMillis", "java/lang/System->currentTimeMillis"},
		{"Ljava/lang/System;", "currentTimeMillis", "java/lang/System->currentTimeMillis"},
		{"java/lang/String", "valueOf(I)Ljava/lang/String;", "java/lang/String->valueOf"},
	}
	for _, c := range cases {
		got, err := stubKey(c.class, c.method)
		if err != nil {
			t.Errorf("stubKey(%q, %q): %v", c.class, c.method, err)
			continue
		}
		if got != c.want {
			t.Errorf("stubKey(%q, %q) = %q, want %q", c.class, c.method, got, c.want)
		}
	}
	if _, err := stubKey("", "x"); err == nil {
		t.Error("empty class should be refused")
	}
}

func TestLookupParsesGuestSignature(t *testing.T) {
	h := newJNI()
	if err := h.put(jniStub{Class: "java/lang/System", Method: "currentTimeMillis", Kind: "number", Num: 1700000000000}); err != nil {
		t.Fatalf("put: %v", err)
	}
	st, ok := h.lookup("java/lang/System->currentTimeMillis()J")
	if !ok {
		t.Fatal("lookup missed the registered stub")
	}
	if st.Num != 1700000000000 {
		t.Errorf("num = %d", st.Num)
	}
	if _, ok := h.lookup("java/lang/System->nanoTime()J"); ok {
		t.Error("unregistered method matched")
	}
}

func TestCoercionPerCallSite(t *testing.T) {
	h := newJNI()
	vm := dvm.NewVM()
	if err := h.put(jniStub{Class: "com/example/Clock", Method: "now", Kind: "number", Num: 42}); err != nil {
		t.Fatal(err)
	}
	sig := "com/example/Clock->now()J"
	if got := h.CallStaticIntMethodV(vm, nil, sig, nil); got != 42 {
		t.Errorf("int = %d, want 42", got)
	}
	if got := h.CallStaticLongMethodV(vm, nil, sig, nil); got != 42 {
		t.Errorf("long = %d, want 42", got)
	}
	if got := h.CallStaticBooleanMethodV(vm, nil, sig, nil); !got {
		t.Error("boolean should be true for a non-zero number")
	}
}

func TestStringStubAnswersObjectCall(t *testing.T) {
	h := newJNI()
	vm := dvm.NewVM()
	if err := h.put(jniStub{Class: "android/content/Context", Method: "getPackageName", Kind: "string", Str: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	obj := h.CallStaticObjectMethodV(vm, nil, "android/content/Context->getPackageName()Ljava/lang/String;", nil)
	if obj == nil {
		t.Fatal("object stub returned nil")
	}
	if got, _ := obj.Value.(string); got != "com.example.app" {
		t.Errorf("value = %v, want com.example.app", obj.Value)
	}
}

func TestNullStubAndMissingMethod(t *testing.T) {
	h := newJNI()
	vm := dvm.NewVM()
	if err := h.put(jniStub{Class: "android/content/Context", Method: "getFilesDir", Kind: "null"}); err != nil {
		t.Fatal(err)
	}
	if obj := h.CallStaticObjectMethodV(vm, nil, "android/content/Context->getFilesDir()Ljava/io/File;", nil); obj != nil {
		t.Errorf("null stub returned %v", obj)
	}
	// Unclaimed calls fall through to AbstractJni's null, identical to a
	// missing class on a device.
	if obj := h.CallStaticObjectMethodV(vm, nil, "android/content/Context->getCacheDir()Ljava/io/File;", nil); obj != nil {
		t.Errorf("unregistered call returned %v", obj)
	}
}

func TestDuplicateStubRefused(t *testing.T) {
	h := newJNI()
	st := jniStub{Class: "java/lang/System", Method: "currentTimeMillis", Kind: "number", Num: 1}
	if err := h.put(st); err != nil {
		t.Fatal(err)
	}
	if err := h.put(st); err == nil {
		t.Error("second registration should be refused")
	}
}

func TestStubFromValue(t *testing.T) {
	if st, err := stubFromValue("a/b", "m", "x"); err != nil || st.Kind != "string" || st.Str != "x" {
		t.Errorf("string: %+v %v", st, err)
	}
	if st, err := stubFromValue("a/b", "m", float64(7)); err != nil || st.Kind != "number" || st.Num != 7 {
		t.Errorf("number: %+v %v", st, err)
	}
	if st, err := stubFromValue("a/b", "m", true); err != nil || st.Kind != "boolean" || !st.Bool {
		t.Errorf("bool: %+v %v", st, err)
	}
	if st, err := stubFromValue("a/b", "m", nil); err != nil || st.Kind != "null" {
		t.Errorf("null: %+v %v", st, err)
	}
	if _, err := stubFromValue("a/b", "m", 1.5); err == nil {
		t.Error("fractional number should be refused")
	}
	if _, err := stubFromValue("a/b", "m", map[string]any{}); err == nil {
		t.Error("object should be refused")
	}
}
