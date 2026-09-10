package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type pluginInspectorEntry struct {
	status protocol.PluginStatus
	info   protocol.PluginInfo
}

type pluginInspectorState struct {
	entries                      []pluginInspectorEntry
	diagnostics                  []protocol.ConfigDiagnostic
	query                        string
	index                        int
	detail                       bool
	returnView                   string
	returnSelected, returnScroll int
}

func (m *Model) pluginInspector() {
	if m.app == nil {
		return
	}
	statuses, err := m.app.PluginStatuses()
	if err != nil {
		message := sanitizeTerminalText(err.Error())
		m.pushLine(styleError.Render(message))
		if m.plugins != nil && m.plugins.screen == "snow:plugins" {
			m.plugins.managementError = message
			m.updatePluginInspectorView()
		}
		return
	}
	if m.plugins == nil {
		m.plugins = &pluginUIState{cache: map[pluginRenderKey]string{}, running: map[string]bool{}}
	}
	p := m.plugins.inspector
	if p == nil || m.plugins.screen != "snow:plugins" {
		p = &pluginInspectorState{}
		m.plugins.inspector = p
		m.plugins.selected, m.plugins.scroll = 0, 0
	}
	selectedID := ""
	if entry := p.selectedEntry(); entry != nil {
		selectedID = entry.status.ID
	}
	p.entries = nil
	infos := m.app.PluginInfos()
	for _, status := range statuses {
		entry := pluginInspectorEntry{status: status}
		for _, info := range infos {
			if info.ID == status.ID {
				entry.info = info
				break
			}
		}
		p.entries = append(p.entries, entry)
	}
	// Running plugins remain inspectable if their saved registration was
	// removed externally since launch.
	for _, info := range infos {
		if slices.ContainsFunc(p.entries, func(e pluginInspectorEntry) bool { return e.status.ID == info.ID }) {
			continue
		}
		p.entries = append(p.entries, pluginInspectorEntry{info: info, status: protocol.PluginStatus{
			ID: info.ID, Path: info.Path, Scope: info.Scope, Enabled: true, Loaded: true,
		}})
	}
	p.diagnostics = m.app.PluginDiagnostics()
	matches := p.matches()
	p.index = min(p.index, max(0, len(matches)-1))
	for i, index := range matches {
		if index < len(p.entries) && p.entries[index].status.ID == selectedID {
			p.index = i
			break
		}
	}
	m.plugins.screen = "snow:plugins"
	m.updatePluginInspectorView()
}

func (p *pluginInspectorState) matches() []int {
	query := strings.ToLower(strings.TrimSpace(p.query))
	var matches []int
	for i, entry := range p.entries {
		if strings.Contains(strings.ToLower(entry.status.ID+" "+entry.info.Name), query) {
			matches = append(matches, i)
		}
	}
	if len(p.diagnostics) > 0 && strings.Contains("diagnostics", query) {
		matches = append(matches, len(p.entries))
	}
	return matches
}

func (p *pluginInspectorState) selectedEntry() *pluginInspectorEntry {
	matches := p.matches()
	if p.index < 0 || p.index >= len(matches) || matches[p.index] >= len(p.entries) {
		return nil
	}
	return &p.entries[matches[p.index]]
}

