package android

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/isesword/golem/dvm"
)

// jniStub is one answered Java callback: when the guest calls
// class.method(...), the host returns Value instead of reaching a real JVM.
type jniStub struct {
	Class  string `json:"class"`
	Method string `json:"method"`
	Kind   string `json:"kind"` // string | number | boolean | null
	Str    string `json:"string,omitempty"`
	Num    int64  `json:"number,omitempty"`
	Bool   bool   `json:"boolean,omitempty"`
}

// jniHandler answers the guest's JNIEnv up-calls from a stub table.
//
// The guest's sig strings arrive in golem's "<class>-><name>(<args>)<ret>"
// form, so a stub is matched on class and method name and its value is
// coerced to whatever the call site asks for: the same numeric stub answers an
// int, long or boolean call, a string stub answers an object call. Everything
// unclaimed falls through to AbstractJni, which returns null/0 — the same
// behaviour a missing Java class has on a device.
type jniHandler struct {
	dvm.AbstractJni

	mu    sync.RWMutex
	stubs map[string]jniStub
}

func newJNI() *jniHandler { return &jniHandler{stubs: map[string]jniStub{}} }

// stubKey normalizes "java.lang.System" and "Ljava/lang/System;" to the class
// form used in signatures, then joins with the method name.
func stubKey(class, method string) (string, error) {
	c := strings.TrimSpace(class)
	c = strings.TrimPrefix(c, "L")
	c = strings.TrimSuffix(c, ";")
	c = strings.ReplaceAll(c, ".", "/")
	m := strings.TrimSpace(method)
	if c == "" || m == "" {
		return "", fmt.Errorf("java stub needs a class and a method")
	}
	if i := strings.IndexByte(m, '('); i >= 0 {
		m = m[:i] // tolerate a full signature in the method argument
	}
	return c + "->" + m, nil
}

func (h *jniHandler) put(st jniStub) error {
	key, err := stubKey(st.Class, st.Method)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.stubs[key]; exists {
		return fmt.Errorf("java stub %s is already registered", key)
	}
	h.stubs[key] = st
	return nil
}

func (h *jniHandler) list() []jniStub {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]jniStub, 0, len(h.stubs))
	for _, st := range h.stubs {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Class != out[j].Class {
			return out[i].Class < out[j].Class
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// lookup resolves a signature to a stub.
func (h *jniHandler) lookup(sig string) (jniStub, bool) {
	i := strings.Index(sig, "->")
	if i < 0 {
		return jniStub{}, false
	}
	class := strings.TrimPrefix(sig[:i], "L")
	class = strings.TrimSuffix(class, ";")
	rest := sig[i+2:]
	if j := strings.IndexByte(rest, '('); j >= 0 {
		rest = rest[:j]
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	st, ok := h.stubs[class+"->"+rest]
	return st, ok
}

// asObject answers a call whose return type is a reference.
func (h *jniHandler) asObject(vm *dvm.VM, sig string) (*dvm.Object, bool) {
	st, ok := h.lookup(sig)
	if !ok {
		return nil, false
	}
	if st.Kind == "null" {
		return nil, true
	}
	if st.Kind == "string" {
		return &dvm.Object{Class: vm.ResolveClass("java/lang/String"), Value: st.Str}, true
	}
	return nil, true
}

func (h *jniHandler) CallStaticObjectMethodV(vm *dvm.VM, _ *dvm.Class, sig string, _ *dvm.VaList) *dvm.Object {
	if o, ok := h.asObject(vm, sig); ok {
		return o
	}
	return h.AbstractJni.CallStaticObjectMethodV(vm, nil, sig, nil)
}

func (h *jniHandler) CallStaticIntMethodV(_ *dvm.VM, _ *dvm.Class, sig string, _ *dvm.VaList) int32 {
	if st, ok := h.lookup(sig); ok {
		if st.Kind == "boolean" && st.Bool {
			return 1
		}
		return int32(st.Num)
	}
	return 0
}

func (h *jniHandler) CallStaticLongMethodV(_ *dvm.VM, _ *dvm.Class, sig string, _ *dvm.VaList) int64 {
	if st, ok := h.lookup(sig); ok {
		return st.Num
	}
	return 0
}

func (h *jniHandler) CallStaticBooleanMethodV(_ *dvm.VM, _ *dvm.Class, sig string, _ *dvm.VaList) bool {
	if st, ok := h.lookup(sig); ok {
		return st.Bool || st.Num != 0
	}
	return false
}

func (h *jniHandler) CallObjectMethodV(vm *dvm.VM, obj *dvm.Object, sig string, _ *dvm.VaList) *dvm.Object {
	if o, ok := h.asObject(vm, sig); ok {
		return o
	}
	// String.toString() and friends: answering with the receiver is what a
	// real JVM does and is what most libraries actually want.
	if obj != nil && obj.Value != nil && strings.HasSuffix(sig, "->toString()Ljava/lang/String;") {
		if s, ok := obj.Value.(string); ok {
			return &dvm.Object{Class: vm.ResolveClass("java/lang/String"), Value: s}
		}
	}
	return h.AbstractJni.CallObjectMethodV(vm, obj, sig, nil)
}

func (h *jniHandler) CallIntMethodV(_ *dvm.VM, _ *dvm.Object, sig string, _ *dvm.VaList) int32 {
	if st, ok := h.lookup(sig); ok {
		return int32(st.Num)
	}
	return 0
}

func (h *jniHandler) CallLongMethodV(_ *dvm.VM, _ *dvm.Object, sig string, _ *dvm.VaList) int64 {
	if st, ok := h.lookup(sig); ok {
		return st.Num
	}
	return 0
}

func (h *jniHandler) CallBooleanMethodV(_ *dvm.VM, _ *dvm.Object, sig string, _ *dvm.VaList) bool {
	if st, ok := h.lookup(sig); ok {
		return st.Bool || st.Num != 0
	}
	return false
}

// stubFromValue builds a stub from the JSON value the model supplied: strings
// answer object calls, numbers answer numeric calls, booleans answer boolean
// calls, and null answers "this Java object does not exist".
func stubFromValue(class, method string, v any) (jniStub, error) {
	st := jniStub{Class: class, Method: method}
	switch x := v.(type) {
	case nil:
		st.Kind = "null"
	case string:
		st.Kind = "string"
		st.Str = x
	case bool:
		st.Kind = "boolean"
		st.Bool = x
	case float64:
		if x != float64(int64(x)) {
			return st, fmt.Errorf("java stub: %v is not an integer", x)
		}
		st.Kind = "number"
		st.Num = int64(x)
	default:
		return st, fmt.Errorf("java stub: want string, number, boolean or null, got %T", v)
	}
	return st, nil
}

// String renders a stub for logs and tool output.
func (s jniStub) String() string {
	switch s.Kind {
	case "string":
		return strconv.Quote(s.Str)
	case "boolean":
		return strconv.FormatBool(s.Bool)
	case "null":
		return "null"
	}
	return strconv.FormatInt(s.Num, 10)
}
