package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type pluginToggleDone struct {
	app    *app.App
	id     string
	status protocol.PluginStatus
	err    error
}

func (m *Model) togglePlugin(id string, enabled bool) tea.Cmd {
	if m.app == nil {
		return nil
	}
	if m.plugins != nil {
		if m.plugins.managementPending {
			return nil
		}
		m.plugins.managementPending = true
		m.plugins.managementError = ""
		m.updatePluginInspectorView()
	}
	active, ctx := m.app, m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		status, err := active.SetPluginEnabled(ctx, id, enabled)
		return pluginToggleDone{app: active, id: id, status: status, err: err}
	}
}

func (m *Model) finishPluginToggle(done pluginToggleDone) {
	if done.app != m.app {
		return
	}
	if m.plugins != nil {
		m.plugins.managementPending = false
	}
	if done.err != nil {
		message := sanitizeTerminalText(done.err.Error())
		if done.id != "" {
			message = sanitizeTerminalLine(done.id) + ": " + message
		}
		m.pushLine(styleError.Render(message))
		if m.plugins != nil && m.plugins.screen == "snow:plugins" {
			m.plugins.managementError = message
			m.plugins.scroll = 0
			m.pluginInspector()
		}
		return
	}
	if m.plugins != nil {
		m.plugins.managementError = ""
	}
	state := "disabled"
	if done.status.Enabled {
		state = "enabled"
	}
	message := done.status.ID + ": " + state + " saved"
	if done.status.RestartRequired {
		message += "; restart Snow to apply"
	}
	m.lastStatus = sanitizeTerminalText(message)
	m.pushLine(styleFooter.Render(m.lastStatus))
	if m.plugins != nil && m.plugins.screen == "snow:plugins" {
		selected := m.plugins.selected
		m.pluginInspector()
		m.plugins.selected, m.plugins.scroll = selected, 0
	}
	m.layout()
	m.refreshTranscript()
}
