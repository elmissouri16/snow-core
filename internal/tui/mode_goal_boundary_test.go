package tui

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type boundaryGoalProvider struct {
	mu       sync.Mutex
	calls    int
	started  chan int
	releases map[int]chan struct{}
}

func newBoundaryGoalProvider() *boundaryGoalProvider {
	return &boundaryGoalProvider{started: make(chan int, 16), releases: make(map[int]chan struct{})}
}

func (*boundaryGoalProvider) ID() string { return "boundary-goal" }
func (*boundaryGoalProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (*boundaryGoalProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, nil
}
func (p *boundaryGoalProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	release := make(chan struct{})
	p.releases[call] = release
	p.mu.Unlock()
	p.started <- call
	return &boundaryGoalStream{release: release}, nil
}

func (p *boundaryGoalProvider) release(call int) {
	p.mu.Lock()
	ch := p.releases[call]
	delete(p.releases, call)
	p.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (p *boundaryGoalProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type boundaryGoalStream struct {
	release <-chan struct{}
	done    bool
}

func (s *boundaryGoalStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.done {
		return protocol.StreamEvent{}, io.EOF
	}
	select {
	case <-ctx.Done():
		return protocol.StreamEvent{}, ctx.Err()
	case <-s.release:
		s.done = true
		return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}, nil
	}
}
func (*boundaryGoalStream) Close() error { return nil }

func waitBoundaryCall(t *testing.T, p *boundaryGoalProvider, greaterThan int) int {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case call := <-p.started:
			if call > greaterThan {
				return call
			}
		case <-deadline.C:
			t.Fatalf("provider calls=%d, want > %d", p.callCount(), greaterThan)
		}
	}
}

func TestQueuedModeSwitchAtRealGoalBoundaryStopsAndConditionallyResumes(t *testing.T) {
	home := testHome(t)
	store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "mode-boundary.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := goalpkg.New(store, home, nil)
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
	a, err := agent.New(agent.Options{
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
	controller.SetEmitter(a.Publish)
	m := newModel(context.Background(), app.Options{})
	m.app = &app.App{Agent: a, Goal: controller, Registry: registry, Session: store}
	t.Cleanup(func() { _ = m.app.Close() })

	goal, err := controller.Create("exercise a real queued mode boundary", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	turnDone := make(chan protocol.AgentEvent, 8)
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvTurnDone {
			turnDone <- event
		}
	})
	a.ContinueGoal()
	first := waitBoundaryCall(t, provider, 0)
	m.busy = true
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.pendingMode == nil || *m.pendingMode != protocol.ModePlan {
		t.Fatalf("pending=%v", m.pendingMode)
	}
	provider.release(first)
	var boundary protocol.AgentEvent
	select {
	case boundary = <-turnDone:
	case <-time.After(2 * time.Second):
		t.Fatal("goal turn did not reach its boundary")
	}
	if !boundary.GoalContinuing {
		t.Fatalf("turn_done=%+v", boundary)
	}
	m.handleAgentEvent(boundary)
	applyModeToggleCommand(t, m, m.beginPendingModeSwitch())
	if a.Mode() != protocol.ModePlan || m.busy {
		t.Fatalf("mode=%q busy=%v", a.Mode(), m.busy)
	}
	callsInPlan := provider.callCount()
	time.Sleep(75 * time.Millisecond)
	if got := provider.callCount(); got != callsInPlan || got > first+1 {
		t.Fatalf("automatic work continued in Plan: before=%d after=%d", callsInPlan, got)
	}

	// Switching back to Default resumes the still-active, non-deferred goal.
	for len(provider.started) > 0 {
		<-provider.started
	}
	_, resumeCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	applyModeToggleCommand(t, m, resumeCmd)
	resumedCall := waitBoundaryCall(t, provider, callsInPlan)
	if a.Mode() != protocol.ModeDefault {
		t.Fatalf("mode=%q want default", a.Mode())
	}

	// Stop the resumed work, make the goal ineligible, and prove another
	// Plan -> Default toggle does not start a provider call.
	if err := a.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.SetStatus(goal.GoalID, protocol.GoalPaused, false); err != nil {
		t.Fatal(err)
	}
	m.busy = false
	m.pendingMode = nil
	m.modeSwitching = false
	callsWhilePaused := provider.callCount()
	_, pausedCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	applyModeToggleCommand(t, m, pausedCmd)
	time.Sleep(75 * time.Millisecond)
	if got := provider.callCount(); got != callsWhilePaused {
		t.Fatalf("paused goal resumed: before=%d after=%d (resumed call %d)", callsWhilePaused, got, resumedCall)
	}
}
