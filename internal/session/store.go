// Package session is the generic session registry: id -> value, in creation
// order, with a last-used clock for the idle reaper.
package session

import (
	"fmt"
	"sync"
	"time"
)

// Hooks lets a durable ledger follow the live set without the store knowing
// anything about persistence. They run after the store lock is released, so a
// hook may read the store back.
type Hooks[T any] struct {
	Added   func(id string, v T)
	Removed func(id string, v T)
}

// Store maps session ids to values.
type Store[T any] struct {
	mu     sync.Mutex
	prefix string
	seq    int
	items  map[string]T
	order  []string
	hooks  Hooks[T]
}

// New returns an empty store whose ids are "<prefix>-1", "<prefix>-2", ...
func New[T any](prefix string) *Store[T] {
	return &Store[T]{prefix: prefix, items: map[string]T{}}
}

// SetHooks installs the lifecycle callbacks. Call it before the first session:
// it exists so the registry can be wired to a ledger at construction time.
func (s *Store[T]) SetHooks(h Hooks[T]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = h
}

// Seed advances the id counter past n. Restoring a ledger uses it so an id the
// ledger already recorded is never handed out again after a restart.
func (s *Store[T]) Seed(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > s.seq {
		s.seq = n
	}
}

// Add stores v under a fresh id and returns it.
func (s *Store[T]) Add(v T) string {
	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("%s-%d", s.prefix, s.seq)
	s.items[id] = v
	s.order = append(s.order, id)
	h := s.hooks.Added
	s.mu.Unlock()

	if h != nil {
		h(id, v)
	}
	return id
}

// AddID stores v under id, which must not be taken yet, and reports whether it
// did. Resuming a recorded id uses this and falls back to Add when the id is
// already live, so a stale ledger can never overwrite a running session.
func (s *Store[T]) AddID(id string, v T) bool {
	s.mu.Lock()
	if _, exists := s.items[id]; exists {
		s.mu.Unlock()
		return false
	}
	s.items[id] = v
	s.order = append(s.order, id)
	h := s.hooks.Added
	s.mu.Unlock()

	if h != nil {
		h(id, v)
	}
	return true
}

// Get returns the value for id.
func (s *Store[T]) Get(id string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[id]
	return v, ok
}

// Len returns the number of live sessions.
func (s *Store[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// IDs returns live session ids in creation order.
func (s *Store[T]) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Delete removes id and returns the removed value.
func (s *Store[T]) Delete(id string) (T, bool) {
	s.mu.Lock()
	v, ok := s.items[id]
	if ok {
		delete(s.items, id)
		for i, cur := range s.order {
			if cur == id {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
	h := s.hooks.Removed
	s.mu.Unlock()

	if h != nil && ok {
		h(id, v)
	}
	return v, ok
}

// CloseAll removes every session and returns them in creation order.
func (s *Store[T]) CloseAll() []T {
	s.mu.Lock()
	out := make([]T, 0, len(s.order))
	ids := make([]string, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.items[id])
		ids = append(ids, id)
	}
	s.items = map[string]T{}
	s.order = nil
	h := s.hooks.Removed
	s.mu.Unlock()

	if h != nil {
		for i, v := range out {
			h(ids[i], v)
		}
	}
	return out
}

// Reap closes sessions whose last-used time is older than cutoff, returning how
// many were closed. touch is read through the optional interface so this package
// stays free of a dependency on any backend.
func (s *Store[T]) Reap(cutoff time.Time, touch func(T) time.Time) int {
	if touch == nil {
		return 0
	}
	s.mu.Lock()
	var stale []string
	for _, id := range s.order {
		if touch(s.items[id]).Before(cutoff) {
			stale = append(stale, id)
		}
	}
	s.mu.Unlock()

	n := 0
	for _, id := range stale {
		if _, ok := s.Delete(id); ok {
			n++
		}
	}
	return n
}
