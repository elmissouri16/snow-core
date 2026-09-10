package tui

import tea "charm.land/bubbletea/v2"

// Bracketed paste is text, never an action key. Route it to the visible editor.
func (m *Model) handlePaste(msg tea.PasteMsg) tea.Cmd {
	m.cancelClipboardReads()
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
		m.loginError = ""
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
	if m.pickSession && m.sessionRenaming && !m.sessionDeleting {
		name := []rune(m.sessionRenameInput + sanitizeTerminalLine(msg.Content))
		m.sessionRenameInput = string(name[:min(72, len(name))])
		return nil
	}
	if m.pickTree && !m.treeLoading && (m.branchAction == "rename" || m.branchAction == "fork") {
		name := []rune(m.branchInput + sanitizeTerminalLine(msg.Content))
		m.branchInput = string(name[:min(64, len(name))])
		return nil
	}
	if m.pickModel {
		m.modelQuery += sanitizeTerminalLine(msg.Content)
		m.modelIndex = 0
		return nil
	}
	if m.plugins != nil && m.plugins.screen == "snow:plugins" && m.plugins.inspector != nil && !m.plugins.inspector.detail {
		inspector := m.plugins.inspector
		query := []rune(inspector.query + sanitizeTerminalLine(msg.Content))
		inspector.query, inspector.index = string(query[:min(120, len(query))]), 0
		return nil
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
		_ = m.updateEditor(msg)
	}
	m.resetInputHistoryNavigation()
	m.prunePastedTextAttachments(m.editor.Value())
	m.layout()
	return m.refreshInputCompletions()
}
