package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

// handleModelPick navigates and searches the /model picker. Slash enters search
// mode so the usual j/k picker bindings remain available until text entry starts.
func (m *Model) handleModelPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modelLoading {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		if msg.Type == tea.KeyEsc {
			m.pickModel = false
			m.modelLoading = false
			m.pickerGeneration++
		}
		return m, nil
	}

	if m.modelSearchActive {
		switch msg.Type {
		case tea.KeyRunes:
			m.modelQuery += string(msg.Runes)
			m.modelIndex = 0
			m.layout()
			return m, nil
		case tea.KeyBackspace:
			runes := []rune(m.modelQuery)
			if len(runes) > 0 {
				m.modelQuery = string(runes[:len(runes)-1])
			}
			m.modelIndex = 0
			m.layout()
			return m, nil
		case tea.KeyCtrlU:
			m.modelQuery = ""
			m.modelIndex = 0
			m.layout()
			return m, nil
		case tea.KeyEsc:
			m.modelQuery = ""
			m.modelIndex = 0
			m.modelSearchActive = false
			m.layout()
			return m, nil
		}
	} else if msg.Type == tea.KeyRunes && string(msg.Runes) == "/" {
		m.modelSearchActive = true
		m.modelIndex = 0
		m.layout()
		return m, nil
	}

	msg = normalizePickerKeyWithMap(msg, m.keys)
	models := m.filteredModels()
	if next, handled := movePicker(m.modelIndex, len(models), pickerKeyAction(msg), m.modelPickerVisibleModels()); handled {
		m.modelIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.clearModelPick()
		if m.settingsReturnToPanel {
			m.settingsReturnToPanel = false
			m.pickSettings = true
		}
	case tea.KeyEnter:
		if len(models) == 0 {
			return m, nil
		}
		model := models[m.modelIndex]
		if len(model.SupportedThinkingLevels()) > 1 {
			m.pickModel = false
			m.startThinkingPickForModel(model, true)
			return m, nil
		}
		m.clearModelPick()
		if m.settingsReturnToPanel {
			m.settingsReturnToPanel = false
			m.pickSettings = true
			resetThinking := m.app != nil && !model.SupportsThinkingLevel(m.app.Agent.Thinking())
			if err := m.applyModel(model); err != nil {
				m.settingsError = err.Error()
				m.settingsStatus = ""
			} else {
				m.settingsError = ""
				m.settingsStatus = "model saved"
				if resetThinking {
					m.settingsStatus = "model saved; thinking reset to off"
				}
			}
		} else {
			m.setModel(model)
		}
	}
	return m, nil
}

func (m *Model) modelPickerVisibleModels() int {
	// An inline modal has ten total rows. Two models leave worst-case room for
	// separate provider headings, search chrome, scroll markers, details, and
	// controls while keeping the selected model visible.
	if m.inlineModalOverlay() {
		return 2
	}
	// Search makes a short list more useful than filling the whole transcript.
	// Keep enough space for headings, search state, details, and controls.
	visible := m.availableOverlayHeight() - 9
	if visible < 4 {
		visible = 4
	}
	if visible > 10 {
		visible = 10
	}
	return visible
}

func (m *Model) modelWindow(models []protocol.Model) (start, end int) {
	end = len(models)
	visible := m.modelPickerVisibleModels()
	if end <= visible {
		return 0, end
	}
	start = m.modelIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > end {
		start = end - visible
	}
	return start, start + visible
}

