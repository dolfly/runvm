// Package session keeps the live test environments addressed by MCP tools.
//
// A session is a resource the model names in later calls, so IDs must be
// stable for the life of the process and cheap to validate: the store is the
// single source of truth, and deleting a session is what releases the
// underlying emulator or page.
package session

import (
	"fmt"
	"sync"
)

// Store is a concurrency-safe map from generated IDs to sessions.
type Store[T any] struct {
	mu     sync.Mutex
	items  map[string]T
	order  []string
	prefix string
	seq    int
}

// New returns a store whose IDs look like "<prefix>-<n>".
func New[T any](prefix string) *Store[T] {
	return &Store[T]{items: map[string]T{}, prefix: prefix}
}

// Add stores v under a fresh ID.
func (s *Store[T]) Add(v T) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("%s-%d", s.prefix, s.seq)
	s.items[id] = v
	s.order = append(s.order, id)
	return id
}

// Get returns the session for id.
func (s *Store[T]) Get(id string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[id]
	return v, ok
}

// Delete removes id and returns the session so the caller can close it.
func (s *Store[T]) Delete(id string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[id]
	if !ok {
		var zero T
		return zero, false
	}
	delete(s.items, id)
	for i, o := range s.order {
		if o == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return v, true
}

// IDs returns the live IDs in creation order.
func (s *Store[T]) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// Len reports how many sessions are live.
func (s *Store[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// CloseAll empties the store and returns every session for the caller to
// close. Used on shutdown so no emulator or child process is left behind.
func (s *Store[T]) CloseAll() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]T, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.items[id])
	}
	s.items = map[string]T{}
	s.order = nil
	return out
}
