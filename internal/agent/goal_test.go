package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func goalAgent(t *testing.T, p *scriptedProvider) (*Agent, *goalpkg.Controller, *session.SQLiteStore) {
	t.Helper()
	st, e := session.NewSQLiteStore(filepath.Join(t.TempDir(), "g.db"), t.TempDir(), session.Options{})
	if e != nil {
		t.Fatal(e)
	}
	c, _ := goalpkg.New(st, t.TempDir(), nil)
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		if e := reg.Register(tool); e != nil {
			t.Fatal(e)
		}
	}
	a, e := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: c})
	if e != nil {
		t.Fatal(e)
	}
	c.SetEmitter(a.Publish)
	t.Cleanup(func() { a.Close(); st.Close() })
	return a, c, st
}
func TestInternalGoalTurnNoUserMessageAndAccountsOnce(t *testing.T) {
	var cID string
	p := &scriptedProvider{}
	a, c, st := goalAgent(t, p)
	g, e := c.Create("finish objective", nil, false)
	if e != nil {
		t.Fatal(e)
	}
	cID = g.GoalID
	p.scripts = [][]protocol.StreamEvent{{{Type: protocol.EvStreamToolCallDone, ToolCallID: "u", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + cID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}, {{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 3, Output: 2, Total: 5, Cost: &protocol.Cost{Currency: "USD", Input: 0.01, Output: 0.02, Total: 0.03}}}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}
	if e := a.TryInternalTurn(context.Background()); e != nil {
		t.Fatal(e)
	}
	got, _ := c.Get()
	if got.Status != protocol.GoalComplete || got.TokensUsed != 5 {
		t.Fatalf("goal=%+v", got)
	}
	if len(got.EstimatedCosts) != 1 || got.EstimatedCosts[0].Total != 0.03 {
		t.Fatalf("estimated costs=%+v", got.EstimatedCosts)
	}
	msgs, _ := st.Messages()
	for _, m := range msgs {
		if m.Role == protocol.RoleUser {
			t.Fatalf("visible user message=%+v", m)
		}
	}
	if len(p.requests) != 2 || len(p.requests[0].InternalContext) != 1 || !strings.Contains(p.requests[0].InternalContext[0].Text, "finish objective") {
		t.Fatalf("requests=%+v", p.requests)
	}
}
func TestGoalUsageUpdatesAfterEachProviderResponse(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	g, _ := c.Create("live usage", nil, false)
	p.scripts = [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 5}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "g", ToolName: "get_goal", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 7}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "u", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 3}}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	var usage []int64
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvThreadGoalUpdated && ev.ThreadGoal != nil && ev.ThreadGoal.Goal != nil {
			usage = append(usage, ev.ThreadGoal.Goal.TokensUsed)
		}
	})
	if err := a.TryInternalTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{5, 12, 15} {
		found := false
		for _, got := range usage {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("goal updates=%v missing %d", usage, want)
		}
	}
}

func TestCumulativeUsageSnapshotChargedOnceAndEventOrder(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	g, _ := c.Create("usage", nil, false)
	p.scripts = [][]protocol.StreamEvent{{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 5, Cost: &protocol.Cost{Currency: "USD", Total: 0.05}}}, {Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 7, Cost: &protocol.Cost{Currency: "USD", Total: 0.07}}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "u", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}, {{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 3, Cost: &protocol.Cost{Currency: "USD", Total: 0.03}}}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}
	var order []protocol.AgentEventType
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvThreadGoalUpdated || ev.Type == protocol.EvTurnDone {
			order = append(order, ev.Type)
		}
	})
	if err := a.TryInternalTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = a.DrainEvents(context.Background())
	got, _ := c.Get()
	if got.TokensUsed != 10 {
		t.Fatalf("tokens=%d", got.TokensUsed)
	}
	if len(got.EstimatedCosts) != 1 || math.Abs(got.EstimatedCosts[0].Total-0.10) > 1e-9 {
		t.Fatalf("cumulative snapshot cost=%+v", got.EstimatedCosts)
	}
	if len(order) == 0 || order[len(order)-1] != protocol.EvTurnDone {
		t.Fatalf("order=%v", order)
	}
}
func TestNoProgressGuardPausesAfterThree(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, c, _ := goalAgent(t, p)
	_, _ = c.Create("no progress", nil, false)
	a.ContinueGoal()
	if err := a.WaitGoal(context.Background()); err != nil {
		t.Fatal(err)
	}
	g, _ := c.Get()
	if g.Status != protocol.GoalPaused || p.call != 3 {
		t.Fatalf("goal=%+v calls=%d", g, p.call)
	}
}

