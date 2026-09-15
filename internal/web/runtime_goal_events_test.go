package web

import (
	"encoding/json/v2"
	"strings"
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func goalProjectionRuntime(t *testing.T) *liveRuntime {
	t.Helper()
	return &liveRuntime{ctx: t.Context(), cancel: func() {}, instanceID: "instance", busy: true, assistant: -1, plan: -1, goal: runtimeGoalState{pending: true, sessionID: "session", branchID: "main"}, snapshot: RuntimeSnapshot{ProjectID: "project", InstanceID: "instance", SessionID: "session", Status: "running", CancelToken: "whole-run-stop", Recovery: RecoveryHint{State: RecoveryAdmissionUnknown}, Goal: &RuntimeGoal{SessionID: "session", BranchID: "main", Status: "none"}}}
}
func goalProjectionEvent(kind protocol.AgentEventType, run, turn string, seq uint64, text string) clientrpc.Event {
	return clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: kind, GoalRunID: run, RootEpoch: 1, TurnID: turn, TurnSequence: seq, Text: text}}
}
func goalProjectionACK(t *testing.T, r *liveRuntime) RuntimeSnapshot {
	t.Helper()
	s, err := r.goalAcknowledged(protocol.RPCResponse{ID: "42", Success: true, Data: protocol.RPCGoalRunAccepted{GoalRunID: "run", GoalID: "goal", SessionID: "session", BranchID: "main"}}, protocol.RPCGoalRunParams{Action: "create", SessionID: "session", BranchID: "main", Objective: "review this work", TokenBudget: new(int64(1000))})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func goalProjectionCompletion(request, run string) clientrpc.Event {
	return clientrpc.Event{GoalRunCompleted: &protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: request, GoalRunID: run, GoalID: "goal", Status: "finished", GoalStatus: protocol.GoalComplete, Error: "SECRET-ERROR"}}
}
func goalBuffer(t *testing.T, r *liveRuntime, event clientrpc.Event) {
	t.Helper()
	r.eventMu.Lock()
	r.mu.Lock()
	consumed, valid := r.bufferGoalEventLocked(event)
	r.mu.Unlock()
	r.eventMu.Unlock()
	if !consumed || !valid {
		t.Fatalf("buffer: %v %v", consumed, valid)
	}
}

func TestGoalACKRaceProjectsOnlyCorrelatedRootRun(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "ACK-first", true: "events-first"}[early], func(t *testing.T) {
			r := goalProjectionRuntime(t)
			events := []clientrpc.Event{goalProjectionEvent(protocol.EvTextDelta, "foreign", "foreign", 99, "FOREIGN"), goalProjectionEvent(protocol.EvTextDelta, "run", "turn-a", 1, "first"), goalProjectionEvent(protocol.EvTurnDone, "run", "turn-a", 1, ""), goalProjectionEvent(protocol.EvTextDelta, "run", "turn-b", 2, "second")}
			if early {
				for _, e := range events {
					goalBuffer(t, r, e)
				}
				if len(r.snapshot.Messages) != 0 {
					t.Fatal("pre-ACK output projected")
				}
				goalProjectionACK(t, r)
			} else {
				goalProjectionACK(t, r)
				for _, e := range events {
					r.consumeEvent(e)
				}
			}
			if len(r.snapshot.Messages) != 2 || r.snapshot.Messages[0].Text != "first" || r.snapshot.Messages[1].Text != "second" {
				t.Fatalf("run turn owners: %+v", r.snapshot.Messages)
			}
			if !r.busy || r.snapshot.CancelToken != "whole-run-stop" || r.snapshot.Status != "running" {
				t.Fatal("individual turn retired root Stop")
			}
			r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
			if !r.busy {
				t.Fatal("prompt completion retired goal")
			}
			r.consumeEvent(goalProjectionCompletion("wrong", "run"))
			r.consumeEvent(goalProjectionCompletion("42", "foreign"))
			if !r.busy {
				t.Fatal("foreign completion retired goal")
			}
			r.consumeEvent(goalProjectionCompletion("42", "run"))
			if r.busy || r.snapshot.Status != "idle" || r.snapshot.CancelToken != "" || r.snapshot.Goal.Running {
				t.Fatal("correlated terminal did not release goal")
			}
		})
	}
}

