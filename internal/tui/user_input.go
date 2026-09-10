package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

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
		key.WithKeys("shift+enter", "alt+enter", "ctrl+j"),
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
		copy.Questions[i].Options = slices.Clone(question.Options)
	}
	m.closeTranscriptSelectionContextMenu()
	m.userInputPending = true
	m.userInputRequest = &copy
	m.userInputIndex = 0
	m.userInputOption = 0
	m.userInputAnswers = make(map[string]string, len(copy.Questions))
	m.userInputDrafts = make(map[string]string, len(copy.Questions))
	m.userInputError = ""
	// Input can originate from an idle plugin command as well as an agent tool.
	// Only turn lifecycle events own busy; userInputPending owns the modal state.
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
	m.userInputWaitRequest = nil
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
	defer m.layoutUserInputEditor()
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
	if value != "" && !question.ChoicesOnly {
		m.userInputOption = len(question.Options)
		m.beginUserInputEditing(value)
	}
}

func (m *Model) beginUserInputEditing(value string) {
	m.userInputEditing = true
	m.userInputEditor.SetValue(value)
	m.userInputEditor.CursorEnd()
	m.userInputEditor.Focus()
	m.layoutUserInputEditor()
	m.refreshUserInputEditorViewport()
}

func (m *Model) handleUserInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if keyMatches(msg, m.keys.Close) {
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	} else if keyMatches(msg, m.keys.Accept) {
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	} else if keyMatches(msg, m.keys.Paste) {
		msg = tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}
	}
	question := m.currentUserInputQuestion()
	if !m.userInputEditing {
		msg = normalizePickerKeyWithMap(msg, m.keys)
	}
	if question == nil {
		m.clearUserInput()
		return m, nil
	}
	switch {
	case msg.Code == 'c' && msg.Mod.Contains(tea.ModCtrl):
		if m.app != nil {
			_ = m.app.RejectUserInput(m.userInputRequest.ID)
		}
		m.requestAbort()
		m.clearUserInput()
		return m, nil
	case msg.Code == tea.KeyEscape:
		requestID := m.userInputRequest.ID
		if m.app != nil {
			if err := m.app.RejectUserInput(requestID); err != nil {
				m.pushLine(styleError.Render("question: " + err.Error()))
			}
		}
		m.clearUserInput()
		m.pushLine(styleFooter.Render("question declined"))
		return m, nil
	case msg.Code == tea.KeyTab && !msg.Mod.Contains(tea.ModShift):
		m.moveUserInputQuestion(1)
		return m, nil
	case msg.Code == tea.KeyTab && msg.Mod.Contains(tea.ModShift):
		m.moveUserInputQuestion(-1)
		return m, nil
	}

	if m.userInputEditing {
		if msg.Code == tea.KeyEnter && msg.Mod == 0 {
			m.commitUserInputAnswer(m.userInputEditor.Value())
			return m, nil
		}
		var cmd tea.Cmd
		m.userInputEditor, cmd = m.userInputEditor.Update(msg)
		m.userInputDrafts[question.ID] = m.userInputEditor.Value()
		if msg.Code == 'v' && msg.Mod.Contains(tea.ModCtrl) {
			m.userInputEditor.Err = nil
			m.userInputError = ""
			if m.pasteCmdOverride != nil {
				cmd = m.pasteCmdOverride
			} else {
				return m, m.startClipboardTextRead()
			}
			cmd = routeTextareaCmd(textareaTargetUserInput, m.userInputRequest.ID, question.ID, cmd)
		}
		return m, cmd
	}

	count := userInputOptionCount(question)
	switch {
	case msg.Code == tea.KeyUp, msg.Code == tea.KeyLeft:
		m.userInputOption = (m.userInputOption - 1 + count) % count
	case msg.Code == tea.KeyDown, msg.Code == tea.KeyRight:
		m.userInputOption = (m.userInputOption + 1) % count
	case msg.Code == tea.KeyEnter:
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
	pluginInput := m.pluginInputPending()
	m.clearUserInput()
	if !pluginInput {
		m.pushLine(styleFooter.Render(fmt.Sprintf("answered %d question(s)", count)))
	}
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
