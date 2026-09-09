package tui

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	requests         chan pluginUIRequest
	commands         []protocol.PluginCommand
	specs            []commandSpec
	views            []protocol.PluginView
	infos            []protocol.PluginInfo
	screen           string
	scroll, selected int
	pending          bool
	cache            map[pluginRenderKey]string
	generation       uint64
	running          map[string]bool
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
				width, height := m.transcript.Width, m.transcript.Height
				m.layout()
				if width != m.transcript.Width || height != m.transcript.Height || top != m.transcriptSelectionTop() {
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
func (m *Model) pluginInspector() {
	if m.plugins == nil {
		m.pushLine(styleFooter.Render("No JavaScript plugins loaded."))
		return
	}
	root := protocol.PluginNode{Type: "column"}
	for _, info := range m.plugins.infos {
		root.Children = append(root.Children, protocol.PluginNode{Type: "text", Text: info.Name + " · " + info.Version, Tone: "accent"}, protocol.PluginNode{Type: "text", Text: "Source: " + info.Scope + " · " + info.Path + "\nCapabilities: " + strings.Join(info.Capabilities, ", ")})
		for _, command := range info.Commands {
			root.Children = append(root.Children, protocol.PluginNode{Type: "text", Text: "/" + command.ID + " — " + command.Description})
		}
		for _, view := range info.Views {
			root.Children = append(root.Children, protocol.PluginNode{Type: "button", Text: "Open " + view.ID + " (" + view.Placement + ")", Action: "open:" + view.ID})
		}
		for _, setting := range info.Settings {
			root.Children = append(root.Children, protocol.PluginNode{Type: "button", Text: "Configure " + setting.Name + " (restart to apply)", Action: "setting:" + info.ID + ":" + setting.Name})
		}
		for _, theme := range info.Themes {
			root.Children = append(root.Children, protocol.PluginNode{Type: "button", Text: "Theme: " + theme.Name, Action: "theme:" + theme.ID})
		}
	}
	for _, diagnostic := range m.app.PluginDiagnostics() {
		root.Children = append(root.Children, protocol.PluginNode{Type: "text", Text: diagnostic.Path + ": " + diagnostic.Message, Tone: "warning"})
	}
	view := protocol.PluginView{ID: "snow:plugins", Title: "Plugins", Placement: "screen", Content: &root}
	m.plugins.views = slices.DeleteFunc(m.plugins.views, func(v protocol.PluginView) bool { return v.ID == view.ID })
	m.plugins.views = append(m.plugins.views, view)
	m.plugins.screen = view.ID
	m.plugins.scroll = 0
	m.plugins.selected = 0
	clear(m.plugins.cache)
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
func (m *Model) handlePluginKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.plugins == nil {
		return false, nil
	}
	if view := m.pluginScreenView(); view != nil {
		actions := pluginActions(view.Content)
		layout := m.pluginScreenLayout(*view)
		m.plugins.scroll, m.plugins.selected = layout.offset, layout.selected
		switch msg.Type {
		case tea.KeyEsc:
			m.plugins.screen = ""
			return true, nil
		case tea.KeyUp:
			m.plugins.scroll = max(0, m.plugins.scroll-1)
		case tea.KeyDown:
			m.plugins.scroll = min(layout.limit, m.plugins.scroll+1)
		case tea.KeyPgUp:
			m.plugins.scroll = max(0, m.plugins.scroll-layout.bodyHeight)
		case tea.KeyPgDown:
			m.plugins.scroll = min(layout.limit, m.plugins.scroll+layout.bodyHeight)
		case tea.KeyHome:
			m.plugins.scroll = 0
		case tea.KeyEnd:
			m.plugins.scroll = layout.limit
		case tea.KeyTab:
			if len(actions) > 0 {
				m.plugins.selected = (m.plugins.selected + 1) % len(actions)
			}
		case tea.KeyShiftTab:
			if len(actions) > 0 {
				m.plugins.selected = (m.plugins.selected + len(actions) - 1) % len(actions)
			}
		case tea.KeyEnter:
			if len(actions) > 0 {
				action := actions[m.plugins.selected%len(actions)]
				name := action.Action
				if view.ID == "snow:plugins" {
					if id, ok := strings.CutPrefix(name, "open:"); ok {
						m.plugins.screen = id
						m.plugins.scroll = 0
						m.plugins.selected = 0
						return true, nil
					}
					if id, ok := strings.CutPrefix(name, "theme:"); ok {
						for _, info := range m.plugins.infos {
							for _, theme := range info.Themes {
								if theme.ID == id {
									_ = m.applyPluginTheme(theme)
									clear(m.plugins.cache)
									m.plugins.screen = ""
									return true, nil
								}
							}
						}
					}
					if id, ok := strings.CutPrefix(name, "setting:"); ok {
						pluginID, setting, _ := strings.Cut(id, ":")
						active := m.app
						m.plugins.screen = ""
						return true, func() tea.Msg {
							err := active.EditPluginSetting(context.Background(), pluginID, setting)
							return pluginCommandDone{app: active, id: "settings", err: err}
						}
					}
				}
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
	m.plugins.views = m.app.PluginViews()
	m.plugins.screen = ""
	clear(m.plugins.cache)
}
