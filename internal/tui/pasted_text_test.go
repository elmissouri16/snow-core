package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestLargeComposerPasteCollapsesAndSubmitsExactText(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()
	m.editor.SetValue("before ")
	m.editor.CursorEnd()
	pasted := strings.Repeat("large paste body\n", 400)

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	if len(m.pastedTexts) != 1 {
		t.Fatalf("pasted attachments = %d, want 1", len(m.pastedTexts))
	}
	compact := m.editor.Value()
	if strings.Contains(compact, pasted) || !strings.Contains(compact, "[Pasted text #1") {
		t.Fatalf("composer did not collapse paste: %q", compact)
	}
	if got := m.expandedPastedText(compact); got != "before "+pasted {
		t.Fatalf("expanded paste mismatch: got %d bytes, want %d", len(got), len("before "+pasted))
	}
	if view := m.View(); strings.Contains(view, "large paste body\nlarge paste body") {
		t.Fatal("rendered view exposed the large paste body")
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || len(m.inputHistory) == 0 {
		t.Fatalf("submission state: cmd=%v history=%d", cmd != nil, len(m.inputHistory))
	}
	if got := m.inputHistory[len(m.inputHistory)-1]; got != "before "+pasted {
		t.Fatalf("submitted history mismatch: got %d bytes, want %d", len(got), len("before "+pasted))
	}
	if len(m.pastedTexts) != 0 {
		t.Fatalf("submitted paste attachments = %d, want 0", len(m.pastedTexts))
	}
}

func TestLargeCtrlVPasteResultCollapses(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	pasted := strings.Repeat("clipboard text", largePasteRuneThreshold)

	_, _ = m.applyTextareaResult(textareaResultMsg{
		target: textareaTargetComposer,
		msg:    tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true},
	})

	if len(m.pastedTexts) != 1 || strings.Contains(m.editor.Value(), pasted) {
		t.Fatalf("Ctrl+V result was not collapsed: editor bytes=%d attachments=%d", len(m.editor.Value()), len(m.pastedTexts))
	}
	if got := m.expandedPastedText(m.editor.Value()); got != pasted {
		t.Fatalf("Ctrl+V expansion mismatch: got %d bytes, want %d", len(got), len(pasted))
	}
}

func TestSmallComposerPasteRemainsEditableText(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	pasted := "first line\nsecond line"

	_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	if got := m.editor.Value(); got != pasted {
		t.Fatalf("small paste = %q, want %q", got, pasted)
	}
	if len(m.pastedTexts) != 0 {
		t.Fatalf("small paste created %d attachments", len(m.pastedTexts))
	}
}

func TestLargePasteLineThresholdAndAttachmentRemoval(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	pasted := strings.Repeat("x\n", largePasteLineThreshold-1)

	_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	if len(m.pastedTexts) != 1 {
		t.Fatalf("line-threshold attachments = %d, want 1", len(m.pastedTexts))
	}

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.editor.Value(); got != "" || len(m.pastedTexts) != 0 {
		t.Fatalf("attachment removal left editor=%q attachments=%d", got, len(m.pastedTexts))
	}
}

func TestBackspaceTreatsCollapsedPasteAsOneAttachment(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.editor.SetValue("prefix ")
	m.editor.CursorEnd()
	pasted := strings.Repeat("body", largePasteRuneThreshold)
	_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := m.editor.Value(); got != "prefix " || len(m.pastedTexts) != 0 {
		t.Fatalf("atomic backspace left editor=%q attachments=%d", got, len(m.pastedTexts))
	}
}

func TestRejectedPromptRestoresCollapsedPaste(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	pasted := strings.Repeat("restore me\n", 500)
	_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	compact := m.editor.Value()
	attachments := m.takePastedTextAttachments()
	m.editor.Reset()
	m.runGeneration = 7
	m.busy = true

	_, _ = m.Update(promptDoneMsg{
		generation: 7, admitted: false, text: compact, historyText: pasted,
		pastedTexts: attachments, err: errors.New("rejected"),
	})

	if len(m.pastedTexts) != 1 || m.editor.Value() != compact {
		t.Fatalf("restored state: editor=%q attachments=%d", m.editor.Value(), len(m.pastedTexts))
	}
	if got := m.expandedPastedText(m.editor.Value()); got != pasted {
		t.Fatalf("restored paste mismatch: got %d bytes, want %d", len(got), len(pasted))
	}
}

func TestPlanCommandKeepsLargePasteCollapsedInTranscript(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("/plan ")
	m.editor.CursorEnd()
	pasted := strings.Repeat("plan body ", largePasteRuneThreshold)
	_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("plan command returned no prompt command")
	}
	for _, line := range m.lines {
		if strings.Contains(line, pasted) {
			t.Fatal("plan transcript exposed the large paste body")
		}
	}
	want := strings.TrimSpace(pasted)
	result, ok := cmd().(promptDoneMsg)
	if !ok || result.historyText != want {
		t.Fatalf("plan prompt result=%T history bytes=%d, want %d", result, len(result.historyText), len(want))
	}
}

func TestPasteExpansionDoesNotRecursivelyExpandTokenText(t *testing.T) {
	first := pastedTextAttachment{token: "[paste-one]", text: "literal [paste-two]"}
	second := pastedTextAttachment{token: "[paste-two]", text: "second body"}
	got := expandPastedTextAttachments("[paste-one] [paste-two]", []pastedTextAttachment{first, second})
	if want := "literal [paste-two] second body"; got != want {
		t.Fatalf("expanded text = %q, want %q", got, want)
	}
}
