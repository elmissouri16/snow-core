package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elmissouri16/snow-core/internal/app"
)

type pluginDiagnosticsTick struct{ app *app.App }

// Poll diagnostics separately from agent events: slow/failed observers must
// remain visible without feeding plugin logs back into plugin subscriptions.
func (m *Model) pollPluginDiagnostics() tea.Cmd {
	if m.app == nil || m.app.PluginManager == nil || !m.app.PluginManager.HasJavaScript() {
		return nil
	}
	m.syncPluginGeneration()
	diagnostics := m.app.PluginDiagnostics()
	if m.pluginDiagnosticCount > len(diagnostics) {
		m.pluginDiagnosticCount = 0
	}
	for _, diagnostic := range diagnostics[m.pluginDiagnosticCount:] {
		m.pushLine(styleFooter.Render(sanitizeTerminalText(diagnostic.Path + ": " + diagnostic.Message)))
	}
	m.pluginDiagnosticCount = len(diagnostics)
	active := m.app
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return pluginDiagnosticsTick{app: active} })
}
