package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestWorkingClickScrollsTranscriptToLiveBottom(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.app.Cfg.TUI.Mouse = true
	m.width = 100
	m.height = 24
	m.busy = true
	m.runStartedAt = time.Now().Add(-time.Minute)
	m.layout()

	content := strings.Repeat("transcript line\n", 200)
	m.transcript.SetContent(content)
	m.transcriptContent = content
	m.transcript.GotoTop()
	if m.transcript.AtBottom() {
		t.Fatal("test transcript unexpectedly starts at bottom")
	}

	y, start, end, ok := m.runStatusMouseBounds()
	if !ok || end <= start {
		t.Fatalf("Working bounds = y:%d x:%d..%d ok:%v", y, start, end, ok)
	}
	_, _ = m.Update(tea.MouseMsg{
		X:      start,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if !m.transcript.AtBottom() {
		t.Fatal("clicking Working did not scroll transcript to the bottom")
	}
}

func TestWorkingClickRequiresApplicationMouseMode(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.app.Cfg.TUI.Mouse = false
	m.busy = true
	m.runStartedAt = time.Now()
	m.width = 80
	m.height = 20
	m.layout()

	y, start, _, ok := m.runStatusMouseBounds()
	if !ok {
		t.Fatal("Working bounds unavailable")
	}
	handled, cmd := m.handleRunStatusMouse(tea.MouseMsg{
		X:      start,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if handled || cmd != nil {
		t.Fatalf("native mouse mode handled Working click: handled=%v cmd=%v", handled, cmd != nil)
	}
}
