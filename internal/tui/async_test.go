package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
)

func TestMentionDiscoveryIsAsyncAndIgnoresStaleEditorState(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := os.WriteFile(filepath.Join(m.app.CWD(), "notes.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.asyncIO = true
	m.editor.SetValue("read @no")
	cmd := m.refreshInputCompletions()
	if cmd == nil || !m.mentionLoading {
		t.Fatalf("mention discovery cmd=%v loading=%v", cmd != nil, m.mentionLoading)
	}
	// The user removed the token before the walk completed. The response is
	// still useful for the cache, but must not reopen the picker.
	m.editor.SetValue("read ")
	m.Update(cmd())
	if m.mentionLoading || m.mentionVisible {
		t.Fatalf("stale mention result reopened picker: loading=%v visible=%v", m.mentionLoading, m.mentionVisible)
	}
	m.editor.SetValue("read @no")
	m.refreshInputCompletions()
	if !m.mentionVisible || len(m.mentionMatches) != 1 || m.mentionMatches[0] != "notes.md" {
		t.Fatalf("cached mention result = visible %v matches %v", m.mentionVisible, m.mentionMatches)
	}
}

func TestAsyncSessionPickerCanCloseBeforeResult(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.asyncIO = true
	_, cmd := m.startSessionPick()
	if cmd == nil || !m.sessionLoading || !m.pickSession {
		t.Fatalf("session picker cmd=%v loading=%v picker=%v", cmd != nil, m.sessionLoading, m.pickSession)
	}
	_, _ = m.handleSessionPick(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickSession || m.sessionLoading {
		t.Fatal("Esc did not close loading session picker")
	}
	// The generation bump makes this result harmless and prevents a closed
	// picker from being resurrected.
	m.Update(cmd())
	if m.pickSession || m.sessionLoading {
		t.Fatal("stale session result reopened picker")
	}
}

func TestAsyncSessionCreateSwitchesAfterCommand(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.asyncIO = true
	old := m.app.Session.ID()
	_, cmd := m.startNewSession()
	if cmd == nil || !m.sessionOpLoading {
		t.Fatalf("new session cmd=%v loading=%v", cmd != nil, m.sessionOpLoading)
	}
	m.Update(cmd())
	if m.sessionOpLoading || m.app.Session.ID() == old || m.app.Session.Path() == "" {
		t.Fatalf("new session switch id=%q old=%q loading=%v", m.app.Session.ID(), old, m.sessionOpLoading)
	}
}

func TestAsyncTreePickerLoadsBranches(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Agent.Prompt(context.Background(), "branch"); err != nil {
		t.Fatal(err)
	}
	messages, err := m.app.Agent.Messages()
	if err != nil || len(messages) == 0 {
		t.Fatalf("messages=%d err=%v", len(messages), err)
	}
	if _, err := m.app.Agent.Fork(messages[0].ID); err != nil {
		t.Fatal(err)
	}
	m.asyncIO = true
	_, cmd := m.startTreePick()
	if cmd == nil || !m.treeLoading {
		t.Fatalf("tree cmd=%v loading=%v", cmd != nil, m.treeLoading)
	}
	m.Update(cmd())
	if m.treeLoading || !m.pickTree || len(m.branches) != 2 {
		t.Fatalf("tree loading=%v picker=%v branches=%d", m.treeLoading, m.pickTree, len(m.branches))
	}
}

func TestAsyncModelPickerShowsLoadingState(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.asyncIO = true
	m.modelList = nil
	m.app.AllModels = nil
	m.app.Models = nil
	_, cmd := m.startModelPick()
	if cmd == nil || !m.pickModel || !m.modelLoading {
		t.Fatalf("model cmd=%v picker=%v loading=%v", cmd != nil, m.pickModel, m.modelLoading)
	}
	if got := stripANSI(m.renderModelPicker()); got == "" || !containsAny(got, "loading models") {
		t.Fatalf("model loading view=%q", got)
	}
	m.Update(cmd())
	if m.modelLoading || !m.pickModel || len(m.modelList) == 0 {
		t.Fatalf("model result loading=%v picker=%v models=%d", m.modelLoading, m.pickModel, len(m.modelList))
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
