package web

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func goalTestManager(t *testing.T, mode string) (*RuntimeManager, Project, string, RuntimeSnapshot) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, mode)
	m.env = slices.DeleteFunc(m.env, func(s string) bool { return strings.HasPrefix(s, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_GOAL_TEST_CHILD=1")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err = m.InspectGoal(t.Context(), p.ID, before.InstanceID, before.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	return m, p, log, before
}
func goalResumeParams(s RuntimeSnapshot) RuntimeGoalRunInput {
	return RuntimeGoalRunInput{ExpectedRevision: s.Revision, Action: "resume", SessionID: s.SessionID, BranchID: s.Goal.BranchID, ExpectedTipID: s.Goal.TipID, ExpectedGoalID: s.Goal.GoalID}
}

func TestGoalRuntimeStopDuringACKAndTurnGap(t *testing.T) {
	m, p, log, before := goalTestManager(t, "goal-gated")
	if !before.Goal.Deferred || before.Status != "idle" {
		t.Fatal("saved goal resumed from read")
	}
	caller, disconnect := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { _, err := m.RunGoal(caller, p.ID, before.InstanceID, goalResumeParams(before)); result <- err }()
	running := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "running" })
	cancelFixtureWaitLog(t, log, "goal_run\n")
	if running.CancelToken == "" {
		t.Fatal("preACK Stop authority absent")
	}
	if err := m.CancelTurn(t.Context(), p.ID, before.InstanceID, running.CancelToken); err != nil {
		t.Fatal(err)
	}
	disconnect() // HTTP ACK loss must not cancel/retry admitted worker work.
	if strings.Count(policyLog(t, log), "abort\n") != 1 {
		t.Fatal("cancel bypassed admission gate")
	}
	cancelFixtureRelease(t, log, ".ack")
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	cancelFixtureWaitLog(t, log, "abort\n")
	waiting := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.CancelRequested && len(s.Messages) == 1 })
	if waiting.Status != "running" || waiting.CancelToken != running.CancelToken {
		t.Fatal("completed-before-abortACK advertised idle")
	}
	cancelFixtureRelease(t, log, ".abort")
	after := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if after.CancelToken != "" || after.Goal.Running || !after.Goal.Deferred {
		t.Fatal("goal cancellation did not retire")
	}
	if strings.Count(policyLog(t, log), "goal_run\n") != 1 {
		t.Fatal("lost HTTP ACK replayed goal")
	}
}

func TestGoalRuntimeReadQueueAndStaleBindingGuards(t *testing.T) {
	m, p, log, before := goalTestManager(t, "goal-normal")
	beforeLog := policyLog(t, log)
	r, _ := m.runtime(p.ID, before.InstanceID)
	r.mu.Lock()
	r.snapshot.Queue = &RuntimeQueue{Items: []RuntimeQueueItem{{ID: "held", Text: "review", State: "held"}}}
	r.mu.Unlock()
	if _, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, goalResumeParams(before)); !errors.Is(err, ErrRuntimeQueueReview) {
		t.Fatalf("held queue: %v", err)
	}
	if policyLog(t, log) != beforeLog {
		t.Fatal("held work guard dispatched RPC")
	}
	r.mu.Lock()
	r.snapshot.Queue = nil
	r.mu.Unlock()
	for _, change := range []func(*protocol.RPCGoalRunParams){func(p *protocol.RPCGoalRunParams) { p.SessionID = "foreign" }, func(p *protocol.RPCGoalRunParams) { p.BranchID = "foreign" }, func(p *protocol.RPCGoalRunParams) { p.ExpectedTipID = "foreign" }, func(p *protocol.RPCGoalRunParams) { p.ExpectedGoalID = "" }} {
		params := goalResumeParams(before)
		change(&params.RPCGoalRunParams)
		if _, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, params); err == nil {
			t.Fatal("stale binding admitted")
		}
	}
	if policyLog(t, log) != beforeLog {
		t.Fatal("stale binding dispatched RPC")
	}
	running, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, goalResumeParams(before))
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != "running" {
		t.Fatal("turn_done retired run")
	}
	if _, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, goalResumeParams(before)); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("second run: %v", err)
	}
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "overlap"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("prompt overlaps native root: %v", err)
	}
	if err := m.CancelTurn(t.Context(), p.ID, before.InstanceID, running.CancelToken); err != nil {
		t.Fatal(err)
	}
	runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && s.CancelToken == "" })
}

