package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// Keep ordinary snippets directly editable, but move genuinely large paste
	// bodies out of Bubbles' textarea render/edit hot path.
	largePasteRuneThreshold = 4 << 10
	largePasteLineThreshold = 40
)

type pastedTextAttachment struct {
	text  string
	token string
}

func shouldCollapsePastedText(text string) bool {
	if utf8.RuneCountInString(text) >= largePasteRuneThreshold {
		return true
	}
	return strings.Count(text, "\n")+1 >= largePasteLineThreshold
}

// collapseComposerPaste replaces a large bracketed/clipboard paste with a
// short inline token. The exact body stays in model state and is expanded only
// when Snow submits or restores the draft.
func (m *Model) collapseComposerPaste(msg tea.KeyMsg) bool {
	if !msg.Paste || msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return false
	}
	text := string(msg.Runes)
	if !shouldCollapsePastedText(text) {
		return false
	}
	attachment := m.newPastedTextAttachment(text)
	m.pastedTexts = append(m.pastedTexts, attachment)
	m.editor.InsertString(attachment.token)
	m.lastStatus = "attached " + attachment.token
	return true
}

func (m *Model) newPastedTextAttachment(text string) pastedTextAttachment {
	m.nextPastedTextID++
	lines := strings.Count(text, "\n") + 1
	characters := utf8.RuneCountInString(text)
	detail := fmt.Sprintf("%d chars", characters)
	if lines > 1 {
		detail = fmt.Sprintf("%d lines · %s", lines, detail)
	}
	return pastedTextAttachment{
		text:  text,
		token: fmt.Sprintf("[Pasted text #%d · %s]", m.nextPastedTextID, detail),
	}
}

func (m *Model) expandedPastedText(text string) string {
	return expandPastedTextAttachments(text, m.pastedTexts)
}

// expandPastedTextAttachments scans only the compact editor value. Inserted
// bodies are never rescanned, so text that happens to contain another token is
// preserved literally rather than recursively expanded.
func expandPastedTextAttachments(text string, attachments []pastedTextAttachment) string {
	if text == "" || len(attachments) == 0 {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	remaining := text
	used := make([]bool, len(attachments))
	for remaining != "" {
		bestIndex := -1
		bestAttachment := -1
		for i, attachment := range attachments {
			if used[i] {
				continue
			}
			if index := strings.Index(remaining, attachment.token); index >= 0 && (bestIndex < 0 || index < bestIndex) {
				bestIndex = index
				bestAttachment = i
			}
		}
		if bestAttachment < 0 {
			out.WriteString(remaining)
			break
		}
		out.WriteString(remaining[:bestIndex])
		out.WriteString(attachments[bestAttachment].text)
		remaining = remaining[bestIndex+len(attachments[bestAttachment].token):]
		used[bestAttachment] = true
	}
	return out.String()
}

func pastedTextAttachmentsReferenced(text string, attachments []pastedTextAttachment) bool {
	for _, attachment := range attachments {
		if !strings.Contains(text, attachment.token) {
			return false
		}
	}
	return true
}

func stripPastedTextAttachmentTokens(text string, attachments []pastedTextAttachment) string {
	for _, attachment := range attachments {
		text = strings.Replace(text, attachment.token, "", 1)
	}
	return text
}

func (m *Model) takePastedTextAttachments() []pastedTextAttachment {
	attachments := append([]pastedTextAttachment(nil), m.pastedTexts...)
	m.pastedTexts = nil
	return attachments
}

func (m *Model) prunePastedTextAttachments(text string) {
	if len(m.pastedTexts) == 0 {
		return
	}
	kept := m.pastedTexts[:0]
	for _, attachment := range m.pastedTexts {
		if strings.Contains(text, attachment.token) {
			kept = append(kept, attachment)
		}
	}
	m.pastedTexts = kept
}

func (m *Model) setComposerValueCollapsingLargeText(text string) {
	m.pastedTexts = nil
	if shouldCollapsePastedText(text) {
		attachment := m.newPastedTextAttachment(text)
		m.pastedTexts = append(m.pastedTexts, attachment)
		m.editor.SetValue(attachment.token)
	} else {
		m.editor.SetValue(text)
	}
	m.editor.CursorEnd()
}

func (m *Model) removeLastPastedTextAttachment() bool {
	if len(m.pastedTexts) == 0 {
		return false
	}
	return m.removePastedTextAttachment(len(m.pastedTexts) - 1)
}

func (m *Model) removePastedTextAtCursor(backward bool) bool {
	if len(m.pastedTexts) == 0 {
		return false
	}
	value := []rune(m.editor.Value())
	cursor := m.editorCursorRuneOffset()
	for attachmentIndex, attachment := range m.pastedTexts {
		token := []rune(attachment.token)
		for start := 0; start+len(token) <= len(value); start++ {
			if !runesEqual(value[start:start+len(token)], token) {
				continue
			}
			end := start + len(token)
			if backward && cursor > start && cursor <= end || !backward && cursor >= start && cursor < end {
				return m.removePastedTextAttachment(attachmentIndex)
			}
		}
	}
	return false
}

func (m *Model) removePastedTextAttachment(index int) bool {
	if index < 0 || index >= len(m.pastedTexts) {
		return false
	}
	attachment := m.pastedTexts[index]
	m.editor.SetValue(strings.Replace(m.editor.Value(), attachment.token, "", 1))
	m.editor.CursorEnd()
	m.pastedTexts = append(m.pastedTexts[:index], m.pastedTexts[index+1:]...)
	m.lastStatus = "removed " + attachment.token
	return true
}

func (m *Model) editorCursorRuneOffset() int {
	value := m.editor.Value()
	lines := strings.Split(value, "\n")
	row := max(0, min(m.editor.Line(), len(lines)-1))
	offset := 0
	for i := 0; i < row; i++ {
		offset += utf8.RuneCountInString(lines[i]) + 1
	}
	info := m.editor.LineInfo()
	column := max(0, info.StartColumn+info.CharOffset)
	return min(utf8.RuneCountInString(value), offset+column)
}

func runesEqual(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
