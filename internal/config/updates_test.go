package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateConfigDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if cfg.Updates.CheckOnStartup || cfg.Updates.AutoUpdate {
		t.Fatalf("updates enabled by default: %+v", cfg.Updates)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"updates":{"check_on_startup":false,"auto_update":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "requires check_on_startup") {
		t.Fatalf("inconsistent update settings error = %v", err)
	}
}

func TestProjectConfigurationCannotOverrideUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"updates":{"check_on_startup":true,"auto_update":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	extensions, err := LoadProjectExtensions(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := ApplyProjectPreferences(&cfg, extensions); err != nil {
		t.Fatal(err)
	}
	if cfg.Updates.CheckOnStartup || cfg.Updates.AutoUpdate {
		t.Fatalf("project configuration changed updates: %+v", cfg.Updates)
	}
}
