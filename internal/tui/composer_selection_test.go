package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func newComposerSelectionTestModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	return m
}

func TestComposerCtrlASelectsOnlyDraft(t *testing.T) {
	m := newComposerSelectionTestModel(t)
	m.editor.SetValue("first line\nsecond line")

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd != nil {
		t.Fatal("Ctrl+A unexpectedly returned a command")
	}
	if !m.composerSelectAll {
		t.Fatal("Ctrl+A did not select the composer draft")
	}
	if got := m.editor.Value(); got != "first line\nsecond line" {
		t.Fatalf("Ctrl+A changed draft to %q", got)
	}
}

func TestComposerTypingReplacesCtrlASelection(t *testing.T) {
	m := newComposerSelectionTestModel(t)
	m.editor.SetValue("replace all of this")

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	if got := m.editor.Value(); got != "n" {
		t.Fatalf("draft = %q, want %q", got, "n")
	}
	if m.composerSelectAll {
		t.Fatal("selection remained active after replacement")
	}
}

func TestComposerDeleteClearsCtrlASelectionAndAttachments(t *testing.T) {
	m := newComposerSelectionTestModel(t)
	m.editor.SetValue("[Image #1] [Pasted text #1 · 4 chars]")
	m.promptImages = []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("x")}}
	m.pastedTexts = []pastedTextAttachment{{token: "[Pasted text #1 · 4 chars]", text: "text"}}

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := m.editor.Value(); got != "" {
		t.Fatalf("draft = %q, want empty", got)
	}
	if len(m.promptImages) != 0 || len(m.pastedTexts) != 0 {
		t.Fatalf("attachments remained after deleting selection: images=%d pasted=%d", len(m.promptImages), len(m.pastedTexts))
	}
	if m.composerSelectAll {
		t.Fatal("selection remained active after deletion")
	}
}

func TestComposerCtrlCCopiesCtrlASelectionInsteadOfQuitting(t *testing.T) {
	m := newComposerSelectionTestModel(t)
	m.editor.SetValue("copy only this draft")
	copied := ""
	m.copySelectionToClipboard = func(text string) error {
		copied = text
		return nil
	}

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C did not schedule a composer copy")
	}
	if _, ok := cmd().(transcriptSelectionCopiedMsg); !ok {
		t.Fatal("Ctrl+C copy returned an unexpected message")
	}
	if copied != "copy only this draft" {
		t.Fatalf("copied %q", copied)
	}
	if !m.composerSelectAll {
		t.Fatal("copy should preserve the composer selection")
	}
}

func TestComposerNavigationCancelsCtrlASelection(t *testing.T) {
	m := newComposerSelectionTestModel(t)
	m.editor.SetValue("draft")

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})

	if m.composerSelectAll {
		t.Fatal("navigation did not cancel composer selection")
	}
	if got := m.editor.Value(); got != "draft" {
		t.Fatalf("navigation changed draft to %q", got)
	}
}
