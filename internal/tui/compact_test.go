package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestModelStartupStaysOutOfTranscript(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	_, _ = m.Update(doneMsg{app: m.app})
	transcript := strings.Join(m.lines, "\n")
	for _, noisy := range []string{"Type /quit", "cwd ", "/help for commands"} {
		if strings.Contains(transcript, noisy) {
			t.Fatalf("startup noise %q leaked into transcript: %q", noisy, transcript)
		}
	}
}

func TestIdleSpinnerStopsAndRestarts(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil || m.spinnerRunning {
		t.Fatalf("idle spinner remained armed: cmd=%v running=%v", cmd != nil, m.spinnerRunning)
	}

	if _, cmd := m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{Type: protocol.EvTextDelta, TurnID: "turn-1", Text: "a"}}}); cmd == nil || !m.spinnerRunning {
		t.Fatalf("busy transition did not start spinner: cmd=%v running=%v", cmd != nil, m.spinnerRunning)
	}
	if _, cmd := m.Update(spinner.TickMsg{}); cmd == nil || !m.spinnerRunning {
		t.Fatalf("busy spinner did not re-arm: cmd=%v running=%v", cmd != nil, m.spinnerRunning)
	}

	m.setRunIdle()
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil || m.spinnerRunning {
		t.Fatalf("completed spinner remained armed: cmd=%v running=%v", cmd != nil, m.spinnerRunning)
	}
	m.editor.SetValue("/compact")
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || !m.spinnerRunning || !m.compacting {
		t.Fatalf("compaction did not restart spinner: cmd=%v running=%v compacting=%v", cmd != nil, m.spinnerRunning, m.compacting)
	}
}

func TestCompactionShowsAnimatedProgress(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	cmd := m.startCompact()
	if cmd == nil || !m.busy || !m.compacting {
		t.Fatalf("compact start: cmd=%v busy=%v compacting=%v", cmd != nil, m.busy, m.compacting)
	}
	before := stripANSI(m.renderCompactionProgress())
	if !strings.Contains(before, "compacting context") {
		t.Fatalf("progress = %q", before)
	}
	_, _ = m.Update(spinner.TickMsg{})
	after := stripANSI(m.renderCompactionProgress())
	if before == after {
		t.Fatalf("spinner frame did not advance: %q", after)
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, Message: "compacting 12 messages"})
	if progress := stripANSI(m.renderCompactionProgress()); !strings.Contains(progress, "compacting 12 messages") {
		t.Fatalf("progress = %q", progress)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone})
	if m.compacting {
		t.Fatal("compaction animation remained active after completion")
	}
	m.abort()
}

func TestTrailingSessionUpdateCannotResurrectCompletedCompaction(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.compacting = true
	m.activeTurnID = "compact-turn"
	m.runStartedAt = m.currentTime()
	result := &protocol.CompactionResult{SummarizedMessages: 4, RetainedMessages: 2}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "compact-turn", TurnOrigin: "compact", Compaction: result})
	if m.busy || m.compacting || !m.runStartedAt.IsZero() {
		t.Fatalf("compaction did not settle: busy=%v compacting=%v started=%v", m.busy, m.compacting, m.runStartedAt)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSessionUpdated, TurnID: "compact-turn", TurnOrigin: "compact"})
	if m.busy || m.compacting || m.activeTurnID != "" || !m.runStartedAt.IsZero() {
		t.Fatalf("trailing session update resurrected compact turn: busy=%v compacting=%v id=%q started=%v", m.busy, m.compacting, m.activeTurnID, m.runStartedAt)
	}
}

func TestCompactStatusReportsOnlyDurablyDeferredGoalAsPaused(t *testing.T) {
	testHome(t)
	a, err := app.New(context.Background(), app.Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(context.Background(), app.Options{})
	m.app = a
	goal, err := a.Goal.Create("compact status", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	m.goal = goal
	result := protocol.CompactionResult{SummarizedMessages: 2, RetainedMessages: 2}

	_, _ = m.Update(compactDoneMsg{generation: m.compactGeneration, result: result})
	if strings.Contains(m.lastStatus, "goal paused") {
		t.Fatalf("nondeferred goal reported paused: %q", m.lastStatus)
	}
	if err := a.Goal.Defer(true); err != nil {
		t.Fatal(err)
	}
	_, _ = m.Update(compactDoneMsg{generation: m.compactGeneration, result: result})
	if !strings.Contains(m.lastStatus, "goal paused; /goal resume to continue") {
		t.Fatalf("deferred goal missing pause guidance: %q", m.lastStatus)
	}
}

func TestModelThinkingPlaceholderTracksProviderWaits(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	m.busy = true
	m.refreshTranscript()

	before := stripANSI(m.transcript.View())
	if !strings.Contains(before, "thinking…") {
		t.Fatalf("initial provider wait has no thinking placeholder: %q", before)
	}
	_, _ = m.Update(spinner.TickMsg{})
	after := stripANSI(m.transcript.View())
	if before == after {
		t.Fatalf("thinking placeholder spinner did not advance: %q", after)
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "glob"})
	if view := stripANSI(m.transcript.View()); strings.Contains(view, "thinking…") {
		t.Fatalf("thinking placeholder remained during tool execution: %q", view)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "glob"})
	if view := stripANSI(m.transcript.View()); !strings.Contains(view, "thinking…") {
		t.Fatalf("thinking placeholder did not return before follow-up response: %q", view)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if view := stripANSI(m.transcript.View()); strings.Contains(view, "thinking…") {
		t.Fatalf("thinking placeholder remained after turn completion: %q", view)
	}
}

func TestModelCompactTranscriptPresentation(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{Input: 100, Output: 10}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Hello!"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	view := stripANSI(m.View())

	if strings.Contains(view, "tokens:") {
		t.Fatal("token diagnostics should stay out of the normal transcript")
	}
	if !strings.Contains(view, "Hello!") || strings.Contains(view, "assistant:") {
		t.Fatalf("short assistant reply should be clean and stay on one line: %q", view)
	}
	wantChromeHeight := fixedChromeHeight + minComposerHeight
	if m.chromeHeight() != wantChromeHeight {
		t.Fatalf("compact chrome height = %d, want %d", m.chromeHeight(), wantChromeHeight)
	}
}
