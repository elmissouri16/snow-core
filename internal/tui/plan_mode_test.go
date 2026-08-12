package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestPlanCommandsAndIndicator(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()
	if _, cmd := m.runCommand("/plan"); cmd != nil {
		t.Fatal("mode-only /plan returned command")
	}
	if m.app.Agent.Mode() != protocol.ModePlan {
		t.Fatalf("mode = %q", m.app.Agent.Mode())
	}
	plain := stripANSI(m.View())
	if !strings.Contains(plain, "mode:plan") {
		t.Fatalf("view missing plan indicator: %q", plain)
	}
	m.runCommand("/default")
	if m.app.Agent.Mode() != protocol.ModeDefault {
		t.Fatalf("mode = %q", m.app.Agent.Mode())
	}
}

func applyModeToggleCommand(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("mode toggle returned no command")
	}
	msg, ok := cmd().(modeSwitchDoneMsg)
	if !ok {
		t.Fatalf("mode toggle command returned %T", msg)
	}
	_, _ = m.Update(msg)
}

func TestTopLevelShiftTabTogglesModeAndNeverEdits(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	applyModeToggleCommand(t, m, cmd)
	if got := m.app.Agent.Mode(); got != protocol.ModePlan {
		t.Fatalf("mode=%q want plan", got)
	}
	if got := m.editor.Value(); got != "" {
		t.Fatalf("Shift+Tab edited composer: %q", got)
	}
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	applyModeToggleCommand(t, m, cmd)
	if got := m.app.Agent.Mode(); got != protocol.ModeDefault {
		t.Fatalf("mode=%q want default", got)
	}
}

func TestShiftTabQueuesUntilTurnBoundaryAndCanCancel(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()
	m.busy = true
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab}); cmd != nil {
		t.Fatal("busy mode toggle ran before turn boundary")
	}
	if m.pendingMode == nil || *m.pendingMode != protocol.ModePlan {
		t.Fatalf("pending mode=%v want plan", m.pendingMode)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "mode:default→plan") {
		t.Fatalf("pending mode indicator missing: %q", view)
	}
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab}); cmd != nil || m.pendingMode != nil {
		t.Fatalf("second Shift+Tab did not cancel: pending=%v cmd=%v", m.pendingMode, cmd != nil)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, GoalContinuing: true})
	if !m.modeSwitchReady || m.app.Agent.Mode() != protocol.ModeDefault || !m.busy {
		t.Fatalf("boundary ready=%v mode=%q busy=%v", m.modeSwitchReady, m.app.Agent.Mode(), m.busy)
	}
	applyModeToggleCommand(t, m, m.beginPendingModeSwitch())
	if got := m.app.Agent.Mode(); got != protocol.ModePlan || m.busy {
		t.Fatalf("queued mode=%q busy=%v want plan/idle", got, m.busy)
	}

	m.busy = true
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	applyModeToggleCommand(t, m, m.beginPendingModeSwitch())
	if got := m.app.Agent.Mode(); got != protocol.ModeDefault {
		t.Fatalf("queued mode=%q want default", got)
	}
}

type failingTUIModeStore struct{ session.Store }

func (*failingTUIModeStore) CollaborationMode() (protocol.CollaborationMode, error) {
	return protocol.ModeDefault, nil
}
func (*failingTUIModeStore) SetCollaborationMode(protocol.CollaborationMode) error {
	return errors.New("injected mode persistence failure")
}

func TestFailedQueuedModeSwitchReconcilesOptimisticGoalBusyState(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	store := &failingTUIModeStore{Store: session.NewMemoryStore(session.Options{CWD: t.TempDir()})}
	if err := m.app.Agent.SetSession(store); err != nil {
		t.Fatal(err)
	}
	m.busy = true
	m.runStartedAt = time.Now()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, GoalContinuing: true})
	cmd := m.beginPendingModeSwitch()
	if cmd == nil {
		t.Fatal("queued switch returned no command")
	}
	msg, ok := cmd().(modeSwitchDoneMsg)
	if !ok || msg.err == nil {
		t.Fatalf("mode result=%T %+v", msg, msg)
	}
	_, _ = m.Update(msg)
	if m.busy || !m.runStartedAt.IsZero() || m.app.Agent.Mode() != protocol.ModeDefault || m.pendingMode != nil {
		t.Fatalf("busy=%v started=%v mode=%q pending=%v", m.busy, m.runStartedAt, m.app.Agent.Mode(), m.pendingMode)
	}
	if len(m.lines) == 0 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "injected mode persistence failure") {
		t.Fatalf("missing inline failure: %v", m.lines)
	}
}

