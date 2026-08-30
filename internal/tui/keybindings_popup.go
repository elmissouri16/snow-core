package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/config"
)

type keybindingScope uint8

const (
	keybindingScopeGlobal keybindingScope = iota
	keybindingScopeProject
)

type keybindingCaptureMode uint8

const (
	keybindingCaptureNone keybindingCaptureMode = iota
	keybindingCaptureReplaceOne
	keybindingCaptureReplaceAll
	keybindingCaptureAdd
)

type keybindingAction struct {
	name  string
	label string
	group string
}

var keybindingActions = []keybindingAction{
	{name: "submit", label: "Submit", group: "Composer"},
	{name: "follow_up", label: "Follow-up", group: "Composer"},
	{name: "newline", label: "Newline", group: "Composer"},
	{name: "paste", label: "Paste", group: "Composer"},
	{name: "abort", label: "Abort", group: "Composer"},
	{name: "quit", label: "Quit", group: "Composer"},
	{name: "toggle_mode", label: "Toggle mode", group: "Composer"},
	{name: "thinking", label: "Thinking effort", group: "Composer"},
	{name: "models", label: "Models", group: "Global"},
	{name: "agents", label: "Agent fleet", group: "Global"},
	{name: "processes", label: "Process fleet", group: "Global"},
	{name: "page_up", label: "Page up", group: "Transcript"},
	{name: "page_down", label: "Page down", group: "Transcript"},
	{name: "top", label: "Top", group: "Transcript"},
	{name: "bottom", label: "Bottom", group: "Transcript"},
	{name: "line_up", label: "Line up", group: "Transcript"},
	{name: "line_down", label: "Line down", group: "Transcript"},
	{name: "picker_up", label: "Previous item", group: "Pickers"},
	{name: "picker_down", label: "Next item", group: "Pickers"},
	{name: "picker_previous", label: "Previous field", group: "Pickers"},
	{name: "picker_next", label: "Next field", group: "Pickers"},
	{name: "picker_page_up", label: "Picker page up", group: "Pickers"},
	{name: "picker_page_down", label: "Picker page down", group: "Pickers"},
	{name: "picker_top", label: "First item", group: "Pickers"},
	{name: "picker_bottom", label: "Last item", group: "Pickers"},
	{name: "accept", label: "Accept", group: "Pickers"},
	{name: "close", label: "Close", group: "Pickers"},
	{name: "branch_fork", label: "Fork branch", group: "Branches"},
	{name: "branch_rename", label: "Rename branch", group: "Branches"},
	{name: "branch_delete", label: "Delete branch", group: "Branches"},
	{name: "confirm", label: "Confirm", group: "Branches"},
}

func (m *Model) keybindingsModalVisible() bool { return m.pickKeybindings }

func (m *Model) startKeybindings(returnToSettings bool) (tea.Model, tea.Cmd) {
	if m.app == nil {
		return m, nil
	}
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("keybindings: wait for the current turn to finish"))
		return m, nil
	}
	m.closeTranscriptSelectionContextMenu()
	m.pickSettings = false
	m.pickKeybindings = true
	m.keybindingsReturnToSettings = returnToSettings
	m.keybindingsScope = keybindingScopeGlobal
	m.keybindingsIndex = 0
	m.keybindingsEditing = false
	m.keybindingsCapture = keybindingCaptureNone
	m.keybindingsStatus = ""
	m.keybindingsError = ""
	m.refreshKeybindingOverrides()
	m.compVisible = false
	return m, nil
}

func (m *Model) closeKeybindings() {
	returnToSettings := m.keybindingsReturnToSettings
	m.pickKeybindings = false
	m.keybindingsReturnToSettings = false
	m.keybindingsEditing = false
	m.keybindingsCapture = keybindingCaptureNone
	m.keybindingsStatus = ""
	m.keybindingsError = ""
	if returnToSettings {
		m.pickSettings = true
	}
}