func TestGoalCompletedBeforeACKAndCancelRetirement(t *testing.T) {
	r := goalProjectionRuntime(t)
	r.snapshot.CancelRequested = true
	r.cancelTaskToken = r.snapshot.CancelToken
	goalBuffer(t, r, goalProjectionEvent(protocol.EvTextDelta, "run", "turn-a", 1, "saved answer"))
	goalBuffer(t, r, goalProjectionCompletion("42", "run"))
	s := goalProjectionACK(t, r)
	if !r.busy || s.Status != "running" || !s.CancelRequested || !s.Goal.Running {
		t.Fatal("completion advertised readiness before cancel retirement")
	}
	r.retireTurnCancel("instance", "session", "whole-run-stop")
	if r.busy || r.snapshot.Status != "idle" || r.snapshot.CancelRequested || r.snapshot.CancelToken != "" {
		t.Fatal("cancel retirement did not release completed goal")
	}
	before := r.snapshot.Revision
	r.consumeEvent(goalProjectionCompletion("42", "run"))
	if r.snapshot.Revision != before {
		t.Fatal("duplicate terminal revised completed goal")
	}
}

func TestGoalTurnGapsRootIdentityAndEpoch(t *testing.T) {
	r := goalProjectionRuntime(t)
	goalProjectionACK(t, r)
	r.consumeEvent(goalProjectionEvent(protocol.EvTextDelta, "run", "turn-a", 1, "first"))
	r.consumeEvent(goalProjectionEvent(protocol.EvTurnDone, "run", "turn-a", 1, ""))
	if !r.turnCancelableLocked("whole-run-stop") {
		t.Fatal("turn gap has no Stop")
	}
	child := goalProjectionEvent(protocol.EvTextDelta, "run", "turn-c", 3, "CHILD")
	child.AgentEvent.Agent = &protocol.AgentRef{ThreadID: "child", Path: "/root/child", ParentPath: "/root", Depth: 1}
	r.consumeEvent(child)
	stale := goalProjectionEvent(protocol.EvTextDelta, "run", "turn-z", 99, "STALE")
	stale.AgentEvent.RootEpoch = 0
	r.consumeEvent(stale)
	retired := goalProjectionEvent(protocol.EvTextDelta, "run", "turn-z", 99, "RETIRED")
	r.retiredEpoch = 1
	r.consumeEvent(retired)
	r.retiredEpoch = 0
	r.consumeEvent(goalProjectionEvent(protocol.EvTextDelta, "run", "turn-b", 2, "second"))
	r.consumeEvent(goalProjectionEvent(protocol.EvTextDelta, "run", "turn-a", 1, "OLD"))
	if len(r.snapshot.Messages) != 2 || r.snapshot.Messages[1].Text != "second" {
		t.Fatalf("foreign/stale event projected: %+v", r.snapshot.Messages)
	}
}

func TestGoalPublicBufferPrivacyBounds(t *testing.T) {
	r := goalProjectionRuntime(t)
	e := goalProjectionEvent(protocol.EvToolStart, "run", "turn", 1, "")
	e.AgentEvent.ToolName = "read"
	e.AgentEvent.ToolCallID = "call"
	e.AgentEvent.Message = "SECRET-PRIVATE"
	goalBuffer(t, r, e)
	goalBuffer(t, r, goalProjectionCompletion("42", "run"))
	data, err := json.Marshal(r.goal.events)
	if err != nil || strings.Contains(string(data), "SECRET") {
		t.Fatalf("private fields retained: %s %v", data, err)
	}
	r.goal.events = nil
	r.goal.bytes = 0
	huge := goalProjectionEvent(protocol.EvTextDelta, "run", "turn", 1, strings.Repeat("x", runtimeMessageBytes+1))
	if _, valid := r.bufferGoalEventLocked(huge); valid {
		t.Fatal("oversized text admitted")
	}
	for range goalEventCount {
		goalBuffer(t, r, goalProjectionEvent(protocol.EvTextDelta, "run", "turn", 1, "x"))
	}
	if _, valid := r.bufferGoalEventLocked(e); valid {
		t.Fatal("event count unbounded")
	}
	r.goal.events = nil
	r.goal.bytes = goalEventBytes
	if _, valid := r.bufferGoalEventLocked(e); valid {
		t.Fatal("event byte bound absent")
	}
}

