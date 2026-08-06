package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoCredential is returned when no credential can be resolved for a
// provider (neither store entry nor environment variable).
var ErrNoCredential = errors.New("auth: no credential available")

// FileStore persists credentials as a JSON map keyed by provider in a
// single file (default ~/.snow/auth.json). The file is created and
// maintained with mode 0600 and written atomically (temp file + rename).
type FileStore struct {
	path string
}

// NewFileStore creates a store backed by path. The file is not touched
// until the first Put/Delete; Get on a missing file returns (zero, false).
func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("auth: empty store path")
	}
	return &FileStore{path: path}, nil
}

// Path implements Store.
func (f *FileStore) Path() string { return f.path }

// load reads the current credential map. A missing file yields an empty
// map; a corrupt file returns an error so callers never silently overwrite
// damaged data.
func (f *FileStore) load() (map[string]Credential, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil
		}
		return nil, fmt.Errorf("auth: read store: %w", err)
	}
	if len(data) == 0 {
		return map[string]Credential{}, nil
	}
	var m map[string]Credential
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("auth: corrupt store %s: %w", f.path, err)
	}
	if m == nil {
		m = map[string]Credential{}
	}
	return m, nil
}

// persistCredential is a defined type without the redacting MarshalJSON
// method, so json.Marshal uses the plain struct encoding for disk writes.
type persistCredential Credential

// save atomically writes the credential map with mode 0600.
func (f *FileStore) save(m map[string]Credential) error {
	pm := make(map[string]persistCredential, len(m))
	for k, v := range m {
		pm[k] = persistCredential(v)
	}
	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: marshal store: %w", err)
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("auth: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return fmt.Errorf("auth: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("auth: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("auth: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("auth: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("auth: close temp: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("auth: rename: %w", err)
	}
	// Ensure the final file keeps 0600 even if a stale file existed with
	// looser permissions (rename preserves the temp file's mode on most
	// systems; chmod is belt-and-braces).
	if err := os.Chmod(f.path, 0o600); err != nil {
		return fmt.Errorf("auth: chmod store: %w", err)
	}
	return nil
}

// Get implements Store.
func (f *FileStore) Get(provider string) (Credential, bool) {
	m, err := f.load()
	if err != nil {
		return Credential{}, false
	}
	c, ok := m[provider]
	if !ok {
		return Credential{}, false
	}
	c.Provider = provider
	return c, true
}

// Put implements Store.
func (f *FileStore) Put(provider string, cred Credential) error {
	m, err := f.load()
	if err != nil {
		return err
	}
	cred.Provider = provider
	m[provider] = cred
	return f.save(m)
}

// Delete implements Store.
func (f *FileStore) Delete(provider string) error {
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[provider]; !ok {
		return nil // idempotent
	}
	delete(m, provider)
	return f.save(m)
}

// ResolveAPIKey resolves an API-key credential for provider by checking the
// store first and then the environment variable envVar. It returns
// ErrNoCredential when neither source yields a non-empty key. Only
// api_key-type store entries are accepted; an oauth entry is ignored for
// API-key resolution.
func ResolveAPIKey(store Store, envVar, provider string) (Credential, error) {
	if store != nil {
		if c, ok := store.Get(provider); ok && c.Type == CredentialAPIKey && c.Key != "" {
			return c, nil
		}
	}
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return Credential{Provider: provider, Type: CredentialAPIKey, Key: v}, nil
		}
	}
	return Credential{}, ErrNoCredential
}
