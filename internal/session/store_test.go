package session

import "testing"

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
