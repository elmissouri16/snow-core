package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func TestTerminalSettingsPreserveOtherPreferences(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	a.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(a.ConfigPath, a.PersistedCfg); err != nil {
		t.Fatal(err)
	}
	_, err := config.Update(a.ConfigPath, func(latest *config.Config) error {
		latest.TUI.Theme = "frost"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Cfg.TUI.Theme = "ember" // Runtime-only selection must remain live.
	if err := a.UpdateTerminalSettings(TerminalSettingsUpdate{Title: new(false), Notifications: new("always")}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TUI.Theme != "frost" || loaded.TUI.TerminalTitle || loaded.TUI.Notifications != "always" || !loaded.TUI.TerminalProgress {
		t.Fatalf("persisted = %+v", loaded.TUI)
	}
	if a.Cfg.TUI.Theme != "ember" || a.Cfg.TUI.TerminalTitle || a.Cfg.TUI.Notifications != "always" {
		t.Fatalf("live = %+v", a.Cfg.TUI)
	}
}

func TestTerminalSettingsFailureLeavesLiveStateUntouched(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	before := a.Cfg.TUI
	if err := a.UpdateTerminalSettings(TerminalSettingsUpdate{Notifications: new("invalid")}); err == nil {
		t.Fatal("invalid policy accepted")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a.ConfigPath = filepath.Join(blocker, "config.json")
	if err := a.UpdateTerminalSettings(TerminalSettingsUpdate{Progress: new(false)}); err == nil {
		t.Fatal("invalid config path saved")
	}
	if a.Cfg.TUI != before {
		t.Fatal("failed save changed live preferences")
	}
}
