package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/snow-core/snow/internal/config"
)

// tuiTheme is intentionally small: semantic roles keep meaning readable even
// when a terminal has limited color support. Adaptive palettes cover both
// background modes; named light/dark palettes target their documented mode.
type tuiTheme struct {
	name   string
	accent lipgloss.TerminalColor
	muted  lipgloss.TerminalColor
	soft   lipgloss.TerminalColor
	warn   lipgloss.TerminalColor
	err    lipgloss.TerminalColor
	ok     lipgloss.TerminalColor
	sep    lipgloss.TerminalColor
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
		t.sep = lipgloss.AdaptiveColor{Light: "#8c959f", Dark: "#6e7681"}
	case "dark":
		// High-luminance semantic colors stay legible on common near-black
		// terminal backgrounds instead of depending on the terminal's ANSI map.
		t.accent, t.muted, t.soft = lipgloss.Color("#5EA1FF"), lipgloss.Color("#A8B3C2"), lipgloss.Color("#F2F5F8")
		t.warn, t.err, t.ok = lipgloss.Color("#FFD166"), lipgloss.Color("#FF7B72"), lipgloss.Color("#56D364")
		t.sep = lipgloss.Color("#6E7681")
	case "light":
		t.accent, t.muted, t.soft = lipgloss.Color("#005CC5"), lipgloss.Color("#57606A"), lipgloss.Color("#1F2328")
		t.warn, t.err, t.ok = lipgloss.Color("#7A4D00"), lipgloss.Color("#B42318"), lipgloss.Color("#116329")
		t.sep = lipgloss.Color("#8C959F")
	case "high-contrast":
		t.accent = lipgloss.AdaptiveColor{Light: "#004FB3", Dark: "#00D7FF"}
		t.muted = lipgloss.AdaptiveColor{Light: "#30363D", Dark: "#FFFFFF"}
		t.soft = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}
		t.warn = lipgloss.AdaptiveColor{Light: "#6F4E00", Dark: "#FFFF00"}
		t.err = lipgloss.AdaptiveColor{Light: "#A4001D", Dark: "#FF6B6B"}
		t.ok = lipgloss.AdaptiveColor{Light: "#006B2D", Dark: "#00FF66"}
		t.sep = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}
	case "nord":
		t.accent, t.muted, t.soft = lipgloss.Color("#88C0D0"), lipgloss.Color("#AAB2C0"), lipgloss.Color("#ECEFF4")
		t.warn, t.err, t.ok = lipgloss.Color("#EBCB8B"), lipgloss.Color("#F08088"), lipgloss.Color("#A3BE8C")
		t.sep = lipgloss.Color("#7F899C")
	case "dracula":
		t.accent, t.muted, t.soft = lipgloss.Color("#BD93F9"), lipgloss.Color("#B9BAC5"), lipgloss.Color("#F8F8F2")
		t.warn, t.err, t.ok = lipgloss.Color("#F1FA8C"), lipgloss.Color("#FF6E6E"), lipgloss.Color("#50FA7B")
		t.sep = lipgloss.Color("#858BAA")
	case "gruvbox":
		t.accent, t.muted, t.soft = lipgloss.Color("#83A598"), lipgloss.Color("#BDAE93"), lipgloss.Color("#EBDBB2")
		t.warn, t.err, t.ok = lipgloss.Color("#FABD2F"), lipgloss.Color("#FB6655"), lipgloss.Color("#B8BB26")
		t.sep = lipgloss.Color("#928374")
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
	return config.BuiltInTUIThemes()
}
