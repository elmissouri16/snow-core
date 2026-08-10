package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/snow-core/snow/internal/config"
)

// tuiTheme is intentionally small: semantic roles keep meaning readable even
// when a terminal has no color support, while the color values adapt to light
// and dark terminal backgrounds.
type tuiTheme struct {
	name     string
	accent   lipgloss.TerminalColor
	muted    lipgloss.TerminalColor
	soft     lipgloss.TerminalColor
	warn     lipgloss.TerminalColor
	err      lipgloss.TerminalColor
	ok       lipgloss.TerminalColor
	sep      lipgloss.TerminalColor
	composer lipgloss.TerminalColor
}

func makeTUITheme(name string) (tuiTheme, error) {
	if name == "" {
		name = "default"
	}
	t := tuiTheme{name: name}
	switch name {
	case "default":
		t.accent = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
		t.muted = lipgloss.AdaptiveColor{Light: "#57606a", Dark: "#8b949e"}
		t.soft = lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#f0f6fc"}
		t.warn = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#e3b341"}
		t.err = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff7b72"}
		t.ok = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#7ee787"}
		t.sep = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#30363d"}
		t.composer = lipgloss.AdaptiveColor{Light: "#f6f8fa", Dark: "#262c36"}
	case "dark":
		t.accent, t.muted, t.soft = lipgloss.Color("39"), lipgloss.Color("245"), lipgloss.Color("252")
		t.warn, t.err, t.ok = lipgloss.Color("214"), lipgloss.Color("196"), lipgloss.Color("42")
		t.sep, t.composer = lipgloss.Color("238"), lipgloss.Color("236")
	case "light":
		t.accent, t.muted, t.soft = lipgloss.Color("25"), lipgloss.Color("242"), lipgloss.Color("235")
		t.warn, t.err, t.ok = lipgloss.Color("130"), lipgloss.Color("124"), lipgloss.Color("28")
		t.sep, t.composer = lipgloss.Color("250"), lipgloss.Color("255")
	case "high-contrast":
		t.accent, t.muted, t.soft = lipgloss.Color("51"), lipgloss.Color("255"), lipgloss.Color("255")
		t.warn, t.err, t.ok = lipgloss.Color("226"), lipgloss.Color("196"), lipgloss.Color("46")
		t.sep, t.composer = lipgloss.Color("255"), lipgloss.Color("232")
	default:
		return tuiTheme{}, fmt.Errorf("unsupported TUI theme %q", name)
	}
	return t, nil
}

// applyTUITheme updates the package style palette used by the existing small
// renderer helpers. Snow runs one terminal model per process, so this keeps
// the change narrow while allowing every existing card/picker to share the
// selected semantic palette.
func applyTUITheme(name string) error {
	t, err := makeTUITheme(name)
	if err != nil {
		return err
	}
	applyResolvedTheme(t)
	return nil
}

func applyCustomTUITheme(custom config.ThemeFile) error {
	base := custom.Extends
	if base == "" {
		base = "default"
	}
	t, err := makeTUITheme(base)
	if err != nil {
		return err
	}
	t.name = custom.Name
	set := func(pair config.AdaptiveColor, target *lipgloss.TerminalColor) {
		if pair.Light == "" && pair.Dark == "" {
			return
		}
		light, dark := pair.Light, pair.Dark
		if light == "" {
			light = dark
		}
		if dark == "" {
			dark = light
		}
		*target = lipgloss.AdaptiveColor{Light: light, Dark: dark}
	}
	set(custom.Colors.Accent, &t.accent)
	set(custom.Colors.Muted, &t.muted)
	set(custom.Colors.Foreground, &t.soft)
	set(custom.Colors.Warning, &t.warn)
	set(custom.Colors.Error, &t.err)
	set(custom.Colors.Success, &t.ok)
	set(custom.Colors.Separator, &t.sep)
	applyResolvedTheme(t)
	return nil
}

func applyResolvedTheme(t tuiTheme) {
	colorAccent, colorMuted, colorSoft = t.accent, t.muted, t.soft
	colorWarn, colorErr, colorOk = t.warn, t.err, t.ok
	styleUser = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleAssistant = lipgloss.NewStyle().Foreground(colorSoft)
	styleTool = lipgloss.NewStyle().Foreground(colorWarn)
	styleError = lipgloss.NewStyle().Foreground(colorErr)
	styleFooter = lipgloss.NewStyle().Foreground(colorMuted)
	styleThinking = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	styleHeader = lipgloss.NewStyle().Foreground(colorSoft).Bold(true)
	styleHeaderDim = lipgloss.NewStyle().Foreground(colorMuted)
	styleDiffAdd = lipgloss.NewStyle().Foreground(colorOk)
	styleDiffDel = lipgloss.NewStyle().Foreground(colorErr)
	styleBrand = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleSep = lipgloss.NewStyle().Foreground(t.sep)
	stylePrompt = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	// Keep the composer transparent. Nested textarea cursor/end-of-buffer
	// backgrounds can otherwise render as isolated bright edge columns in
	// terminals that disagree about light/dark background detection.
	styleComposer = lipgloss.NewStyle()
	styleCompletion = lipgloss.NewStyle().Foreground(colorMuted)
	styleCompletionSelected = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
}

func themeChoices() []string {
	return []string{"default", "dark", "light", "high-contrast"}
}