func TestAutomaticGoalRecoversFromTransientProviderFailure(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	g, err := c.Create("recover automatically", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	transient := responsesapi.NewResponseError("chatgpt", 0, "network request failed", "network_error", "")
	p.scripts = [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamError, Err: transient}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "complete", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	retrying := make(chan struct{}, 1)
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvError && strings.Contains(event.Message, "retrying active goal") {
			select {
			case retrying <- struct{}{}:
			default:
			}
		}
	})
	a.ContinueGoal()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != protocol.GoalComplete || p.call != 3 {
		t.Fatalf("goal=%+v calls=%d", got, p.call)
	}
	select {
	case <-retrying:
	default:
		t.Fatal("missing transient retry event")
	}
}

func TestAutomaticGoalBlocksAfterTransientRetryExhaustion(t *testing.T) {
	transient := responsesapi.NewResponseError("chatgpt", 0, "network request failed", "network_error", "")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamError, Err: transient}}}}
	a, c, _ := goalAgent(t, p)
	_, _ = c.Create("stop after bounded retry", nil, false)
	a.ContinueGoal()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != protocol.GoalBlocked || p.call != 2 {
		t.Fatalf("goal=%+v calls=%d", got, p.call)
	}
}

type accountingErrorStore struct {
	*session.SQLiteStore
	err error
}

func (s *accountingErrorStore) AccountGoal(string, int64, int64, *protocol.Cost) (*protocol.ThreadGoal, bool, error) {
	return nil, false, s.err
}

func TestAccountingFailureStopsGoalAndReturnsError(t *testing.T) {
	base, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "account-error.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	store := &accountingErrorStore{SQLiteStore: base, err: errors.New("injected accounting failure")}
	controller, err := goalpkg.New(store, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(controller) {
		_ = reg.Register(tool)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "work"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{Provider: p, Registry: reg, Session: store, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: controller})
	if err != nil {
		t.Fatal(err)
	}
	controller.SetEmitter(a.Publish)
	defer func() { a.Close(); base.Close() }()
	g, err := controller.Create("account reliably", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var done *protocol.AgentEvent
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvTurnDone {
			copy := ev
			done = &copy
		}
	})
	err = a.TryInternalTurn(context.Background())
	if err == nil || !strings.Contains(err.Error(), "injected accounting failure") {
		t.Fatalf("turn err=%v", err)
	}
	if drainErr := a.DrainEvents(context.Background()); drainErr != nil {
		t.Fatal(drainErr)
	}
	got, _ := controller.Get()
	if got.GoalID != g.GoalID || got.Status != protocol.GoalBlocked {
		t.Fatalf("goal=%+v", got)
	}
	if done == nil || done.GoalContinuing {
		t.Fatalf("turn_done=%+v", done)
	}
}

func TestUsageLimitErrorStopsGoal(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamError, Err: &providerpkg.LimitError{Provider: "test", Status: 429, Message: "quota"}}}}}
	a, c, _ := goalAgent(t, p)
	g, _ := c.Create("quota goal", nil, false)
	_ = a.TryInternalTurn(context.Background())
	got, _ := c.Get()
	if got.GoalID != g.GoalID || got.Status != protocol.GoalUsageLimited {
		t.Fatalf("goal=%+v", got)
	}
}

