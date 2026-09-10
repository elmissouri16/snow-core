package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTerminalPreferencesDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"theme":"frost","mouse":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI.TerminalTitle || !cfg.TUI.TerminalProgress || cfg.TUI.Notifications != "unfocused" || cfg.TUI.Mouse {
		t.Fatalf("legacy defaults = %+v", cfg.TUI)
	}
	cfg.TUI.TerminalTitle, cfg.TUI.TerminalProgress, cfg.TUI.Notifications = false, false, "off"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.TUI != cfg.TUI {
		t.Fatalf("disabled preferences lost: %+v, %v", loaded.TUI, err)
	}
}

func TestTerminalNotificationPolicyValidation(t *testing.T) {
	for _, value := range []string{"off", "unfocused", "always"} {
		if err := ValidateTerminalNotifications(value); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"notifications":"sometimes"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid notification policy accepted")
	}
}
