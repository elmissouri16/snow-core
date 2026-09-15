package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestGoalRunLegacyAppMutationsRejectBeforeAndDuringOwner(t *testing.T) {
	a := managedGoalRunApp(t)
	if _, err := a.CreateGoal("invisible goal", nil, false); err == nil {
		t.Fatal("managed idle legacy create accepted")
	}
	initial, err := a.GoalState()
	if err != nil || initial != nil {
		t.Fatalf("legacy create mutated: %+v %v", initial, err)
	}
	p := goalRunCreateParams(a)
	h, err := a.StartGoalRun(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	// Also exercise the between-native-turn state: same owner, nondeferred goal,
	// no current provider call. Rejected controls must not turn this into Stop.
	if err := a.Goal.Defer(false); err != nil {
		t.Fatal(err)
	}
	before, _ := a.GoalState()
	tip := a.Session.BranchTip()
	controls := map[string]func() error{
		"create":         func() error { _, err := a.CreateGoal("replace", nil, true); return err },
		"edit":           func() error { _, err := a.EditGoal("changed"); return err },
		"pause":          func() error { _, err := a.PauseGoal(); return err },
		"resume":         func() error { _, err := a.ResumeGoal(); return err },
		"clear":          a.ClearGoal,
		"continue":       a.ContinueGoal,
		"set_session":    func() error { return a.SetSession(a.Session) },
		"select_branch":  func() error { return a.SelectBranch(p.BranchID) },
		"fork_branch":    func() error { _, err := a.ForkBranch(tip); return err },
		"rename_branch":  func() error { _, err := a.RenameBranch(p.BranchID, "changed"); return err },
		"delete_branch":  func() error { return a.DeleteBranch(p.BranchID) },
		"rename_session": func() error { return a.RenameSession("changed") },
		"fork_session":   func() error { _, err := a.ForkSession(t.Context(), protocol.SessionForkOptions{}); return err },
	}
	for name, control := range controls {
		t.Run(name, func(t *testing.T) {
			if err := control(); err == nil {
				t.Fatal("control accepted explicit owner")
			}
			select {
			case <-h.Done():
				t.Fatal("control preempted explicit owner")
			default:
			}
			after, _ := a.GoalState()
			deferred, _ := a.GoalContinuationDeferred()
			if a.Agent.GoalRunID() != h.ID() || !reflect.DeepEqual(before, after) || deferred || a.Session.BranchTip() != tip || a.Session.ID() != p.SessionID || a.Session.(session.ActiveBranchStore).ActiveBranchID() != p.BranchID {
				t.Fatalf("control changed goal/runtime: goal=%+v deferred=%v", after, deferred)
			}
		})
	}
	if err := a.Agent.AbortContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Abort did not retain cancellation: %v", err)
	}
	deferred, err := a.GoalContinuationDeferred()
	if err != nil || !deferred {
		t.Fatalf("Abort deferral=%v err=%v", deferred, err)
	}
	// Managed mode is immutable process policy: even after completion the legacy
	// controls cannot clear or resume a goal without an explicit reviewed run.
	for name, control := range map[string]func() error{"clear": a.ClearGoal, "continue": a.ContinueGoal, "pause": func() error { _, err := a.PauseGoal(); return err }} {
		if err := control(); err == nil {
			t.Fatalf("managed idle %s accepted", name)
		}
	}
}
