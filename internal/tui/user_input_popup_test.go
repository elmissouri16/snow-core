package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestUserInputCardIsCentered(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, size := range [][2]int{{140, 40}, {100, 30}, {60, 16}, {40, 12}, {20, 8}} {
			t.Run(fmt.Sprintf("%dx%d_inline_%v", size[0], size[1], inline), func(t *testing.T) {
				m := pluginScreenTestModel(t, size[0], size[1])
				m.inlineTranscript = inline
				m.editor.SetValue("original draft")
				m.layout()
				beforeHeight := m.transcript.Height
				m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{{ID: "note", Header: "Notes", Question: "Save a workspace note"}}})
				m.userInputEditor.SetValue("cool")
				if m.renderOverlays() != "" || m.transcript.Height != beforeHeight {
					t.Fatal("input still takes space above the composer")
				}
				card := m.renderUserInput()
				width, height := lipgloss.Width(card), lipgloss.Height(card)
				if width > 80 || width > m.managedFrameWidth() || height > m.managedFrameHeight() {
					t.Fatalf("unbounded input card %dx%d", width, height)
				}
				frame := m.View()
				if lipgloss.Width(frame) != m.managedFrameWidth() || lipgloss.Height(frame) != m.managedFrameHeight() {
					t.Fatal("input changed frame dimensions")
				}
				x, y := (m.managedFrameWidth()-width)/2, (m.managedFrameHeight()-height)/2
				if []rune(strings.Split(stripANSI(frame), "\n")[y])[x] != '╭' || !strings.Contains(stripANSI(card), "cool") {
					t.Fatalf("missing centered input or editor:\n%s", stripANSI(frame))
				}
				m.clearUserInput()
				if m.editor.Value() != "original draft" || m.transcript.Height != beforeHeight || m.busy {
					t.Fatal("closing input changed the composer or agent state")
				}
			})
		}
	}
}

func TestUserInputCardKeepsSelectionAndErrorsVisible(t *testing.T) {
	m := pluginScreenTestModel(t, 100, 30)
	question := protocol.UserInputQuestion{ID: "choice", Header: "Choose", Question: strings.Repeat("A long question with context. ", 30)}
	for i := range 16 {
		question.Options = append(question.Options, protocol.UserInputOption{Label: fmt.Sprintf("Choice %02d", i), Description: strings.Repeat("界", 50)})
	}
	m.startUserInput(protocol.UserInputRequest{ID: "input", Questions: []protocol.UserInputQuestion{question}})
	for _, size := range [][2]int{{100, 30}, {40, 12}, {20, 8}, {120, 40}} {
		m.width, m.height = size[0], size[1]
		m.layout()
		for i := range len(question.Options) + 1 {
			m.userInputOption = i
			card := m.renderUserInput()
			label := "Other"
			if i < len(question.Options) {
				label = question.Options[i].Label
			}
			if !strings.Contains(stripANSI(card), "› "+label) || lipgloss.Width(card) > m.managedFrameWidth() || lipgloss.Height(card) > m.managedFrameHeight() {
				t.Fatalf("choice is hidden:\n%s", stripANSI(card))
			}
		}
		m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEnter}) // Other opens the editor.
		m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEnter}) // Empty answer stays open.
		if !m.userInputPending || !strings.Contains(stripANSI(m.renderUserInput()), "Answer") {
			t.Fatal("empty-answer validation disappeared")
		}
		m.prepareUserInputQuestion()
	}
}

func TestUserInputCardKeepsMultilineDraftAcrossResize(t *testing.T) {
	m := pluginScreenTestModel(t, 120, 32)
	m.startUserInput(protocol.UserInputRequest{ID: "form", Questions: []protocol.UserInputQuestion{
		{ID: "first", Header: "Details", Question: "First detail"},
		{ID: "second", Header: "Details", Question: "Second detail"},
	}})
	value := strings.Repeat("wrapped text 界 ", 20) + "\nlast draft line"
	m.userInputEditor.SetValue(value)
	m.userInputEditor.CursorEnd()
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyTab})
	m.userInputEditor.SetValue("second draft")
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	for _, width := range []int{120, 42, 100} {
		m.width = width
		m.layout()
		if m.userInputEditor.Value() != value || !strings.Contains(stripANSI(m.renderUserInput()), "last draft line") {
			t.Fatalf("width=%d draft preserved=%v editor=%q card=\n%s", width, m.userInputEditor.Value() == value, stripANSI(m.userInputEditor.View()), stripANSI(m.renderUserInput()))
		}
	}
}

func TestUserInputCardTitleAndPermissionPriority(t *testing.T) {
	m := pluginScreenTestModel(t, 100, 30)
	m.plugins.infos = []protocol.PluginInfo{{Name: "Workspace Notes"}}
	m.plugins.screen = "notes:main"
	m.startUserInput(protocol.UserInputRequest{ID: "plugin-input", Questions: []protocol.UserInputQuestion{{
		ID: "answer", Header: "Plugin", Question: "Workspace Notes: Save a workspace note",
	}}})
	card := stripANSI(m.renderUserInput())
	if strings.Count(card, "Workspace Notes") != 1 || strings.Contains(card, "? Plugin") || strings.Contains(card, "[1]") {
		t.Fatalf("dialog retained the generic header: %s", card)
	}
	m.permPending = true
	if strings.Contains(stripANSI(m.View()), "Save a workspace note") {
		t.Fatal("question covered a permission request")
	}
	m.permPending = false
	if !strings.Contains(stripANSI(m.View()), "Save a workspace note") {
		t.Fatal("question did not resume")
	}
	m.clearUserInput()
	if !strings.Contains(stripANSI(m.View()), "Add a note") {
		t.Fatal("parent plugin panel did not resume")
	}
}
