package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type pluginReloadDone struct {
	app    *app.App
	id     string
	result protocol.PluginReloadResult
	err    error
}

func (m *Model) reloadPlugin(id string) tea.Cmd {
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
		result, err := active.ReloadPlugin(ctx, id)
		return pluginReloadDone{app: active, id: id, result: result, err: err}
	}
}
func (m *Model) finishPluginReload(done pluginReloadDone) {
	if done.app != m.app {
		return
	}
	if m.plugins != nil {
		m.plugins.managementPending = false
	}
	if done.err != nil {
		m.finishPluginToggle(pluginToggleDone{app: done.app, id: done.id, err: done.err})
		return
	}
	m.syncPluginGeneration()

	m.lastStatus = sanitizeTerminalText(done.id + ": reloaded")
	m.pushLine(styleFooter.Render(m.lastStatus))
	for _, diagnostic := range done.result.Diagnostics {
		m.pushLine(styleError.Render(sanitizeTerminalText(diagnostic.Phase + ": " + diagnostic.Message)))
	}
	if m.plugins != nil && m.plugins.screen == "snow:plugins" {
		m.pluginInspector()
	}
	m.layout()
	m.refreshTranscript()
}

// refreshPluginCatalog updates aliases, shortcuts, views, and renderer metadata
// for every generation change, including reload initiated through another facade.
func (m *Model) refreshPluginCatalog() {
	if m.plugins == nil || m.app == nil {
		return
	}
	m.plugins.commands = m.app.PluginCommands()
	m.plugins.infos = m.app.PluginInfos()
	m.plugins.specs = nil
	for _, command := range m.plugins.commands {
		m.plugins.specs = append(m.plugins.specs, commandSpec{name: "/" + command.ID, desc: command.Description, argHint: command.ArgumentHint})
		if command.Alias != "" {
			m.plugins.specs = append(m.plugins.specs, commandSpec{name: "/" + command.Alias, desc: command.Description, argHint: command.ArgumentHint})
		}
	}
}
