package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPasteThresholdsPreserveReplacementSemantics(t *testing.T) {
	for _, body := range []string{
		"small", strings.Repeat("界", largePasteRuneThreshold-1), strings.Repeat("界", largePasteRuneThreshold),
		strings.Repeat("x\n", largePasteLineThreshold-2), strings.Repeat("x\n", largePasteLineThreshold-1),
	} {
		for _, selected := range []string{"c", "first\nsecond"} {
			for _, forward := range []bool{false, true} {
				t.Run(fmt.Sprintf("runes=%d/lines=%d/multi=%v/forward=%v", utf8.RuneCountInString(body), strings.Count(body, "\n")+1, strings.Contains(selected, "\n"), forward), func(t *testing.T) {
					m := prepareScrollableModel(t)
					prefix, suffix := "ab", " suffix"
					m.editor.SetValue(prefix + selected + suffix)
					m.editor.MoveToEnd()
					for range utf8.RuneCountInString(suffix) {
						m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
					}
					direction := tea.KeyLeft
					if forward {
						for range utf8.RuneCountInString(selected) {
							m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
						}
						direction = tea.KeyRight
					}
					for range utf8.RuneCountInString(selected) {
						m.Update(tea.KeyPressMsg{Code: direction, Mod: tea.ModShift})
					}
					if m.editor.SelectedText() != selected {
						t.Fatalf("selection=%q", m.editor.SelectedText())
					}
					m.Update(tea.PasteMsg{Content: body})
					want := prefix + body + suffix
					if got := m.expandedPastedText(m.editor.Value()); got != want || m.editor.HasSelection() {
						t.Fatalf("paste replacement mismatch: got runes=%d want=%d selection=%v", utf8.RuneCountInString(got), utf8.RuneCountInString(want), m.editor.HasSelection())
					}
					count := 0
					if shouldCollapsePastedText(body) {
						count = 1
					}
					if len(m.pastedTexts) != count {
						t.Fatalf("attachments=%d want=%d", len(m.pastedTexts), count)
					}
					m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					if len(m.inputHistory) != 1 || m.inputHistory[0] != want {
						t.Fatal("submission did not retain exact expanded replacement")
					}
				})
			}
		}
	}
}

func TestPasteReplacementPrunesDisplacedTextAttachments(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, large := range []bool{false, true} {
			t.Run(fmt.Sprintf("all=%v/large=%v", all, large), func(t *testing.T) {
				m := prepareScrollableModel(t)
				first := m.newPastedTextAttachment(strings.Repeat("first", 1000))
				second := m.newPastedTextAttachment(strings.Repeat("second", 1000))
				m.pastedTexts = []pastedTextAttachment{first, second}
				m.editor.SetValue(first.token + " " + second.token)
				m.editor.MoveToEnd()
				if all {
					m.promptImages = []protocol.ContentBlock{{Type: protocol.BlockImage}}
					m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
				} else {
					for range utf8.RuneCountInString(second.token) {
						m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
					}
					if m.editor.SelectedText() != second.token {
						t.Fatal("missing attachment selection")
					}
				}
				body := "replacement"
				if large {
					body = strings.Repeat("new", largePasteRuneThreshold)
				}
				m.Update(tea.PasteMsg{Content: body})
				want := body
				count := 0
				if !all {
					want = first.text + " " + body
					count++
				}
				if large {
					count++
				}
				if m.expandedPastedText(m.editor.Value()) != want || m.editor.HasSelection() || m.composerSelectAll {
					t.Fatal("attachment replacement changed expanded content or retained selection")
				}
				if len(m.pastedTexts) != count || len(m.promptImages) != 0 {
					t.Fatalf("stale attachments: text=%d image=%d", len(m.pastedTexts), len(m.promptImages))
				}
				for _, attachment := range m.pastedTexts {
					if attachment.token == second.token {
						t.Fatal("displaced attachment retained")
					}
				}
			})
		}
	}
}
