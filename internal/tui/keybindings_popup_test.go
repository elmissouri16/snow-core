package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
)

func newKeybindingsTestModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	return m
}

func keybindingActionIndex(name string) int {
	for i, action := range keybindingActions {
		if action.name == name {
			return i
		}
	}
	return -1
}

func TestKeybindingActionInventoryMatchesConfig(t *testing.T) {
	var got []string
	for _, action := range keybindingActions {
		got = append(got, action.name)
	}
	if !slices.Equal(got, config.KeybindingActions) {
		t.Fatalf("TUI actions=%v config actions=%v", got, config.KeybindingActions)
	}
}

func TestKeybindingsCommandAndSettingsEntry(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.runCommand("/keybindings")
	if !m.pickKeybindings || m.pickSettings {
		t.Fatalf("direct command popup=%v settings=%v", m.pickKeybindings, m.pickSettings)
	}
	view := stripANSI(m.renderKeybindings())
	for _, want := range []string{"Keybindings · Global", "Composer", "Submit", "default", "Enter edit", "S scope"} {
		if !strings.Contains(view, want) {
			t.Fatalf("popup missing %q: %q", want, view)
		}
	}
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickKeybindings || m.pickSettings {
		t.Fatalf("direct close popup=%v settings=%v", m.pickKeybindings, m.pickSettings)
	}

	_, _ = m.startSettings()
	m.settingsIndex = settingsKeybindings
	_, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickKeybindings || m.pickSettings || !m.keybindingsReturnToSettings {
		t.Fatalf("settings handoff popup=%v settings=%v return=%v", m.pickKeybindings, m.pickSettings, m.keybindingsReturnToSettings)
	}
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickKeybindings || !m.pickSettings {
		t.Fatalf("settings return popup=%v settings=%v", m.pickKeybindings, m.pickSettings)
	}
}

func TestKeybindingsCommandRejectsArguments(t *testing.T) {
	m := newKeybindingsTestModel(t)
	before := len(m.lines)
	_, _ = m.runCommand("/keybindings extra")
	if m.pickKeybindings || len(m.lines) != before+1 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "takes no arguments") {
		t.Fatalf("popup=%v lines=%v", m.pickKeybindings, m.lines[before:])
	}
}

func TestKeybindingsReplaceCaptureSavesAndAppliesGlobally(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("submit")
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.keybindingsEditIndex = len(m.keybindingsDraft) // Replace all row.
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	if !slices.Equal(m.keybindingsDraft, []string{"ctrl+x"}) || m.keybindingsCapture != keybindingCaptureNone {
		t.Fatalf("captured draft=%v mode=%v", m.keybindingsDraft, m.keybindingsCapture)
	}
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.keybindingsEditing || m.keybindingsError != "" || !slices.Equal(m.keys.Submit.Keys(), []string{"ctrl+x"}) {
		t.Fatalf("saved editing=%v error=%q keys=%v", m.keybindingsEditing, m.keybindingsError, m.keys.Submit.Keys())
	}
	loaded, diagnostics := config.LoadKeybindings(config.GlobalDir(), "", false)
	if len(diagnostics) != 0 || !slices.Equal(loaded.Bindings["submit"], []string{"ctrl+x"}) {
		t.Fatalf("loaded=%+v diagnostics=%+v", loaded, diagnostics)
	}
	info, err := os.Stat(filepath.Join(config.GlobalDir(), "keybindings.yaml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("global file mode=%v err=%v", info, err)
	}
}

func TestKeybindingsAddDeleteAltCaptureAndRestoreEmergencyAbort(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("abort")
	m.openKeybindingEditor()
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if slices.Contains(m.keybindingsDraft, "ctrl+c") {
		t.Fatalf("delete did not remove ctrl+c: %v", m.keybindingsDraft)
	}
	m.keybindingsEditIndex = len(m.keybindingsDraft) + 1 // Add key row.
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}, Alt: true})
	if !slices.Contains(m.keybindingsDraft, "alt+z") {
		t.Fatalf("Alt capture draft=%v error=%q", m.keybindingsDraft, m.keybindingsError)
	}
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	for _, mandatory := range []string{"esc", "alt+z", "ctrl+c"} {
		if !slices.Contains(m.keys.Abort.Keys(), mandatory) {
			t.Fatalf("runtime abort=%v missing %q", m.keys.Abort.Keys(), mandatory)
		}
	}
}

