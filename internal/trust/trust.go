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

// New loads or creates the trust store at path.
func New(path string) (*Store, error) {
	s := &Store{path: path, items: make(map[string]Level)}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("trust: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.items); err != nil {
		return nil, fmt.Errorf("trust: corrupt %s: %w", path, err)
	}
	if s.items == nil {
		s.items = make(map[string]Level)
	}
	return s, nil
}

// Get returns the decision for a directory, walking parents when no exact
// match exists (closest ancestor wins, like pi).
func (s *Store) Get(cwd string) (Level, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := cwd
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[string]Level)
	}
	s.items[cwd] = lvl
	return s.save()
}

func (s *Store) save() error {
	if s.path == "" {
		return nil // in-memory only
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("trust: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s.items, "", "  ")
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
	ok = true
	return nil
}

// HasSensitiveResources reports whether a project directory contains
// trust-sensitive resources that should trigger a trust prompt.
func HasSensitiveResources(cwd string) bool {
	for _, p := range []string{
		filepath.Join(cwd, ".snow", "config.json"),
		filepath.Join(cwd, ".snow", "plugins"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