// renderModelPicker renders a compact, searchable, provider-grouped /model list.
func (m *Model) renderModelPicker() string {
	if !m.pickModel {
		return ""
	}
	if m.modelLoading {
		return styleHeaderDim.Render("models\n  loading models…")
	}
	if len(m.modelList) == 0 {
		return ""
	}
	models := m.filteredModels()
	start, end := m.modelWindow(models)
	var b strings.Builder
	b.WriteString(styleHeader.Render("models"))
	b.WriteString(styleHeaderDim.Render(fmt.Sprintf("  ·  %d available", len(m.modelList))))
	b.WriteString("\n")
	if m.modelSearchActive || m.modelQuery != "" {
		cursor := ""
		if m.modelSearchActive {
			cursor = "_"
		}
		b.WriteString(styleHeaderDim.Render(fmt.Sprintf("  search: %s%s  ·  %d matches", m.modelQuery, cursor, len(models))))
	} else {
		b.WriteString(styleHeaderDim.Render("  press / to search"))
	}
	b.WriteString("\n")
	if len(models) == 0 {
		b.WriteString(styleFooter.Render("  no matching models"))
		b.WriteString("\n")
	} else {
		if start > 0 {
			b.WriteString(styleHeaderDim.Render("  ↑ more"))
			b.WriteString("\n")
		}
		for i := start; i < end; i++ {
			mm := models[i]
			if i == start || mm.Provider != models[i-1].Provider {
				b.WriteString(styleHeaderDim.Render("  " + mm.Provider))
				b.WriteString("\n")
			}
			line := mm.ID
			if mm.DisplayName != "" && !strings.EqualFold(mm.DisplayName, mm.ID) {
				line += "  (" + mm.DisplayName + ")"
			}
			if m.app != nil && mm.Provider == m.app.Model.Provider && mm.ID == m.app.Model.ID {
				line += "  ✓ current"
			}
			if i == m.modelIndex {
				b.WriteString(styleCompletionSelected.Render("› " + line))
			} else {
				b.WriteString(styleCompletion.Render("  " + line))
			}
			b.WriteString("\n")
		}
		if end < len(models) {
			b.WriteString(styleHeaderDim.Render("  ↓ more"))
			b.WriteString("\n")
		}
		selected := models[m.modelIndex]
		var details []string
		if selected.ContextWindow > 0 {
			details = append(details, "ctx "+formatTokenCount(int64(selected.ContextWindow)))
		}
		if levels := selected.SupportedThinkingLevels(); len(levels) > 1 {
			parts := make([]string, 0, len(levels)-1)
			for _, level := range levels[1:] {
				parts = append(parts, string(level))
			}
			details = append(details, "thinking "+strings.Join(parts, "/"))
		}
		if len(details) > 0 {
			b.WriteString(styleHeaderDim.Render("  " + strings.Join(details, "  ·  ")))
			b.WriteString("\n")
		}
	}
	b.WriteString(styleFooter.Render("(↑/↓ choose · / search · Enter apply · Esc cancel)"))
	return strings.TrimSuffix(b.String(), "\n")
}

// setModel switches the active provider/model and persists the complete choice
// for the current project so another working directory keeps its own selection.
func (m *Model) setModel(selected protocol.Model) {
	if m.app == nil {
		return
	}
	if selected.Provider == "" {
		selected.Provider = m.app.ProviderID
	}
	currentThinking := m.app.Agent.Thinking()
	resetThinking := !selected.SupportsThinkingLevel(currentThinking)
	if err := m.applyModel(selected); err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return
	}
	if resetThinking {
		m.pushLine(styleTool.Render("thinking changed from " + string(currentThinking) + " to off because model " + strconv.Quote(selected.ID) + " does not advertise that effort"))
	}
}

func (m *Model) applyModelAndThinking(selected protocol.Model, level protocol.ThinkingLevel) error {
	if m.app == nil {
		return fmt.Errorf("model: app is not ready")
	}
	parsed, err := protocol.ParseThinkingLevel(string(level))
	if err != nil {
		return err
	}
	if selected.Provider == "" {
		selected.Provider = m.app.ProviderID
	}
	if !selected.SupportsThinkingLevel(parsed) {
		return fmt.Errorf("model %q does not advertise thinking level %q", selected.ID, parsed)
	}

	oldProvider := m.app.ProviderID
	oldModel := m.app.Agent.Model()
	oldAppModel := m.app.Model
	oldThinking := m.app.Agent.Thinking()
	oldCfg := m.app.Cfg
	oldPersistedCfg := m.app.PersistedCfg
	rollback := func() error {
		if err := m.app.SetProviderModelThinking(oldProvider, oldModel, oldThinking); err != nil {
			return err
		}
		m.app.Model = oldAppModel
		m.app.Cfg = oldCfg
		m.app.PersistedCfg = oldPersistedCfg
		return nil
	}

	if err := m.app.SetProviderModelThinking(selected.Provider, selected, parsed); err != nil {
		return err
	}

	candidate, err := m.persistProjectSelection(config.ProjectSelection{
		Provider: selected.Provider,
		Model:    selected.ID,
		Thinking: string(parsed),
	})
	if err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return fmt.Errorf("persist model and thinking: %w (rollback failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("persist model and thinking: %w", err)
	}
	m.app.Model = selected
	m.app.PersistedCfg = candidate
	m.app.Cfg.DefaultProvider = selected.Provider
	m.app.Cfg.DefaultModel = selected.ID
	m.app.Cfg.Thinking = string(parsed)
	return nil
}