func TestPlanModeCancelsAutomaticGoalAndDefaultResumesOnce(t *testing.T) {
	p := &resumeAfterPromptProvider{started: make(chan struct{})}
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "plan-goal.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := goalpkg.New(st, t.TempDir(), nil)
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		_ = reg.Register(tool)
	}
	a, err := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: c})
	if err != nil {
		t.Fatal(err)
	}
	c.SetEmitter(a.Publish)
	defer func() { a.Close(); st.Close() }()
	g, _ := c.Create("plan interaction", nil, false)
	p.goalID = g.GoalID
	a.ContinueGoal()
	<-p.started
	if err := a.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if a.Mode() != protocol.ModePlan {
		t.Fatalf("mode=%s", a.Mode())
	}
	if got, _ := c.Get(); got.Status != protocol.GoalActive || got.TokensUsed != 0 {
		t.Fatalf("plan transition changed goal: %+v", got)
	}
	if err := a.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(); got.Status != protocol.GoalComplete {
		t.Fatalf("default mode did not resume goal: %+v", got)
	}
	p.mu.Lock()
	calls := p.calls
	p.mu.Unlock()
	if calls != 4 {
		t.Fatalf("provider calls=%d want canceled+resume+tool-followup (4)", calls)
	}
}

func TestReturningToDefaultDoesNotClearGoalDeferral(t *testing.T) {
	a, c, _ := goalAgent(t, &scriptedProvider{})
	if _, err := c.Create("stay deferred across modes", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Defer(true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if err := a.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * automaticTurnDelay)
	deferred, err := c.Deferred()
	if err != nil || !deferred || a.IsRunning() {
		t.Fatalf("deferred=%v running=%v err=%v", deferred, a.IsRunning(), err)
	}
}

func TestFailedPlanModePersistenceRestartsAutomaticGoal(t *testing.T) {
	p := &resumeAfterPromptProvider{started: make(chan struct{})}
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "failed-plan-goal.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := goalpkg.New(st, t.TempDir(), nil)
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		_ = reg.Register(tool)
	}
	a, err := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: c})
	if err != nil {
		t.Fatal(err)
	}
	c.SetEmitter(a.Publish)
	defer func() { a.Close(); st.Close() }()
	g, _ := c.Create("resume after failed mode persistence", nil, false)
	p.goalID = g.GoalID
	a.ContinueGoal()
	<-p.started
	a.opts.Session = &failingThreadStateStore{Store: st, mode: protocol.ModeDefault, setErr: errors.New("mode write failed")}
	if err := a.SetMode(protocol.ModePlan); err == nil || !strings.Contains(err.Error(), "mode write failed") {
		t.Fatalf("SetMode err=%v", err)
	}
	if a.Mode() != protocol.ModeDefault {
		t.Fatalf("mode=%q want default", a.Mode())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(); got.Status != protocol.GoalComplete {
		t.Fatalf("goal did not resume after failed mode switch: %+v", got)
	}
}