func (m *Model) updatePluginInspectorView() {
	p := m.plugins.inspector
	if p == nil {
		return
	}
	root := protocol.PluginNode{Type: "column"}
	title := "Plugins"
	addText := func(text, tone string) {
		root.Children = append(root.Children, protocol.PluginNode{Type: "text", Text: text, Tone: tone})
	}
	addAction := func(text, action string) {
		root.Children = append(root.Children, protocol.PluginNode{Type: "button", Text: text, Action: action})
	}
	if p.detail {
		if m.plugins.managementError != "" {
			addText(m.plugins.managementError, "error")
		}
		if entry := p.selectedEntry(); entry != nil {
			s, info := entry.status, entry.info
			title = s.ID
			saved, running := "disabled", "not loaded"
			if s.Enabled {
				saved = "enabled"
			}
			if s.Loaded {
				running = "loaded"
			}
			addText("Saved: "+saved+" · Running: "+running, "accent")
			if s.RestartRequired {
				addText("restart required · Restart Snow to apply changes.", "warning")
			}
			if s.CanToggle {
				action, label := "enable:", "Enable"
				if s.Enabled {
					action, label = "disable:", "Disable"
				}
				if m.plugins.managementPending {
					label = "Saving…"
				} else {
					label += " on next launch"
				}
				addAction(label, action+s.ID)
			} else {
				addText("Controlled by launch options", "muted")
			}
			if info.Name != "" {
				addText(info.Name+" · "+info.Version, "")
			}
			addText(s.Scope+" · "+s.Path, "muted")
			if len(info.Capabilities) > 0 {
				addText("Capabilities: "+strings.Join(info.Capabilities, ", "), "muted")
			}
			for _, view := range info.Views {
				addAction("Open "+cmp.Or(view.Title, view.Name, view.ID), "open:"+view.ID)
			}
			for _, setting := range info.Settings {
				addAction("Configure "+cmp.Or(setting.Title, setting.Name)+" (next launch)", "setting:"+info.ID+":"+setting.Name)
			}
			for _, theme := range info.Themes {
				addAction("Use theme: "+theme.Name, "theme:"+theme.ID)
			}
			if len(info.Commands) > 0 {
				addText("Commands", "accent")
			}
			for _, command := range info.Commands {
				label := "/" + command.ID
				if command.Alias != "" {
					label += " (/" + command.Alias + ")"
				}
				addText(label+" — "+command.Description, "")
			}
		} else if len(p.matches()) > 0 {
			title = "Plugin diagnostics"
			for _, diagnostic := range p.diagnostics {
				addText(diagnostic.Path+": "+diagnostic.Message, "warning")
			}
		}
	}
	view := protocol.PluginView{ID: "snow:plugins", Title: title, Placement: "screen", Content: &root}
	m.plugins.views = slices.DeleteFunc(m.plugins.views, func(v protocol.PluginView) bool { return v.ID == view.ID })
	m.plugins.views = append(m.plugins.views, view)
	clear(m.plugins.cache)
}

func (m *Model) pluginInspectorListGeometry() pickerCardGeometry {
	g := m.pickerCardGeometry()
	g.innerHeight = min(g.innerHeight, max(8, len(m.plugins.inspector.matches())+4))
	g.outerHeight = g.innerHeight + 2
	g.listHeight = max(1, g.innerHeight-4)
	if m.plugins.managementError != "" && g.listHeight > 1 {
		g.listHeight--
	}
	return g
}

func (m *Model) renderPluginInspectorList() string {
	p := m.plugins.inspector
	g := m.pluginInspectorListGeometry()
	width := g.innerWidth
	matches := p.matches()
	status := fmt.Sprintf("%d plugins", len(p.entries))
	if p.query != "" {
		status = fmt.Sprintf("%d matches", len(matches))
	}
	query := "Search plugins…"
	if p.query != "" {
		query = "Search: " + p.query
	}
	line := func(text string) string {
		return " " + truncateDisplayText(sanitizeTerminalLine(text), max(1, width-2))
	}
	parts := []string{
		renderPickerCardHeader("Plugins", status, width),
		styleFooter.Render(line(query)),
		styleSep.Render(strings.Repeat("─", width)),
	}
	if m.plugins.managementError != "" && g.innerHeight > 5 {
		parts = append(parts, styleError.Render(line(m.plugins.managementError)))
	}
	var rows []string
	index := min(max(0, p.index), max(0, len(matches)-1))
	start := min(max(0, index-g.listHeight/2), max(0, len(matches)-g.listHeight))
	for i := start; i < min(len(matches), start+g.listHeight); i++ {
		label, state := "Diagnostics", fmt.Sprintf("%d messages", len(p.diagnostics))
		if matches[i] < len(p.entries) {
			s := p.entries[matches[i]].status
			label, state = s.ID, "Disabled"
			if s.Enabled {
				state = "Enabled"
			}
			if s.RestartRequired {
				state += " · restart"
			}
		}
		prefix, style := "  ", styleCompletion
		if i == index {
			prefix, style = "› ", styleCompletionSelected
		}
		// Preserve a useful name in small terminals; full saved/running status
		// is always available on the plugin's details page.
		if width < 38 {
			state = ""
		}
		nameWidth := max(1, width-4-lipgloss.Width(state))
		label = truncateDisplayText(sanitizeTerminalLine(label), nameWidth)
		row := prefix + label + strings.Repeat(" ", max(0, nameWidth-lipgloss.Width(label))) + state
		rows = append(rows, " "+style.Render(truncateDisplayText(row, width-2)))
	}
	if len(matches) == 0 {
		message := "No matching plugins. Backspace to edit search."
		if p.query == "" {
			message = "No plugins registered. Use snow plugin add <directory>."
		}
		rows = append(rows, styleFooter.Render(line(message)))
	}
	parts = append(parts, fitFrame(strings.Join(rows, "\n"), width, g.listHeight))
	hint := " ↑↓ choose · Enter manage · Type to filter · Esc close "
	if width < 58 {
		hint = " ↑↓ choose · Enter · Esc close "
	}
	if width < 30 {
		hint = " ↑↓ ↵ · Esc close "
	}
	parts = append(parts, styleFooter.Render(truncateDisplayText(hint, width)))
	return renderPickerCard(fitFrame(strings.Join(parts, "\n"), width, g.innerHeight), g)
}

