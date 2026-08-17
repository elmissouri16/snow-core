package tui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

func branchDepth(branches []protocol.SessionBranch, branch protocol.SessionBranch) int {
	parents := map[string]string{}
	for _, b := range branches {
		parents[b.ID] = b.ParentID
	}
	depth := 0
	seen := map[string]bool{}
	for parent := branch.ParentID; parent != "" && !seen[parent]; parent = parents[parent] {
		seen[parent] = true
		depth++
		if depth > 8 {
			break
		}
	}
	return depth
}

func (m *Model) treePickerVisibleItems() int {
	total := len(m.branches)
	if total == 0 {
		return 0
	}
	visible := m.height - 12
	if m.inlineModalOverlay() {
		visible = m.availableOverlayHeight() - 4 // title, two scroll markers, hint
	}
	if visible < 1 {
		visible = 1
	}
	if visible > total {
		visible = total
	}
	return visible
}

func (m *Model) treeWindow() (start, end int) {
	total := len(m.branches)
	visible := m.treePickerVisibleItems()
	if total == 0 || total <= visible {
		return 0, total
	}
	start = m.branchIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m *Model) treePickerRows() int {
	if !m.pickTree {
		return 0
	}
	if m.treeLoading {
		return 2
	}
	start, end := m.treeWindow()
	rows := 2 + end - start
	if start > 0 {
		rows++
	}
	if end < len(m.branches) {
		rows++
	}
	return rows
}

func (m *Model) renderTreePicker() string {
	if !m.pickTree {
		return ""
	}
	if m.treeLoading {
		return styleHeaderDim.Render("branches\n  loading branches…")
	}
	start, end := m.treeWindow()
	width := max(1, m.width-2)
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render(truncateRunes(fmt.Sprintf("branches (%d)", len(m.branches)), width)) + "\n")
	if start > 0 {
		b.WriteString(styleHeaderDim.Render("  ↑ more branches") + "\n")
	}
	for i := start; i < end; i++ {
		branch := m.branches[i]
		marker := "  "
		if branch.Active {
			marker = "✓ "
		}
		name := branch.Name
		if name == "" {
			name = branch.ID
		}
		indent := strings.Repeat("  ", branchDepth(m.branches, branch))
		connector := "└─ "
		if branch.ParentID == "" {
			connector = ""
		}
		line := fmt.Sprintf("%s%s%s%s  ·  %s  ·  %d messages", marker, indent, connector, name, shortSessionID(branch.ID), branch.Messages)
		if branch.Preview != "" {
			line += "  ·  " + branch.Preview
		}
		line = truncateRunes(line, max(8, m.width-4))
		if i == m.branchIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	if end < len(m.branches) {
		b.WriteString(styleHeaderDim.Render("  ↓ more branches") + "\n")
	}
	hint := fmt.Sprintf("(%s choose · %s switch · %s fork · %s rename · %s delete · %s cancel)", m.keys.PickerDown.Help().Key, m.keys.Accept.Help().Key, m.keys.BranchFork.Help().Key, m.keys.BranchRename.Help().Key, m.keys.BranchDelete.Help().Key, m.keys.Close.Help().Key)
	if m.branchAction == "fork" {
		hint = "Fork name (blank = automatic): " + m.branchInput + "_"
	}
	if m.branchAction == "rename" {
		hint = "Rename: " + m.branchInput + "_"
	}
	if m.branchAction == "delete" {
		hint = "Delete selected leaf branch? " + m.keys.Confirm.Help().Key + "/" + m.keys.Close.Help().Key
	}
	b.WriteString(styleFooter.Render(truncateRunes(hint, width)))
	return strings.TrimSuffix(b.String(), "\n")
}

// handlePermissionPick resolves an interactive permission request with
// arrows + Enter. Esc denies (safe default).
func (m *Model) handlePermissionPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.permChoice = (m.permChoice - 1 + permChoices) % permChoices
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.permChoice = (m.permChoice + 1) % permChoices
	case tea.KeyEnter:
		m.resolvePermission()
	case tea.KeyEsc:
		m.permChoice = permChoiceDeny
		m.resolvePermission()
	}
	return m, nil
}

