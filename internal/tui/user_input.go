package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func newUserInputEditor() textarea.Model {
	editor := textarea.New()
	editor.Prompt = ""
	editor.Placeholder = "Type your answer…"
	editor.ShowLineNumbers = false
	editor.CharLimit = 8 * 1024
	editor.SetWidth(72)
	editor.SetHeight(3)
	editor.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"),
		key.WithHelp("ctrl+j", "insert newline"),
	)
	editor.Blur()
	return editor
}

func (m *Model) startUserInput(req protocol.UserInputRequest) {
	copy := req
	copy.Questions = make([]protocol.UserInputQuestion, len(req.Questions))
	for i, question := range req.Questions {
		copy.Questions[i] = question
		copy.Questions[i].Options = append([]protocol.UserInputOption(nil), question.Options...)
	}
	m.closeTranscriptSelectionContextMenu()
	m.userInputPending = true
	m.userInputRequest = &copy
	m.userInputIndex = 0
	m.userInputOption = 0
	m.userInputAnswers = make(map[string]string, len(copy.Questions))
	m.userInputDrafts = make(map[string]string, len(copy.Questions))
	m.userInputError = ""
	m.busy = true
	m.editor.Blur()
	m.prepareUserInputQuestion()
	m.layout()
}

func (m *Model) clearUserInput() {
	if !m.userInputPending && m.userInputRequest == nil {
		return
	}
	m.userInputPending = false
	m.userInputRequest = nil
	m.userInputIndex = 0
	m.userInputOption = 0
	m.userInputEditing = false
	m.userInputAnswers = nil
	m.userInputDrafts = nil
	m.userInputError = ""
	m.userInputEditor.Reset()
	m.userInputEditor.Blur()
	if m.app != nil && m.lastErr == nil {
		m.editor.Focus()
	}
	m.layout()
}

func (m *Model) currentUserInputQuestion() *protocol.UserInputQuestion {
	if !m.userInputPending || m.userInputRequest == nil || m.userInputIndex < 0 || m.userInputIndex >= len(m.userInputRequest.Questions) {
		return nil
	}
	return &m.userInputRequest.Questions[m.userInputIndex]
}

func (m *Model) prepareUserInputQuestion() {
	question := m.currentUserInputQuestion()
	if question == nil {
		return
	}
	m.userInputError = ""
	m.userInputOption = 0
	value := m.userInputDrafts[question.ID]
	if value == "" {
		value = m.userInputAnswers[question.ID]
	}
	if len(question.Options) == 0 {
		m.beginUserInputEditing(value)
		return
	}
	m.userInputEditing = false
	m.userInputEditor.Blur()
	for i, option := range question.Options {
		if option.Label == value {
			m.userInputOption = i
			return
		}
	}
	if value != "" {
		m.userInputOption = len(question.Options)
		m.beginUserInputEditing(value)
	}
}

func (m *Model) beginUserInputEditing(value string) {
	m.userInputEditing = true
	m.userInputEditor.SetValue(value)
	m.userInputEditor.CursorEnd()
	m.userInputEditor.Focus()
}

func (m *Model) handleUserInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if keyMatches(msg, m.keys.Close) {
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	} else if keyMatches(msg, m.keys.Accept) {
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	} else if keyMatches(msg, m.keys.Paste) {
		msg = tea.KeyMsg{Type: tea.KeyCtrlV}
	}
	question := m.currentUserInputQuestion()
	if !m.userInputEditing {
		msg = normalizePickerKeyWithMap(msg, m.keys)
	}
	if question == nil {
		m.clearUserInput()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		m.requestAbort()
		m.clearUserInput()
		return m, nil
	case tea.KeyEsc:
		requestID := m.userInputRequest.ID
		if m.app != nil {
			if err := m.app.RejectUserInput(requestID); err != nil {
				m.pushLine(styleError.Render("question: " + err.Error()))
			}
		}
		m.clearUserInput()
		m.pushLine(styleFooter.Render("question declined"))
		return m, nil
	case tea.KeyTab:
		m.moveUserInputQuestion(1)
		return m, nil
	case tea.KeyShiftTab:
		m.moveUserInputQuestion(-1)
		return m, nil
	}

	if m.userInputEditing {
		if msg.Type == tea.KeyEnter && !msg.Alt {
			m.commitUserInputAnswer(m.userInputEditor.Value())
			return m, nil
		}
		var cmd tea.Cmd
		m.userInputEditor, cmd = m.userInputEditor.Update(msg)
		m.userInputDrafts[question.ID] = m.userInputEditor.Value()
		if msg.Type == tea.KeyCtrlV {
			m.userInputEditor.Err = nil
			m.userInputError = ""
			if m.pasteCmdOverride != nil {
				cmd = m.pasteCmdOverride
			}
			cmd = routeTextareaCmd(textareaTargetUserInput, m.userInputRequest.ID, question.ID, cmd)
		}
		return m, cmd
	}

	count := len(question.Options) + 1 // automatic Other
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft:
		m.userInputOption = (m.userInputOption - 1 + count) % count
	case tea.KeyDown, tea.KeyRight:
		m.userInputOption = (m.userInputOption + 1) % count
	case tea.KeyEnter:
		if m.userInputOption == len(question.Options) {
			m.beginUserInputEditing(m.userInputDrafts[question.ID])
		} else {
			m.commitUserInputAnswer(question.Options[m.userInputOption].Label)
		}
	}
	return m, nil
}

