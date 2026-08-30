package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	goal := &protocol.ThreadGoal{GoalID: "goal", Objective: "work", Status: protocol.GoalBlocked, BlockedReason: "CI service is unavailable"}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: goal}})
	if m.busy || !m.runStartedAt.IsZero() {
		t.Fatalf("terminal goal snapshot left boundary active: busy=%v started=%v", m.busy, m.runStartedAt)
	}
	if transcript := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(transcript, "Goal blocked: CI service is unavailable") {
		t.Fatalf("blocked reason missing from transcript: %q", transcript)
	}
}

func TestStaleTurnEventsCannotSettleOrMutateNewRun(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.activeTurnID = "new"
	m.runStartedAt = time.Now()
	m.subagentFleetOpen = true

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "old", Text: "stale"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "old"})
	if !m.busy || m.activeTurnID != "new" {
		t.Fatalf("stale turn settled new run: busy=%v id=%q", m.busy, m.activeTurnID)
	}
	if strings.Contains(m.assistantBuf.String(), "stale") {
		t.Fatal("stale turn output entered the new run transcript")
	}
	if activity := strings.Join(m.subagentFleetActivity["root"], "\n"); strings.Contains(activity, "stale") {
		t.Fatalf("stale turn output entered root fleet activity: %q", activity)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "new"})
	if m.busy || m.activeTurnID != "" {
		t.Fatalf("matching turn did not settle: busy=%v id=%q", m.busy, m.activeTurnID)
	}
}

func TestQueuedIntermediateTurnReducesBeforeLatestAdmission(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Agent.Prompt(context.Background(), "intermediate turn"); err != nil {
		t.Fatal(err)
	}
	_, intermediateID := m.app.Agent.LatestTurn()
	intermediateSequence := m.app.Agent.TurnSequenceWatermark()
	if err := m.app.Agent.Prompt(context.Background(), "latest turn"); err != nil {
		t.Fatal(err)
	}
	_, latestID := m.app.Agent.LatestTurn()
	latestSequence := m.app.Agent.TurnSequenceWatermark()
	if intermediateID == "" || latestID == "" || intermediateID == latestID || intermediateSequence == 0 || latestSequence <= intermediateSequence {
		t.Fatalf("turn identities intermediate=%q latest=%q", intermediateID, latestID)
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: intermediateID, TurnSequence: intermediateSequence, Text: "intermediate output"})
	if !m.busy || m.activeTurnID != intermediateID {
		t.Fatalf("intermediate turn was dropped: busy=%v id=%q", m.busy, m.activeTurnID)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: intermediateID, TurnSequence: intermediateSequence})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: latestID, TurnSequence: latestSequence, Text: "latest output"})
	if !m.busy || m.activeTurnID != latestID || m.assistantBuf.String() != "latest output" {
		t.Fatalf("latest turn not adopted: busy=%v id=%q text=%q", m.busy, m.activeTurnID, m.assistantBuf.String())
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: intermediateID, TurnSequence: intermediateSequence, Text: " stale replay"})
	if strings.Contains(m.assistantBuf.String(), "stale replay") {
		t.Fatalf("older turn reclaimed latest transcript: %q", m.assistantBuf.String())
	}
}

func TestAuthoritativeGoalContinuationReplacesStaleUIProjection(t *testing.T) {
	store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "goal-visibility.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := goalpkg.New(store, testHome(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(controller) {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	provider := newBoundaryGoalProvider()
	runtime, err := agent.New(agent.Options{
		Provider:   provider,
		Registry:   registry,
		Session:    store,
		Permission: permission.NewService(permission.ModeAllow, nil),
		Model:      protocol.Model{Provider: provider.ID(), ID: "m", SupportsTools: true},
		Goal:       controller,
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.SetEmitter(runtime.Publish)
	m := newModel(context.Background(), app.Options{})
	m.app = &app.App{Agent: runtime, Goal: controller, Registry: registry, Session: store}
	t.Cleanup(func() { _ = m.app.Close() })

	goal, err := controller.Create("keep the live goal visible", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ContinueGoal()
	call := waitBoundaryCall(t, provider, 0)
	origin, turnID, running := runtime.ActiveTurn()
	turnSequence := runtime.TurnSequenceWatermark()
	if !running || origin != "goal" || turnID == "" || turnSequence == 0 {
		t.Fatalf("active turn = origin %q id %q running %v", origin, turnID, running)
	}
	if _, err := controller.SetStatus(goal.GoalID, protocol.GoalPaused, false); err != nil {
		t.Fatal(err)
	}
	provider.release(call)
	waitCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := runtime.WaitIdle(waitCtx); err != nil {
		t.Fatal(err)
	}
	if _, _, running := runtime.ActiveTurn(); running {
		t.Fatal("goal turn still running before delayed marker reduction")
	}
	if latestOrigin, latestID := runtime.LatestTurn(); latestOrigin != "goal" || latestID != turnID {
		t.Fatalf("latest turn = origin %q id %q, want goal/%q", latestOrigin, latestID, turnID)
	}

	m.subagentFleetOpen = true
	m.activeTurnID = "stale-turn"
	m.handleAgentEvent(protocol.AgentEvent{
		Type: protocol.EvThreadGoalUpdated, TurnID: turnID, TurnOrigin: "goal", TurnSequence: turnSequence, GoalContinuing: true,
		ThreadGoal: &protocol.ThreadGoalUpdate{Goal: goal.Clone()},
	})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: turnID, TurnOrigin: "goal", TurnSequence: turnSequence, Text: "visible continuation"})

	if !m.busy || m.activeTurnID != turnID {
		t.Fatalf("continuation was not adopted: busy=%v id=%q want=%q", m.busy, m.activeTurnID, turnID)
	}
	if !strings.Contains(m.assistantBuf.String(), "visible continuation") {
		t.Fatalf("live goal output missing from transcript buffer: %q", m.assistantBuf.String())
	}
	if activity := strings.Join(m.subagentFleetActivity["root"], "\n"); !strings.Contains(activity, "visible continuation") {
		t.Fatalf("test did not reproduce fleet activity path: %q", activity)
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
	view := stripANSI(m.View())
	if !strings.Contains(view, "Agent Skills") {
		t.Fatalf("selected setting clipped in short centered card: %q", view)
	}
	if got := lipgloss.Height(view); got != m.managedFrameHeight() {
		t.Fatalf("short centered settings frame height=%d want=%d", got, m.managedFrameHeight())
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