// resolvePermission delivers the selected decision to the blocked asker and
// clears the picker.
func (m *Model) resolvePermission() {
	d := permission.DecisionDeny
	switch m.permChoice {
	case permChoiceAllow:
		d = permission.DecisionAllow
	case permChoiceAlways:
		d = permission.DecisionAllowAlways
	}
	m.permPending = false
	m.permRequest = nil
	m.permAgent = nil
	if m.asker != nil {
		_ = m.asker.Respond(d)
	}
	m.pushLine(styleFooter.Render("permission: " + string(d)))
}

// renderPermissionPicker renders the allow/deny selector.
func (m *Model) renderPermissionPicker() string {
	if !m.permPending || m.permRequest == nil {
		return ""
	}
	req := m.permRequest
	label := "🔐 " + req.Tool + " · " + string(req.Risk)
	if m.permAgent != nil {
		label += " · " + string(m.permAgent.Path)
	}
	if len(req.Paths) > 0 {
		label += " · " + strings.Join(req.Paths, ", ")
	}
	if req.Reason != "" {
		label += " · " + req.Reason
	}
	var b strings.Builder
	b.WriteString(styleTool.Render(label) + "\n")
	options := []struct {
		id   int
		name string
		hint string
	}{
		{permChoiceAllow, "Allow", "this request"},
		{permChoiceAlways, "Allow always", "all matching requests this session"},
		{permChoiceDeny, "Deny", "this request"},
	}
	for _, o := range options {
		line := o.name
		if o.hint != "" {
			line += "  (" + o.hint + ")"
		}
		if o.id == m.permChoice {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleFooter.Render("(↑/↓ choose, Enter confirm, Esc deny)"))
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) startPermissionModePick() (tea.Model, tea.Cmd) {
	m.pickPermissionMode = true
	m.permissionModeIndex = 0
	if m.app != nil {
		switch m.app.Perm.Mode() {
		case permission.ModeAllow:
			m.permissionModeIndex = 1
		case permission.ModeDeny:
			m.permissionModeIndex = 2
		}
	}
	m.compVisible = false
	return m, nil
}

func (m *Model) handlePermissionModePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	const count = 3
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.permissionModeIndex = (m.permissionModeIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.permissionModeIndex = (m.permissionModeIndex + 1) % count
	case tea.KeyEnter:
		m.applyPermissionMode()
	case tea.KeyEsc:
		m.pickPermissionMode = false
	}
	return m, nil
}

func (m *Model) applyPermissionMode() {
	modes := []permission.Mode{permission.ModeAsk, permission.ModeAllow, permission.ModeDeny}
	mode := modes[m.permissionModeIndex]
	if err := m.setPermissionMode(mode, true); err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return
	}
	m.pickPermissionMode = false
}

func (m *Model) setPermissionMode(mode permission.Mode, announce bool) error {
	if m.app == nil {
		return fmt.Errorf("permissions: app is not ready")
	}
	if mode != permission.ModeAsk && mode != permission.ModeAllow && mode != permission.ModeDeny {
		return fmt.Errorf("invalid permission mode %q", mode)
	}
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.PermissionMode = string(mode)
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist permissions: %w", err)
	}
	if err := m.app.SetPermissionDefault(mode); err != nil {
		return err
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.PermissionMode = candidate.PermissionMode
	if announce {
		m.pushLine(styleFooter.Render("permission mode: " + string(mode)))
	}
	return nil
}