func (m *Model) moveUserInputQuestion(delta int) {
	if m.userInputRequest == nil || len(m.userInputRequest.Questions) == 0 {
		return
	}
	if question := m.currentUserInputQuestion(); question != nil && m.userInputEditing {
		m.userInputDrafts[question.ID] = m.userInputEditor.Value()
	}
	count := len(m.userInputRequest.Questions)
	m.userInputIndex = (m.userInputIndex + delta + count) % count
	m.prepareUserInputQuestion()
}

func (m *Model) commitUserInputAnswer(value string) {
	question := m.currentUserInputQuestion()
	if question == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		m.userInputError = "Answer cannot be empty"
		return
	}
	if len(value) > 8*1024 {
		m.userInputError = "Answer is too long (maximum 8 KiB)"
		return
	}
	m.userInputAnswers[question.ID] = value
	m.userInputDrafts[question.ID] = value
	m.userInputError = ""

	if len(m.userInputAnswers) == len(m.userInputRequest.Questions) {
		m.resolveUserInput()
		return
	}
	for step := 1; step <= len(m.userInputRequest.Questions); step++ {
		next := (m.userInputIndex + step) % len(m.userInputRequest.Questions)
		if _, answered := m.userInputAnswers[m.userInputRequest.Questions[next].ID]; !answered {
			m.userInputIndex = next
			m.prepareUserInputQuestion()
			return
		}
	}
}

func (m *Model) resolveUserInput() {
	if m.app == nil || m.userInputRequest == nil {
		return
	}
	response := protocol.UserInputResponse{
		RequestID: m.userInputRequest.ID,
		Answers:   make([]protocol.UserInputAnswer, 0, len(m.userInputRequest.Questions)),
	}
	for _, question := range m.userInputRequest.Questions {
		response.Answers = append(response.Answers, protocol.UserInputAnswer{
			QuestionID: question.ID,
			Answer:     m.userInputAnswers[question.ID],
		})
	}
	if err := m.app.ReplyUserInput(response); err != nil {
		m.userInputError = err.Error()
		return
	}
	count := len(response.Answers)
	m.clearUserInput()
	m.pushLine(styleFooter.Render(fmt.Sprintf("answered %d question(s)", count)))
}

func (m *Model) renderUserInput() string {
	question := m.currentUserInputQuestion()
	if question == nil || m.userInputRequest == nil {
		return ""
	}
	width := max(20, m.width-4)
	var b strings.Builder
	tabs := make([]string, 0, len(m.userInputRequest.Questions))
	for i, candidate := range m.userInputRequest.Questions {
		mark := fmt.Sprintf("%d", i+1)
		if _, answered := m.userInputAnswers[candidate.ID]; answered {
			mark += "✓"
		}
		if i == m.userInputIndex {
			mark = "[" + mark + "]"
		}
		tabs = append(tabs, mark)
	}
	title := fmt.Sprintf("? %s  %s", sanitizeTerminalText(question.Header), strings.Join(tabs, " "))
	b.WriteString(styleTool.Render(truncateRunes(title, width)) + "\n")
	wrapped := xansi.Wordwrap(sanitizeTerminalText(question.Question), width, "")
	wrapped = xansi.Hardwrap(wrapped, width, true)
	editorView := ""
	if m.userInputEditing {
		editorView = styleComposer.Width(width).Render(m.userInputEditor.View())
	}
	if m.inlineModalOverlay() {
		// Keep every actionable row visible inside the fixed inline frame. A valid
		// question can be 1,000 characters, so letting prose consume the frame
		// would hide all choices (or the free-form editor) below the truncation
		// boundary. Only the descriptive question text is elided.
		reserved := 2 // title + controls hint
		if m.userInputError != "" {
			reserved++
		}
		if m.userInputEditing {
			reserved += lipgloss.Height(editorView)
		} else {
			reserved += len(question.Options) + 1 // choices + Other
		}
		wrapped = truncateOverlayLines(wrapped, max(1, m.availableOverlayHeight()-reserved))
	}
	b.WriteString(styleAssistant.Render(wrapped) + "\n")

	if m.userInputEditing {
		b.WriteString(editorView + "\n")
		b.WriteString(styleFooter.Render("(Enter accept · Ctrl+V paste · Ctrl+J newline · Tab next · Shift+Tab previous · Esc decline)"))
	} else {
		for i, option := range question.Options {
			line := sanitizeTerminalText(option.Label)
			if option.Description != "" {
				line += "  " + styleHeaderDim.Render(sanitizeTerminalText(option.Description))
			}
			prefix := "  "
			style := styleCompletion
			if i == m.userInputOption {
				prefix = "› "
				style = styleCompletionSelected
			}
			b.WriteString(style.Render(prefix+truncateRunes(line, width-2)) + "\n")
		}
		prefix := "  "
		style := styleCompletion
		if m.userInputOption == len(question.Options) {
			prefix = "› "
			style = styleCompletionSelected
		}
		b.WriteString(style.Render(prefix+"Other  "+styleHeaderDim.Render("type a custom answer")) + "\n")
		b.WriteString(styleFooter.Render("(↑/↓ choose · Enter accept · Tab next · Shift+Tab previous · Esc decline)"))
	}
	if m.userInputError != "" {
		b.WriteString("\n" + styleError.Render(truncateRunes(sanitizeTerminalText(m.userInputError), width)))
	}
	return strings.TrimSuffix(lipgloss.NewStyle().MaxWidth(width).Render(b.String()), "\n")
}

func truncateOverlayLines(value string, limit int) string {
	lines := strings.Split(value, "\n")
	if limit <= 0 || len(lines) <= limit {
		return value
	}
	lines = lines[:limit]
	last := strings.TrimRight(lines[len(lines)-1], " …")
	lines[len(lines)-1] = last + "…"
	return strings.Join(lines, "\n")
}