func (m *Model) handlePluginInspectorKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	p := m.plugins.inspector
	if !p.detail {
		matches := p.matches()
		last := max(0, len(matches)-1)
		p.index = min(max(0, p.index), last)
		switch msg.Type {
		case tea.KeyEsc:
			m.plugins.screen = ""
		case tea.KeyUp, tea.KeyShiftTab:
			p.index = max(0, p.index-1)
		case tea.KeyDown, tea.KeyTab:
			p.index = min(last, p.index+1)
		case tea.KeyPgUp:
			p.index = max(0, p.index-m.pluginInspectorListGeometry().listHeight)
		case tea.KeyPgDown:
			p.index = min(last, p.index+m.pluginInspectorListGeometry().listHeight)
		case tea.KeyHome:
			p.index = 0
		case tea.KeyEnd:
			p.index = last
		case tea.KeyEnter:
			if len(matches) > 0 {
				p.detail = true
				m.plugins.selected, m.plugins.scroll = 0, 0
				m.updatePluginInspectorView()
			}
		case tea.KeyBackspace, tea.KeyDelete:
			query := []rune(p.query)
			p.query = string(query[:max(0, len(query)-1)])
			p.index = 0
		case tea.KeyCtrlU:
			p.query, p.index = "", 0
		case tea.KeyRunes, tea.KeySpace:
			text := string(msg.Runes)
			if msg.Type == tea.KeySpace {
				text = " "
			}
			query := []rune(p.query + sanitizeTerminalLine(text))
			p.query = string(query[:min(120, len(query))])
			p.index = 0
		}
		return true, nil
	}
	view := m.pluginScreenView()
	layout := m.pluginScreenLayout(*view)
	m.plugins.scroll, m.plugins.selected = layout.offset, layout.selected
	count := len(layout.actions)
	switch msg.Type {
	case tea.KeyEsc:
		p.detail = false
		m.plugins.selected, m.plugins.scroll = 0, 0
		m.updatePluginInspectorView()
	case tea.KeyUp, tea.KeyShiftTab:
		if count > 0 {
			m.plugins.selected = (m.plugins.selected + count - 1) % count
		} else {
			m.plugins.scroll = max(0, m.plugins.scroll-1)
		}
	case tea.KeyDown, tea.KeyTab:
		if count > 0 {
			m.plugins.selected = (m.plugins.selected + 1) % count
		} else {
			m.plugins.scroll = min(layout.limit, m.plugins.scroll+1)
		}
	case tea.KeyPgUp:
		m.plugins.scroll = max(0, m.plugins.scroll-layout.bodyHeight)
	case tea.KeyPgDown:
		m.plugins.scroll = min(layout.limit, m.plugins.scroll+layout.bodyHeight)
	case tea.KeyHome:
		m.plugins.selected, m.plugins.scroll = 0, 0
	case tea.KeyEnd:
		m.plugins.selected, m.plugins.scroll = max(0, count-1), layout.limit
	case tea.KeyEnter:
		if count > 0 && !m.plugins.managementPending {
			return true, m.runPluginInspectorAction(layout.actions[m.plugins.selected].Action)
		}
	}
	return true, nil
}

func (m *Model) runPluginInspectorAction(action string) tea.Cmd {
	kind, id, _ := strings.Cut(action, ":")
	switch kind {
	case "enable", "disable":
		return m.togglePlugin(id, kind == "enable")
	case "open":
		m.plugins.inspector.returnView = id
		m.plugins.inspector.returnSelected = m.plugins.selected
		m.plugins.inspector.returnScroll = m.plugins.scroll
		m.plugins.screen = id
		m.plugins.scroll, m.plugins.selected = 0, 0
	case "theme":
		for _, info := range m.plugins.infos {
			for _, theme := range info.Themes {
				if theme.ID == id {
					if err := m.applyPluginTheme(theme); err != nil {
						m.plugins.managementError = sanitizeTerminalText(err.Error())
					}
					m.updatePluginInspectorView()
					return nil
				}
			}
		}
	case "setting":
		pluginID, setting, _ := strings.Cut(id, ":")
		active, ctx := m.app, m.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		m.plugins.screen = ""
		return func() tea.Msg {
			err := active.EditPluginSetting(ctx, pluginID, setting)
			return pluginCommandDone{app: active, id: "settings", err: err}
		}
	}
	return nil
}
