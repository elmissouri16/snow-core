package auth

import (
	"sync"
)

// MemoryStore is a non-persistent Store for tests and SDK ephemeral use.
type MemoryStore struct {
	mu   sync.RWMutex
	m    map[string]Credential
	path string
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string]Credential)}
}

// NewMemoryStoreForTest is an alias used by test fixtures.
func NewMemoryStoreForTest() *MemoryStore {
	return NewMemoryStore()
}

// Get implements Store.
func (s *MemoryStore) Get(provider string) (Credential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.m[provider]
	return c, ok
}

// Put implements Store.
func (s *MemoryStore) Put(provider string, cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cred.Provider = provider
	s.m[provider] = cred
	return nil
}

// Delete implements Store.
func (s *MemoryStore) Delete(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, provider)
	return nil
}

// Update implements Store.
func (s *MemoryStore) Update(provider string, fn UpdateFunc) (Credential, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.m[provider]
	next, save, err := fn(current, exists)
	if err != nil {
		return current, exists, err
	}
	if save {
		next.Provider = provider
		s.m[provider] = next
		return next, true, nil
	}
	return next, exists, nil
}

// Path implements Store.
func (s *MemoryStore) Path() string { return s.path }
