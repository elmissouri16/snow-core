package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCustomThemeInheritanceAndReservedPalette(t *testing.T) {
	custom := config.ThemeFile{Version: 1, Name: "ocean", Extends: "dark", Colors: config.ThemeColors{Accent: config.AdaptiveColor{Light: "#112233", Dark: "39"}}}
	if err := applyCustomTUITheme(custom); err != nil {
		t.Fatal(err)
	}
	if err := applyTUITheme("default"); err != nil {
		t.Fatal(err)
	}
}

func TestThemeRefreshUpdatesThinkingAndWorkingAnimations(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	if err := applyTUITheme("dracula"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applyTUITheme("default") })
	m.refreshThemeStyles()
	if got := m.spinner.Style.GetForeground(); got != colorAccent {
		t.Fatalf("working animation color = %v, want theme accent %v", got, colorAccent)
	}
	if got := m.thinkingSpinner.Style.GetForeground(); got != colorAccent {
		t.Fatalf("thinking animation color = %v, want theme accent %v", got, colorAccent)
	}
}

func TestStartupThemeApplicationDoesNotPersistEffectiveProjectConfig(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	path := filepath.Join(t.TempDir(), "config.json")
	global := config.Default()
	global.TUI.Theme = "dark"
	if err := config.Save(path, global); err != nil {
		t.Fatal(err)
	}
	m.app.ConfigPath = path
	m.app.PersistedCfg = global
	m.app.Cfg = global
	m.app.Cfg.TUI.Theme = "light" // simulate trusted project effective override
	m.app.Cfg.Compaction.Guidance = "project-only"
	if err := m.applyThemeSelection("light", false, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "project-only") {
		t.Fatal("project config leaked to global config")
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.TUI.Theme != "dark" {
		t.Fatalf("startup persisted theme %q", reloaded.TUI.Theme)
	}
}

func TestCustomPickerBindingDrivesRuntimeModelPicker(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	keys, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"picker_down": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	m.keys = keys
	m.pickModel = true
	m.modelList = []protocol.Model{{ID: "a"}, {ID: "b"}}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.modelIndex != 1 {
		t.Fatalf("model index=%d", m.modelIndex)
	}
}

func TestCustomThinkingBindingDrivesRuntimePicker(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	model := m.app.Agent.Model()
	model.SupportsThinking = true
	model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow}
	if err := m.app.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}
	keys, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"thinking": {"ctrl+y"}})
	if err != nil {
		t.Fatal(err)
	}
	m.keys = keys
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlY})
	if !m.pickThinking {
		t.Fatal("custom thinking shortcut did not open picker")
	}
}

func TestEmergencyKeysCannotBeShadowed(t *testing.T) {
	for name, overrides := range map[string]map[string][]string{
		"submit ctrl+c":    {"submit": {"ctrl+c"}},
		"submit esc":       {"submit": {"esc"}},
		"follow-up ctrl+c": {"follow_up": {"ctrl+c"}},
		"follow-up esc":    {"follow_up": {"esc"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := applyKeybindingOverrides(tuiKeys, overrides); err == nil {
				t.Fatal("emergency binding shadow accepted")
			}
		})
	}
	keys, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"abort": {"ctrl+x"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ctrl+x", "ctrl+c", "esc"} {
		found := false
		for _, got := range keys.Abort.Keys() {
			found = found || got == want
		}
		if !found {
			t.Fatalf("abort keys %v missing %q", keys.Abort.Keys(), want)
		}
	}
}

func TestConfiguredHelpAndEmergencyEscape(t *testing.T) {
	keys, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"follow_up": {"ctrl+f"}, "abort": {"ctrl+x"}, "quit": {"ctrl+q"}})
	if err != nil {
		t.Fatal(err)
	}
	var help strings.Builder
	for _, row := range keys.FullHelp() {
		for _, binding := range row {
			help.WriteString(binding.Help().Key)
			help.WriteByte(' ')
		}
	}
	for _, want := range []string{"ctrl+f", "ctrl+x", "ctrl+q"} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("help %q missing %q", help.String(), want)
		}
	}
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"accept": {"esc"}, "close": {"q"}}); err == nil {
		t.Fatal("escape shadowing accepted")
	}
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"follow_up": {"enter"}}); err == nil {
		t.Fatal("busy follow-up shadows steer")
	}
}

func TestKeybindingOverridesEmergencyAndValidation(t *testing.T) {
	keys, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"submit": {"ctrl+s"}, "close": {"q"}})
	if err != nil {
		t.Fatal(err)
	}
	if keys.Submit.Keys()[0] != "ctrl+s" {
		t.Fatalf("submit=%v", keys.Submit.Keys())
	}
	foundEsc := false
	for _, value := range keys.Close.Keys() {
		if value == "esc" {
			foundEsc = true
		}
	}
	if !foundEsc {
		t.Fatalf("close=%v", keys.Close.Keys())
	}
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"unknown": {"x"}}); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := applyKeybindingOverrides(tuiKeys, map[string][]string{"submit": {}}); err == nil {
		t.Fatal("empty mandatory action accepted")
	}
}