func (m *Model) renderPermissionModePicker() string {
	if !m.pickPermissionMode {
		return ""
	}
	modes := []permission.Mode{permission.ModeAsk, permission.ModeAllow, permission.ModeDeny}
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render("permissions") + "\n")
	for i, mode := range modes {
		line := string(mode)
		if i == m.permissionModeIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleFooter.Render("(↑/↓ choose, Enter apply, Esc cancel)"))
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) startSettings() (tea.Model, tea.Cmd) {
	if m.app == nil {
		return m, nil
	}
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("settings: wait for the current turn to finish"))
		return m, nil
	}
	m.pickSettings = true
	m.settingsIndex = 0
	m.settingsStatus = ""
	m.settingsError = ""
	m.compVisible = false
	return m, nil
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.settingsIndex = (m.settingsIndex - 1 + settingsCount) % settingsCount
	case tea.KeyDown, tea.KeyTab:
		m.settingsIndex = (m.settingsIndex + 1) % settingsCount
	case tea.KeyEsc:
		m.pickSettings = false
		m.settingsError = ""
		m.settingsStatus = ""
	case tea.KeyEnter:
		if m.settingsIndex == settingsModel {
			if m.compatibleLoginPending {
				m.settingsStatus = "waiting for openai-compatible model discovery"
				return m, nil
			}
			m.pickSettings = false
			m.settingsReturnToPanel = true
			return m.startModelPick()
		}
		m.cycleSetting(1)
	case tea.KeyLeft:
		if m.settingsIndex != settingsModel {
			m.cycleSetting(-1)
		}
	case tea.KeyRight:
		if m.settingsIndex != settingsModel {
			m.cycleSetting(1)
		}
	}
	return m, nil
}

func (m *Model) cycleSetting(direction int) {
	m.settingsError = ""
	m.settingsStatus = ""
	var err error
	switch m.settingsIndex {
	case settingsTheme:
		values := m.themeChoices()
		next := cycleValue(values, m.themeName, direction)
		err = m.setTheme(next, false)
		if err == nil {
			m.settingsStatus = "theme saved"
		}
	case settingsThinking:
		levels := m.app.Agent.Model().SupportedThinkingLevels()
		current := m.app.Agent.Thinking()
		next := cycleValue(levels, current, direction)
		err = m.setThinking(next, false)
		if err == nil {
			m.settingsStatus = "thinking effort saved"
		}
	case settingsReasoningSummary:
		if !m.chatGPTSettingsEnabled() {
			m.settingsStatus = "reasoning summary is available for ChatGPT only"
			return
		}
		values := protocol.KnownReasoningSummaries()
		next := cycleValue(values, m.app.Agent.ReasoningSummary(), direction)
		err = m.setReasoningSummary(next)
		if err == nil {
			m.settingsStatus = "reasoning summary saved"
		}
	case settingsTextVerbosity:
		if !m.chatGPTSettingsEnabled() {
			m.settingsStatus = "text verbosity is available for ChatGPT only"
			return
		}
		values := protocol.KnownTextVerbosities()
		next := cycleValue(values, m.app.Agent.TextVerbosity(), direction)
		err = m.setTextVerbosity(next)
		if err == nil {
			m.settingsStatus = "text verbosity saved"
		}
	case settingsPermission:
		values := []permission.Mode{permission.ModeAsk, permission.ModeAllow, permission.ModeDeny}
		next := cycleValue(values, m.app.Perm.Mode(), direction)
		err = m.setPermissionMode(next, false)
		if err == nil {
			m.settingsStatus = "permission mode saved"
		}
	case settingsSubagents:
		next := cycleValue([]bool{false, true}, m.app.Cfg.Subagents.Enabled, direction)
		err = m.setSubagentsEnabled(next)
		if err == nil {
			m.settingsStatus = "subagent setting saved; restart Snow to apply"
		}
	case settingsSubagentConcurrency:
		next := m.app.Cfg.Subagents.MaxConcurrentThreads + direction
		if next < 1 {
			next = 1
		}
		err = m.setSubagentConcurrency(next)
		if err == nil {
			m.settingsStatus = "subagent concurrency saved; restart Snow to apply"
		}
	case settingsSkills:
		next := cycleValue([]bool{true, false}, !m.app.Cfg.Skills.Disabled, direction)
		err = m.setSkillsEnabled(next)
		if err == nil {
			m.settingsStatus = "skills setting saved; restart Snow to apply"
		}
	}
	if err != nil {
		m.settingsError = err.Error()
		m.settingsStatus = ""
	}
}

