package agent

import (
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSessionReasoningAgentAdmissionNeverPreemptsWork(t *testing.T) {
	for name, setup := range map[string]func(*Agent){
		"running":                func(a *Agent) { a.running = true },
		"automatic continuation": func(a *Agent) { a.autoRunning = true },
		"explicit goal handle":   func(a *Agent) { a.goalRun = &GoalRunHandle{} },
		"pending queue":          func(a *Agent) { a.queueControl.items = []protocol.QueueControlItem{{ID: "queued"}} },
		"review queue":           func(a *Agent) { a.queueControl.review = []protocol.QueueControlItem{{ID: "held"}} },
		"closed":                 func(a *Agent) { a.closed = true },
	} {
		t.Run(name, func(t *testing.T) {
			store := session.NewMemoryStore(session.Options{})
			a := newPlanAgent(t, &scriptedProvider{}, nil, store)
			unlock := a.LockAdmission()
			defer unlock()
			before, err := a.SessionReasoningAdmitted(store.ID())
			if err != nil {
				t.Fatal(err)
			}
			setup(a)
			if _, err := a.SessionReasoningAdmitted(store.ID()); !errors.Is(err, ErrSessionReasoningBusy) {
				t.Fatalf("read changed work: %v", err)
			}
			if _, err := a.SetSessionReasoningAdmitted(protocol.RPCSessionReasoningSetParams{Expected: before.RPCSessionReasoningState, Field: "thinking", Value: "off"}); !errors.Is(err, ErrSessionReasoningBusy) {
				t.Fatalf("mutation preempted work: %v", err)
			}
			if a.Thinking() != before.Thinking {
				t.Fatal("busy rejection changed thinking")
			}
		})
	}
}

func TestSessionReasoningAgentRejectsNonterminalGoalWithoutContinuation(t *testing.T) {
	a, controller, store := goalAgent(t, &scriptedProvider{})
	active, err := controller.Create("hold this goal without continuation", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	unlock := a.LockAdmission()
	defer unlock()
	if _, err := a.SessionReasoningAdmitted(store.ID()); !errors.Is(err, ErrSessionReasoningBusy) {
		t.Fatalf("goal accepted: %v", err)
	}
	after, err := controller.Get()
	if err != nil || after.GoalID != active.GoalID || after.Status != active.Status || a.running || a.autoRunning {
		t.Fatalf("inspection changed goal: %+v %v", after, err)
	}
}