func TestPromptSubmissionWaitsForAsynchronousModeSwitch(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("must run in selected mode")
	m.modeSwitching = true
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.busy || m.editor.Value() != "must run in selected mode" {
		t.Fatalf("cmd=%v busy=%v editor=%q", cmd != nil, m.busy, m.editor.Value())
	}
	if m.lastStatus != "waiting for mode switch" {
		t.Fatalf("status=%q", m.lastStatus)
	}
}

func TestShiftTabKeepsCompletionNavigationPrecedence(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.compVisible = true
	m.compMatches = []string{"/default", "/help", "/plan"}
	m.compIndex = 1
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil || m.compIndex != 0 || m.app.Agent.Mode() != protocol.ModeDefault {
		t.Fatalf("cmd=%v index=%d mode=%q", cmd != nil, m.compIndex, m.app.Agent.Mode())
	}
}

func TestStructuredPlanRenderingAndImplementationPrompt(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	_ = m.app.Agent.SetMode(protocol.ModePlan)
	m.width, m.height = 100, 30
	m.layout()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: &protocol.PlanItem{ID: "p"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "# Ship\n- test\n", Plan: &protocol.PlanItem{ID: "p"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanCompleted, Plan: &protocol.PlanItem{ID: "p", Text: "# Ship\n- test\n"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if !m.planPrompt || !strings.Contains(stripANSI(m.View()), "Implement this plan?") || !strings.Contains(stripANSI(m.View()), "Ship") {
		t.Fatalf("view = %q", stripANSI(m.View()))
	}
	_, cmd := m.handlePlanImplementationKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("implementation selection returned no command")
	}
	_ = cmd()
	if m.app.Agent.Mode() != protocol.ModeDefault {
		t.Fatalf("mode = %q", m.app.Agent.Mode())
	}
}

func TestPlanKeywordNudge(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	probe := &planNudgeSession{Store: m.app.Session, activeID: "feature"}
	m.app.Session = probe

	m.editor.SetValue("ordinary prompt")
	if m.planNudgeVisible() {
		t.Fatal("unexpected plan nudge")
	}
	if probe.activeCalls != 0 || probe.branchesCalls != 0 {
		t.Fatalf("ordinary prompt queried branch state: active=%d branches=%d", probe.activeCalls, probe.branchesCalls)
	}

	m.editor.SetValue("make a plan first")
	if !m.planNudgeVisible() {
		t.Fatal("expected plan nudge")
	}
	if probe.branchesCalls != 0 {
		t.Fatalf("plan nudge loaded rich branch history %d times", probe.branchesCalls)
	}
	m.nudgeDismissed[m.planNudgeScope()] = true
	if m.planNudgeVisible() {
		t.Fatal("dismissed nudge remained visible")
	}

	probe.activeID = "other"
	if !m.planNudgeVisible() {
		t.Fatal("dismissal leaked to another branch")
	}
	if probe.branchesCalls != 0 {
		t.Fatalf("plan nudge loaded rich branch history %d times", probe.branchesCalls)
	}
}

type planNudgeSession struct {
	session.Store
	activeID      string
	activeCalls   int
	branchesCalls int
}

func (s *planNudgeSession) ActiveBranchID() string {
	s.activeCalls++
	return s.activeID
}

func (s *planNudgeSession) Branches() ([]protocol.SessionBranch, error) {
	s.branchesCalls++
	return []protocol.SessionBranch{{ID: s.activeID, Active: true}}, nil
}

func (s *planNudgeSession) SelectBranch(string) error { return nil }

func (s *planNudgeSession) ForkBranch(string) (protocol.SessionBranch, error) {
	return protocol.SessionBranch{}, errors.New("not implemented")
}
