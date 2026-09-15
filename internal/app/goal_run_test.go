package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func managedGoalRunApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.Context(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", ManagedExplicitGoals: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "goal-run.db"), a.CWD(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetSession(st); err != nil {
		t.Fatal(err)
	}
	return a
}
func goalRunCreateParams(a *App) protocol.RPCGoalRunParams {
	return protocol.RPCGoalRunParams{Action: "create", SessionID: a.Session.ID(), BranchID: a.Session.(session.ActiveBranchStore).ActiveBranchID(), ExpectedTipID: a.Session.BranchTip(), Objective: "reviewed objective", TokenBudget: new(int64(1000))}
}

func TestGoalRunAppBoundAdmissionAndInspection(t *testing.T) {
	a := managedGoalRunApp(t)
	p := goalRunCreateParams(a)
	for name, change := range map[string]func(*protocol.RPCGoalRunParams){
		"session":          func(p *protocol.RPCGoalRunParams) { p.SessionID = "wrong" },
		"branch":           func(p *protocol.RPCGoalRunParams) { p.BranchID = "wrong" },
		"tip":              func(p *protocol.RPCGoalRunParams) { p.ExpectedTipID = "wrong" },
		"expected absence": func(p *protocol.RPCGoalRunParams) { p.ExpectedGoalID = "not-absent" },
		"budget":           func(p *protocol.RPCGoalRunParams) { p.TokenBudget = new(int64(0)) },
		"objective":        func(p *protocol.RPCGoalRunParams) { p.Objective = " " },
	} {
		t.Run(name, func(t *testing.T) {
			bad := p
			change(&bad)
			if _, err := a.StartGoalRun(t.Context(), bad); err == nil {
				t.Fatal("unsafe admission")
			}
			g, _ := a.Goal.Get()
			if g != nil {
				t.Fatal("rejected request mutated goal")
			}
		})
	}
	h, err := a.StartGoalRun(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := a.InspectGoal(t.Context(), protocol.RPCGoalInspectParams{SessionID: p.SessionID, BranchID: p.BranchID})
	if err != nil {
		t.Fatal(err)
	}
	if inspected.GoalRunID != h.ID() || inspected.Goal.GoalID != h.GoalID() || !inspected.Deferred || inspected.BudgetRemaining == nil || *inspected.BudgetRemaining != 1000 {
		t.Fatalf("inspection=%+v", inspected)
	}
	if _, err := a.StartGoalRun(t.Context(), p); err == nil {
		t.Fatal("parallel handle admitted")
	}
	h.Cancel()
	if err := h.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p.Action, p.ExpectedGoalID, p.Objective, p.TokenBudget = "resume", h.GoalID(), "", nil
	resumed, err := a.StartGoalRun(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID() == h.ID() || resumed.GoalID() != h.GoalID() {
		t.Fatal("resume replaced semantic goal or reused operation identity")
	}
	resumed.Cancel()
	_ = resumed.Wait(context.Background())
}

func TestGoalRunAppNoReplacementAndNoTerminalResume(t *testing.T) {
	a := managedGoalRunApp(t)
	p := goalRunCreateParams(a)
	h, err := a.StartGoalRun(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	h.Cancel()
	_ = h.Wait(context.Background())
	p.ExpectedGoalID = h.GoalID()
	if _, err := a.StartGoalRun(t.Context(), p); err == nil {
		t.Fatal("unfinished goal replaced")
	}
	if _, err := a.Goal.SetStatus(h.GoalID(), protocol.GoalComplete, false); err != nil {
		t.Fatal(err)
	}
	p.Action, p.Objective, p.TokenBudget = "resume", "", nil
	if _, err := a.StartGoalRun(t.Context(), p); err == nil {
		t.Fatal("completed goal resumed")
	}
}

func TestGoalRunAppUnlimitedBudgetCreateAndResume(t *testing.T) {
	a := managedGoalRunApp(t)
	p := goalRunCreateParams(a)
	p.TokenBudget = nil
	h, err := a.StartGoalRun(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	h.Cancel()
	_ = h.Wait(context.Background())
	inspection, err := a.InspectGoal(t.Context(), protocol.RPCGoalInspectParams{SessionID: p.SessionID, BranchID: p.BranchID})
	if err != nil || inspection.BudgetRemaining != nil || inspection.Goal.TokenBudget != nil {
		t.Fatalf("unlimited inspection=%+v err=%v", inspection, err)
	}
	p.Action, p.ExpectedGoalID, p.Objective = "resume", h.GoalID(), ""
	h, err = a.StartGoalRun(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	h.Cancel()
	_ = h.Wait(context.Background())
}

func TestGoalRunInspectDiscoversCurrentBranchWithoutMutation(t *testing.T) {
	a := managedGoalRunApp(t)
	before := a.Session.BranchTip()
	result, err := a.InspectGoal(t.Context(), protocol.RPCGoalInspectParams{SessionID: a.Session.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if result.BranchID != a.Session.(session.ActiveBranchStore).ActiveBranchID() || result.Goal != nil || a.Session.BranchTip() != before {
		t.Fatalf("inspection=%+v", result)
	}
	if _, err := a.InspectGoal(t.Context(), protocol.RPCGoalInspectParams{SessionID: "wrong"}); err == nil {
		t.Fatal("discovery ignored session identity")
	}
	p := goalRunCreateParams(a)
	p.BranchID = ""
	if _, err := a.StartGoalRun(t.Context(), p); !errors.Is(err, ErrGoalRunRejected) {
		t.Fatalf("mutation accepted discovery binding: %v", err)
	}
}