func (m *Model) applyModel(selected protocol.Model) error {
	if m.app == nil {
		return fmt.Errorf("model: app is not ready")
	}
	currentThinking := m.app.Agent.Thinking()
	if selected.Provider == "" {
		selected.Provider = m.app.ProviderID
	}
	if !selected.SupportsThinkingLevel(currentThinking) {
		// Model discovery endpoints commonly return ID-only records. Keep their
		// conservative capability metadata, but never leave the TUI in an invalid
		// model/effort combination that rejects the next prompt.
		return m.applyModelAndThinking(selected, protocol.ThinkingOff)
	}
	oldProvider := m.app.ProviderID
	oldModel := m.app.Agent.Model()
	oldAppModel := m.app.Model
	oldCfg := m.app.Cfg
	if selected.Provider != m.app.ProviderID {
		if err := m.app.SetProvider(selected.Provider); err != nil {
			return err
		}
	}
	if err := m.app.SetModel(selected); err != nil {
		if oldProvider != m.app.ProviderID {
			_ = m.app.SetProvider(oldProvider)
		}
		return err
	}
	candidate, err := m.persistProjectSelection(config.ProjectSelection{
		Provider: selected.Provider,
		Model:    selected.ID,
		Thinking: string(m.app.Agent.Thinking()),
	})
	if err != nil {
		if oldProvider != m.app.ProviderID {
			_ = m.app.SetProvider(oldProvider)
		}
		_ = m.app.SetModel(oldModel)
		m.app.Model = oldAppModel
		m.app.Cfg = oldCfg
		return fmt.Errorf("persist model: %w", err)
	}
	m.app.Model = selected
	m.app.PersistedCfg = candidate
	m.app.Cfg.DefaultProvider = selected.Provider
	m.app.Cfg.DefaultModel = selected.ID
	return nil
}

func (m *Model) startForkPick() (tea.Model, tea.Cmd) {
	if m.busy || m.app == nil || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("fork: wait for the current turn to finish"))
		return m, nil
	}
	m.pickFork = true
	m.forkIndex = 0
	m.forkLoading = false
	m.compVisible = false
	m.pickerGeneration++
	return m, nil
}

func (m *Model) handleForkPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.forkLoading {
		return m, nil
	}
	if next, handled := movePicker(m.forkIndex, len(forkChoices), pickerKeyAction(msg), len(forkChoices)); handled {
		m.forkIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.forkIndex = (m.forkIndex - 1 + len(forkChoices)) % len(forkChoices)
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.forkIndex = (m.forkIndex + 1) % len(forkChoices)
	case tea.KeyEsc:
		m.pickFork = false
	case tea.KeyEnter:
		switch m.forkIndex {
		case 0:
			m.forkLoading = true
			generation := m.pickerGeneration
			application := m.app
			return m, func() tea.Msg {
				created, err := application.ForkBranchWithOptions(protocol.BranchForkOptions{})
				return branchActionMsg{generation: generation, branch: created, action: "fork", err: err}
			}
		case 1:
			m.pickFork = false
			m.sessionOpLoading = true
			m.sessionOpGeneration++
			generation := m.sessionOpGeneration
			application := m.app
			ctx := m.ctx
			return m, func() tea.Msg {
				result, err := application.ForkSession(ctx, protocol.SessionForkOptions{})
				if err != nil {
					return sessionStoreMsg{generation: generation, path: "fork", err: err}
				}
				store, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(result.SessionPath)
				if err != nil {
					err = fmt.Errorf("open created fork %s: %w", result.SessionPath, err)
				}
				return sessionStoreMsg{generation: generation, path: "fork", store: store, err: err}
			}
		case 2:
			m.forkLoading = true
			generation := m.pickerGeneration
			application := m.app
			ctx := m.ctx
			return m, func() tea.Msg {
				result, err := application.ForkWorktree(ctx, protocol.SessionWorktreeForkOptions{})
				return worktreeForkMsg{generation: generation, result: result, err: err}
			}
		}
	}
	return m, nil
}

