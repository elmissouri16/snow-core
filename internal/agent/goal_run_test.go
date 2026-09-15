package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func admittedTestGoalRun(t *testing.T, a *Agent, id string) *GoalRunHandle {
	t.Helper()
	if err := a.opts.Goal.Defer(true); err != nil {
		t.Fatal(err)
	}
	unlock := a.LockAdmission()
	defer unlock()
	h, err := a.StartGoalRunAdmitted(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestGoalRunGateAndStableMultipleTurnHandle(t *testing.T) {
	p := &scriptedProvider{}
	a, c, st := goalAgent(t, p)
	a.opts.ManagedExplicitGoals = true
	g, err := c.Create("owned objective", new(int64(10000)), false)
	if err != nil {
		t.Fatal(err)
	}
	p.scripts = [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: "first continuation"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "second continuation"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "complete", ToolName: "update_goal", Arguments: []byte(fmt.Sprintf(`{"goal_id":%q,"status":"complete"}`, g.GoalID))}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	var mu sync.Mutex
	var events []protocol.AgentEvent
	a.Subscribe(func(e protocol.AgentEvent) { mu.Lock(); events = append(events, e); mu.Unlock() })
	h := admittedTestGoalRun(t, a, g.GoalID)
	done := h.Done()
	a.ContinueGoal() // Legacy calls cannot replace this handle or queue a worker.
	select {
	case <-done:
		t.Fatal("completed before release")
	case <-time.After(20 * time.Millisecond):
	}
	if len(p.requests) != 0 {
		t.Fatal("provider ran before ACK gate")
	}
	if err := a.Prompt(t.Context(), "not owned"); !errors.Is(err, ErrPromptRejected) {
		t.Fatalf("prompt preempted run: %v", err)
	}
	if err := a.GoalRunReadyAdmitted(); err == nil {
		t.Fatal("concurrent run admitted")
	}
	h.Release()
	h.Release()
	if err := h.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(t.Context()); err != nil {
		t.Fatal("result was consumed", err)
	}
	if done != h.Done() || h.GoalStatus() != protocol.GoalComplete {
		t.Fatal("unstable handle")
	}
	if len(p.requests) != 4 {
		t.Fatalf("requests=%d", len(p.requests))
	}
	_ = a.DrainEvents(t.Context())
	mu.Lock()
	defer mu.Unlock()
	turns := map[string]bool{}
	for _, event := range events {
		if event.Type == protocol.EvTurnDone {
			if event.GoalRunID != h.ID() {
				t.Fatalf("uncorrelated native turn: %+v", event)
			}
			turns[event.TurnID] = true
		}
	}
	if len(turns) != 3 {
		t.Fatalf("native turns=%v", turns)
	}
	messages, _ := st.Messages()
	for _, message := range messages {
		if message.Role == protocol.RoleUser {
			t.Fatal("invented user prompt")
		}
	}
}

func TestGoalRunCancelBeforeReleaseAndAbortPersistDeferral(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprint(abort), func(t *testing.T) {
			p := &scriptedProvider{}
			a, c, _ := goalAgent(t, p)
			a.opts.ManagedExplicitGoals = true
			g, err := c.Create("cancel", new(int64(100)), false)
			if err != nil {
				t.Fatal(err)
			}
			h := admittedTestGoalRun(t, a, g.GoalID)
			if abort {
				if err := a.AbortContext(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else {
				h.Cancel()
			}
			if err := h.Wait(t.Context()); !errors.Is(err, context.Canceled) {
				t.Fatalf("result=%v", err)
			}
			h.Release()
			if len(p.requests) != 0 {
				t.Fatal("canceled pre-ACK request executed")
			}
			deferred, err := c.Deferred()
			if err != nil || !deferred {
				t.Fatalf("deferred=%v err=%v", deferred, err)
			}
			if a.GoalRunID() != "" {
				t.Fatal("owner leaked")
			}
		})
	}
}

func TestGoalRunAdmissionPolicyPlanAndQueue(t *testing.T) {
	p := &scriptedProvider{}
	a, _, _ := goalAgent(t, p)
	if err := a.GoalRunReadyAdmitted(); err == nil {
		t.Fatal("opt-in policy not required")
	}
	a.opts.ManagedExplicitGoals = true
	for name, change := range map[string]func(){
		"plan":       func() { a.mode = protocol.ModePlan },
		"running":    func() { a.running = true },
		"pending":    func() { a.queuedInputs = []protocol.QueuedInput{{Text: "pending"}} },
		"review":     func() { a.queueControl.review = []protocol.QueueControlItem{{ID: "review"}} },
		"delivering": func() { a.queueControl.items = []protocol.QueueControlItem{{State: "delivering"}} },
	} {
		t.Run(name, func(t *testing.T) {
			change()
			if err := a.GoalRunReadyAdmitted(); err == nil {
				t.Fatal("unsafe admission accepted")
			}
			a.mode = protocol.ModeDefault
			a.running = false
			a.queuedInputs = nil
			a.queueControl = queueControlState{}
		})
	}
	if err := a.GoalRunReadyAdmitted(); err != nil {
		t.Fatal(err)
	}
	a.ContinueGoal()
	if len(p.requests) != 0 || a.autoRunning {
		t.Fatal("legacy continuation started under managed policy")
	}
}

func TestGoalRunOrdinaryPromptCannotCreateInvisibleGoal(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "create", ToolName: "create_goal", Arguments: []byte(`{"objective":"invisible","token_budget":100}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, c, st := goalAgent(t, p)
	a.opts.ManagedExplicitGoals = true
	if err := a.Prompt(t.Context(), "ordinary work"); err != nil {
		t.Fatal(err)
	}
	goal, err := c.Get()
	if err != nil || goal != nil {
		t.Fatalf("invisible goal=%+v err=%v", goal, err)
	}
	for _, request := range p.requests {
		for _, tool := range request.Tools {
			if tool.Name == "create_goal" || tool.Name == "update_goal" || tool.Name == "get_goal" {
				t.Fatalf("ordinary request exposed %s", tool.Name)
			}
		}
	}
	messages, _ := st.Messages()
	blocked := false
	for _, message := range messages {
		if message.ToolName == "create_goal" {
			blocked = message.IsError
		}
	}
	if !blocked {
		t.Fatal("guessed tool dispatch was not denied")
	}
}

func TestGoalRunToolOwnership(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	a.opts.ManagedExplicitGoals = true
	g, err := c.Create("owned", new(int64(100)), false)
	if err != nil {
		t.Fatal(err)
	}
	h := admittedTestGoalRun(t, a, g.GoalID)
	defer func() { h.Cancel(); _ = h.Wait(context.Background()) }()
	a.mu.Lock()
	a.turnOrigin = "goal"
	a.mu.Unlock()
	if a.managedGoalToolAllowed("create_goal") {
		t.Fatal("owned goal can be replaced")
	}
	if err := a.checkManagedGoalToolCall("update_goal", []byte(`{"goal_id":"wrong","status":"complete"}`)); err == nil {
		t.Fatal("wrong goal updated")
	}
	if err := a.checkManagedGoalToolCall("update_goal", []byte(fmt.Sprintf(`{"goal_id":%q,"status":"complete"}`, g.GoalID))); err != nil {
		t.Fatal(err)
	}
}

func TestGoalRunKeepsRealErrorAndBudgetOwnership(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		failure := errors.New("native provider failure")
		p := &scriptedProvider{}
		a, c, _ := goalAgent(t, p)
		a.opts.ManagedExplicitGoals = true
		g, err := c.Create("error", new(int64(100)), false)
		if err != nil {
			t.Fatal(err)
		}
		p.resolveErr = failure
		h := admittedTestGoalRun(t, a, g.GoalID)
		h.Release()
		for range 2 {
			if err := h.Wait(t.Context()); !errors.Is(err, failure) {
				t.Fatalf("worker error consumed: %v", err)
			}
		}
		if h.GoalStatus() != protocol.GoalBlocked {
			t.Fatalf("status=%s", h.GoalStatus())
		}
	})
	t.Run("budget", func(t *testing.T) {
		p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
			{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 10, Output: 10, Total: 20}}, {Type: protocol.EvStreamTextDelta, Text: "productive"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
			{{Type: protocol.EvStreamTextDelta, Text: "budget report"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		}}
		a, c, _ := goalAgent(t, p)
		a.opts.ManagedExplicitGoals = true
		g, err := c.Create("bounded", new(int64(10)), false)
		if err != nil {
			t.Fatal(err)
		}
		h := admittedTestGoalRun(t, a, g.GoalID)
		h.Release()
		if err := h.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		if h.GoalStatus() != protocol.GoalBudgetLimited {
			t.Fatalf("status=%s", h.GoalStatus())
		}
		if len(p.requests) != 2 {
			t.Fatalf("budget report escaped handle: requests=%d", len(p.requests))
		}
		for _, tool := range p.requests[1].Tools {
			t.Fatalf("budget cleanup exposed tool %s", tool.Name)
		}
	})
}
