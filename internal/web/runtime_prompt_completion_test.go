package web

import (
	"errors"
	"strings"
	"testing"
)

func TestPromptCompletionRefreshHoldsReadinessUntilAuthoritativeScope(t *testing.T) {
	for _, status := range []string{"completed", "failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			m, p, log, before := compactionTestManager(t, "prompt-scope-gated-"+status)
			if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "one real admission"); err != nil {
				t.Fatal(err)
			}
			cancelFixtureWaitLog(t, log, "prompt\ngoal_inspect\n")
			held, _ := m.Snapshot(p.ID)
			if held.Status != "running" || held.Goal.TipID != before.Goal.TipID || held.CancelToken == "" {
				t.Fatal("advertised idle scope while authoritative tip was still pending")
			}
			if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "must not run"); !errors.Is(err, ErrRuntimeBusy) {
				t.Fatalf("next prompt during scope refresh: %v", err)
			}
			if _, err := m.StartCompaction(t.Context(), p.ID, before.InstanceID, compactionInput(held)); !errors.Is(err, ErrRuntimeBusy) {
				t.Fatalf("compaction during scope refresh: %v", err)
			}
			cancelFixtureRelease(t, log, ".scope")
			ready := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
			if ready.Goal.TipID != "after-prompt" || ready.Goal.BranchID != before.Goal.BranchID || ready.Goal.Status != "none" || ready.Goal.Running || ready.CancelToken != "" {
				t.Fatalf("idle readiness retained stale scope: %+v", ready.Goal)
			}
			if strings.Count(policyLog(t, log), "prompt\n") != 1 || strings.Contains(policyLog(t, log), "compaction_start\n") {
				t.Fatal("completion refresh replayed or admitted provider work")
			}
		})
	}
}

func TestPromptCompletionMalformedScopeFailsClosed(t *testing.T) {
	m, p, _, before := compactionTestManager(t, "prompt-scope-malformed")
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "admitted once"); err != nil && !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatal(err)
	}
	failed := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
	if failed.Goal.TipID != before.Goal.TipID || failed.Goal.BranchID != before.Goal.BranchID {
		t.Fatal("malformed scope was projected as authoritative")
	}
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "no replacement"); err == nil {
		t.Fatal("failed scope read admitted new work")
	}
}

func TestPromptCompletionLateScopeCannotOverwriteReplacementRoot(t *testing.T) {
	m, p, log, before := compactionTestManager(t, "prompt-scope-gated-completed")
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "original"); err != nil {
		t.Fatal(err)
	}
	cancelFixtureWaitLog(t, log, "prompt\ngoal_inspect\n")
	r, err := m.runtime(p.ID, before.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.rootEpoch++
	r.turnID = "replacement-turn"
	r.snapshot.Goal = r.snapshot.Goal.clone()
	r.snapshot.Goal.TipID = "replacement-tip"
	r.mu.Unlock()
	cancelFixtureRelease(t, log, ".scope")
	// drain owns eventMu throughout refresh: joining that gate proves the old
	// read has returned and attempted projection before examining replacement.
	r.eventMu.Lock()
	r.eventMu.Unlock()
	after, _ := m.Snapshot(p.ID)
	if after.Status != "running" || after.Goal.TipID != "replacement-tip" {
		t.Fatal("late completion overwrote or released a replacement root")
	}
}

func TestPromptCompletionScopeGenerationFence(t *testing.T) {
	for name, change := range map[string]func(*liveRuntime){
		"instance":     func(r *liveRuntime) { r.instanceID = "replacement" },
		"session":      func(r *liveRuntime) { r.snapshot.SessionID = "replacement" },
		"branch":       func(r *liveRuntime) { r.snapshot.Goal.BranchID = "replacement" },
		"epoch":        func(r *liveRuntime) { r.rootEpoch++ },
		"turn":         func(r *liveRuntime) { r.turnID = "replacement" },
		"admission":    func(r *liveRuntime) { r.activityPrompt++ },
		"request":      func(r *liveRuntime) { r.promptID = "replacement" },
		"cancel token": func(r *liveRuntime) { r.snapshot.CancelToken = "replacement" },
		"failure":      func(r *liveRuntime) { r.snapshot.Status = "failed" },
		"closing":      func(r *liveRuntime) { r.snapshot.Status = "closing" },
		"transition":   func(r *liveRuntime) { r.transitioning = true },
		"goal owner":   func(r *liveRuntime) { r.goal.active = true },
		"manual owner": func(r *liveRuntime) { r.compaction.active = true },
	} {
		t.Run(name, func(t *testing.T) {
			r := &liveRuntime{ctx: t.Context(), busy: true, instanceID: "instance", turnID: "turn", rootEpoch: 3, activityPrompt: 5, promptID: "request", snapshot: RuntimeSnapshot{Status: "running", SessionID: "session", CancelToken: "token", Goal: &RuntimeGoal{BranchID: "main"}}}
			b := runtimePromptCompletionBinding{instance: "instance", session: "session", branch: "main", turn: "turn", request: "request", cancelToken: "token", epoch: 3, activity: 5}
			if !r.promptCompletionMatchesLocked(b) {
				t.Fatal("original generation rejected")
			}
			change(r)
			if r.promptCompletionMatchesLocked(b) {
				t.Fatal("stale completion accepted replacement authority")
			}
		})
	}
}
