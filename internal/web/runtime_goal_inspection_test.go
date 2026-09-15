package web

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"
)

func goalInspectionActiveRuntime(t *testing.T, mode string) (*RuntimeManager, Project, string, RuntimeSnapshot) {
	t.Helper()
	m, p, log, before := goalTestManager(t, mode)
	if _, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, goalResumeParams(before)); err != nil {
		t.Fatal(err)
	}
	// Wait for all initial goal events, including goal_updated before this text,
	// so no unrelated startup revision can obscure a mismatch rejection.
	running := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Status == "running" && len(s.Messages) == 1 && s.Goal != nil && s.Goal.Running
	})
	return m, p, log, running
}

func TestGoalInspectionDuringRunPreservesExactBindingAndStop(t *testing.T) {
	m, p, log, before := goalInspectionActiveRuntime(t, "goal-inspect-active")
	after, err := m.InspectGoal(t.Context(), p.ID, before.InstanceID, before.SessionID, before.Goal.BranchID)
	if err != nil {
		t.Fatal(err)
	}
	g := after.Goal
	if g == nil || g.SessionID != before.SessionID || g.BranchID != before.Goal.BranchID || g.TipID != "live-tip" || g.GoalID != before.Goal.GoalID || g.GoalRunID != before.Goal.GoalRunID || !g.Running || g.Deferred {
		t.Fatalf("active inspection changed authoritative binding: %+v", g)
	}
	if after.InstanceID != before.InstanceID || after.Status != "running" || after.CancelToken != before.CancelToken || after.Revision <= before.Revision || after.GoalRunACK != nil {
		t.Fatalf("read changed root ownership: %+v", after)
	}
	beforeLog := policyLog(t, log)
	if _, err := m.InspectGoal(t.Context(), p.ID, before.InstanceID, "foreign-session", g.BranchID); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("foreign caller session: %v", err)
	}
	if policyLog(t, log) != beforeLog {
		t.Fatal("foreign caller session dispatched worker read")
	}
	if strings.Count(beforeLog, "goal_run\n") != 1 || strings.Count(beforeLog, "abort\n") != 1 {
		t.Fatal("active inspection executed or aborted work")
	}
}

func TestGoalInspectionRejectsMismatchedActiveWorkerBinding(t *testing.T) {
	for _, binding := range []string{"run", "branch", "session", "goal"} {
		t.Run(binding, func(t *testing.T) {
			m, p, log, before := goalInspectionActiveRuntime(t, "goal-inspect-wrong-"+binding)
			beforeGoal, err := json.Marshal(before.Goal)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.InspectGoal(t.Context(), p.ID, before.InstanceID, before.SessionID, before.Goal.BranchID)
			if err == nil {
				t.Fatal("mismatched active binding accepted")
			}
			after, _ := m.Snapshot(p.ID)
			afterGoal, e := json.Marshal(after.Goal)
			if e != nil {
				t.Fatal(e)
			}
			if string(afterGoal) != string(beforeGoal) {
				t.Fatalf("mismatch overwrote goal: before=%s after=%s", beforeGoal, afterGoal)
			}
			if strings.Count(policyLog(t, log), "goal_run\n") != 1 {
				t.Fatal("rejected inspection replayed goal")
			}
		})
	}
}

func TestGoalInspectionSlowReadCannotOverwriteNewerEventOrCompletion(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "update"
		if completed {
			name = "completion"
		}
		t.Run(name, func(t *testing.T) {
			m, p, log, before := goalInspectionActiveRuntime(t, "goal-inspect-delayed-"+name)
			type result struct {
				snapshot RuntimeSnapshot
				err      error
			}
			reply := make(chan result, 1)
			go func() {
				s, e := m.InspectGoal(t.Context(), p.ID, before.InstanceID, before.SessionID, before.Goal.BranchID)
				reply <- result{s, e}
			}()
			newer := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
				return s.Goal != nil && s.Goal.TokensUsed == 80 && (completed && s.Status == "idle" || !completed && s.Status == "running")
			})
			if newer.Goal.Objective != "newer authoritative goal" || newer.Goal.TipID == "stale-read-tip" {
				t.Fatalf("newer fixture event not projected: %+v", newer.Goal)
			}
			select {
			case got := <-reply:
				t.Fatalf("read returned before deterministic release: %+v", got)
			default:
			}
			cancelFixtureRelease(t, log, ".inspect")
			var got result
			select {
			case got = <-reply:
			case <-time.After(5 * time.Second):
				t.Fatal("delayed inspection did not return")
			}
			if got.err != nil {
				t.Fatal(got.err)
			}
			wantGoal, err := json.Marshal(newer.Goal)
			if err != nil {
				t.Fatal(err)
			}
			gotGoal, err := json.Marshal(got.snapshot.Goal)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotGoal) != string(wantGoal) || got.snapshot.Status != newer.Status || got.snapshot.Revision != newer.Revision || got.snapshot.CancelToken != newer.CancelToken {
				t.Fatalf("stale inspection overwrote newer state: want=%s got=%s status=%s revision=%d", wantGoal, gotGoal, got.snapshot.Status, got.snapshot.Revision)
			}
			if completed && (got.snapshot.Goal.Running || got.snapshot.Goal.GoalRunID != "" || got.snapshot.Goal.Status != "complete" || got.snapshot.CancelToken != "") {
				t.Fatal("old read revived completed goal authority")
			}
			if !completed && (!got.snapshot.Goal.Running || got.snapshot.Goal.GoalRunID != before.Goal.GoalRunID || got.snapshot.CancelToken != before.CancelToken) {
				t.Fatal("old read replaced active run authority")
			}
			if strings.Count(policyLog(t, log), "goal_run\n") != 1 {
				t.Fatal("read conflict retried goal execution")
			}
		})
	}
}