func TestGoalSnapshotProjectionOwnsBudgetAndCosts(t *testing.T) {
	g := &protocol.ThreadGoal{SessionID: "session", BranchID: "main", GoalID: "goal", Objective: "objective", Status: protocol.GoalActive, TokenBudget: new(int64(100)), TokensUsed: 20, EstimatedCosts: []protocol.Cost{{Currency: "USD", Total: 0.5}}}
	projected, valid := projectRuntimeGoal(protocol.RPCGoalInspection{SessionID: "session", BranchID: "main", Goal: g, Deferred: true})
	if !valid || !projected.Deferred || *projected.BudgetRemaining != 80 {
		t.Fatalf("goal projection: %+v", projected)
	}
	s := RuntimeSnapshot{Goal: projected}
	clone := s.clone()
	*clone.Goal.TokenBudget = 200
	clone.Goal.EstimatedCosts[0].Total = 10
	if *s.Goal.TokenBudget != 100 || s.Goal.EstimatedCosts[0].Total != 0.5 {
		t.Fatal("snapshot aliases budget/cost")
	}
	r := goalProjectionRuntime(t)
	r.goal = runtimeGoalState{}
	r.snapshot.Goal = projected
	if !r.goalBlocksHistoryLocked() {
		t.Fatal("nonterminal goal allows history mutation")
	}
	r.snapshot.Goal.Status = "complete"
	if r.goalBlocksHistoryLocked() {
		t.Fatal("terminal goal blocks history")
	}
	r.goal.active = true
	if !r.goalBlocksHistoryLocked() || r.queueActiveLocked() {
		t.Fatal("active goal grants history or queue")
	}
}

func TestGoalUsageAdvancesAcrossNativeTurns(t *testing.T) {
	r := goalProjectionRuntime(t)
	r.snapshot.Telemetry = &RuntimeTelemetry{Available: true, TotalTokens: 100}
	r.usageBase = *r.snapshot.Telemetry
	goalProjectionACK(t, r)
	for i, turn := range []string{"turn-one", "turn-two"} {
		r.consumeEvent(goalProjectionEvent(protocol.EvTextDelta, "run", turn, uint64(i+1), "answer"))
		done := goalProjectionEvent(protocol.EvTurnDone, "run", turn, uint64(i+1), "")
		done.AgentEvent.Usage = &protocol.Usage{Input: 10, Output: 5, Total: 15}
		r.consumeEvent(done)
	}
	if r.snapshot.Telemetry.TotalTokens != 130 {
		t.Fatalf("turn usage replaced earlier run usage: %+v", r.snapshot.Telemetry)
	}
}

func TestGoalUpdatesAreExactPublicProjections(t *testing.T) {
	r := goalProjectionRuntime(t)
	goalProjectionACK(t, r)
	g := &protocol.ThreadGoal{SessionID: "session", BranchID: "main", GoalID: "goal", Objective: "reviewed objective", Status: protocol.GoalActive, TokenBudget: new(int64(100)), TokensUsed: 25, EstimatedCosts: []protocol.Cost{{Currency: "USD", Total: 0.25}}}
	event := goalProjectionEvent(protocol.EvThreadGoalUpdated, "run", "", 0, "")
	event.AgentEvent.ThreadGoal = &protocol.ThreadGoalUpdate{Goal: g}
	event.AgentEvent.Message = "SECRET-PROVIDER"
	r.consumeEvent(event)
	if r.snapshot.Goal.Objective != "reviewed objective" || r.snapshot.Goal.TokensUsed != 25 || *r.snapshot.Goal.BudgetRemaining != 75 || !r.snapshot.Goal.Running {
		t.Fatalf("goal usage projection: %+v", r.snapshot.Goal)
	}
	before := r.snapshot.Revision
	foreign := event.AgentEvent.Clone()
	foreign.ThreadGoal.Goal.SessionID = "foreign"
	r.consumeEvent(clientrpc.Event{AgentEvent: &foreign})
	if r.snapshot.Revision != before {
		t.Fatal("foreign session goal repainted current panel")
	}
	data, _ := json.Marshal(r.snapshot)
	if strings.Contains(string(data), "SECRET") {
		t.Fatal("raw event message entered goal snapshot")
	}
	r.consumeEvent(goalProjectionCompletion("42", "run"))
	// Read-only lifecycle notifications can refresh the reviewed branch after
	// execution ends; they cannot change ownership or restart its worker.
	idle := event.AgentEvent.Clone()
	idle.GoalRunID = ""
	idle.ThreadGoal.Goal.Status = protocol.GoalPaused
	r.consumeEvent(clientrpc.Event{AgentEvent: &idle})
	if r.snapshot.Goal.Status != "paused" || r.busy || r.goal.active || r.snapshot.CancelToken != "" {
		t.Fatal("read-only goal notification resumed work")
	}
}

func TestGoalRunACKCloneOwnsReceipt(t *testing.T) {
	original := RuntimeSnapshot{GoalRunACK: &RuntimeGoalRunACK{SessionID: "session", BranchID: "main", GoalID: "goal", GoalRunID: "run"}}
	copied := original.clone()
	copied.GoalRunACK.GoalRunID = "foreign"
	if original.GoalRunACK.GoalRunID != "run" {
		t.Fatal("snapshot clone aliases ACK receipt")
	}
}
