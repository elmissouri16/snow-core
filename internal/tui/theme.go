package tui

import (
	"fmt"

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

func snowTheme() tuiTheme {
	return tuiTheme{
		name:   "default",
		accent: adaptive("#0969DA", "#58A6FF"),
		muted:  adaptive("#57606A", "#8B949E"),
		soft:   adaptive("#24292F", "#F0F6FC"),
		warn:   adaptive("#9A6700", "#E3B341"),
		err:    adaptive("#CF222E", "#FF7B72"),
		ok:     adaptive("#1A7F37", "#7EE787"),
		sep:    adaptive("#8C959F", "#6E7681"),
	}
}

var activeTUITheme = snowTheme()

func makeTUITheme(name string) (tuiTheme, error) {
	if name == "" {
		name = "default"
	}
	canonical := name
	switch name {
	case "dark", "light":
		canonical = "default"
	case "nord":
		canonical = "frost"
	case "gruvbox":
		canonical = "ember"
	case "dracula":
		canonical = "aurora"
	}

	var t tuiTheme
	switch canonical {
	case "default":
		t = snowTheme()
	case "frost":
		t = tuiTheme{
			accent: adaptive("#006A7A", "#67E8F9"),
			muted:  adaptive("#52606D", "#94A3B8"),
			soft:   adaptive("#172B4D", "#E6F6FF"),
			warn:   adaptive("#8A4B00", "#FBBF24"),
			err:    adaptive("#B42318", "#FDA4AF"),
			ok:     adaptive("#166534", "#86EFAC"),
			sep:    adaptive("#8091A5", "#64748B"),
		}
	case "ember":
		t = tuiTheme{
			accent: adaptive("#9A3412", "#FDBA74"),
			muted:  adaptive("#62564B", "#B8A99A"),
			soft:   adaptive("#29211A", "#FFF7ED"),
			warn:   adaptive("#7C4A03", "#FDE047"),
			err:    adaptive("#B42318", "#FB7185"),
			ok:     adaptive("#166534", "#86EFAC"),
			sep:    adaptive("#8A7968", "#7C6F64"),
		}
	case "aurora":
		t = tuiTheme{
			accent: adaptive("#6D28D9", "#C4B5FD"),
			muted:  adaptive("#5B5668", "#A7A0B8"),
			soft:   adaptive("#211B2E", "#FAF5FF"),
			warn:   adaptive("#854D0E", "#FDE047"),
			err:    adaptive("#BE123C", "#FDA4AF"),
			ok:     adaptive("#166534", "#86EFAC"),
			sep:    adaptive("#877F96", "#756E86"),
		}
	case "high-contrast":
		// Retained as a hidden compatibility theme for saved selections and
		// custom theme inheritance.
		t = tuiTheme{
			accent: adaptive("#004FB3", "#00D7FF"),
			muted:  adaptive("#30363D", "#FFFFFF"),
			soft:   adaptive("#000000", "#FFFFFF"),
			warn:   adaptive("#6F4E00", "#FFFF00"),
			err:    adaptive("#A4001D", "#FF6B6B"),
			ok:     adaptive("#006B2D", "#00FF66"),
			sep:    adaptive("#000000", "#FFFFFF"),
		}
	default:
		return tuiTheme{}, fmt.Errorf("unsupported TUI theme %q", name)
	}
	t.name = name
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
	switch name {
	case "default":
		return "Snow"
	case "frost":
		return "Frost"
	case "ember":
		return "Ember"
	case "aurora":
		return "Aurora"
	default:
		return name
	}
}
