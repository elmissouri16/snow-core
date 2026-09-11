package tui

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type pluginUIResponse struct {
	raw json.RawMessage
	err error
}
type pluginUIRequest struct {
	app   *app.App
	event protocol.PluginUIEvent
	reply chan pluginUIResponse
	ctx   context.Context
}
type pluginRefreshMsg struct{ app *app.App }
type pluginCommandDone struct {
	app        *app.App
	id         string
	generation uint64
	result     plugin.ToolResult
	err        error
}
type pluginRenderKey struct {
	id         string
	width      int
	theme      string
	screenBody bool
}
type pluginUIState struct {
	requests          chan pluginUIRequest
	commands          []protocol.PluginCommand
	specs             []commandSpec
	views             []protocol.PluginView
	infos             []protocol.PluginInfo
	screen            string
	scroll, selected  int
	pending           bool
	cache             map[pluginRenderKey]string
	generation        uint64
	running           map[string]bool
	managementError   string
	managementPending bool
	inspector         *pluginInspectorState
}

func (m *Model) attachPlugins() tea.Cmd {
	if m.app == nil || m.app.PluginManager == nil || !m.app.PluginManager.HasJavaScript() {
		return nil
	}
	p := &pluginUIState{generation: m.app.PluginGeneration(), requests: make(chan pluginUIRequest, 64), commands: m.app.PluginCommands(), views: m.app.PluginViews(), infos: m.app.PluginInfos(), cache: map[pluginRenderKey]string{}, running: map[string]bool{}}
	for _, command := range p.commands {
		p.specs = append(p.specs, commandSpec{name: "/" + command.ID, desc: command.Description, argHint: command.ArgumentHint})
		if command.Alias != "" {
			p.specs = append(p.specs, commandSpec{name: "/" + command.Alias, desc: command.Description, argHint: command.ArgumentHint})
		}
	}
	m.plugins = p
	m.loadAuxiliaryTUIConfig()
	active := m.app
	m.app.AttachPluginUI(func(ctx context.Context, event protocol.PluginUIEvent) (json.RawMessage, error) {
		request := pluginUIRequest{app: active, event: event, reply: make(chan pluginUIResponse, 1), ctx: ctx}
		select {
		case p.requests <- request:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		select {
		case response := <-request.reply:
			return response.raw, response.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	return m.waitPluginUI()
}
func (m *Model) waitPluginUI() tea.Cmd {
	if m.plugins == nil {
		return nil
	}
	requests, ctx := m.plugins.requests, m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		select {
		case request := <-requests:
			return request
		case <-ctx.Done():
			return nil
		}
	}
}
func (m *Model) pluginSpecs() []commandSpec {
	if m.plugins == nil {
		return nil
	}
	return m.plugins.specs
}
func (m *Model) handlePluginUI(request pluginUIRequest) tea.Cmd {
	response := pluginUIResponse{raw: []byte("null")}
	defer func() { request.reply <- response }()
	if request.app != m.app || m.plugins == nil || request.ctx.Err() != nil || request.event.Generation != m.app.PluginGeneration() {
		response.err = errors.New("stale plugin UI request")
		return m.waitPluginUI()
	}
	var arg struct {
		Name    string              `json:"name"`
		Text    string              `json:"text"`
		Content protocol.PluginNode `json:"content"`
	}
	if err := jsonv2.Unmarshal(request.event.Arguments, &arg); err != nil {
		response.err = err
		return m.waitPluginUI()
	}
	p := m.plugins
	switch request.event.Operation {
	case "ui.update":
		for i := range p.views {
			if p.views[i].ID == request.event.PluginID+":"+arg.Name {
				top := m.transcriptSelectionTop()
				p.views[i].Content = &arg.Content
				clear(p.cache)
				// Every message can produce a frame before the coalesced refresh.
				// Keep its geometry current even when existing content grows/shrinks.
				width, height := m.transcript.Width(), m.transcript.Height()
				m.layout()
				if width != m.transcript.Width() || height != m.transcript.Height() || top != m.transcriptSelectionTop() {
					m.clearTranscriptSelection()
					m.refreshTranscriptForced()
				}
			}
		}
	case "ui.open":
		p.screen = request.event.PluginID + ":" + arg.Name
		p.scroll = 0
		p.selected = 0
	case "ui.close":
		if strings.HasPrefix(p.screen, request.event.PluginID+":") {
			p.screen = ""
		}
	case "ui.notify":
		m.lastStatus = sanitizeTerminalText(request.event.PluginID + ": " + arg.Text)
	case "ui.editorGet":
		response.raw, _ = jsonv2.Marshal(m.editor.Value())
	case "ui.editorSet", "ui.editorInsert":
		if m.composerCoveredByModal() || m.permPending || m.userInputPending {
			response.err = errors.New("composer is covered by an interaction")
			break
		}
		if request.event.Operation == "ui.editorSet" {
			m.editor.SetValue(arg.Text)
		} else {
			m.editor.InsertString(arg.Text)
		}
		m.refreshPalette()
	case "ui.theme":
		response.err = errors.New("theme is not registered by this plugin")
		for _, info := range p.infos {
			for _, theme := range info.Themes {
				if theme.PluginID == request.event.PluginID && theme.Name == arg.Name {
					response.err = m.applyPluginTheme(theme)
					clear(p.cache)
				}
			}
		}
	default:
		response.err = errors.New("unsupported plugin UI operation")
	}
	if !p.pending {
		p.pending = true
		active := m.app
		return tea.Batch(m.waitPluginUI(), tea.Tick(33*time.Millisecond, func(time.Time) tea.Msg { return pluginRefreshMsg{app: active} }))
	}
	return m.waitPluginUI()
}
func (m *Model) runPluginCommand(name, input string) (bool, tea.Cmd) {
	if m.plugins == nil {
		return false, nil
	}
	name = strings.TrimPrefix(name, "/")
	for _, command := range m.plugins.commands {
		if command.ID != name && command.Alias != name {
			continue
		}
		if m.plugins.running[command.ID] {
			m.lastStatus = "plugin command already running"
			return true, nil
		}
		m.plugins.running[command.ID] = true
		active, ctx := m.app, m.ctx
		generation := active.PluginGeneration()
		if ctx == nil {
			ctx = context.Background()
		}
		return true, func() tea.Msg {
			result, err := active.RunPluginCommand(ctx, command.ID, input)
			return pluginCommandDone{app: active, id: command.ID, generation: generation, result: result, err: err}
		}
	}
	return false, nil
}
func (m *Model) finishPluginCommand(done pluginCommandDone) {
	if m.app != done.app || m.plugins == nil {
		return
	}
	delete(m.plugins.running, done.id)
	m.syncPluginGeneration()
	if done.generation != 0 && done.generation != m.app.PluginGeneration() {
		m.layout()
		return
	}
	if done.result.IsError && done.err == nil {
		m.pushLine(styleError.Render(sanitizeTerminalText(done.id + ": command failed")))
	}
	if done.err != nil {
		if errors.Is(done.err, context.Canceled) {
			m.pushLine(styleFooter.Render(sanitizeTerminalText(done.id + ": cancelled")))
		} else {
			m.pushLine(styleError.Render(sanitizeTerminalText(done.id + ": " + done.err.Error())))
		}
	}
	for _, block := range done.result.Content {
		if block.Type == protocol.BlockText && block.Text != "" {
			text := sanitizeTerminalText(block.Text)
			if done.result.IsError {
				text = styleError.Render(text)
			}
			m.pushLine(text)
		}
	}
	m.layout()
	m.refreshTranscript()
}
func (m *Model) pluginScreenView() *protocol.PluginView {
	if m.plugins == nil || m.plugins.screen == "" {
		return nil
	}
	for i := range m.plugins.views {
		if m.plugins.views[i].ID == m.plugins.screen {
			return &m.plugins.views[i]
		}
	}
	return nil
}
func (m *Model) handlePluginKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.plugins == nil {
		return false, nil
	}
	if view := m.pluginScreenView(); view != nil {
		if view.ID == "snow:plugins" && m.plugins.inspector != nil {
			return m.handlePluginInspectorKey(msg)
		}
		actions := pluginActions(view.Content)
		layout := m.pluginScreenLayout(*view)
		m.plugins.scroll, m.plugins.selected = layout.offset, layout.selected
		switch {
		case msg.Code == tea.KeyEscape:
			if p := m.plugins.inspector; p != nil && p.returnView == view.ID {
				p.returnView = ""
				m.plugins.screen = "snow:plugins"
				m.plugins.selected, m.plugins.scroll = p.returnSelected, p.returnScroll
				return true, nil
			}
			m.plugins.screen = ""
			return true, nil
		case msg.Code == tea.KeyUp:
			m.plugins.scroll = max(0, m.plugins.scroll-1)
		case msg.Code == tea.KeyDown:
			m.plugins.scroll = min(layout.limit, m.plugins.scroll+1)
		case msg.Code == tea.KeyPgUp:
			m.plugins.scroll = max(0, m.plugins.scroll-layout.bodyHeight)
		case msg.Code == tea.KeyPgDown:
			m.plugins.scroll = min(layout.limit, m.plugins.scroll+layout.bodyHeight)
		case msg.Code == tea.KeyHome:
			m.plugins.scroll = 0
		case msg.Code == tea.KeyEnd:
			m.plugins.scroll = layout.limit
		case msg.Code == tea.KeyTab && !msg.Mod.Contains(tea.ModShift):
			if len(actions) > 0 {
				m.plugins.selected = (m.plugins.selected + 1) % len(actions)
			}
		case msg.Code == tea.KeyTab && msg.Mod.Contains(tea.ModShift):
			if len(actions) > 0 {
				m.plugins.selected = (m.plugins.selected + len(actions) - 1) % len(actions)
			}
		case msg.Code == tea.KeyEnter:
			if len(actions) > 0 {
				action := actions[m.plugins.selected%len(actions)]
				name := action.Action
				if !strings.Contains(name, ":") {
					name = view.PluginID + ":" + name
				}
				if strings.HasPrefix(name, view.PluginID+":") {
					return m.runPluginCommand(name, action.Input)
				}
			}
		}
		return true, nil
	}
	if m.composerCoveredByModal() {
		return false, nil
	}
	for _, command := range m.plugins.commands {
		if key.Matches(msg, m.keys.Plugins["plugin:"+command.ID]) {
			return m.runPluginCommand(command.ID, "")
		}
	}
	return false, nil
}
func pluginActions(node *protocol.PluginNode) []protocol.PluginNode {
	if node == nil {
		return nil
	}
	var out []protocol.PluginNode
	if node.Type == "button" && node.Action != "" {
		out = append(out, *node)
	}
	for _, child := range node.Children {
		out = append(out, pluginActions(&child)...)
	}
	return out
}
func (m *Model) renderPluginView(view protocol.PluginView, width int) string {
	return m.renderPluginViewContent(view, width, false)
}

func (m *Model) renderPluginViewContent(view protocol.PluginView, width int, screenBody bool) string {
	if view.Content == nil {
		return ""
	}
	key := pluginRenderKey{id: view.ID, width: width, theme: activeTUITheme.name, screenBody: screenBody}
	if text, ok := m.plugins.cache[key]; ok {
		return text
	}
	content := *view.Content
	if screenBody {
		content, _ = pluginScreenContent(content)
	}
	text := renderPluginNode(content, width)
	if len(text) <= 128<<10 {
		m.plugins.cache[key] = text
	}
	return text
}
func (m *Model) pluginPlacement(placement string, width int) string {
	if placement != "sidebar" {
		return m.pluginChrome()[placement]
	}
	return m.pluginPlacementContent(placement, width)
}

func (m *Model) pluginPlacementContent(placement string, width int) string {
	if m.plugins == nil {
		return ""
	}
	var parts []string
	for _, view := range m.plugins.views {
		if view.Placement == placement && view.Content != nil {
			parts = append(parts, m.renderPluginView(view, width))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	text := strings.Join(parts, "\n")
	if placement != "sidebar" {
		limit := 2
		if placement == "above_input" {
			limit = 6
		}
		lines := strings.Split(text, "\n")
		text = strings.Join(lines[:min(len(lines), limit)], "\n")
	}
	return text
}
func (m *Model) pluginChromeHeight() int {
	rows := 0
	for _, text := range m.pluginChrome() {
		if text != "" {
			rows += lipgloss.Height(text)
		}
	}
	return rows
}
func (m *Model) pluginSidebarWidth() int {
	if m.plugins == nil || m.width < 100 {
		return 0
	}
	for _, view := range m.plugins.views {
		if view.Placement == "sidebar" && view.Content != nil {
			return min(36, m.width/3)
		}
	}
	return 0
}

func (m *Model) syncPluginGeneration() {
	if m.plugins == nil || m.app == nil {
		return
	}
	generation := m.app.PluginGeneration()
	if generation == m.plugins.generation {
		return
	}
	m.plugins.generation = generation
	m.refreshPluginCatalog()
	inspector := m.plugins.screen == "snow:plugins"
	m.plugins.views = m.app.PluginViews()
	m.plugins.screen = ""
	clear(m.plugins.cache)
	if inspector {
		m.plugins.screen = "snow:plugins"
		m.pluginInspector()
	}
}
