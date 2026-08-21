package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// hydrateInputHistory replaces composer history with user messages from the
// active session branch. Session messages remain the source of truth across
// resume, branch changes, and compaction.
func (m *Model) hydrateInputHistory(messages []protocol.Message) {
	history := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role != protocol.RoleUser {
			continue
		}
		var text strings.Builder
		for _, block := range message.Content {
			if block.Type == protocol.BlockText {
				text.WriteString(block.Text)
			}
		}
		if value := text.String(); strings.TrimSpace(value) != "" {
			history = append(history, value)
		}
	}
	m.inputHistory = history
	m.resetInputHistoryNavigation()
}

// rememberInputHistory records text as it was entered in the composer. The
// durable session will provide the history again after a resume.
func (m *Model) rememberInputHistory(text string) {
	if strings.TrimSpace(text) == "" {
		m.resetInputHistoryNavigation()
		return
	}
	m.inputHistory = append(m.inputHistory, text)
	m.resetInputHistoryNavigation()
}

// forgetNewestInputHistory rolls back an optimistic history entry when prompt
// admission fails before the user message reaches the session.
func (m *Model) forgetNewestInputHistory(text string) {
	last := len(m.inputHistory) - 1
	if last >= 0 && m.inputHistory[last] == text {
		m.inputHistory = m.inputHistory[:last]
	}
	m.resetInputHistoryNavigation()
}

func (m *Model) resetInputHistoryNavigation() {
	m.inputHistoryIndex = len(m.inputHistory)
	m.inputHistoryDraft = ""
}

// navigateInputHistory applies shell-style history navigation to the ordinary
// composer. Up starts browsing from an empty or single-line draft; multiline
// drafts retain textarea arrow navigation. Once browsing starts, Up and Down
// traverse every entry and Down past the newest entry restores the saved draft.
func (m *Model) navigateInputHistory(msg tea.KeyMsg) (bool, tea.Cmd) {
	if msg.Type != tea.KeyUp && msg.Type != tea.KeyDown {
		return false, nil
	}
	if len(m.promptImages) > 0 {
		return false, nil
	}

	browsing := m.inputHistoryIndex >= 0 && m.inputHistoryIndex < len(m.inputHistory)
	if msg.Type == tea.KeyUp {
		if len(m.inputHistory) == 0 {
			return false, nil
		}
		if !browsing {
			// Do not steal vertical cursor movement from a multiline textarea.
			if strings.ContainsAny(m.editor.Value(), "\r\n") {
				return false, nil
			}
			m.inputHistoryDraft = m.editor.Value()
			m.inputHistoryIndex = len(m.inputHistory)
		}
		if m.inputHistoryIndex > 0 {
			m.inputHistoryIndex--
		}
		m.showInputHistoryValue(m.inputHistory[m.inputHistoryIndex])
		return true, nil
	}

	if !browsing {
		return false, nil
	}
	m.inputHistoryIndex++
	if m.inputHistoryIndex == len(m.inputHistory) {
		m.showInputHistoryValue(m.inputHistoryDraft)
	} else {
		m.showInputHistoryValue(m.inputHistory[m.inputHistoryIndex])
	}
	return true, nil
}

func (m *Model) showInputHistoryValue(value string) {
	m.editor.SetValue(value)
	m.editor.CursorEnd()
	// Recalled $skill, /command, or @mention text must not open a picker that
	// captures the next arrow key and interrupts history traversal. Editing the
	// recalled value refreshes completions normally.
	m.compVisible = false
	m.compMatches = nil
	m.compIndex = 0
	m.skillVisible = false
	m.skillMatches = nil
	m.skillIndex = 0
	m.mentionVisible = false
	m.mentionMatches = nil
	m.mentionIndex = 0
	m.layout()
}
