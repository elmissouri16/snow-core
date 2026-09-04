package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateConfigDefaultsAndIgnoresRemovedAutoUpdate(t *testing.T) {
	cfg := Default()
	if cfg.Updates.CheckOnStartup {
		t.Fatalf("startup update check enabled by default: %+v", cfg.Updates)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"updates":{"check_on_startup":true,"auto_update":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Updates.CheckOnStartup {
		t.Fatal("startup update check was not loaded")
	}
}

func TestProjectConfigurationCannotOverrideUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"updates":{"check_on_startup":true}}`), 0o600); err != nil {
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
	if cfg.Updates.CheckOnStartup {
		t.Fatalf("project configuration changed updates: %+v", cfg.Updates)
	}
}
