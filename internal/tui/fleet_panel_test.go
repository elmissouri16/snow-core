package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func fleetPanelTestModel(t *testing.T, processes bool) *Model {
	t.Helper()
	if processes {
		return processFleetTestModel(t)
	}
	return fleetTestModel(t)
}

func fleetPanelTestCard(m *Model, processes bool) string {
	if processes {
		return m.renderProcessFleetModal()
	}
	return m.renderSubagentFleetModal()
}

func TestFleetPanelsCenteredBoundedAndResponsive(t *testing.T) {
	for _, processes := range []bool{true, false} {
		for _, inline := range []bool{false, true} {
			for _, state := range []string{"populated", "empty", "loading", "error"} {
				t.Run(fmt.Sprintf("processes=%v/inline=%v/%s", processes, inline, state), func(t *testing.T) {
					m := fleetPanelTestModel(t, processes)
					m.inlineTranscript = inline
					m.editor.SetValue("preserve this draft")
					if state == "populated" {
						m.processFleetIndex, m.subagentFleetIndex = 1, 1
						if processes {
							m.processFleetList[1].Name = "chosen 界面 " + strings.Repeat("界", 80)
						} else {
							m.subagentFleetList.Agents[1].Agent.Path = protocol.AgentPath("chosen 界面 " + strings.Repeat("界", 80))
						}
					} else {
						m.processFleetList, m.subagentFleetList.Agents = nil, nil
						m.processFleetLoading, m.subagentFleetLoading = state == "loading", state == "loading"
						if state == "error" {
							m.processFleetError, m.subagentFleetError = "inventory failed", "inventory failed"
						}
					}
					for _, size := range [][2]int{{240, 70}, {120, 36}, {100, 30}, {64, 24}, {40, 12}, {20, 8}, {27, 10}, {160, 50}} {
						m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						card := fleetPanelTestCard(m, processes)
						assertCenteredSelectionCard(t, m, card)
						if lipgloss.Width(card) > fleetCardMaxWidth || lipgloss.Height(card) > fleetCardMaxHeight {
							t.Fatal("fleet grew beyond the card cap")
						}
						plain := stripANSI(card)
						if !strings.Contains(plain, "Esc") || !strings.Contains(plain, "r") {
							t.Fatalf("lost compact close/refresh controls:\n%s", card)
						}
						if state == "populated" && (!strings.Contains(plain, "›") || !strings.Contains(plain, "chosen")) {
							t.Fatalf("lost selected identity at %dx%d:\n%s", size[0], size[1], card)
						}
						if m.editor.Value() != "preserve this draft" {
							t.Fatal("resize changed draft")
						}
					}
				})
			}
		}
	}
}

func TestFleetPanelsKeepBackgroundAndHostRequestPrecedence(t *testing.T) {
	for _, processes := range []bool{true, false} {
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprintf("processes=%v/inline=%v", processes, inline), func(t *testing.T) {
				m := fleetPanelTestModel(t, processes)
				m.inlineTranscript = inline
				m.width, m.height = 180, 50
				m.editor.SetValue("preserve draft")
				m.lines = strings.Split(strings.Repeat("background transcript\n", 100), "\n")
				if inline {
					// Settled inline history belongs to native scrollback, not a
					// pending print batch that the first Update would flush.
					m.inlineCommitted = len(m.lines)
				}
				m.transcriptBaseDirty = true
				m.layout()
				m.refreshTranscriptForced()
				m.transcript.GotoBottom()
				height, offset := m.transcript.Height(), m.transcript.YOffset()
				for _, msg := range []tea.Msg{
					tea.KeyPressMsg{Code: tea.KeyPgUp}, tea.KeyPressMsg{Code: tea.KeyHome},
					tea.MouseWheelMsg{X: 1, Y: 1, Button: tea.MouseWheelUp},
					tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft},
					tea.PasteMsg{Content: "do not paste"},
				} {
					m.Update(msg)
				}
				if m.editor.Value() != "preserve draft" || m.transcript.YOffset() != offset {
					t.Fatalf("fleet input changed background: draft=%q offset=%d want=%d", m.editor.Value(), m.transcript.YOffset(), offset)
				}
				m.startUserInput(protocol.UserInputRequest{ID: "question", Questions: []protocol.UserInputQuestion{{ID: "note", Header: "Note", Question: "Choose a note"}}})
				if strings.Contains(stripANSI(m.viewContent()), "fleet inspector") {
					t.Fatal("fleet painted over a host question")
				}
				m.clearUserInput()
				m.handleAgentEvent(permRequestEvent("bash"))
				if strings.Contains(stripANSI(m.viewContent()), "fleet inspector") || !strings.Contains(stripANSI(m.viewContent()), "Allow this scope") {
					t.Fatal("fleet painted over tool approval")
				}
				m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				assertCenteredSelectionCard(t, m, fleetPanelTestCard(m, processes))
				m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				m.layout()
				if m.processFleetOpen || m.subagentFleetOpen || m.editor.Value() != "preserve draft" || m.transcript.Height() != height {
					t.Fatal("close did not restore background layout/draft")
				}
			})
		}
	}
}

func TestFleetPanelComponentsFitTinyFrames(t *testing.T) {
	for _, processes := range []bool{true, false} {
		m := fleetPanelTestModel(t, processes)
		for width := 1; width <= 35; width++ {
			for height := 1; height <= 12; height++ {
				m.width, m.height = width, height
				card := fleetPanelTestCard(m, processes)
				if lipgloss.Width(card) > m.managedFrameWidth() || lipgloss.Height(card) > m.managedFrameHeight() {
					t.Fatalf("processes=%v: card exceeds %dx%d", processes, width, height)
				}
			}
		}
	}
}