func TestKeybindingsNamedKeyCaptureFlowsThroughHandler(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("close")
	m.openKeybindingEditor()
	m.keybindingsEditIndex = len(m.keybindingsDraft)
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !slices.Equal(m.keybindingsDraft, []string{"enter"}) || m.keybindingsCapture != keybindingCaptureNone {
		t.Fatalf("named capture draft=%v capture=%v", m.keybindingsDraft, m.keybindingsCapture)
	}
}

func TestKeybindingsCollisionDoesNotWriteOrApply(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("submit")
	m.openKeybindingEditor()
	m.keybindingsDraft = []string{"ctrl+t"} // Thinking already owns ctrl+t.
	m.saveKeybindingDraft()
	if m.keybindingsError == "" || !m.keybindingsEditing {
		t.Fatalf("collision error=%q editing=%v", m.keybindingsError, m.keybindingsEditing)
	}
	if !slices.Equal(m.keys.Submit.Keys(), []string{"enter"}) {
		t.Fatalf("collision changed runtime submit=%v", m.keys.Submit.Keys())
	}
	if _, err := os.Stat(filepath.Join(config.GlobalDir(), "keybindings.yaml")); !os.IsNotExist(err) {
		t.Fatalf("collision created file: %v", err)
	}
}

func TestKeybindingsResetRemovesScopeOverride(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("submit")
	if err := m.persistKeybinding("submit", []string{"ctrl+x"}, false); err != nil {
		t.Fatal(err)
	}
	m.resetSelectedKeybinding()
	if m.keybindingsError != "" || !slices.Equal(m.keys.Submit.Keys(), []string{"enter"}) {
		t.Fatalf("reset error=%q submit=%v", m.keybindingsError, m.keys.Submit.Keys())
	}
	loaded, diagnostics := config.LoadKeybindings(config.GlobalDir(), "", false)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if _, exists := loaded.Bindings["submit"]; exists {
		t.Fatalf("reset retained override: %+v", loaded.Bindings)
	}
}

