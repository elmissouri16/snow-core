package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
)

func TestTUIThemesRenderAndPersist(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 20
	m.layout()
	for _, name := range themeChoices() {
		if err := m.setTheme(name, false); err != nil {
			t.Fatalf("set theme %q: %v", name, err)
		}
		if m.themeName != name || m.app.Cfg.TUI.Theme != name {
			t.Fatalf("theme name=%q config=%q want %q", m.themeName, m.app.Cfg.TUI.Theme, name)
		}
		if got := m.View(); got == "" || !strings.Contains(stripANSI(got), "snow") {
			t.Fatalf("theme %q produced empty view", name)
		}
	}
}

func TestPickerJAndKDoNotAffectComposer(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 20
	m.layout()
	m.pickPermissionMode = true
	m.permissionModeIndex = 1
	m.editor.SetValue("draft")
	_, _ = m.handleKey(teaKeyRunes('j'))
	if m.permissionModeIndex != 2 || m.editor.Value() != "draft" {
		t.Fatalf("j picker index=%d editor=%q", m.permissionModeIndex, m.editor.Value())
	}
	_, _ = m.handleKey(teaKeyRunes('k'))
	if m.permissionModeIndex != 1 || m.editor.Value() != "draft" {
		t.Fatalf("k picker index=%d editor=%q", m.permissionModeIndex, m.editor.Value())
	}
}

func teaKeyRunes(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
