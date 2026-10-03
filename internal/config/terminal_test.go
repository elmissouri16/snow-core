package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTerminalPreferencesDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"tui":{"theme":"frost","mouse":false,"terminal_progress":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TUI.TerminalTitle || cfg.TUI.Notifications != "unfocused" || cfg.TUI.Mouse {
		t.Fatalf("legacy defaults = %+v", cfg.TUI)
	}
	cfg.TUI.TerminalTitle, cfg.TUI.Notifications = false, "off"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"terminal_progress"`)) {
		t.Fatal("removed terminal progress preference was saved")
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