func onOff(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func cycleValue[T comparable](values []T, current T, direction int) T {
	if len(values) == 0 {
		return current
	}
	index := 0
	for i, value := range values {
		if value == current {
			index = i
			break
		}
	}
	if direction < 0 {
		index = (index - 1 + len(values)) % len(values)
	} else {
		index = (index + 1) % len(values)
	}
	return values[index]
}

func (m *Model) chatGPTSettingsEnabled() bool {
	return m.app != nil && m.app.ProviderID == "chatgpt"
}

func (m *Model) loadAuxiliaryTUIConfig() {
	if m.app == nil {
		return
	}
	themes, themeDiagnostics := config.LoadThemes(config.GlobalDir(), m.app.ProjectInputRoot, m.app.ProjectAllowed)
	scopes, keyDiagnostics := config.LoadKeybindingScopes(config.GlobalDir(), m.app.ProjectInputRoot, m.app.ProjectAllowed)
	m.customThemes = themes
	m.auxDiagnostics = append(themeDiagnostics, keyDiagnostics...)
	m.keys = tuiKeys
	for _, scope := range scopes {
		keys, err := applyKeybindingOverrides(m.keys, scope.File.Bindings)
		if err != nil {
			m.auxDiagnostics = append(m.auxDiagnostics, config.Diagnostic{Path: scope.Path, Message: err.Error()})
			continue
		}
		m.keys = keys
	}
	m.editor.KeyMap.InsertNewline = m.keys.Newline
	m.userInputEditor.KeyMap.InsertNewline = m.keys.Newline
}

func (m *Model) themeChoices() []string {
	values := themeChoices()
	var custom []string
	for name := range m.customThemes {
		custom = append(custom, name)
	}
	sort.Strings(custom)
	return append(values, custom...)
}

func (m *Model) setTheme(name string, announce bool) error {
	return m.applyThemeSelection(name, announce, true)
}

func (m *Model) refreshThemeStyles() {
	normalizeTextareaStyles(&m.editor)
	normalizeTextareaStyles(&m.userInputEditor)
	m.spinner.Style = lipgloss.NewStyle().Foreground(colorAccent)
	m.thinkingSpinner.Style = lipgloss.NewStyle().Foreground(colorAccent)
}

func (m *Model) applyThemeSelection(name string, announce, persist bool) error {
	if _, custom := m.customThemes[name]; !custom {
		if err := config.ValidateTUITheme(name); err != nil {
			return err
		}
	}
	if name == "" {
		name = "default"
	}
	old := m.themeName
	var applyErr error
	if custom, ok := m.customThemes[name]; ok {
		applyErr = applyCustomTUITheme(custom)
	} else {
		applyErr = applyTUITheme(name)
	}
	if applyErr != nil {
		return applyErr
	}
	m.refreshThemeStyles()
	if m.app != nil && persist {
		candidate, err := m.persistConfig(func(latest *config.Config) error {
			latest.TUI.Theme = name
			return nil
		})
		if err != nil {
			if custom, ok := m.customThemes[old]; ok {
				_ = applyCustomTUITheme(custom)
			} else {
				_ = applyTUITheme(old)
			}
			m.refreshThemeStyles()
			return fmt.Errorf("persist theme: %w", err)
		}
		m.app.PersistedCfg = candidate
		m.app.Cfg.TUI.Theme = name
	}
	m.themeName = name
	if announce {
		m.pushLine(styleFooter.Render("theme: " + name))
	}
	return nil
}

func (m *Model) setReasoningSummary(summary protocol.ReasoningSummary) error {
	old := m.app.Agent.ReasoningSummary()
	if err := m.app.Agent.SetReasoningSummary(summary); err != nil {
		return err
	}
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.ReasoningSummary = string(m.app.Agent.ReasoningSummary())
		return nil
	})
	if err != nil {
		_ = m.app.Agent.SetReasoningSummary(old)
		return fmt.Errorf("persist reasoning summary: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.ReasoningSummary = candidate.ReasoningSummary
	return nil
}

func (m *Model) setTextVerbosity(verbosity protocol.TextVerbosity) error {
	old := m.app.Agent.TextVerbosity()
	if err := m.app.Agent.SetTextVerbosity(verbosity); err != nil {
		return err
	}
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.TextVerbosity = string(m.app.Agent.TextVerbosity())
		return nil
	})
	if err != nil {
		_ = m.app.Agent.SetTextVerbosity(old)
		return fmt.Errorf("persist text verbosity: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.TextVerbosity = candidate.TextVerbosity
	return nil
}

func (m *Model) setSubagentsEnabled(enabled bool) error {
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.Subagents.Enabled = enabled
		return latest.Subagents.ValidateSubagents()
	})
	if err != nil {
		return fmt.Errorf("persist subagent setting: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Subagents.Enabled = enabled
	return nil
}

func (m *Model) setSubagentConcurrency(limit int) error {
	if limit < 1 {
		return errors.New("subagent concurrency must be positive")
	}
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.Subagents.MaxConcurrentThreads = limit
		if latest.Subagents.MaxAgentsPerSession < limit {
			latest.Subagents.MaxAgentsPerSession = limit
		}
		return latest.Subagents.ValidateSubagents()
	})
	if err != nil {
		return fmt.Errorf("persist subagent concurrency: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Subagents.MaxConcurrentThreads = limit
	if m.app.Cfg.Subagents.MaxAgentsPerSession < limit {
		m.app.Cfg.Subagents.MaxAgentsPerSession = limit
	}
	return nil
}

func (m *Model) setSkillsEnabled(enabled bool) error {
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.Skills.Disabled = !enabled
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist skills setting: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Skills.Disabled = candidate.Skills.Disabled
	return nil
}

func (m *Model) renderSettings() string {
	if !m.pickSettings || m.app == nil {
		return ""
	}
	model := m.app.Agent.Model()
	rows := []string{
		"Model  " + model.Provider + "/" + model.ID,
		"Theme  " + m.themeName,
		"Thinking effort  " + string(m.app.Agent.Thinking()),
		"Reasoning summary  " + string(m.app.Agent.ReasoningSummary()),
		"Text verbosity  " + string(m.app.Agent.TextVerbosity()),
		"Permission mode  " + string(m.app.Perm.Mode()),
		"Subagents  " + onOff(m.app.Cfg.Subagents.Enabled) + " (restart to apply)",
		fmt.Sprintf("Concurrent subagents  %d (restart to apply)", m.app.Cfg.Subagents.MaxConcurrentThreads),
		"Agent Skills  " + onOff(!m.app.Cfg.Skills.Disabled) + " (restart to apply)",
	}
	if !m.chatGPTSettingsEnabled() {
		rows[settingsReasoningSummary] += "  (ChatGPT only)"
		rows[settingsTextVerbosity] += "  (ChatGPT only)"
	}
	var b strings.Builder
	header := styleHeaderDim.Render("settings")
	if m.inlineTranscript {
		header = styleHeaderDim.Render("settings  (↑/↓ row · ←/→ change · Enter select · Esc close)")
		if m.settingsError != "" {
			header = styleError.Render("settings: " + m.settingsError)
		} else if m.settingsStatus != "" {
			header = styleFooter.Render("settings: " + m.settingsStatus)
		}
	}
	b.WriteString(header + "\n")
	start, end := 0, len(rows)
	if m.inlineTranscript {
		// Header consumes one row; keep a selected-row-centered window for short
		// terminals rather than clipping the bottom of the fixed list.
		visible := max(1, m.availableOverlayHeight()-1)
		if end > visible {
			start = m.settingsIndex - visible/2
			if start < 0 {
				start = 0
			}
			if start+visible > end {
				start = end - visible
			}
			end = start + visible
		}
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := "  "
		style := styleCompletion
		if i == m.settingsIndex {
			prefix = "› "
			style = styleCompletionSelected
		} else if !m.chatGPTSettingsEnabled() && (i == settingsReasoningSummary || i == settingsTextVerbosity) {
			style = styleHeaderDim
		}
		b.WriteString(style.Render(prefix + row))
		b.WriteString("\n")
	}
	if !m.inlineTranscript {
		b.WriteString(styleFooter.Render("(↑/↓ row, ←/→ change, Enter select, Esc close)"))
		if m.settingsError != "" {
			b.WriteString("\n" + styleError.Render(m.settingsError))
		} else if m.settingsStatus != "" {
			b.WriteString("\n" + styleFooter.Render(m.settingsStatus))
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}
