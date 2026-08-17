package tui

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func TestBuiltInThemeChoices(t *testing.T) {
	want := []string{"default", "dark", "light", "high-contrast", "nord", "dracula", "gruvbox"}
	got := themeChoices()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("theme choices=%v want %v", got, want)
	}
	for _, name := range got {
		if _, err := makeTUITheme(name); err != nil {
			t.Fatalf("make theme %q: %v", name, err)
		}
	}
}

func TestBuiltInThemeTextContrast(t *testing.T) {
	tests := []struct {
		name       string
		background string
		dark       bool
	}{
		{name: "default", background: "#FFFFFF"},
		{name: "default", background: "#0D1117", dark: true},
		{name: "dark", background: "#0D1117", dark: true},
		{name: "light", background: "#FFFFFF"},
		{name: "high-contrast", background: "#FFFFFF"},
		{name: "high-contrast", background: "#000000", dark: true},
		{name: "nord", background: "#2E3440", dark: true},
		{name: "dracula", background: "#282A36", dark: true},
		{name: "gruvbox", background: "#282828", dark: true},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.background, func(t *testing.T) {
			theme, err := makeTUITheme(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			colors := map[string]lipgloss.TerminalColor{
				"accent": theme.accent, "muted": theme.muted, "foreground": theme.soft,
				"warning": theme.warn, "error": theme.err, "success": theme.ok,
			}
			for role, color := range colors {
				foreground := resolvedHexColor(t, color, tt.dark)
				if ratio := contrastRatio(t, foreground, tt.background); ratio < 4.5 {
					t.Errorf("%s %s contrast %.2f with %s; want >= 4.5", role, foreground, ratio, tt.background)
				}
			}
			separator := resolvedHexColor(t, theme.sep, tt.dark)
			if ratio := contrastRatio(t, separator, tt.background); ratio < 3 {
				t.Errorf("separator %s contrast %.2f with %s; want >= 3", separator, ratio, tt.background)
			}
		})
	}
}

func resolvedHexColor(t *testing.T, color lipgloss.TerminalColor, dark bool) string {
	t.Helper()
	switch color := color.(type) {
	case lipgloss.Color:
		return string(color)
	case lipgloss.AdaptiveColor:
		if dark {
			return color.Dark
		}
		return color.Light
	default:
		t.Fatalf("unsupported test color type %T", color)
		return ""
	}
}

func contrastRatio(t *testing.T, foreground, background string) float64 {
	t.Helper()
	lighter, darker := relativeLuminance(t, foreground), relativeLuminance(t, background)
	if darker > lighter {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(t *testing.T, color string) float64 {
	t.Helper()
	if len(color) != 7 || color[0] != '#' {
		t.Fatalf("test color %q is not #RRGGBB", color)
	}
	channels := make([]float64, 3)
	for i := range channels {
		value, err := strconv.ParseUint(color[1+i*2:3+i*2], 16, 8)
		if err != nil {
			t.Fatalf("parse test color %q: %v", color, err)
		}
		channel := float64(value) / 255
		if channel <= 0.04045 {
			channels[i] = channel / 12.92
		} else {
			channels[i] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
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