func (m *Model) handleKeybindingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keybindingsCapture != keybindingCaptureNone {
		m.captureKeybinding(msg)
		return m, nil
	}
	if m.keybindingsEditing {
		return m.handleKeybindingEditorKey(msg)
	}
	m.keybindingsError = ""
	switch msg.Type {
	case tea.KeyEsc:
		m.closeKeybindings()
		return m, nil
	case tea.KeyEnter:
		m.openKeybindingEditor()
		return m, nil
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'S':
				m.toggleKeybindingScope()
				return m, nil
			case 'R':
				m.resetSelectedKeybinding()
				return m, nil
			}
		}
	}
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.keybindingsIndex = (m.keybindingsIndex - 1 + len(keybindingActions)) % len(keybindingActions)
	case tea.KeyDown, tea.KeyTab:
		m.keybindingsIndex = (m.keybindingsIndex + 1) % len(keybindingActions)
	case tea.KeyPgUp:
		m.keybindingsIndex = max(0, m.keybindingsIndex-8)
	case tea.KeyPgDown:
		m.keybindingsIndex = min(len(keybindingActions)-1, m.keybindingsIndex+8)
	case tea.KeyHome:
		m.keybindingsIndex = 0
	case tea.KeyEnd:
		m.keybindingsIndex = len(keybindingActions) - 1
	}
	return m, nil
}

func (m *Model) openKeybindingEditor() {
	if len(keybindingActions) == 0 {
		return
	}
	action := keybindingActions[clampPickerIndex(m.keybindingsIndex, len(keybindingActions))]
	m.keybindingsDraft = m.editableKeybinding(action.name)
	m.keybindingsEditing = true
	m.keybindingsEditIndex = 0
	m.keybindingsStatus = ""
	m.keybindingsError = ""
}

func (m *Model) handleKeybindingEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.keybindingsError = ""
	rowCount := len(m.keybindingsDraft) + 2 // Replace all, Add key.
	if msg.Type == tea.KeyCtrlS {
		m.saveKeybindingDraft()
		return m, nil
	}
	if msg.Type == tea.KeyEsc {
		m.keybindingsEditing = false
		m.keybindingsDraft = nil
		m.keybindingsStatus = "changes discarded"
		return m, nil
	}
	if msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete {
		if m.keybindingsEditIndex < len(m.keybindingsDraft) {
			m.keybindingsDraft = append(m.keybindingsDraft[:m.keybindingsEditIndex], m.keybindingsDraft[m.keybindingsEditIndex+1:]...)
			m.keybindingsEditIndex = min(m.keybindingsEditIndex, len(m.keybindingsDraft)+1)
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'R' {
		m.keybindingsDraft = m.inheritedKeybinding(m.selectedKeybindingAction().name)
		m.keybindingsEditIndex = 0
		m.keybindingsStatus = "draft reset to inherited/default keys"
		return m, nil
	}
	if msg.Type == tea.KeyEnter {
		switch {
		case m.keybindingsEditIndex < len(m.keybindingsDraft):
			m.keybindingsCapture = keybindingCaptureReplaceOne
		case m.keybindingsEditIndex == len(m.keybindingsDraft):
			m.keybindingsCapture = keybindingCaptureReplaceAll
		default:
			m.keybindingsCapture = keybindingCaptureAdd
		}
		m.keybindingsStatus = "press one key"
		return m, nil
	}
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.keybindingsEditIndex = (m.keybindingsEditIndex - 1 + rowCount) % rowCount
	case tea.KeyDown, tea.KeyTab:
		m.keybindingsEditIndex = (m.keybindingsEditIndex + 1) % rowCount
	case tea.KeyHome:
		m.keybindingsEditIndex = 0
	case tea.KeyEnd:
		m.keybindingsEditIndex = rowCount - 1
	}
	return m, nil
}

func (m *Model) captureKeybinding(msg tea.KeyMsg) {
	name, err := keyNameFromMessage(msg)
	if err != nil {
		m.keybindingsError = err.Error()
		m.keybindingsStatus = "press one supported key"
		return
	}
	switch m.keybindingsCapture {
	case keybindingCaptureReplaceOne:
		if m.keybindingsEditIndex < len(m.keybindingsDraft) {
			m.keybindingsDraft[m.keybindingsEditIndex] = name
		}
	case keybindingCaptureReplaceAll:
		m.keybindingsDraft = []string{name}
		m.keybindingsEditIndex = 0
	case keybindingCaptureAdd:
		if !containsString(m.keybindingsDraft, name) {
			m.keybindingsDraft = append(m.keybindingsDraft, name)
		}
		m.keybindingsEditIndex = len(m.keybindingsDraft) - 1
	}
	m.keybindingsDraft = uniqueKeyNames(m.keybindingsDraft)
	m.keybindingsCapture = keybindingCaptureNone
	m.keybindingsStatus = "captured " + name + " · Ctrl+S saves"
	m.keybindingsError = ""
}