func (m *Model) renderForkPicker() string {
	if !m.pickFork {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render("Fork conversation") + "\n")
	if m.forkLoading {
		b.WriteString(styleCompletion.Render("  validating and creating fork…") + "\n")
	} else {
		for i, choice := range forkChoices {
			if i == m.forkIndex {
				b.WriteString(styleCompletionSelected.Render("› " + choice))
			} else {
				b.WriteString(styleCompletion.Render("  " + choice))
			}
			b.WriteString("\n")
		}
	}
	hint := "(↑/↓ choose · Enter confirm · Esc cancel)"
	if m.forkLoading {
		hint = "creating safely; the current workspace remains active"
	}
	b.WriteString(styleFooter.Render(hint))
	return b.String()
}

func (m *Model) currentSessions() ([]session.SessionInfo, error) {
	return session.NewFileIndex(session.DefaultSessionsRoot()).List(m.app.CWD())
}

func (m *Model) noSessionsResumeMessage() string {
	hint := "/new"
	if m.startupResumeRequired {
		hint = "snow"
	}
	return "no sessions to resume for " + m.app.CWD() + " (use " + hint + " to create one)"
}

func currentSessionID(a *app.App) string {
	if a == nil || a.Agent == nil {
		return ""
	}
	id, _, err := a.Agent.SessionIdentity()
	if err != nil {
		return ""
	}
	return id
}

func currentSessionPath(a *app.App) string {
	if a == nil || a.Agent == nil {
		return ""
	}
	_, path, err := a.Agent.SessionIdentity()
	if err != nil {
		return ""
	}
	return path
}

func (m *Model) startSessionPick() (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("session: wait for the current turn to finish"))
		return m, nil
	}
	m.sessionRenaming = false
	m.sessionRenameInput = ""
	m.sessionDeleting = false
	m.sessionDeleteInFlight = false
	if m.asyncIO {
		m.pickSession = true
		m.sessionLoading = true
		m.sessions = nil
		m.pickerGeneration++
		generation := m.pickerGeneration
		return m, func() tea.Msg {
			infos, err := m.currentSessions()
			return sessionListMsg{generation: generation, sessions: infos, err: err}
		}
	}
	infos, err := m.currentSessions()
	if err != nil {
		m.pushLine(styleError.Render("session list: " + err.Error()))
		if m.startupResumeRequired {
			return m, m.quitCmd()
		}
		return m, nil
	}
	if len(infos) == 0 {
		m.sessions = nil
		m.pickSession = false
		m.pushLine(styleFooter.Render(m.noSessionsResumeMessage()))
		if m.startupResumeRequired {
			return m, m.quitCmd()
		}
		return m, nil
	}
	m.sessions = infos
	m.sessionIndex = 0
	for i, info := range infos {
		if info.ID == currentSessionID(m.app) {
			m.sessionIndex = i
			break
		}
	}
	m.pickSession = true
	m.compVisible = false
	return m, nil
}

func (m *Model) handleSessionPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.sessionDeleting {
		if keyMatches(msg, m.keys.Close) {
			m.sessionDeleting = false
			return m, nil
		}
		if keyMatches(msg, m.keys.Accept) || keyMatches(msg, m.keys.Confirm) {
			return m.executeSessionDelete()
		}
		return m, nil
	}
	if m.sessionRenaming {
		switch {
		case keyMatches(msg, m.keys.Close):
			m.sessionRenaming, m.sessionRenameInput = false, ""
		case keyMatches(msg, m.keys.Accept):
			return m.executeSessionRename()
		case msg.Type == tea.KeyBackspace:
			r := []rune(m.sessionRenameInput)
			if len(r) > 0 {
				m.sessionRenameInput = string(r[:len(r)-1])
			}
		case msg.Type == tea.KeyRunes:
			if len([]rune(m.sessionRenameInput))+len(msg.Runes) <= 72 {
				m.sessionRenameInput += string(msg.Runes)
			}
		}
		return m, nil
	}
	if m.sessionLoading {
		if m.sessionDeleteInFlight {
			return m, nil
		}
		if msg.Type == tea.KeyEsc {
			m.pickSession = false
			m.sessionLoading = false
			m.pickerGeneration++
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
		}
		return m, nil
	}
	count := len(m.sessions)
	if count == 0 {
		m.pickSession = false
		return m, nil
	}
	if next, handled := movePicker(m.sessionIndex, count, pickerKeyAction(msg), m.sessionPickerVisibleItems()); handled {
		m.sessionIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.sessionIndex = (m.sessionIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.sessionIndex = (m.sessionIndex + 1) % count
	case tea.KeyPgUp:
		m.sessionIndex -= m.sessionPickerVisibleItems()
		if m.sessionIndex < 0 {
			m.sessionIndex = 0
		}
	case tea.KeyPgDown:
		m.sessionIndex += m.sessionPickerVisibleItems()
		if m.sessionIndex >= count {
			m.sessionIndex = count - 1
		}
	case tea.KeyHome:
		m.sessionIndex = 0
	case tea.KeyEnd:
		m.sessionIndex = count - 1
	case tea.KeyEsc:
		m.pickSession = false
		m.sessions = nil
		if m.startupResumeRequired {
			return m, m.quitCmd()
		}
	case tea.KeyEnter:
		return m.openSession(m.sessions[m.sessionIndex].Path)
	case tea.KeyRunes:
		switch {
		case keyMatches(msg, m.keys.BranchRename):
			m.sessionRenaming = true
			m.sessionRenameInput = m.sessions[m.sessionIndex].Name
		case keyMatches(msg, m.keys.BranchDelete):
			m.sessionDeleting = true
		}
	}
	return m, nil
}

