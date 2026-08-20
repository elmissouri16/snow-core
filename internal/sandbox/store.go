package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const storeVersion = 1

// Record is the operator-owned association between one canonical project and
// its persistent smolvm machine. It contains policy, never secret values.
type Record struct {
	Project      string    `json:"project"`
	Machine      string    `json:"machine"`
	Executable   string    `json:"executable"`
	Source       string    `json:"source"`
	Profile      string    `json:"profile,omitempty"`
	SourceKind   string    `json:"source_kind"` // image|pack
	GuestCWD     string    `json:"guest_cwd"`
	ReadOnly     bool      `json:"read_only"`
	Network      bool      `json:"network"`
	Stopped      bool      `json:"stopped,omitempty"`
	CPUs         int       `json:"cpus"`
	MemoryMiB    int       `json:"memory_mib"`
	StorageGiB   int       `json:"storage_gib,omitempty"`
	OverlayGiB   int       `json:"overlay_gib,omitempty"`
	EnvAllowlist []string  `json:"env_allowlist,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r Record) clone() Record {
	r.EnvAllowlist = append([]string(nil), r.EnvAllowlist...)
	return r
}

type storeFile struct {
	Version  int               `json:"version"`
	Projects map[string]Record `json:"projects"`
}

// Store serializes exact canonical-project records across Snow processes.
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) Get(project string) (Record, bool, error) {
	if s == nil {
		return Record{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := loadStore(s.path)
	if err != nil {
		return Record{}, false, err
	}
	record, ok := state.Projects[filepath.Clean(project)]
	return record.clone(), ok, nil
}

func (s *Store) update(ctx context.Context, fn func(*storeFile) error) error {
	if s == nil || s.path == "" {
		return errors.New("sandbox: empty state path")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("sandbox: mkdir state: %w", err)
	}
	unlock, err := lockStoreFileContext(ctx, s.path+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	state, err := loadStore(s.path)
	if err != nil {
		return err
	}
	if err := fn(&state); err != nil {
		return err
	}
	return saveStore(s.path, state)
}

func loadStore(path string) (storeFile, error) {
	state := storeFile{Version: storeVersion, Projects: map[string]Record{}}
	if path == "" {
		return state, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("sandbox: read state %s: %w", path, err)
	}
	if len(data) > 1<<20 {
		return state, errors.New("sandbox: state exceeds 1 MiB")
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("sandbox: corrupt state %s: %w", path, err)
	}
	if state.Version != storeVersion {
		return state, fmt.Errorf("sandbox: unsupported state version %d", state.Version)
	}
	if state.Projects == nil {
		state.Projects = map[string]Record{}
	}
	normalized := make(map[string]Record, len(state.Projects))
	for key, record := range state.Projects {
		if !filepath.IsAbs(key) || filepath.Clean(key) != key || record.Project != key {
			return state, fmt.Errorf("sandbox: invalid stored project identity %q", key)
		}
		if err := validateStoredRecord(record); err != nil {
			return state, fmt.Errorf("sandbox: invalid record for %s: %w", key, err)
		}
		normalized[key] = record.clone()
	}
	state.Projects = normalized
	return state, nil
}

func saveStore(path string, state storeFile) error {
	state.Version = storeVersion
	if state.Projects == nil {
		state.Projects = map[string]Record{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("sandbox: encode state: %w", err)
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".snow-sandboxes-*.tmp")
	if err != nil {
		return fmt.Errorf("sandbox: create temporary state: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("sandbox: chmod temporary state: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("sandbox: write state: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sandbox: sync state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("sandbox: close state: %w", err)
	}
	// The temporary file is already 0600. Rename is the final fallible commit:
	// callers must never observe an error after routing state became visible.
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("sandbox: replace state: %w", err)
	}
	ok = true
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