func keyNameFromMessage(msg tea.KeyMsg) (string, error) {
	if msg.Paste || msg.Type == tea.KeyRunes && len(msg.Runes) != 1 {
		return "", fmt.Errorf("pasted or multi-rune input cannot be a shortcut")
	}
	name := strings.ToLower(strings.TrimSpace(msg.String()))
	if !validKeyName(name) {
		return "", fmt.Errorf("unsupported shortcut %q", sanitizeTerminalLine(msg.String()))
	}
	return name, nil
}

func uniqueKeyNames(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func containsString(values []string, value string) bool {
	return slices.Contains(values, value)
}

func (m *Model) selectedKeybindingAction() keybindingAction {
	return keybindingActions[clampPickerIndex(m.keybindingsIndex, len(keybindingActions))]
}

func (m *Model) toggleKeybindingScope() {
	if m.keybindingsScope == keybindingScopeGlobal {
		if !m.app.ProjectAllowed {
			m.keybindingsError = "project keybindings require a trusted project"
			return
		}
		m.keybindingsScope = keybindingScopeProject
		m.keybindingsStatus = "editing project overrides"
	} else {
		m.keybindingsScope = keybindingScopeGlobal
		m.keybindingsStatus = "editing global overrides"
	}
}

func (m *Model) saveKeybindingDraft() {
	if len(m.keybindingsDraft) == 0 {
		m.keybindingsError = "at least one binding is required"
		return
	}
	action := m.selectedKeybindingAction()
	if err := m.persistKeybinding(action.name, uniqueKeyNames(m.keybindingsDraft), false); err != nil {
		m.keybindingsError = err.Error()
		return
	}
	m.keybindingsEditing = false
	m.keybindingsDraft = nil
	m.keybindingsStatus = action.label + " saved and applied"
	if m.keybindingsScope == keybindingScopeGlobal && m.app != nil && m.app.ProjectAllowed {
		if _, shadowed := m.keybindingsProjectOverrides[action.name]; shadowed {
			m.keybindingsStatus = action.label + " saved globally; project override remains active"
		}
	}
}

func (m *Model) resetSelectedKeybinding() {
	action := m.selectedKeybindingAction()
	if err := m.persistKeybinding(action.name, nil, true); err != nil {
		m.keybindingsError = err.Error()
		return
	}
	if m.keybindingsScope == keybindingScopeProject {
		m.keybindingsStatus = action.label + " now inherits global/default keys"
	} else if m.app != nil && m.app.ProjectAllowed {
		if _, shadowed := m.keybindingsProjectOverrides[action.name]; shadowed {
			m.keybindingsStatus = action.label + " global override removed; project override remains active"
		} else {
			m.keybindingsStatus = action.label + " reset to default"
		}
	} else {
		m.keybindingsStatus = action.label + " reset to default"
	}
}

func (m *Model) persistKeybinding(action string, values []string, remove bool) error {
	scope, err := m.keybindingWriteScope()
	if err != nil {
		return err
	}
	scope.Validate = func(file config.KeybindingsFile) error {
		return m.validateKeybindingCandidate(file.Bindings)
	}
	_, err = config.UpdateKeybindings(scope, func(file *config.KeybindingsFile) error {
		candidate := cloneKeybindingMap(file.Bindings)
		if remove {
			delete(candidate, action)
		} else {
			candidate[action] = slices.Clone(values)
		}
		file.Bindings = candidate
		return nil
	})
	if err != nil {
		return err
	}
	m.loadAuxiliaryTUIConfig()
	m.refreshKeybindingOverrides()
	return nil
}

func (m *Model) keybindingWriteScope() (config.KeybindingWriteScope, error) {
	if m.keybindingsScope == keybindingScopeProject {
		if m.app == nil || !m.app.ProjectAllowed {
			return config.KeybindingWriteScope{}, fmt.Errorf("project keybindings require a trusted project")
		}
		globalRoot := config.GlobalDir()
		return config.KeybindingWriteScope{
			Path:             filepath.Join(m.app.ProjectInputRoot, ".snow", "keybindings.yaml"),
			ConfinedRoot:     m.app.ProjectInputRoot,
			CoordinationRoot: globalRoot,
			CoordinationPath: filepath.Join(globalRoot, "keybindings.yaml"),
		}, nil
	}
	root := config.GlobalDir()
	return config.KeybindingWriteScope{
		Path: filepath.Join(root, "keybindings.yaml"), ConfinedRoot: root, Global: true,
		CoordinationRoot: root, CoordinationPath: filepath.Join(root, "keybindings.yaml"),
	}, nil
}

func (m *Model) validateKeybindingCandidate(selected map[string][]string) error {
	global, project := m.rawKeybindingScopes()
	if m.keybindingsScope == keybindingScopeProject {
		project = selected
	} else {
		global = selected
	}
	keys, err := applyKeybindingOverrides(tuiKeys, global)
	if err != nil {
		return err
	}
	if m.app != nil && m.app.ProjectAllowed {
		keys, err = applyKeybindingOverrides(keys, project)
		if err != nil {
			return err
		}
	}
	_ = keys
	return nil
}

func (m *Model) rawKeybindingScopes() (global, project map[string][]string) {
	global = map[string][]string{}
	project = map[string][]string{}
	if m.app == nil {
		return global, project
	}
	scopes, _ := config.LoadKeybindingScopes(config.GlobalDir(), m.app.ProjectInputRoot, m.app.ProjectAllowed)
	globalPath := filepath.Join(config.GlobalDir(), "keybindings.yaml")
	projectPath := filepath.Join(m.app.ProjectInputRoot, ".snow", "keybindings.yaml")
	for _, scope := range scopes {
		switch filepath.Clean(scope.Path) {
		case filepath.Clean(globalPath):
			global = cloneKeybindingMap(scope.File.Bindings)
		case filepath.Clean(projectPath):
			project = cloneKeybindingMap(scope.File.Bindings)
		}
	}
	return global, project
}

func (m *Model) refreshKeybindingOverrides() {
	global, project := m.rawKeybindingScopes()
	m.keybindingsGlobalOverrides = global
	m.keybindingsProjectOverrides = project
}

func cloneKeybindingMap(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for action, values := range in {
		out[action] = slices.Clone(values)
	}
	return out
}

func (m *Model) inheritedKeybinding(action string) []string {
	if m.keybindingsScope == keybindingScopeGlobal {
		return slices.Clone(config.DefaultKeybindings()[action])
	}
	global := m.keybindingsGlobalOverrides
	keys, err := applyKeybindingOverrides(tuiKeys, global)
	if err != nil {
		return slices.Clone(config.DefaultKeybindings()[action])
	}
	return slices.Clone(keybindingForAction(keys, action).Keys())
}

func (m *Model) editableKeybinding(action string) []string {
	if m.keybindingsScope == keybindingScopeProject {
		if values, ok := m.keybindingsProjectOverrides[action]; ok {
			return slices.Clone(values)
		}
		return m.inheritedKeybinding(action)
	}
	if values, ok := m.keybindingsGlobalOverrides[action]; ok {
		return slices.Clone(values)
	}
	return slices.Clone(config.DefaultKeybindings()[action])
}

func keybindingSourceFromMaps(action string, projectAllowed bool, global, project map[string][]string) string {
	if projectAllowed {
		if _, ok := project[action]; ok {
			return "project"
		}
	}
	if _, ok := global[action]; ok {
		return "global"
	}
	return "default"
}

func keybindingForAction(keys tuiKeyMap, action string) key.Binding {
	switch action {
	case "submit":
		return keys.Submit
	case "follow_up":
		return keys.FollowUp
	case "newline":
		return keys.Newline
	case "paste":
		return keys.Paste
	case "abort":
		return keys.Abort
	case "quit":
		return keys.Quit
	case "toggle_mode":
		return keys.Mode
	case "thinking":
		return keys.Thinking
	case "models":
		return keys.Models
	case "agents":
		return keys.Agents
	case "processes":
		return keys.Processes
	case "page_up":
		return keys.PageUp
	case "page_down":
		return keys.PageDown
	case "top":
		return keys.Top
	case "bottom":
		return keys.Bottom
	case "line_up":
		return keys.LineUp
	case "line_down":
		return keys.LineDown
	case "picker_up":
		return keys.PickerUp
	case "picker_down":
		return keys.PickerDown
	case "picker_previous":
		return keys.PickerPrev
	case "picker_next":
		return keys.PickerNext
	case "picker_page_up":
		return keys.PickerPageUp
	case "picker_page_down":
		return keys.PickerPageDown
	case "picker_top":
		return keys.PickerTop
	case "picker_bottom":
		return keys.PickerBottom
	case "accept":
		return keys.Accept
	case "close":
		return keys.Close
	case "branch_fork":
		return keys.BranchFork
	case "branch_rename":
		return keys.BranchRename
	case "branch_delete":
		return keys.BranchDelete
	case "confirm":
		return keys.Confirm
	default:
		return key.Binding{}
	}
}

func (m *Model) overlayKeybindingsModal(frame string) string {
	return m.overlayCenteredModal(frame, m.renderKeybindings())
}

func (m *Model) renderKeybindings() string {
	if !m.pickKeybindings || m.app == nil {
		return ""
	}
	geometry := m.pickerCardGeometry()
	scope := "Global"
	if m.keybindingsScope == keybindingScopeProject {
		scope = "Project"
	}
	title := "Keybindings · " + scope
	header := renderPickerCardHeader(title, fmt.Sprintf("%d of %d", m.keybindingsIndex+1, len(keybindingActions)), geometry.innerWidth)
	message := styleHeaderDim.Render(truncateDisplayText(" Changes save and apply immediately", geometry.innerWidth))
	if m.keybindingsError != "" {
		message = styleError.Render(truncateDisplayText(" "+sanitizeTerminalLine(m.keybindingsError), geometry.innerWidth))
	} else if m.keybindingsStatus != "" {
		message = styleFooter.Render(truncateDisplayText(" "+sanitizeTerminalLine(m.keybindingsStatus), geometry.innerWidth))
	}
	separator := styleSep.Render(strings.Repeat("─", geometry.innerWidth))
	bodyHeight := max(1, geometry.innerHeight-4)
	bodyText := m.renderKeybindingActionRows(geometry.innerWidth, bodyHeight)
	footerText := " ↑/↓ navigate · Enter edit · S scope · R reset/inherit · Esc close "
	if m.keybindingsEditing {
		bodyText = m.renderKeybindingEditorRows(geometry.innerWidth, bodyHeight)
		footerText = " ↑/↓ navigate · Enter capture · Del remove · R reset · Ctrl+S save · Esc cancel "
	}
	if m.keybindingsCapture != keybindingCaptureNone {
		footerText = " Press one supported key (Enter and Esc can be captured) "
	}
	body := lipgloss.NewStyle().Width(geometry.innerWidth).Height(bodyHeight).MaxWidth(geometry.innerWidth).MaxHeight(bodyHeight).Render(bodyText)
	footer := styleFooter.Render(truncateDisplayText(footerText, geometry.innerWidth))
	return renderPickerCard(lipgloss.JoinVertical(lipgloss.Left, header, message, separator, body, footer), geometry)
}

func (m *Model) renderKeybindingActionRows(width, height int) string {
	if len(keybindingActions) == 0 || height <= 0 {
		return ""
	}
	selected := clampPickerIndex(m.keybindingsIndex, len(keybindingActions))
	start, end := settingsCardWindow(selected, len(keybindingActions), height)
	global, project := m.keybindingsGlobalOverrides, m.keybindingsProjectOverrides
	projectAllowed := m.app != nil && m.app.ProjectAllowed
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		action := keybindingActions[i]
		prefix := "  "
		style := styleCompletion
		if i == selected {
			prefix = "› "
			style = styleCompletionSelected
		}
		binding := strings.Join(keybindingForAction(m.keys, action.name).Keys(), "/")
		source := keybindingSourceFromMaps(action.name, projectAllowed, global, project)
		line := fmt.Sprintf("%s%-10s %-18s %-20s [%s]", prefix, action.group, action.label, binding, source)
		rows = append(rows, style.Render(truncateDisplayText(line, width)))
	}
	return strings.Join(rows, "\n")
}

func (m *Model) renderKeybindingEditorRows(width, height int) string {
	action := m.selectedKeybindingAction()
	rows := make([]string, 0, len(m.keybindingsDraft)+3)
	rows = append(rows, styleHeaderDim.Render(truncateDisplayText("Editing "+action.label, width)))
	labels := slices.Clone(m.keybindingsDraft)
	labels = append(labels, "Replace all…", "Add key…")
	available := max(1, height-1)
	selected := clampPickerIndex(m.keybindingsEditIndex, len(labels))
	start, end := settingsCardWindow(selected, len(labels), available)
	for i := start; i < end; i++ {
		prefix := "  "
		style := styleCompletion
		if i == selected {
			prefix = "› "
			style = styleCompletionSelected
		}
		label := labels[i]
		if i < len(m.keybindingsDraft) {
			label = "Key  " + label
		}
		rows = append(rows, style.Render(truncateDisplayText(prefix+label, width)))
	}
	return strings.Join(rows, "\n")
}
