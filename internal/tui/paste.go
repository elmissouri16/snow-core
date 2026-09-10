package tui

import tea "charm.land/bubbletea/v2"

// Bracketed paste is text, never an action key. Route it to the visible editor.
func (m *Model) handlePaste(msg tea.PasteMsg) tea.Cmd {
	if m.permPending {
		return nil
	}
	if m.keybindingsCapture != keybindingCaptureNone {
		m.keybindingsError = "pasted input cannot be a shortcut"
		return nil
	}
	if m.userInputPending {
		if question := m.currentUserInputQuestion(); question != nil && m.userInputEditing {
			var cmd tea.Cmd
			m.userInputEditor, cmd = m.userInputEditor.Update(msg)
			m.userInputDrafts[question.ID] = m.userInputEditor.Value()
			m.layoutUserInputEditor()
			return cmd
		}
		return nil
	}
	if m.loginMode {
		m.secretBuf.WriteString(msg.Content)
		return nil
	}
	if m.loginProfileMode || m.loginEndpointMode {
		target := textareaTargetLoginEndpoint
		if m.loginProfileMode {
			target = textareaTargetLoginProfile
		}
		_, cmd := m.applyTextareaResult(textareaResultMsg{target: target, pasteGeneration: m.loginFieldGeneration, msg: msg})
		return cmd
	}
	if m.composerCoveredByModal() || m.pluginScreenView() != nil {
		return nil
	}
	if m.composerSelectAll {
		m.editor.Reset()
		m.promptImages = nil
		m.pastedTexts = nil
		m.composerSelectAll = false
	}
	if !m.collapseComposerPaste(msg) {
		m.editor, _ = m.editor.Update(msg)
	}
	m.resetInputHistoryNavigation()
	m.prunePastedTextAttachments(m.editor.Value())
	m.layout()
	return m.refreshInputCompletions()
}