func TestPromptWithModePreservesGoalDeferral(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "planning"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, c, _ := goalAgent(t, p)
	if _, err := c.Create("stay deferred", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Defer(true); err != nil {
		t.Fatal(err)
	}
	if err := a.PromptWithMode(context.Background(), "plan this", protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	deferred, err := c.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	if a.Mode() != protocol.ModePlan || p.call != 1 {
		t.Fatalf("mode=%q provider calls=%d", a.Mode(), p.call)
	}
}

func TestPlanModeRejectsGoalTurnWithoutAccounting(t *testing.T) {
	a, c, _ := goalAgent(t, &scriptedProvider{})
	g, _ := c.Create("plan excluded", nil, false)
	if e := a.SetMode(protocol.ModePlan); e != nil {
		t.Fatal(e)
	}
	if e := a.TryInternalTurn(context.Background()); e == nil {
		t.Fatal("plan internal turn succeeded")
	}
	got, _ := c.Get()
	if got.GoalID != g.GoalID || got.TokensUsed != 0 {
		t.Fatalf("goal=%+v", got)
	}
}

type cancelGoalProvider struct {
	started chan struct{}
	calls   int
}

func (p *cancelGoalProvider) ID() string                                           { return "cancel-goal" }
func (p *cancelGoalProvider) ListModels(context.Context) ([]protocol.Model, error) { return nil, nil }
func (p *cancelGoalProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, nil
}
func (p *cancelGoalProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
	p.calls++
	return &cancelGoalStream{started: p.started}, nil
}

type cancelGoalStream struct {
	started chan struct{}
	once    bool
}

func (s *cancelGoalStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if !s.once {
		s.once = true
		close(s.started)
	}
	<-ctx.Done()
	return protocol.StreamEvent{}, ctx.Err()
}
func (*cancelGoalStream) Close() error { return nil }

type resumeAfterPromptProvider struct {
	mu      sync.Mutex
	started chan struct{}
	goalID  string
	calls   int
}

func (p *resumeAfterPromptProvider) ID() string { return "resume-after-prompt" }
func (p *resumeAfterPromptProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *resumeAfterPromptProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, nil
}
func (p *resumeAfterPromptProvider) Chat(_ context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	switch call {
	case 1:
		return &cancelGoalStream{started: p.started}, nil
	case 2:
		return &e2eStream{events: []protocol.StreamEvent{{Type: protocol.EvStreamTextDelta, Text: "user turn"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}, nil
	case 3:
		return &e2eStream{events: []protocol.StreamEvent{{Type: protocol.EvStreamToolCallDone, ToolCallID: "done", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + p.goalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}}, nil
	default:
		return &e2eStream{events: []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}, nil
	}
}

func TestUserPromptRestartsInterruptedAutomaticGoal(t *testing.T) {
	p := &resumeAfterPromptProvider{started: make(chan struct{})}
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "resume.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := goalpkg.New(st, t.TempDir(), nil)
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		_ = reg.Register(tool)
	}
	a, err := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: c})
	if err != nil {
		t.Fatal(err)
	}
	c.SetEmitter(a.Publish)
	defer func() { a.Close(); st.Close() }()
	g, _ := c.Create("resume after user guidance", nil, false)
	p.goalID = g.GoalID
	a.ContinueGoal()
	<-p.started
	if err := a.Prompt(context.Background(), "additional guidance"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := c.Get()
	if got.Status != protocol.GoalComplete {
		t.Fatalf("goal did not resume after user turn: %+v", got)
	}
}

func TestPreFirstTurnStopPersistsDeferral(t *testing.T) {
	a, c, _ := goalAgent(t, &scriptedProvider{})
	_, _ = c.Create("defer before first turn", nil, false)
	done := make(chan struct{})
	close(done)
	a.mu.Lock()
	a.autoRunning = true
	a.autoDone = done
	a.mu.Unlock()
	if err := a.StopGoal(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	deferred, err := c.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	a.mu.Lock()
	a.autoRunning = false
	a.autoDone = nil
	a.mu.Unlock()
}

func TestAbortContextCancelsOrdinaryPrompt(t *testing.T) {
	p := &cancelGoalProvider{started: make(chan struct{})}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	promptDone := make(chan error, 1)
	go func() { promptDone <- a.Prompt(context.Background(), "wait") }()
	<-p.started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.AbortContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-promptDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ordinary prompt was not aborted")
	}
}

func TestUserPromptDoesNotResumeAbortedGoalUntilExplicitContinue(t *testing.T) {
	p := &resumeAfterPromptProvider{started: make(chan struct{})}
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "abort-prompt.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := goalpkg.New(st, t.TempDir(), nil)
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		_ = reg.Register(tool)
	}
	a, err := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true}, Goal: c})
	if err != nil {
		t.Fatal(err)
	}
	c.SetEmitter(a.Publish)
	defer func() { a.Close(); st.Close() }()
	g, _ := c.Create("resume only explicitly", nil, false)
	p.goalID = g.GoalID
	a.ContinueGoal()
	<-p.started
	a.Abort()
	if err := a.WaitGoal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "commit current progress"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * automaticTurnDelay)
	deferred, err := c.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	if got, _ := c.Get(); got.Status != protocol.GoalActive || p.calls != 2 || a.IsRunning() {
		t.Fatalf("goal=%+v calls=%d running=%v", got, p.calls, a.IsRunning())
	}
	if err := c.Defer(false); err != nil {
		t.Fatal(err)
	}
	a.ContinueGoal()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.WaitGoal(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get(); got.Status != protocol.GoalComplete {
		t.Fatalf("goal did not resume after explicit continue: %+v", got)
	}
}

