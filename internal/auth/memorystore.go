package auth

import (
	"sync"
)

// MemoryStore is a non-persistent Store for tests and SDK ephemeral use.
type MemoryStore struct {
	mu        sync.RWMutex
	m         map[string]Credential
	path      string
	refreshMu sync.Mutex
	refreshes map[string]*sync.Mutex
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string]Credential)}
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

// Accounts enumerates only persisted aliases.
func (s *MemoryStore) Accounts(providerID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return storedAccounts(providerID, s.m), nil
}

// WithRefreshLock coordinates refresh and login across services sharing a store
// without holding the credential map lock during network operations.
func (s *MemoryStore) WithRefreshLock(key string, fn func() error) error {
	if fn == nil {
		return nil
	}
	s.refreshMu.Lock()
	if s.refreshes == nil {
		s.refreshes = make(map[string]*sync.Mutex)
	}
	lock := s.refreshes[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.refreshes[key] = lock
	}
	s.refreshMu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	return fn()
}
