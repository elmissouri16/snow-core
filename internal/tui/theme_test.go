package tui

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTUIThemesRenderAndPersist(t *testing.T) {
	t.Cleanup(func() { _ = applyTUITheme("default") })
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
	want := []string{"default", "frost", "ember", "aurora"}
	got := themeChoices()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("theme choices=%v want %v", got, want)
	}
	for _, name := range got {
		if _, err := makeTUITheme(name); err != nil {
			t.Fatalf("make theme %q: %v", name, err)
		}
	}
	labels := []string{"Snow", "Frost", "Ember", "Aurora"}
	for i, name := range got {
		if label := themeDisplayName(name); label != labels[i] {
			t.Errorf("theme label %q=%q want %q", name, label, labels[i])
		}
	}
}

func TestLegacyThemesRemainHiddenAndSupported(t *testing.T) {
	choices := strings.Join(themeChoices(), ",")
	for _, name := range []string{"dark", "light", "high-contrast", "nord", "dracula", "gruvbox"} {
		if strings.Contains(choices, name) {
			t.Errorf("legacy theme %q remains selectable", name)
		}
		if !config.IsBuiltInTUITheme(name) {
			t.Errorf("legacy theme %q is no longer reserved", name)
		}
		if _, err := makeTUITheme(name); err != nil {
			t.Errorf("legacy theme %q no longer resolves: %v", name, err)
		}
	}
}

func TestLegacyThemeCyclesIntoSelectableCatalog(t *testing.T) {
	choices := themeChoices()
	if got := cycleThemeValue(choices, "dracula", 1); got != "default" {
		t.Fatalf("forward legacy cycle=%q want default", got)
	}
	if got := cycleThemeValue(choices, "dracula", -1); got != "aurora" {
		t.Fatalf("backward legacy cycle=%q want aurora", got)
	}
}

func TestBuiltInThemeTextContrast(t *testing.T) {
	for _, name := range themeChoices() {
		for _, tt := range []struct {
			mode       string
			background string
			dark       bool
		}{
			{mode: "light", background: "#FFFFFF"},
			{mode: "dark", background: "#0D1117", dark: true},
		} {
			t.Run(name+"/"+tt.mode, func(t *testing.T) {
				theme, err := makeTUITheme(name)
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
}

func TestThemeSwitchRerendersDurableMarkdownAndPreservesComposer(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	t.Cleanup(func() { _ = applyTUITheme("default") })
	m.width, m.height = 100, 30
	m.layout()
	message := protocol.NewAssistantMessage(
		"themed-markdown",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{{Type: protocol.BlockText, Text: "# Result\n\nUse `snow`."}},
		protocol.StopStop,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	beforePlain := stripANSI(strings.Join(m.lines, "\n"))
	beforeRenderer := m.md.renderer
	m.editor.SetValue("draft prompt")

	if err := m.setTheme("ember", false); err != nil {
		t.Fatal(err)
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); got != beforePlain {
		t.Fatalf("theme switch changed transcript text\nbefore: %q\nafter:  %q", beforePlain, got)
	}
	if m.editor.Value() != "draft prompt" {
		t.Fatalf("theme switch changed composer to %q", m.editor.Value())
	}
	if m.md.renderer == nil || m.md.renderer == beforeRenderer {
		t.Fatal("theme switch did not recreate the markdown renderer")
	}
	theme, _ := makeTUITheme("ember")
	want := resolvedThemeColor(theme.soft, lipgloss.HasDarkBackground())
	if got := dereferenceString(m.md.style.Text.Color); got != want {
		t.Fatalf("active markdown foreground=%q want %q", got, want)
	}
}

func TestMarkdownStylesUseSemanticThemeColors(t *testing.T) {
	theme, err := makeTUITheme("ember")
	if err != nil {
		t.Fatal(err)
	}
	style := markdownStyleForTheme(theme, true, false)
	if got := dereferenceString(style.Text.Color); got != "#FFF7ED" {
		t.Fatalf("markdown foreground=%q want #FFF7ED", got)
	}
	if got := dereferenceString(style.H1.Color); got != "#FDBA74" {
		t.Fatalf("markdown heading=%q want #FDBA74", got)
	}
	if got := dereferenceString(style.Code.Color); got != "#FDE047" {
		t.Fatalf("markdown code=%q want #FDE047", got)
	}
	thinking := markdownStyleForTheme(theme, true, true)
	if got := dereferenceString(thinking.Text.Color); got != "#B8A99A" {
		t.Fatalf("thinking foreground=%q want #B8A99A", got)
	}
	for _, background := range []*string{style.Document.BackgroundColor, style.Code.BackgroundColor, style.CodeBlock.BackgroundColor} {
		if background != nil {
			t.Fatalf("theme introduced markdown background %q", *background)
		}
	}
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
