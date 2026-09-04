package auth

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
)

// ErrNoCredential is returned when no credential can be resolved for a
// provider (neither store entry nor environment variable).
var ErrNoCredential = errors.New("auth: no credential available")

// FileStore persists credentials as a JSON map keyed by provider in a
// single file (default ~/.snow/auth.json). The file is created and
// maintained with mode 0600 and written atomically (temp file + rename).
type FileStore struct {
	path string
	// mu serializes load-modify-save cycles so concurrent Put/Delete from
	// the same process cannot lose updates.
	mu sync.Mutex
	// Get uses a stat-validated snapshot. Mutating operations never consult it:
	// Update must re-read under the cross-process lock to preserve OAuth rotation.
	cache      map[string]Credential
	cacheInfo  os.FileInfo
	cacheValid bool
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

func (f *FileStore) loadCached() (map[string]Credential, error) {
	if f.cacheValid {
		if info, err := os.Stat(f.path); err == nil && f.cacheInfo != nil && os.SameFile(info, f.cacheInfo) && info.ModTime().Equal(f.cacheInfo.ModTime()) && info.Size() == f.cacheInfo.Size() {
			return f.cache, nil
		}
		f.invalidateCache()
	}
	m, err := f.load()
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(f.path)
	if err == nil {
		f.cache = m
		f.cacheInfo = info
		f.cacheValid = true
	}
	return m, nil
}

func (f *FileStore) invalidateCache() {
	f.cache = nil
	f.cacheInfo = nil
	f.cacheValid = false
}

func cloneCredential(c Credential) Credential {
	out := c
	out.Extra = cloneExtra(c.Extra)
	return out
}

func cloneExtra(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneExtraValue(value)
	}
	return out
}

func cloneExtraValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneExtra(value)
	case map[string]string:
		out := make(map[string]string, len(value))
		maps.Copy(out, value)
		return out
	case []string:
		return slices.Clone(value)
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = cloneExtraValue(value[i])
		}
		return out
	default:
		return value
	}
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
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("auth: open parent for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) || closeErr != nil {
		return fmt.Errorf("auth: sync parent: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

// Get implements Store. A corrupt file surfaces as "no credential" but is
// never overwritten; Put/Delete return the corruption error.
func (f *FileStore) Get(provider string) (Credential, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.loadCached()
	if err != nil {
		return Credential{}, false
	}
	c, ok := m[provider]
	if !ok {
		return Credential{}, false
	}
	c.Provider = provider
	return cloneCredential(c), true
}

// Put implements Store.
func (f *FileStore) Put(provider string, cred Credential) error {
	_, _, err := f.Update(provider, func(Credential, bool) (Credential, bool, error) {
		return cred, true, nil
	})
	return err
}

// Delete implements Store.
func (f *FileStore) Delete(provider string) error {
	return f.withExclusiveLock(func() error {
		m, err := f.load()
		if err != nil {
			return err
		}
		if _, ok := m[provider]; !ok {
			return nil
		}
		delete(m, provider)
		if err := f.save(m); err != nil {
			return err
		}
		f.invalidateCache()
		return nil
	})
}

// Update implements Store. The callback runs under a process-wide advisory
// lock so refresh-token rotation cannot race another Snow process.
func (f *FileStore) Update(provider string, fn UpdateFunc) (Credential, bool, error) {
	var out Credential
	var exists bool
	err := f.withExclusiveLock(func() error {
		m, err := f.load()
		if err != nil {
			return err
		}
		current, ok := m[provider]
		current.Provider = provider
		next, save, err := fn(current, ok)
		if err != nil {
			out, exists = current, ok
			return err
		}
		out, exists = next, ok
		if !save {
			return nil
		}
		next.Provider = provider
		m[provider] = next
		out, exists = next, true
		if err := f.save(m); err != nil {
			return err
		}
		f.invalidateCache()
		return nil
	})
	return out, exists, err
}

// WithRefreshLock serializes one provider's refresh across processes without
// holding the auth store's global read/write lock during network I/O.
func (f *FileStore) WithRefreshLock(provider string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir refresh lock: %w", err)
	}
	sum := sha256.Sum256([]byte(provider))
	lockPath := fmt.Sprintf("%s.%x.refresh.lock", f.path, sum[:8])
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("auth: open refresh lock: %w", err)
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return fmt.Errorf("auth: chmod refresh lock: %w", err)
	}
	if err := lockFile(lock); err != nil {
		return fmt.Errorf("auth: lock refresh: %w", err)
	}
	defer unlockFile(lock)
	return fn()
}

func (f *FileStore) withExclusiveLock(fn func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir lock: %w", err)
	}
	lockPath := f.path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("auth: open lock: %w", err)
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return fmt.Errorf("auth: chmod lock: %w", err)
	}
	if err := lockFile(lock); err != nil {
		return fmt.Errorf("auth: lock store: %w", err)
	}
	defer unlockFile(lock)
	return fn()
}
