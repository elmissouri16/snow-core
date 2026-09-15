package agent

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestGoalRunControlsRejectReservedOwnerAcrossGaps(t *testing.T) {
	for _, deferred := range []bool{true, false} {
		name := "before_release"
		if !deferred {
			name = "interturn_gap"
		}
		t.Run(name, func(t *testing.T) {
			p := &scriptedProvider{}
			a, c, st := goalAgent(t, p)
			a.opts.ManagedExplicitGoals = true
			g, err := c.Create("preserve owned goal", new(int64(1000)), false)
			if err != nil {
				t.Fatal(err)
			}
			h := admittedTestGoalRun(t, a, g.GoalID)
			// An owned native gap has running=false and autoRunning=true, just like
			// the reserved pre-ACK phase, but continuation is no longer deferred.
			if err := c.Defer(deferred); err != nil {
				t.Fatal(err)
			}
			other, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "other.db"), t.TempDir(), session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = other.Close() })
			beforeGoal, _ := c.Get()
			beforeTip, beforeBranch := st.BranchTip(), st.ActiveBranchID()
			beforeModel, beforeMode := a.Model(), a.Mode()
			controls := map[string]func() error{
				"mode":           func() error { return a.SetMode(protocol.ModePlan) },
				"model":          func() error { return a.SetModel(protocol.Model{Provider: p.ID(), ID: "other", SupportsTools: true}) },
				"provider":       func() error { return a.SetProvider(p) },
				"provider_model": func() error { return a.SetProviderAndModel(p, protocol.Model{Provider: p.ID(), ID: "other"}) },
				"provider_model_thinking": func() error {
					return a.SetProviderModelThinking(p, protocol.Model{Provider: p.ID(), ID: "other"}, protocol.ThinkingOff)
				},
				"thinking":          func() error { return a.SetThinking(protocol.ThinkingOff) },
				"reasoning_summary": func() error { return a.SetReasoningSummary(protocol.ReasoningSummaryDetailed) },
				"text_verbosity":    func() error { return a.SetTextVerbosity(protocol.TextVerbosityHigh) },
				"session":           func() error { return a.SetSession(other) },
				"session_rename":    func() error { return a.RenameSession("changed") },
				"branch":            func() error { return a.SelectBranch(beforeBranch) },
				"branch_fork":       func() error { _, err := a.Fork(beforeTip); return err },
				"branch_rename":     func() error { _, err := a.RenameBranch(beforeBranch, "changed"); return err },
				"branch_delete":     func() error { return a.DeleteBranch(beforeBranch) },
				"compact":           func() error { _, err := a.Compact(t.Context()); return err },
				"mailbox_run":       func() error { return a.RunMailbox(t.Context()) },
				"steer":             func() error { return a.Steer("unowned steering") },
				"follow_up":         func() error { return a.FollowUp("unowned followup") },
				"idle_session": func() error {
					unlock := a.LockAdmission()
					defer unlock()
					_, err := a.IdleSessionAdmitted("external snapshot")
					return err
				},
			}
			for name, control := range controls {
				t.Run(name, func(t *testing.T) {
					if err := control(); err == nil {
						t.Fatal("control accepted owned runtime")
					}
					select {
					case <-h.Done():
						t.Fatal("control preempted owner")
					default:
					}
					afterGoal, _ := c.Get()
					afterDeferred, _ := c.Deferred()
					if a.GoalRunID() != h.ID() || !reflect.DeepEqual(beforeGoal, afterGoal) || afterDeferred != deferred || st.BranchTip() != beforeTip || st.ActiveBranchID() != beforeBranch || a.Mode() != beforeMode || !reflect.DeepEqual(a.Model(), beforeModel) {
						t.Fatalf("control changed owned state: goal=%+v deferred=%v", afterGoal, afterDeferred)
					}
					if len(p.requests) != 0 {
						t.Fatal("control started provider before release")
					}
				})
			}
			unlock := a.LockAdmission()
			_, _, busy, err := a.SessionIdentityAdmitted()
			unlock()
			if err != nil || !busy {
				t.Fatalf("session inventory did not fence owner: busy=%v err=%v", busy, err)
			}
			if err := a.StopGoal(t.Context(), true); err != nil {
				t.Fatal(err)
			}
			if err := h.Wait(t.Context()); !errors.Is(err, context.Canceled) {
				t.Fatalf("Stop did not join owner: %v", err)
			}
			if a.GoalRunID() != "" || len(p.requests) != 0 {
				t.Fatal("Stop leaked owner or started provider")
			}
			afterGoal, _ := c.Get()
			if afterGoal.GoalID != g.GoalID || afterGoal.Status != protocol.GoalActive {
				t.Fatalf("Stop replaced goal: %+v", afterGoal)
			}
		})
	}
}

func TestGoalRunQueueOwnerGuardSurvivesReopenedNativeQueue(t *testing.T) {
	p := &scriptedProvider{}
	a, c, st := goalAgent(t, p)
	a.opts.ManagedExplicitGoals = true
	g, err := c.Create("owner", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := admittedTestGoalRun(t, a, g.GoalID)
	a.mu.Lock()
	a.running, a.queueAccepting = true, true
	a.turnOrigin, a.turnID = "user", "old-root"
	a.queueControl = queueControlState{sessionID: st.ID(), turnID: "old-root", revision: 1, ready: true}
	a.mu.Unlock()
	for _, kind := range []protocol.QueuedInputKind{protocol.QueuedInputSteer, protocol.QueuedInputFollowUp} {
		if _, err := a.QueueInput(kind, "unowned"); err == nil {
			t.Fatal("native queue reopening bypassed owner")
		}
	}
	q, err := a.QueueControlSnapshot(protocol.RPCQueueListParams{SessionID: st.ID(), TurnID: "old-root"})
	if err != nil || q.Accepting {
		t.Fatalf("queue advertised admission: %+v %v", q, err)
	}
	if _, err := a.MutateQueueControl("queue_enqueue", protocol.RPCQueueUpdateParams{SessionID: st.ID(), TurnID: "old-root", Revision: 1, Text: "unowned"}); !errors.Is(err, ErrQueueRejected) {
		t.Fatalf("queue mutation=%v", err)
	}
	events := make(chan protocol.AgentEvent, 1)
	a.Subscribe(func(e protocol.AgentEvent) {
		if e.Type == protocol.EvQueueUpdated {
			events <- e
		}
	})
	a.queuePublishMu.Lock()
	a.mu.Lock()
	a.publishQueueControlLocked()
	a.running, a.queueAccepting = false, false
	a.turnOrigin, a.turnID = "", ""
	a.mu.Unlock()
	a.queuePublishMu.Unlock()
	if err := a.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e := <-events; e.GoalRunID != h.ID() {
		t.Fatalf("direct queue event lost owner: %+v", e)
	}
	h.Cancel()
	_ = h.Wait(t.Context())
}
