package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/config"
)

// tuiTheme is intentionally small: semantic roles keep meaning readable even
// when a terminal has limited color support. Every selectable palette is
// adaptive so it remains coherent on both light and dark terminal backgrounds.
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

func adaptive(light, dark string) lipgloss.TerminalColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

func fromResolvedTheme(resolved config.ResolvedTheme) tuiTheme {
	colors := resolved.Colors
	return tuiTheme{
		name:   resolved.Name,
		accent: adaptive(colors.Accent.Light, colors.Accent.Dark),
		muted:  adaptive(colors.Muted.Light, colors.Muted.Dark),
		soft:   adaptive(colors.Foreground.Light, colors.Foreground.Dark),
		warn:   adaptive(colors.Warning.Light, colors.Warning.Dark),
		err:    adaptive(colors.Error.Light, colors.Error.Dark),
		ok:     adaptive(colors.Success.Light, colors.Success.Dark),
		sep:    adaptive(colors.Separator.Light, colors.Separator.Dark),
	}
}

func snowTheme() tuiTheme {
	resolved, _ := config.ResolveBuiltInTheme("default")
	return fromResolvedTheme(resolved)
}

var activeTUITheme = snowTheme()

func makeTUITheme(name string) (tuiTheme, error) {
	resolved, err := config.ResolveBuiltInTheme(name)
	if err != nil {
		return tuiTheme{}, err
	}
	return fromResolvedTheme(resolved), nil
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
	resolved, err := config.ResolveCustomTheme(custom, "custom")
	if err != nil {
		return err
	}
	applyResolvedTheme(fromResolvedTheme(resolved))
	return nil
}

func applyResolvedTheme(t tuiTheme) {
	activeTUITheme = t
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

func themeDisplayName(name string) string {
	return config.ThemeDisplayName(name)
}