func (m *Model) executeSessionDelete() (tea.Model, tea.Cmd) {
	if m.sessionIndex < 0 || m.sessionIndex >= len(m.sessions) {
		m.sessionDeleting = false
		return m, nil
	}
	selected := m.sessions[m.sessionIndex]
	name := selected.Name
	if name == "" {
		name = shortSessionID(selected.ID)
	}
	m.sessionDeleting = false
	m.sessionDeleteInFlight = m.asyncIO
	m.sessionLoading = m.asyncIO
	m.pickerGeneration++
	generation := m.pickerGeneration
	return m, func() tea.Msg {
		err := m.app.DeleteSession(selected.Path, selected.ID)
		return sessionDeleteMsg{generation: generation, path: selected.Path, name: name, err: err}
	}
}

func (m *Model) executeSessionRename() (tea.Model, tea.Cmd) {
	if m.sessionIndex < 0 || m.sessionIndex >= len(m.sessions) {
		m.sessionRenaming = false
		return m, nil
	}
	title := strings.TrimSpace(m.sessionRenameInput)
	m.sessionRenaming, m.sessionRenameInput = false, ""
	selected := m.sessions[m.sessionIndex]
	index := m.sessionIndex
	m.sessionLoading = m.asyncIO
	m.pickerGeneration++
	generation := m.pickerGeneration
	run := func() tea.Msg {
		var err error
		if selected.ID == currentSessionID(m.app) {
			err = m.app.RenameSession(title)
		} else {
			err = session.NewFileIndex(session.DefaultSessionsRoot()).Rename(m.app.CWD(), selected.Path, title)
		}
		return sessionRenameMsg{generation: generation, index: index, title: title, err: err}
	}
	return m, run
}

func (m *Model) startNewSession() (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("session: wait for the current turn to finish"))
		return m, nil
	}
	if m.asyncIO {
		m.sessionOpLoading = true
		m.lastStatus = "creating session…"
		m.sessionOpGeneration++
		generation := m.sessionOpGeneration
		return m, func() tea.Msg {
			st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Create(m.app.CWD())
			return sessionStoreMsg{generation: generation, path: "new", store: st, err: err}
		}
	}
	st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Create(m.app.CWD())
	if err != nil {
		m.pushLine(styleError.Render("new session: " + err.Error()))
		return m, nil
	}
	if err := m.switchSession(st); err != nil {
		_ = st.Close()
		m.pushLine(styleError.Render("new session: " + err.Error()))
		return m, nil
	}
	m.lastStatus = "new session"
	return m, nil
}

func (m *Model) openSession(path string) (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("session: wait for the current turn to finish"))
		return m, nil
	}
	m.pickSession = false
	m.sessions = nil
	if m.asyncIO {
		m.sessionOpLoading = true
		m.lastStatus = "opening session…"
		m.sessionOpGeneration++
		generation := m.sessionOpGeneration
		return m, func() tea.Msg {
			st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(path)
			return sessionStoreMsg{generation: generation, path: path, store: st, err: err}
		}
	}
	st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(path)
	if err != nil {
		m.pushLine(styleError.Render("resume session: " + err.Error()))
		return m, nil
	}
	if err := m.switchSession(st); err != nil {
		_ = st.Close()
		m.pushLine(styleError.Render("resume session: " + err.Error()))
		return m, nil
	}
	m.lastStatus = "resumed " + shortSessionID(st.ID())
	return m, nil
}