func TestAbortDefersWithoutRestart(t *testing.T) {
	p := &cancelGoalProvider{started: make(chan struct{})}
	st, e := session.NewSQLiteStore(filepath.Join(t.TempDir(), "a.db"), t.TempDir(), session.Options{})
	if e != nil {
		t.Fatal(e)
	}
	c, _ := goalpkg.New(st, t.TempDir(), nil)
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		_ = reg.Register(tool)
	}
	a, e := New(Options{Provider: p, Registry: reg, Session: st, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: p.ID(), ID: "m"}, Goal: c})
	if e != nil {
		t.Fatal(e)
	}
	c.SetEmitter(a.Publish)
	defer func() { a.Close(); st.Close() }()
	g, _ := c.Create("abort", nil, false)
	a.ContinueGoal()
	<-p.started
	a.Abort()
	if e := a.WaitGoal(context.Background()); e != nil {
		t.Fatal(e)
	}
	got, _ := c.Get()
	deferred, _ := c.Deferred()
	if got.GoalID != g.GoalID || got.Status != protocol.GoalActive || !deferred || p.calls != 1 {
		t.Fatalf("goal=%+v deferred=%v calls=%d", got, deferred, p.calls)
	}
}

func TestManualCompactPausesRunningAutomaticGoal(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: "working"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, c, st := goalAgent(t, p)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "error"}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("manual-pause-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Create("pause after manual compact", nil, false); err != nil {
		t.Fatal(err)
	}
	a.ContinueGoal()
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.RLock()
		running := a.autoRunning
		a.mu.RUnlock()
		p.mu.Lock()
		calls := p.call
		p.mu.Unlock()
		if running && calls >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic goal did not start")
		}
		time.Sleep(time.Millisecond)
	}
	result, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.SummarizedMessages == 0 {
		t.Fatalf("compact result=%+v", result)
	}
	time.Sleep(3 * automaticTurnDelay)
	deferred, err := c.Deferred()
	if err != nil || !deferred || a.IsRunning() {
		t.Fatalf("deferred=%v running=%v err=%v", deferred, a.IsRunning(), err)
	}
	p.mu.Lock()
	calls := p.call
	p.mu.Unlock()
	if calls != 2 {
		t.Fatalf("provider calls=%d want goal turn + summary only", calls)
	}
}

func TestManualCompactDefersIdleActiveGoalAcrossLaterContinuation(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	g, err := c.Create("wait for surface readiness", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if deferred, err := c.Deferred(); err != nil || deferred {
		t.Fatalf("initial deferred=%v err=%v", deferred, err)
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	deferred, err := c.Deferred()
	if err != nil || !deferred {
		t.Fatalf("manual compact deferred=%v err=%v", deferred, err)
	}

	// A later readiness/mode transition may ask the agent to continue. The
	// durable manual-compaction boundary must still suppress a provider call.
	a.ContinueGoal()
	if err := a.WaitGoal(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * automaticTurnDelay)
	got, _ := c.Get()
	if p.call != 0 || got.GoalID != g.GoalID || got.Status != protocol.GoalActive || a.IsRunning() {
		t.Fatalf("provider calls=%d goal=%+v running=%v", p.call, got, a.IsRunning())
	}
}

func TestAbortCompactionPersistsActiveGoalDeferral(t *testing.T) {
	a, c, _ := goalAgent(t, &scriptedProvider{})
	_, _ = c.Create("defer through compact abort", nil, false)
	done := make(chan struct{})
	close(done)
	a.mu.Lock()
	a.running = true
	a.turnOrigin = "compact"
	a.activeDone = done
	a.mu.Unlock()
	if err := a.StopGoal(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	deferred, err := c.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	a.mu.Lock()
	a.running = false
	a.activeDone = nil
	a.mu.Unlock()
}
