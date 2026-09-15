package app

import (
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func compactionStartParams(a *App) protocol.RPCCompactionStartParams {
	return protocol.RPCCompactionStartParams{SessionID: a.Session.ID(), BranchID: a.Session.(session.ActiveBranchStore).ActiveBranchID(), ExpectedTipID: a.Session.BranchTip()}
}

func TestCompactionAppExactBindingAndPlanAdmission(t *testing.T) {
	a := managedGoalRunApp(t)
	p := compactionStartParams(a)
	for name, change := range map[string]func(*protocol.RPCCompactionStartParams){
		"session":       func(p *protocol.RPCCompactionStartParams) { p.SessionID = "wrong" },
		"branch":        func(p *protocol.RPCCompactionStartParams) { p.BranchID = "wrong" },
		"tip":           func(p *protocol.RPCCompactionStartParams) { p.ExpectedTipID = "wrong" },
		"empty session": func(p *protocol.RPCCompactionStartParams) { p.SessionID = "" },
		"empty branch":  func(p *protocol.RPCCompactionStartParams) { p.BranchID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := p
			change(&bad)
			if _, err := a.StartCompaction(t.Context(), bad); !errors.Is(err, ErrCompactionRejected) || errors.Is(err, ErrCompactionOutcomeUnknown) {
				t.Fatalf("admission=%v", err)
			}
			if a.Session.BranchTip() != p.ExpectedTipID {
				t.Fatal("rejected request changed history")
			}
		})
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	p = compactionStartParams(a)
	h, err := a.StartCompaction(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	accepted := h.Accepted()
	if accepted.SessionID != p.SessionID || accepted.BranchID != p.BranchID || accepted.CompactionID == "" || accepted.TurnOrigin != "compact" {
		t.Fatalf("accepted=%+v", accepted)
	}
	if _, err := a.StartCompaction(t.Context(), p); !errors.Is(err, ErrCompactionRejected) {
		t.Fatalf("parallel=%v", err)
	}
	h.Release()
	if err := h.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	completion, ready := h.Completion()
	if !ready || completion.Status != "noop" || completion.CompactionID != accepted.CompactionID || a.Agent.Mode() != protocol.ModePlan {
		t.Fatalf("completion=%+v", completion)
	}
}

func TestCompactionAppRejectsUnfinishedGoalWithoutDeferring(t *testing.T) {
	a := managedGoalRunApp(t)
	goal, err := a.Goal.Create("unfinished objective", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []protocol.ThreadGoalStatus{protocol.GoalActive, protocol.GoalPaused, protocol.GoalBlocked} {
		if status == protocol.GoalBlocked {
			if _, err := a.Goal.SetStatus(goal.GoalID, protocol.GoalActive, false); err != nil {
				t.Fatal(err)
			}
		}
		if status != protocol.GoalActive {
			if _, err := a.Goal.SetStatus(goal.GoalID, status, false); err != nil {
				t.Fatal(err)
			}
		}
		before, err := a.Goal.Deferred()
		if err != nil {
			t.Fatal(err)
		}
		p := compactionStartParams(a)
		if _, err := a.StartCompaction(t.Context(), p); !errors.Is(err, ErrCompactionRejected) {
			t.Fatalf("status=%s admission=%v", status, err)
		}
		after, err := a.Goal.Deferred()
		if err != nil || after != before || a.Session.BranchTip() != p.ExpectedTipID {
			t.Fatalf("rejection mutated goal: deferred=%v err=%v", after, err)
		}
	}
}

func TestCompactionAppRejectsOwnedGoalWithoutStop(t *testing.T) {
	a := managedGoalRunApp(t)
	goal, err := a.StartGoalRun(t.Context(), goalRunCreateParams(a))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { goal.Cancel(); _ = goal.Wait(t.Context()) }()
	if _, err := a.StartCompaction(t.Context(), compactionStartParams(a)); !errors.Is(err, ErrCompactionRejected) {
		t.Fatalf("admission=%v", err)
	}
	select {
	case <-goal.Done():
		t.Fatal("compaction preempted automatic owner")
	default:
	}
}
