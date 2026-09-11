package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCollapsedHistoryTraversesBothDirectionsAndRestoresDraft(t *testing.T) {
	for _, large := range []string{strings.Repeat("界", largePasteRuneThreshold), strings.Repeat("line\n", largePasteLineThreshold-1)} {
		for _, draft := range []string{"", "current draft"} {
			m := prepareScrollableModel(t)
			m.hydrateInputHistoryValues([]string{"oldest", large, "newest"})
			m.editor.SetValue(draft)
			for _, step := range []struct {
				code  rune
				want  string
				index int
			}{
				{tea.KeyUp, "newest", 2}, {tea.KeyUp, large, 1}, {tea.KeyUp, "oldest", 0}, {tea.KeyUp, "oldest", 0},
				{tea.KeyDown, large, 1}, {tea.KeyDown, "newest", 2}, {tea.KeyDown, draft, 3},
			} {
				m.Update(tea.KeyPressMsg{Code: step.code})
				if m.expandedPastedText(m.editor.Value()) != step.want || m.inputHistoryIndex != step.index {
					t.Fatalf("history failed at index %d: got index %d", step.index, m.inputHistoryIndex)
				}
				wantAttachments := 0
				if step.want == large {
					wantAttachments = 1
				}
				if len(m.pastedTexts) != wantAttachments {
					t.Fatalf("attachments=%d want=%d", len(m.pastedTexts), wantAttachments)
				}
			}
		}
	}
}

func TestAttachmentDraftsDoNotEnterHistoryAfterIndependentEdits(t *testing.T) {
	for _, mode := range []string{"fresh-paste", "edited-recall", "pasted-recall", "image"} {
		t.Run(mode, func(t *testing.T) {
			m := prepareScrollableModel(t)
			large := strings.Repeat("x", largePasteRuneThreshold)
			m.hydrateInputHistoryValues([]string{"oldest", large})
			switch mode {
			case "fresh-paste":
				m.Update(tea.PasteMsg{Content: large})
			case "image":
				m.promptImages = []protocol.ContentBlock{{Type: protocol.BlockImage}}
				m.editor.SetValue(imageAttachmentToken(0))
			default:
				m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
				if mode == "edited-recall" {
					m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
				} else {
					m.Update(tea.PasteMsg{Content: "!"})
				}
			}
			before := m.expandedPastedText(m.editor.Value())
			for _, code := range []rune{tea.KeyUp, tea.KeyDown} {
				m.Update(tea.KeyPressMsg{Code: code})
				if m.expandedPastedText(m.editor.Value()) != before || m.inputHistoryIndex != len(m.inputHistory) {
					t.Fatal("independent attachment draft entered history")
				}
			}
		})
	}
}

func TestHistoryReplacementReconcilesCursorBeforeFirstAndSameHeightFrames(t *testing.T) {
	for _, width := range []int{35, 100} {
		for _, multiline := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/multiline=%v", width, multiline), func(t *testing.T) {
				m := prepareScrollableModel(t)
				m.width = width
				m.layout()
				older, newer, draft := strings.Repeat("older ", 100), strings.Repeat("newer ", 100), strings.Repeat("draft ", 100)
				if multiline {
					older = strings.Repeat("older line\n", 19) + "older end"
					newer = strings.Repeat("newer line\n", 19) + "newer end"
				}
				m.hydrateInputHistoryValues([]string{older, newer})
				m.editor.SetValue(draft)
				height := 0
				for i, code := range []rune{tea.KeyUp, tea.KeyUp, tea.KeyDown, tea.KeyDown} {
					m.Update(tea.KeyPressMsg{Code: code})
					want := []string{newer, older, newer, draft}[i]
					if m.editor.Value() != want {
						t.Fatalf("step %d recalled wrong value", i)
					}
					offset := m.editor.ScrollYOffset()
					if offset <= 0 {
						t.Fatalf("step %d insertion point offscreen before View", i)
					}
					m.View()
					if m.editor.ScrollYOffset() != offset {
						t.Fatalf("step %d viewport changed during frame", i)
					}
					if i > 0 && m.editor.Height() != height {
						t.Fatal("fixture must exercise same-height replacement")
					}
					height = m.editor.Height()
					// Cursor is at the end: the bottom visible row must contain the tail.
					tail := strings.TrimSpace(want[max(0, len(want)-9):])
					if !strings.Contains(m.editor.View(), tail) {
						t.Fatalf("step %d insertion tail %q not visible", i, tail)
					}
				}
			})
		}
	}
}

func TestRemovingCollapsedHistoryAttachmentEndsNavigation(t *testing.T) {
	for _, code := range []rune{tea.KeyBackspace, tea.KeyEscape} {
		m := prepareScrollableModel(t)
		m.rememberInputHistory(strings.Repeat("x", largePasteRuneThreshold))
		m.editor.SetValue("saved draft")
		m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		m.Update(tea.KeyPressMsg{Code: code})
		if m.editor.Value() != "" || len(m.pastedTexts) != 0 || m.inputHistoryIndex != len(m.inputHistory) || m.inputHistoryDraft != "" {
			t.Fatal("removing recalled attachment retained history navigation")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if m.editor.Value() != "" {
			t.Fatal("Down restored obsolete pre-edit draft")
		}
	}
}
