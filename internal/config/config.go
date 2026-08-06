// Package config loads and merges global and project configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultToolOutputBytes is the default per-tool result cap (256 KiB).
const DefaultToolOutputBytes = 262144

// DefaultBashTimeout is the default bash tool timeout.
const DefaultBashTimeout = 120 * time.Second

// DefaultContextCapBytes is the hard cap for injected project context (100 KiB).
const DefaultContextCapBytes = 100 * 1024

// ProviderConfig holds per-provider overrides.
type ProviderConfig struct {
	BaseURL     string `json:"base_url,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
}

// TUIConfig holds TUI preferences.
type TUIConfig struct {
	Theme string `json:"theme,omitempty"`
	Mouse bool   `json:"mouse,omitempty"`
}

// Config is the global snow configuration.
type Config struct {
	DefaultProvider      string                    `json:"default_provider,omitempty"`
	DefaultModel         string                    `json:"default_model,omitempty"`
	PermissionMode       string                    `json:"permission_mode,omitempty"` // ask|allow|deny
	DefaultProjectTrust  string                    `json:"default_project_trust,omitempty"` // ask|always|never
	Thinking             string                    `json:"thinking,omitempty"`
	ToolOutputBytes      int                       `json:"tool_output_bytes,omitempty"`
	BashTimeoutMS        int                       `json:"bash_timeout_ms,omitempty"`
	ContextCapBytes      int                       `json:"context_cap_bytes,omitempty"`
	Providers            map[string]ProviderConfig `json:"providers,omitempty"`
	TUI                  TUIConfig                 `json:"tui,omitempty"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		DefaultProvider:     "opencode-go",
		PermissionMode:      "ask",
		DefaultProjectTrust: "ask",
		Thinking:            "off",
		ToolOutputBytes:     DefaultToolOutputBytes,
		BashTimeoutMS:       int(DefaultBashTimeout / time.Millisecond),
		ContextCapBytes:     DefaultContextCapBytes,
		Providers: map[string]ProviderConfig{
			"opencode-go": {},
			"chatgpt":     {},
		},
		TUI: TUIConfig{Theme: "default", Mouse: false},
	}
}

// GlobalDir returns the snow global config directory.
func GlobalDir() string {
	if d := os.Getenv("SNOW_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".snow"
	}
	return filepath.Join(home, ".snow")
}

// DefaultPaths returns the standard global file paths.
func DefaultPaths() (configPath, authPath, trustPath string) {
	dir := GlobalDir()
	return filepath.Join(dir, "config.json"),
		filepath.Join(dir, "auth.json"),
		filepath.Join(dir, "trust.json")
}

// Load reads the global config file if present and merges onto defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	return cfg, nil
}

// Save writes the config file (creating parent dirs).
func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// BashTimeout returns the configured bash timeout as a duration.
func (c Config) BashTimeout() time.Duration {
	if c.BashTimeoutMS <= 0 {
		return DefaultBashTimeout
	}
	return time.Duration(c.BashTimeoutMS) * time.Millisecond
}

// ToolOutputLimit returns the tool output byte cap.
func (c Config) ToolOutputLimit() int {
	if c.ToolOutputBytes <= 0 {
		return DefaultToolOutputBytes
	}
	return c.ToolOutputBytes
}
