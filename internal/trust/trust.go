// Package trust implements the project trust decision store. Trust gates
// loading of project-local resources (.snow/config.json, plugins); it is an
// input-loading guard, NOT a sandbox (see IMPLEMENTATION.md §8.4).
package trust

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Level is a trust decision.
type Level string

const (
	LevelAsk   Level = "ask"
	LevelAllow Level = "allow"
	LevelDeny  Level = "deny"
)

// Store persists trust decisions keyed by canonical project path.
type Store struct {
	mu    sync.Mutex
	path  string
	items map[string]Level
}

// Resolution is the effective project trust policy for one canonical path.
type Resolution struct {
	Path   string
	Level  Level
	Prompt bool
}

// ParseDefault normalizes the configured project-trust default. always/never
// remain accepted aliases for the previously documented spelling.
func ParseDefault(value string) (Level, error) {
	switch Level(strings.ToLower(strings.TrimSpace(value))) {
	case "", LevelAsk:
		return LevelAsk, nil
	case LevelAllow, "always":
		return LevelAllow, nil
	case LevelDeny, "never":
		return LevelDeny, nil
	default:
		return "", fmt.Errorf("trust: invalid default_project_trust %q (use ask, allow, or deny)", value)
	}
}

// CanonicalPath returns a stable absolute trust key. Existing symlink aliases
// resolve to their target so preflight and runtime cannot disagree.
func CanonicalPath(cwd string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("trust: canonical path: %w", err)
	}
	abs = filepath.Clean(abs)
	// Resolve the nearest existing ancestor, then reattach any nonexistent
	// suffix. This keeps /tmp and /tmp/new/project on the same canonical key on
	// systems where /tmp itself is a symlink (notably macOS).
	base := abs
	var suffix []string
	for {
		if _, err := os.Lstat(base); err == nil {
			break
		}
		parent := filepath.Dir(base)
		if parent == base {
			break
		}
		suffix = append(suffix, filepath.Base(base))
		base = parent
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		abs = filepath.Clean(resolved)
		for i := len(suffix) - 1; i >= 0; i-- {
			abs = filepath.Join(abs, suffix[i])
		}
	}
	return abs, nil
}

// Resolve applies the nearest persisted decision before the configured
// default. An explicit/persisted ask means an interactive surface should ask.
func Resolve(cwd, configuredDefault string, store *Store) (Resolution, error) {
	path, err := CanonicalPath(cwd)
	if err != nil {
		return Resolution{}, err
	}
	fallback, err := ParseDefault(configuredDefault)
	if err != nil {
		return Resolution{}, err
	}
	if store != nil {
		if level, ok := store.Get(path); ok {
			if level == LevelAsk {
				return Resolution{Path: path, Level: LevelAsk, Prompt: true}, nil
			}
			return Resolution{Path: path, Level: level}, nil
		}
	}
	if fallback == LevelAsk {
		return Resolution{Path: path, Level: LevelAsk, Prompt: true}, nil
	}
	return Resolution{Path: path, Level: fallback}, nil
}

// New loads or creates the trust store at path.
func New(path string) (*Store, error) {
	items, err := loadItems(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, items: items}, nil
}

func loadItems(path string) (map[string]Level, error) {
	items := make(map[string]Level)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return items, nil
		}
		return nil, fmt.Errorf("trust: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return items, nil
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("trust: corrupt %s: %w", path, err)
	}
	if items == nil {
		items = make(map[string]Level)
	}
	normalized := make(map[string]Level, len(items))
	for path, level := range items {
		if level != LevelAsk && level != LevelAllow && level != LevelDeny {
			return nil, fmt.Errorf("trust: invalid decision %q for %s", level, path)
		}
		// Persisted keys are historical identities. Only normalize them
		// lexically: re-evaluating symlinks here could transfer an old allow to a
		// different target after the alias was replaced.
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("trust: stored path %q is not absolute", path)
		}
		normalized[filepath.Clean(path)] = level
	}
	return normalized, nil
}

// Get returns the decision for a directory, walking parents when no exact
// match exists (closest ancestor wins, like pi).
func (s *Store) Get(cwd string) (Level, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := CanonicalPath(cwd)
	if err != nil {
		dir = filepath.Clean(cwd)
	}
	for {
		if lvl, ok := s.items[dir]; ok {
			return lvl, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Set records a decision and persists it.
func (s *Store) Set(cwd string, lvl Level) error {
	if lvl != LevelAsk && lvl != LevelAllow && lvl != LevelDeny {
		return fmt.Errorf("trust: invalid decision %q", lvl)
	}
	canonical, err := CanonicalPath(cwd)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		if s.items == nil {
			s.items = make(map[string]Level)
		}
		s.items[canonical] = lvl
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("trust: mkdir: %w", err)
	}
	unlock, err := lockStoreFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	// Reload under the interprocess lock so independent Snow processes merge
	// exact-project decisions instead of replacing each other's snapshots.
	items, err := loadItems(s.path)
	if err != nil {
		return err
	}
	items[canonical] = lvl
	if err := s.save(items); err != nil {
		return err
	}
	// Publish only after the durable write succeeds. Callers must never observe
	// a trust decision that was rejected by storage.
	s.items = items
	return nil
}

func (s *Store) save(items map[string]Level) error {
	if s.path == "" {
		return nil // in-memory only
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("trust: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	f, err := os.CreateTemp(dir, ".snow-trust-*.tmp")
	if err != nil {
		return fmt.Errorf("trust: create temporary file: %w", err)
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
		return fmt.Errorf("trust: chmod: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("trust: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("trust: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("trust: close: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("trust: replace: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("trust: chmod store: %w", err)
	}
	ok = true
	return nil
}
