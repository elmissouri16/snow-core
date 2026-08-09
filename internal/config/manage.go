package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

// UpdateMCPServers atomically edits only the mcp_servers object while
// preserving unrelated and unknown top-level configuration fields.
func UpdateMCPServers(path string, global bool, update func(map[string]publicmcp.ServerSpec) error) error {
	return updateSection(path, global, "mcp_servers", func(raw json.RawMessage) (json.RawMessage, error) {
		servers := map[string]publicmcp.ServerSpec{}
		if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &servers); err != nil {
				return nil, fmt.Errorf("config: parse mcp_servers: %w", err)
			}
		}
		if err := update(servers); err != nil {
			return nil, err
		}
		return json.Marshal(servers)
	})
}

// UpdateSkills atomically edits only the global skills object.
func UpdateSkills(path string, update func(*SkillsConfig) error) error {
	return updateSection(path, true, "skills", func(raw json.RawMessage) (json.RawMessage, error) {
		cfg := SkillsConfig{Overrides: map[string]bool{}}
		if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("config: parse skills: %w", err)
			}
		}
		if cfg.Overrides == nil {
			cfg.Overrides = map[string]bool{}
		}
		if err := update(&cfg); err != nil {
			return nil, err
		}
		return json.Marshal(cfg)
	})
}

// UpdateProjectSkills atomically edits only a project's skills policy.
func UpdateProjectSkills(path string, update func(*ProjectSkillsConfig) error) error {
	return updateSection(path, false, "skills", func(raw json.RawMessage) (json.RawMessage, error) {
		cfg := ProjectSkillsConfig{Overrides: map[string]bool{}}
		if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("config: parse project skills: %w", err)
			}
		}
		if cfg.Overrides == nil {
			cfg.Overrides = map[string]bool{}
		}
		if err := update(&cfg); err != nil {
			return nil, err
		}
		return json.Marshal(cfg)
	})
}

func updateSection(path string, global bool, key string, update func(json.RawMessage) (json.RawMessage, error)) error {
	if path == "" {
		return errors.New("config: empty path")
	}
	root := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	next, err := update(root[key])
	if err != nil {
		return err
	}
	root[key] = next
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	mode := os.FileMode(0o644)
	if global {
		mode = 0o600
	} else if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	return atomicWrite(path, encoded, mode)
}

func atomicWrite(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	file, err := os.CreateTemp(dir, ".snow-config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temporary file: %w", err)
	}
	tmp := file.Name()
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return fmt.Errorf("config: chmod temporary file: %w", err)
	}
	if _, err = io.Copy(file, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("config: write temporary file: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("config: sync temporary file: %w", err)
	}
	if err = file.Close(); err != nil {
		file = nil
		return fmt.Errorf("config: close temporary file: %w", err)
	}
	file = nil
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: replace %s: %w", path, err)
	}
	return nil
}
