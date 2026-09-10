package tui

import (
	"io"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) terminalThemeInit() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, tea.Raw(ansi.RequestModeLightDark))
}

func (m *Model) updateTerminalTheme(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		if msg.Color == nil {
			return true, nil
		}
		dark := msg.IsDark()
		m.backgroundKnown = true
		if m.backgroundDark == dark {
			return true, nil
		}
		m.backgroundDark, terminalDark = dark, dark
		// Re-resolve the active palette itself: a plugin theme need not have the
		// same name as the persisted built-in/custom theme selection.
		applyResolvedTheme(activeTUITheme)
		m.refreshThemeStyles()
		m.rerenderThemedTranscript()
		m.layout()
		return true, nil
	case tea.FocusMsg:
		return true, tea.RequestBackgroundColor
	case uv.DarkColorSchemeEvent, uv.LightColorSchemeEvent:
		// Notifications describe system appearance. The terminal may use a custom
		// background, so query its actual color instead of assuming light or dark.
		return true, tea.RequestBackgroundColor
	case tea.ModeReportMsg:
		if msg.Mode != ansi.ModeLightDark {
			return false, nil
		}
		if m.themeModeReported {
			return true, nil
		}
		m.themeModeReported = true
		if msg.Value == ansi.ModeReset {
			m.themeModeOwned = true
			return true, tea.Raw(ansi.SetModeLightDark)
		}
		return true, nil
	}
	return false, nil
}

// Called only after Program.Run has stopped its renderer, including context
// cancellation and restart. Restore only a mode Snow changed from reset to set.
func (m *Model) restoreTerminalAppearance(output io.Writer) error {
	if !m.themeModeOwned {
		return nil
	}
	m.themeModeOwned = false
	_, err := io.WriteString(output, ansi.ResetModeLightDark)
	return err
}

// Rehydrating durable Markdown must not reset navigation or live run counters.
func (m *Model) preserveThemeRuntimeState() func() {
	history, index, draft := m.inputHistory, m.inputHistoryIndex, m.inputHistoryDraft
	plan := m.latestPlan
	lastUsage := m.lastUsage
	usage, tokens, estimated := m.lastRequestUsage, m.contextTokens, m.contextEstimated
	contextVersion, contextNeeded := m.contextRefreshVersion, m.contextRefreshNeeded
	turns, steps := m.turnCount, m.stepCount
	statsVersion, statsNeeded := m.runStatsRefreshVersion, m.runStatsRefreshNeeded
	return func() {
		m.inputHistory, m.inputHistoryIndex, m.inputHistoryDraft = history, index, draft
		m.latestPlan = plan
		m.lastUsage = lastUsage
		m.lastRequestUsage, m.contextTokens, m.contextEstimated = usage, tokens, estimated
		m.contextRefreshVersion, m.contextRefreshNeeded = contextVersion, contextNeeded
		m.turnCount, m.stepCount = turns, steps
		m.runStatsRefreshVersion, m.runStatsRefreshNeeded = statsVersion, statsNeeded
	}
}