func TestGoalRuntimeLostRPCACKFailsClosedAndFastCompletion(t *testing.T) {
	for _, mode := range []string{"goal-lost-rpc-ack", "goal-completed-before-ack"} {
		t.Run(mode, func(t *testing.T) {
			m, p, log, before := goalTestManager(t, mode)
			after, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, goalResumeParams(before))
			if mode == "goal-lost-rpc-ack" {
				if !errors.Is(err, ErrRuntimeUnavailable) {
					t.Fatalf("lost ACK: %v", err)
				}
				runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "failed" })
			} else {
				if err != nil || after.GoalRunACK == nil || after.GoalRunACK.GoalRunID != "run" || after.GoalRunACK.SessionID != before.SessionID || after.GoalRunACK.GoalID != before.Goal.GoalID {
					t.Fatalf("fast completion lost immutable ACK: %+v %v", after, err)
				}
				// Call responses and the event-drain consumer are concurrent. A
				// preACK wire completion may retire just after the HTTP snapshot;
				// the receipt is valid independently of the latest live state.
				done := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
				if len(done.Messages) != 1 || done.Goal.Status != "complete" || done.GoalRunACK != nil {
					t.Fatalf("fast completion/replay projection: %+v", done)
				}
			}
			for range 3 {
				m.Snapshot(p.ID)
			}
			if strings.Count(policyLog(t, log), "goal_run\n") != 1 {
				t.Fatal("read/reconnect retried goal")
			}
		})
	}
}

func TestGoalReviewedRevisionRejectsConfigurationRacesBeforeMutation(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*RuntimeSnapshot)
	}{
		{"provider", func(s *RuntimeSnapshot) { s.Provider = "other-provider" }},
		{"model", func(s *RuntimeSnapshot) { s.Model = "other-model" }},
		{"permission", func(s *RuntimeSnapshot) { s.PermissionMode = "deny" }},
		{"mode", func(s *RuntimeSnapshot) { s.Mode = "plan" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			m, p, log, before := goalTestManager(t, "goal-normal")
			r, _ := m.runtime(p.ID, before.InstanceID)
			r.mu.Lock()
			change.mutate(&r.snapshot)
			r.publishLocked()
			r.mu.Unlock()
			beforeLog := policyLog(t, log)
			if _, err := m.RunGoal(t.Context(), p.ID, before.InstanceID, goalResumeParams(before)); !errors.Is(err, ErrRuntimeInvalid) {
				t.Fatalf("stale reviewed %s admitted: %v", change.name, err)
			}
			after, _ := m.Snapshot(p.ID)
			if beforeLog != policyLog(t, log) || after.Recovery.State != before.Recovery.State || after.Status != "idle" || after.CancelToken != "" {
				t.Fatal("stale consent mutated before rejection")
			}
		})
	}
}

func TestGoalNonterminalHistoryGuardsPreserveExistingPreparation(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "edit-normal")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	source := before.Messages[0]
	prepared, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := m.runtime(p.ID, before.InstanceID)
	r.mu.Lock()
	original := r.messageEdit.preparation
	r.snapshot.Goal = &RuntimeGoal{SessionID: before.SessionID, BranchID: "main", GoalID: "saved-goal", Status: "paused", Deferred: true}
	r.mu.Unlock()
	beforeLog := policyLog(t, log)
	if _, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, source.ID); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("paused-goal prepare: %v", err)
	}
	if _, err := m.CommitMessageEdit(t.Context(), p.ID, before.InstanceID, prepared.EditToken, "replacement"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("paused-goal commit: %v", err)
	}
	r.mu.Lock()
	unchanged := r.messageEdit.preparation == original
	r.mu.Unlock()
	if !unchanged || policyLog(t, log) != beforeLog {
		t.Fatal("nonterminal goal guard invalidated preparation or dispatched history mutation")
	}
}
