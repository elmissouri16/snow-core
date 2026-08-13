package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestAutomaticCompactionKeepsOrdinaryRunLockedUntilTurnDone(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.runStartedAt = time.Now()
	// turn_done cleared the completed goal turn ID while GoalContinuing kept the
	// worker projection locked at the safe boundary.
	m.activeTurnID = ""

	result := &protocol.CompactionResult{Automatic: true, SummarizedMessages: 4}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, TurnID: "compact-turn", TurnOrigin: "compact"})
	if !m.busy || !m.compacting {
		t.Fatalf("automatic compaction start unlocked run: busy=%v compacting=%v", m.busy, m.compacting)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "compact-turn", TurnOrigin: "user", Compaction: result})
	if !m.busy || m.compacting {
		t.Fatalf("automatic compaction completion state: busy=%v compacting=%v", m.busy, m.compacting)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "compact-turn", TurnOrigin: "user"})
	if m.busy {
		t.Fatal("ordinary turn remained locked after turn_done")
	}
}

func TestTerminalGoalSnapshotReleasesBoundaryProjection(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.runStartedAt = time.Now()
	goal := &protocol.ThreadGoal{GoalID: "goal", Objective: "work", Status: protocol.GoalBlocked}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: goal}})
	if m.busy || !m.runStartedAt.IsZero() {
		t.Fatalf("terminal goal snapshot left boundary active: busy=%v started=%v", m.busy, m.runStartedAt)
	}
}

func TestStaleTurnEventsCannotSettleOrMutateNewRun(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.activeTurnID = "new"
	m.runStartedAt = time.Now()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "old", Text: "stale"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "old"})
	if !m.busy || m.activeTurnID != "new" {
		t.Fatalf("stale turn settled new run: busy=%v id=%q", m.busy, m.activeTurnID)
	}
	if strings.Contains(m.assistantBuf.String(), "stale") {
		t.Fatal("stale turn output entered the new run transcript")
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "new"})
	if m.busy || m.activeTurnID != "" {
		t.Fatalf("matching turn did not settle: busy=%v id=%q", m.busy, m.activeTurnID)
	}
}

func TestAbortGoalBoundaryReleasesProjectionImmediately(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.runStartedAt = time.Now()
	m.requestAbort()
	if m.busy || !m.runStartedAt.IsZero() {
		t.Fatalf("abort left boundary projection active: busy=%v started=%v", m.busy, m.runStartedAt)
	}
}

func TestPermissionRequestPreemptsExistingPicker(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 30
	m.inlineTranscript = true
	m.pickModel = true
	m.modelList = []protocol.Model{{Provider: "fake", ID: "other"}}
	request := protocol.PermissionRequest{Tool: "bash", Risk: "exec"}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPermissionRequest, Agent: &protocol.AgentRef{Path: "child"}, Permission: &protocol.Permission{Request: request}})

	view := stripANSI(m.renderOverlays())
	if !strings.Contains(view, "bash") || strings.Contains(view, "other") {
		t.Fatalf("blocking permission overlay did not preempt model picker: %q", view)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.permChoice != permChoiceAlways {
		t.Fatalf("permission did not own keyboard: choice=%d", m.permChoice)
	}
}

func TestSessionOperationGenerationIsIndependentFromPickerGeneration(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.asyncIO = true
	_, cmd := m.startNewSession()
	if cmd == nil || !m.sessionOpLoading {
		t.Fatal("new session operation did not start")
	}
	generation := m.sessionOpGeneration
	m.pickerGeneration++ // an unrelated /sessions or /model refresh
	msg := cmd()
	result, ok := msg.(sessionStoreMsg)
	if !ok || result.generation != generation {
		t.Fatalf("session result=%T %+v generation=%d", msg, msg, generation)
	}
	_, _ = m.Update(result)
	if m.sessionOpLoading {
		t.Fatal("unrelated picker generation stranded session loading")
	}
}

func TestShortInlineSettingsKeepsSelectionVisible(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 8
	m.inlineTranscript = true
	m.pickSettings = true
	m.settingsIndex = settingsSkills
	m.layout()
	view := stripANSI(m.renderOverlays())
	if !strings.Contains(view, "Agent Skills") {
		t.Fatalf("selected setting clipped on short terminal: %q", view)
	}
}

func TestInlineFooterShowsCurrentRuntimeSelection(t *testing.T) {
	for _, width := range []int{120, 80} {
		m := newModel(context.Background(), app.Options{})
		buildAppForTest(t, m)
		m.width, m.height = width, 30
		m.inlineTranscript = true
		footer := stripANSI(m.renderFooter())
		for _, want := range []string{"fake", "default", "off"} {
			if !strings.Contains(footer, want) {
				t.Fatalf("inline runtime footer width %d missing %q: %q", width, want, footer)
			}
		}
	}
}

func TestAutomaticCompactionAdoptsFollowingGoalTurn(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, TurnID: "compact"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "compact", Compaction: &protocol.CompactionResult{Automatic: true}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, TurnID: "next", GoalContinuing: true,
		ThreadGoal: &protocol.ThreadGoalUpdate{Goal: &protocol.ThreadGoal{GoalID: "goal", Status: protocol.GoalActive}}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "next", Text: "continued"})
	if m.activeTurnID != "next" || !m.busy || m.assistantBuf.String() != "continued" {
		t.Fatalf("next goal turn not adopted: id=%q busy=%v text=%q", m.activeTurnID, m.busy, m.assistantBuf.String())
	}
}