func TestKeybindingsProjectScopeOverridesAndInherits(t *testing.T) {
	m := newKeybindingsTestModel(t)
	m.app.ProjectAllowed = true
	m.app.ProjectInputRoot = m.app.CWD()
	_, _ = m.startKeybindings(false)
	m.toggleKeybindingScope()
	if m.keybindingsScope != keybindingScopeProject {
		t.Fatalf("scope=%v error=%q", m.keybindingsScope, m.keybindingsError)
	}
	m.keybindingsIndex = keybindingActionIndex("models")
	if err := m.persistKeybinding("models", []string{"alt+z"}, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.keys.Models.Keys(), []string{"alt+z"}) {
		t.Fatalf("project models=%v", m.keys.Models.Keys())
	}
	path := filepath.Join(m.app.ProjectInputRoot, ".snow", "keybindings.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	m.resetSelectedKeybinding()
	if !slices.Equal(m.keys.Models.Keys(), []string{"alt+m"}) || !strings.Contains(m.keybindingsStatus, "inherits") {
		t.Fatalf("inherited models=%v status=%q error=%q", m.keys.Models.Keys(), m.keybindingsStatus, m.keybindingsError)
	}
}

func TestKeybindingsEditorUsesSelectedScopeInsteadOfEffectiveProjectValue(t *testing.T) {
	m := newKeybindingsTestModel(t)
	m.app.ProjectAllowed = true
	m.app.ProjectInputRoot = m.app.CWD()
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("models")
	if err := m.persistKeybinding("models", []string{"alt+x"}, false); err != nil {
		t.Fatal(err)
	}
	m.toggleKeybindingScope()
	if err := m.persistKeybinding("models", []string{"alt+z"}, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.keys.Models.Keys(), []string{"alt+z"}) {
		t.Fatalf("effective project keys=%v", m.keys.Models.Keys())
	}
	m.keybindingsScope = keybindingScopeGlobal
	m.openKeybindingEditor()
	if !slices.Equal(m.keybindingsDraft, []string{"alt+x"}) {
		t.Fatalf("global draft leaked effective project value: %v", m.keybindingsDraft)
	}
	m.keybindingsDraft = []string{"alt+y"}
	m.saveKeybindingDraft()
	if !strings.Contains(m.keybindingsStatus, "project override remains active") || !slices.Equal(m.keys.Models.Keys(), []string{"alt+z"}) {
		t.Fatalf("status=%q effective=%v", m.keybindingsStatus, m.keys.Models.Keys())
	}
	m.resetSelectedKeybinding()
	if !strings.Contains(m.keybindingsStatus, "global override removed") || !slices.Equal(m.keys.Models.Keys(), []string{"alt+z"}) {
		t.Fatalf("reset status=%q effective=%v", m.keybindingsStatus, m.keys.Models.Keys())
	}
}

func TestKeybindingsLowercaseControlsRemainAvailableForConfiguredNavigation(t *testing.T) {
	m := newKeybindingsTestModel(t)
	keys, err := applyKeybindingOverrides(m.keys, map[string][]string{"picker_down": {"s"}})
	if err != nil {
		t.Fatal(err)
	}
	m.keys = keys
	_, _ = m.startKeybindings(false)
	_, _ = m.handleKeybindingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if m.keybindingsIndex != 1 || m.keybindingsScope != keybindingScopeGlobal {
		t.Fatalf("index=%d scope=%v", m.keybindingsIndex, m.keybindingsScope)
	}
}

func TestKeybindingsCaptureAcceptsEscapeAndRejectsPaste(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = keybindingActionIndex("close")
	m.openKeybindingEditor()
	m.keybindingsCapture = keybindingCaptureReplaceAll
	m.captureKeybinding(tea.KeyMsg{Type: tea.KeyEsc})
	if !slices.Equal(m.keybindingsDraft, []string{"esc"}) {
		t.Fatalf("escape draft=%v error=%q", m.keybindingsDraft, m.keybindingsError)
	}
	m.keybindingsCapture = keybindingCaptureAdd
	m.captureKeybinding(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("many"), Paste: true})
	if m.keybindingsError == "" || m.keybindingsCapture != keybindingCaptureAdd {
		t.Fatalf("paste error=%q capture=%v", m.keybindingsError, m.keybindingsCapture)
	}
}

func TestKeybindingsRejectsProjectScopeWhenUntrustedAndBusyCommand(t *testing.T) {
	m := newKeybindingsTestModel(t)
	_, _ = m.startKeybindings(false)
	m.toggleKeybindingScope()
	if m.keybindingsScope != keybindingScopeGlobal || !strings.Contains(m.keybindingsError, "trusted") {
		t.Fatalf("scope=%v error=%q", m.keybindingsScope, m.keybindingsError)
	}
	m.pickKeybindings = false
	m.busy = true
	m.editor.SetValue("/keybindings")
	before := len(m.lines)
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickKeybindings || len(m.lines) != before+1 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "wait") {
		t.Fatalf("busy popup=%v lines=%v", m.pickKeybindings, m.lines[before:])
	}
	if m.editor.Value() != "" {
		t.Fatalf("busy command remained in composer: %q", m.editor.Value())
	}
}

func TestKeybindingsPopupRemainsBoundedOnSmallTerminal(t *testing.T) {
	m := newKeybindingsTestModel(t)
	m.width = 44
	m.height = 12
	m.layout()
	_, _ = m.startKeybindings(false)
	m.keybindingsIndex = len(keybindingActions) - 1
	card := m.renderKeybindings()
	if width := transcriptSelectionBlockWidth(card); width > m.width {
		t.Fatalf("card width=%d terminal=%d", width, m.width)
	}
	if height := strings.Count(card, "\n") + 1; height > m.height {
		t.Fatalf("card height=%d terminal=%d", height, m.height)
	}
	if plain := stripANSI(card); !strings.Contains(plain, "Confirm") {
		t.Fatalf("selected tail action is not visible: %q", plain)
	}
}
