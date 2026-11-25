package session

import (
	"sync"
	"testing"
	"time"
)

func TestStoreLifecycle(t *testing.T) {
	s := New[string]("t")
	a := s.Add("first")
	b := s.Add("second")
	if a == b {
		t.Fatalf("ids collided: %q", a)
	}
	if got, ok := s.Get(a); !ok || got != "first" {
		t.Fatalf("Get(%q) = %q, %v", a, got, ok)
	}
	if ids := s.IDs(); len(ids) != 2 || ids[0] != a || ids[1] != b {
		t.Fatalf("IDs() = %v, want creation order", ids)
	}
	if got, ok := s.Delete(a); !ok || got != "first" {
		t.Fatalf("Delete(%q) = %q, %v", a, got, ok)
	}
	if _, ok := s.Get(a); ok {
		t.Fatal("deleted session is still reachable")
	}
	if ids := s.IDs(); len(ids) != 1 || ids[0] != b {
		t.Fatalf("IDs() after delete = %v", ids)
	}
	if _, ok := s.Delete(a); ok {
		t.Fatal("deleting twice should report false")
	}
}

func TestCloseAll(t *testing.T) {
	s := New[int]("t")
	s.Add(1)
	s.Add(2)
	got := s.CloseAll()
	if len(got) != 2 {
		t.Fatalf("CloseAll returned %d items", len(got))
	}
	if s.Len() != 0 {
		t.Fatalf("store not empty after CloseAll: %d", s.Len())
	}
}

func TestHooksFollowLifecycle(t *testing.T) {
	s := New[string]("t")
	var mu sync.Mutex
	var added, removed []string
	s.SetHooks(Hooks[string]{
		Added:   func(id string, _ string) { mu.Lock(); added = append(added, id); mu.Unlock() },
		Removed: func(id string, _ string) { mu.Lock(); removed = append(removed, id); mu.Unlock() },
	})

	a := s.Add("x")
	b := s.Add("y")
	s.Delete(a)
	s.CloseAll()

	mu.Lock()
	defer mu.Unlock()
	if len(added) != 2 || added[0] != a || added[1] != b {
		t.Errorf("added = %v, want [%s %s]", added, a, b)
	}
	if len(removed) != 2 || removed[0] != a || removed[1] != b {
		t.Errorf("removed = %v, want [%s %s]", removed, a, b)
	}
}

// A hook that reads the store back must not deadlock: the callbacks run after
// the lock is released.
func TestHookCanReadStore(t *testing.T) {
	s := New[string]("t")
	s.SetHooks(Hooks[string]{Added: func(id string, _ string) {
		if _, ok := s.Get(id); !ok {
			t.Errorf("hook could not read the session it was told about")
		}
		_ = s.IDs()
	}})
	s.Add("x")
}

func TestAddIDAndSeed(t *testing.T) {
	s := New[string]("t")
	if !s.AddID("h5-7", "resumed") {
		t.Fatal("AddID on a free id should succeed")
	}
	if s.AddID("h5-7", "other") {
		t.Fatal("AddID must refuse a taken id")
	}
	if got, _ := s.Get("h5-7"); got != "resumed" {
		t.Errorf("AddID overwrote the live session: %q", got)
	}

	// A ledger restored with h5-7 must not hand out h5-1..h5-7 again.
	s.Seed(7)
	if id := s.Add("fresh"); id != "t-8" {
		t.Errorf("Add after Seed(7) = %q, want t-8", id)
	}
	s.Seed(3) // a lower seed must not rewind the counter
	if id := s.Add("fresh2"); id != "t-9" {
		t.Errorf("Add after Seed(3) = %q, want t-9", id)
	}
}

func TestReapUsesTouch(t *testing.T) {
	s := New[string]("t")
	old := s.Add("old")
	s.Add("new")
	touch := func(v string) time.Time {
		if v == "old" {
			return time.Unix(0, 0)
		}
		return time.Now()
	}
	if n := s.Reap(time.Now(), touch); n != 1 {
		t.Fatalf("Reap closed %d, want 1", n)
	}
	if _, ok := s.Get(old); ok {
		t.Error("the idle session survived Reap")
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}
	if n := s.Reap(time.Now(), nil); n != 0 {
		t.Error("Reap without a touch func must be a no-op")
	}
}
