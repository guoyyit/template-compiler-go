package engine

import "sync"

// Source 是一份模板源。
type Source struct {
	Name string
	Text string
}

// Store 保存模板源码，并发安全。
type Store struct {
	mu      sync.RWMutex
	sources map[string]Source
}

func NewStore() *Store {
	return &Store{sources: make(map[string]Source)}
}

func (s *Store) Add(name, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources[name] = Source{Name: name, Text: text}
}

func (s *Store) Get(name string) (Source, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src, ok := s.sources[name]
	return src, ok
}

func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.sources))
	for n := range s.sources {
		names = append(names, n)
	}
	return names
}
