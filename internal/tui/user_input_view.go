package tui

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type userInputCardLayout struct {
	geometry                          pickerCardGeometry
	title, question                   string
	controls                          []string
	questionHeight, answerHeight, gap int
	footerSeparator                   bool
}

func (m *Model) userInputCardLayout() userInputCardLayout {
	layout := userInputCardLayout{geometry: m.pickerCardGeometry()}
	question := m.currentUserInputQuestion()
	if question == nil {
		return layout
	}
	layout.title, layout.question = cmp.Or(question.Header, "Your input"), question.Question
	// The host prefixes plugin questions with their registered display name.
	// Use that name as the card title without changing the broker's request.
	if m.plugins != nil && strings.HasPrefix(m.userInputRequest.ID, "plugin-") {
		for _, info := range m.plugins.infos {
			if prompt, ok := strings.CutPrefix(question.Question, info.Name+": "); ok {
				layout.title, layout.question = info.Name, prompt
				break
			}
		}
	}
	width := max(1, layout.geometry.innerWidth-2)
	layout.question = xansi.Hardwrap(xansi.Wordwrap(sanitizeTerminalText(layout.question), width, ""), width, true)
	layout.controls = m.userInputControls(layout.geometry.innerWidth)
	if layout.geometry.innerHeight < 10 {
		layout.controls = layout.controls[:1]
	}
	layout.footerSeparator = layout.geometry.innerHeight >= 10
	chromeHeight := 2 + len(layout.controls) // title and its separator
	if layout.footerSeparator {
		chromeHeight++
	}
	if m.userInputError != "" {
		chromeHeight++
	}
	answerHeight := min(6, userInputOptionCount(question))
	if m.userInputEditing {
		answerHeight = 5 // three editor rows and its border
	}
	questionHeight := min(4, lipgloss.Height(layout.question))
	layout.geometry.innerHeight = min(layout.geometry.innerHeight, max(10, chromeHeight+questionHeight+1+answerHeight))
	layout.geometry.outerHeight = layout.geometry.innerHeight + 2
	available := max(2, layout.geometry.innerHeight-chromeHeight)
	layout.answerHeight = min(answerHeight, available-1)
	layout.questionHeight = min(questionHeight, max(1, available-layout.answerHeight))
	layout.gap = min(1, max(0, available-layout.answerHeight-layout.questionHeight))
	return layout
}

func (m *Model) userInputControls(width int) []string {
	primary := " ↑/↓ choose · Enter accept · Esc decline "
	if m.userInputEditing {
		primary = " Enter accept · Esc decline "
	}
	if width < 40 {
		primary = " ↑↓ · Enter · Esc "
		if m.userInputEditing {
			primary = " Enter · Esc "
		}
	}
	controls := []string{primary}
	if m.userInputEditing {
		controls = append(controls, " Ctrl+V paste · Ctrl+J newline ")
	}
	if m.userInputRequest != nil && len(m.userInputRequest.Questions) > 1 {
		controls = append(controls, " Tab next · Shift+Tab previous ")
	}
	return controls
}

func (m *Model) layoutUserInputEditor() {
	if !m.userInputPending {
		return
	}
	layout := m.userInputCardLayout()
	width, height := max(1, layout.geometry.innerWidth-2), layout.answerHeight
	if height >= 3 {
		width, height = max(1, width-2), height-2
	}
	if m.userInputEditor.Width() == width && m.userInputEditor.Height() == max(1, height) {
		return
	}
	m.userInputEditor.SetWidth(width)
	m.userInputEditor.SetHeight(max(1, height))
	m.refreshUserInputEditorViewport()
}

func (m *Model) refreshUserInputEditorViewport() {
	// Textarea rebuilds its wrapped viewport in View and follows the cursor in
	// Update. Refresh both after resizing/restoring a draft, before the frame is
	// drawn, so the insertion point cannot remain outside the visible field.
	_ = m.userInputEditor.View()
	m.userInputEditor, _ = m.userInputEditor.Update(nil)
}

func (m *Model) renderUserInput() string {
	question := m.currentUserInputQuestion()
	if question == nil {
		return ""
	}
	layout := m.userInputCardLayout()
	width := layout.geometry.innerWidth
	status := ""
	if count := len(m.userInputRequest.Questions); count > 1 {
		status = fmt.Sprintf("%d of %d · %d answered", m.userInputIndex+1, count, len(m.userInputAnswers))
	}
	separator := styleSep.Render(strings.Repeat("─", width))
	parts := []string{renderPickerCardHeader(layout.title, status, width), separator}
	prompt := truncateOverlayLines(layout.question, layout.questionHeight)
	for line := range strings.SplitSeq(prompt, "\n") {
		parts = append(parts, " "+styleAssistant.Render(truncateDisplayText(line, max(1, width-2))))
	}
	if layout.gap > 0 {
		parts = append(parts, "")
	}
	var answer string
	if m.userInputEditing {
		answer = m.userInputEditor.View()
		if layout.answerHeight >= 3 {
			answer = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).
				Width(max(1, width-4)).Render(answer)
		}
		answer = lipgloss.NewStyle().Padding(0, 1).Render(answer)
	} else {
		answer = m.renderUserInputOptions(width, layout.answerHeight)
	}
	parts = append(parts, fitFrame(answer, width, layout.answerHeight))
	if m.userInputError != "" {
		parts = append(parts, " "+styleError.Render(truncateDisplayText(sanitizeTerminalLine(m.userInputError), max(1, width-2))))
	}
	footer := make([]string, 0, len(layout.controls)+1)
	if layout.footerSeparator {
		footer = append(footer, separator)
	}
	for _, hint := range layout.controls {
		footer = append(footer, styleFooter.Render(truncateDisplayText(hint, width)))
	}
	bodyHeight := max(1, layout.geometry.innerHeight-len(footer))
	content := fitFrame(strings.Join(parts, "\n"), width, bodyHeight) + "\n" + strings.Join(footer, "\n")
	return renderPickerCard(content, layout.geometry)
}

func (m *Model) renderUserInputOptions(width, height int) string {
	question := m.currentUserInputQuestion()
	count := userInputOptionCount(question)
	selected := min(max(0, m.userInputOption), count-1)
	start := min(max(0, selected-height/2), max(0, count-height))
	var rows []string
	for i := start; i < min(count, start+height); i++ {
		label, description := "Other", "type a custom answer"
		if i < len(question.Options) {
			label, description = question.Options[i].Label, question.Options[i].Description
		}
		prefix, style := "  ", styleCompletion
		if i == selected {
			prefix, style = "› ", styleCompletionSelected
		}
		line := prefix + sanitizeTerminalLine(label)
		if description != "" {
			line += "  " + sanitizeTerminalLine(description)
		}
		rows = append(rows, " "+style.Render(truncateDisplayText(line, max(1, width-2))))
	}
	return strings.Join(rows, "\n")
}

func userInputOptionCount(question *protocol.UserInputQuestion) int {
	count := len(question.Options)
	if !question.ChoicesOnly {
		count++
	}
	return max(1, count)
}
