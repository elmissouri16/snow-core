package tui

import (
	"context"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestComposerInputHistoryNavigatesAndRestoresDraft(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.inputHistory = []string{"first prompt", "second\nprompt", "$review latest"}
	m.resetInputHistoryNavigation()
	m.editor.SetValue("current draft")

	assertHistoryKey := func(key tea.KeyType, want string) {
		t.Helper()
		_, _ = m.handleKey(tea.KeyMsg{Type: key})
		if got := m.editor.Value(); got != want {
			t.Fatalf("after %v editor = %q, want %q", key, got, want)
		}
	}

	assertHistoryKey(tea.KeyUp, "$review latest")
	if m.skillVisible || m.compVisible || m.mentionVisible {
		t.Fatal("recalled input opened a completion picker that would capture history arrows")
	}
	assertHistoryKey(tea.KeyUp, "second\nprompt")
	assertHistoryKey(tea.KeyUp, "first prompt")
	assertHistoryKey(tea.KeyUp, "first prompt")
	assertHistoryKey(tea.KeyDown, "second\nprompt")
	assertHistoryKey(tea.KeyDown, "$review latest")
	assertHistoryKey(tea.KeyDown, "current draft")
}

func TestComposerInputHistoryLeavesMultilineDraftNavigationToTextarea(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.inputHistory = []string{"older"}
	m.resetInputHistoryNavigation()
	m.editor.SetValue("line one\nline two")
	beforeIndex := m.inputHistoryIndex

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.editor.Value(); got != "line one\nline two" {
		t.Fatalf("multiline draft was replaced by history: %q", got)
	}
	if m.inputHistoryIndex != beforeIndex {
		t.Fatalf("multiline draft started history browsing at index %d", m.inputHistoryIndex)
	}
}

func TestComposerInputHistoryEditingEndsNavigation(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.inputHistory = []string{"older", "latest"}
	m.resetInputHistoryNavigation()

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.inputHistoryIndex != 1 {
		t.Fatalf("history index = %d, want 1", m.inputHistoryIndex)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if m.inputHistoryIndex != len(m.inputHistory) {
		t.Fatalf("editing recalled input left history navigation active at %d", m.inputHistoryIndex)
	}
	if got := m.editor.Value(); got != "latest!" {
		t.Fatalf("edited recalled input = %q", got)
	}
}

func TestHydrateSessionBuildsHistoryFromActiveUserMessages(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	appendMessage := func(message protocol.Message) {
		t.Helper()
		if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	first := protocol.NewUserMessage("history-user-1", m.app.Session.BranchTip(), "first prompt")
	appendMessage(first)
	assistant := protocol.NewAssistantMessage(
		"history-assistant", m.app.Session.BranchTip(), "fake", "fake-model",
		[]protocol.ContentBlock{protocol.NewTextBlock("answer")}, protocol.StopStop, nil,
	)
	appendMessage(assistant)
	second := protocol.NewUserMessage("history-user-2", m.app.Session.BranchTip(), " second\n  prompt ")
	appendMessage(second)
	imageOnly := protocol.NewUserContentMessage(
		"history-user-image", m.app.Session.BranchTip(),
		[]protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte("png")}},
	)
	appendMessage(imageOnly)

	m.hydrateSession()
	want := []string{"first prompt", " second\n  prompt "}
	if !slices.Equal(m.inputHistory, want) {
		t.Fatalf("input history = %#v, want %#v", m.inputHistory, want)
	}
	if m.inputHistoryIndex != len(want) {
		t.Fatalf("history index = %d, want %d", m.inputHistoryIndex, len(want))
	}
}

func TestRejectedPromptIsRemovedFromInputHistory(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Agent.SetModel(protocol.Model{Provider: "fake", ID: "thinking", SupportsThinking: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingHigh}}); err != nil {
		t.Fatal(err)
	}
	if err := m.app.Agent.SetThinking(protocol.ThinkingHigh); err != nil {
		t.Fatal(err)
	}
	if err := m.app.Agent.SetModel(protocol.Model{Provider: "openai-compatible", ID: "no-thinking", SupportsTools: true}); err != nil {
		t.Fatal(err)
	}

	result, ok := m.startPrompt("rejected prompt")().(promptDoneMsg)
	if !ok || result.err == nil || result.admitted {
		t.Fatalf("prompt result = %#v", result)
	}
	if !slices.Equal(m.inputHistory, []string{"rejected prompt"}) {
		t.Fatalf("optimistic input history = %#v", m.inputHistory)
	}
	_, _ = m.Update(result)
	if len(m.inputHistory) != 0 {
		t.Fatalf("rejected input remained in history: %#v", m.inputHistory)
	}
}
