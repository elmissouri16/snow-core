package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func steerProjectionRuntime(t *testing.T) *liveRuntime {
	t.Helper()
	r := &liveRuntime{ctx: t.Context(), instanceID: "instance", busy: true, promptID: "prompt-ack", turnID: "PRIVATE-root", rootEpoch: 7, steerSupported: true, snapshot: RuntimeSnapshot{SessionID: "session", InstanceID: "instance", Status: "running"}}
	r.queue = runtimeQueueState{sourceUserID: "durable-user", control: &protocol.QueueControl{SessionID: "session", TurnID: r.turnID, Revision: 1, Accepting: true}}
	r.refreshSteerLocked()
	return r
}

func pendingSteer(r *liveRuntime, id, text string) protocol.RPCManagedSteerResult {
	ack := protocol.RPCManagedSteerResult{SessionID: r.snapshot.SessionID, TurnID: r.turnID, RootEpoch: r.rootEpoch, RequestID: id, ItemID: "native-" + id, Status: "accepted"}
	r.steer.records = append(r.steer.records, runtimeSteerRecord{RuntimeSteerItem: RuntimeSteerItem{RequestID: id, Text: text, Status: "uncertain"}, token: r.steer.token, session: ack.SessionID, turn: ack.TurnID, epoch: ack.RootEpoch, pending: true})
	return ack
}

func steerChange(r *liveRuntime, ack protocol.RPCManagedSteerResult, revision uint64, kind string) bool {
	change := protocol.QueueControlChange{Kind: kind, ItemID: ack.ItemID}
	if kind == "delivered" {
		change.Text = "literal steering"
		change.UserEntryID = "durable-steer"
		change.PreviousUserEntryID = "durable-user"
		change.SpanID = "input-span"
	}
	return r.applySteerControlLocked(&protocol.QueueControl{SessionID: ack.SessionID, TurnID: ack.TurnID, Revision: revision, Change: change})
}

func TestSteerAdmissionRequiresReviewedExactOrdinaryRoot(t *testing.T) {
	for name, mutate := range map[string]func(*liveRuntime){
		"pending root ACK":             func(r *liveRuntime) { r.promptID = "" },
		"missing durable source":       func(r *liveRuntime) { r.queue.sourceUserID = "" },
		"goal pending":                 func(r *liveRuntime) { r.goal.pending = true },
		"goal active":                  func(r *liveRuntime) { r.goal.active = true },
		"unfinished goal":              func(r *liveRuntime) { r.snapshot.Goal = &RuntimeGoal{GoalID: "goal", Status: "blocked"} },
		"compaction pending":           func(r *liveRuntime) { r.compaction.pending = true },
		"compaction active":            func(r *liveRuntime) { r.compaction.active = true },
		"compaction without user root": func(r *liveRuntime) { r.turnID = "compaction-root" },
		"root complete":                func(r *liveRuntime) { r.busy = false },
		"not accepting":                func(r *liveRuntime) { r.queue.control.Accepting = false },
		"cancel":                       func(r *liveRuntime) { r.snapshot.CancelRequested = true },
		"transition":                   func(r *liveRuntime) { r.transitioning = true },
		"permission":                   func(r *liveRuntime) { r.snapshot.Status = "permission" },
		"input":                        func(r *liveRuntime) { r.snapshot.Status = "input" },
		"failure":                      func(r *liveRuntime) { r.snapshot.Status = "failed" },
		"disconnected":                 func(r *liveRuntime) { ctx, cancel := context.WithCancel(t.Context()); cancel(); r.ctx = ctx },
	} {
		t.Run(name, func(t *testing.T) {
			r := steerProjectionRuntime(t)
			token, revision := r.steer.token, r.steer.revision
			mutate(r)
			m := &RuntimeManager{workers: map[string]*liveRuntime{"project": r}}
			if _, err := m.SteerCurrentRun(t.Context(), "project", "instance", "session", token, revision, "request", "literal"); err == nil {
				t.Fatal("inadmissible root dispatched to absent worker")
			}
			if r.snapshot.Steer.CanSteer {
				t.Fatal("stale steering authority")
			}
		})
	}
	r := steerProjectionRuntime(t)
	m := &RuntimeManager{workers: map[string]*liveRuntime{"project": r}}
	for _, token := range []string{"", "stop-token", "queue-token", "old-generation"} {
		if _, err := m.SteerCurrentRun(t.Context(), "project", "instance", "session", token, r.steer.revision, "request", "literal"); !errors.Is(err, ErrRuntimeInvalid) {
			t.Fatalf("foreign token: %v", err)
		}
	}
	if _, err := m.SteerCurrentRun(t.Context(), "project", "instance", "session", r.steer.token, 0, "request", "literal"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("zero projection revision authorized")
	}
	oldToken, oldRevision := r.steer.token, r.steer.revision
	r.turnID, r.queue.control.TurnID = "new-root", "new-root"
	r.refreshSteerLocked()
	if r.steer.token == oldToken {
		t.Fatal("token rebound to new root")
	}
	if _, err := m.SteerCurrentRun(t.Context(), "project", "instance", "session", oldToken, oldRevision, "request", "literal"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("stale root token authorized")
	}
}

func TestSteerNativeEventsAndACKOrdersNeverClaimOptimisticDelivery(t *testing.T) {
	for _, before := range []bool{true, false} {
		for _, kind := range []string{"delivered", "discarded"} {
			t.Run(fmt.Sprintf("before=%v/%s", before, kind), func(t *testing.T) {
				r := steerProjectionRuntime(t)
				ack := pendingSteer(r, "request", "literal steering")
				token := r.steer.token
				if !steerChange(r, ack, 2, "steer_accepted") {
					t.Fatal("acceptance event rejected")
				}
				if before {
					if !steerChange(r, ack, 3, kind) {
						t.Fatal("preACK native terminal rejected")
					}
				}
				if !r.reconcileSteerACKLocked(ack, token) {
					t.Fatal("receipt rejected")
				}
				if !before && r.steer.records[0].Status != "accepted" {
					t.Fatal("acceptance claimed delivery")
				}
				if !before {
					steerChange(r, ack, 3, kind)
				}
				r.snapshot.CancelRequested = true
				r.refreshSteerLocked()
				if r.steer.records[0].Status != kind {
					t.Fatal("Stop regressed native terminal")
				}
				r.reconcileSteerACKLocked(ack, token)
				steerChange(r, ack, 2, "steer_accepted")
				if r.steer.records[0].Status != kind || len(r.snapshot.Messages) != 0 {
					t.Fatal("late ACK regressed terminal or created optimistic chat")
				}
			})
		}
	}
}

func TestSteerStopFailureAndCompletionRemainUnknownUntilNativeTerminal(t *testing.T) {
	for _, terminal := range []string{"stop", "failure", "completion"} {
		t.Run(terminal, func(t *testing.T) {
			r := steerProjectionRuntime(t)
			ack := pendingSteer(r, "request", "literal steering")
			r.reconcileSteerACKLocked(ack, r.steer.token)
			switch terminal {
			case "stop":
				r.snapshot.CancelRequested = true
			case "failure":
				r.snapshot.Status = "failed"
			case "completion":
				r.busy = false
			}
			r.refreshSteerLocked()
			if r.snapshot.Steer.Items[0].Status != "uncertain" {
				t.Fatal("inferred discard")
			}
			if !steerChange(r, ack, 2, "delivered") {
				t.Fatal("native post-failure delivery rejected")
			}
			r.refreshSteerLocked()
			if r.snapshot.Steer.Items[0].Status != "delivered" {
				t.Fatal("lost native post-failure delivery")
			}
		})
	}
}

func TestSteerGenericFollowUpEventsCannotResolveSteering(t *testing.T) {
	r := steerProjectionRuntime(t)
	ack := pendingSteer(r, "request", "same text")
	r.reconcileSteerACKLocked(ack, r.steer.token)
	other := ack
	other.ItemID = "queue-next-item"
	steerChange(r, other, 3, "delivered")
	steerChange(r, other, 4, "discarded")
	if r.steer.records[0].Status != "accepted" || len(r.steer.changes) != 0 {
		t.Fatal("Queue next event confused with steering")
	}
	oldToken := r.steer.token
	r.turnID, r.queue.control.TurnID = "new-root", "new-root"
	r.refreshSteerLocked()
	if steerChange(r, ack, 5, "delivered") {
		t.Fatal("retired root event accepted into new root")
	}
	if !r.reconcileSteerACKLocked(ack, oldToken) || r.steer.token == oldToken || r.steer.records[0].Status != "uncertain" {
		t.Fatal("old receipt rebound to current root")
	}
}

func TestSteerSharedCapacityAndBoundedPublicHistory(t *testing.T) {
	r := steerProjectionRuntime(t)
	for i := range 7 {
		r.queue.control.ReviewItems = append(r.queue.control.ReviewItems, protocol.QueueControlItem{ID: fmt.Sprint(i), Text: "held", State: "held"})
	}
	if !r.steerCapacityLocked("last") {
		t.Fatal("eighth native item rejected")
	}
	ack := pendingSteer(r, "request", "last")
	r.reconcileSteerACKLocked(ack, r.steer.token)
	if r.steerCapacityLocked("ninth") {
		t.Fatal("steering bypassed shared review count")
	}
	count, bytes := r.steerPendingUsageLocked()
	if count != 1 || bytes != 4 {
		t.Fatal("Queue next capacity helper omitted steering")
	}
	r.queue.control.ReviewItems = nil
	for i := range 4 {
		r.queue.control.Items = append(r.queue.control.Items, protocol.QueueControlItem{ID: fmt.Sprint(i), Text: strings.Repeat("x", 64<<10), State: "pending"})
	}
	if r.steerCapacityLocked("x") {
		t.Fatal("steering bypassed shared bytes")
	}
	r.queue.control.Items = nil
	for i := range 30 {
		r.trimSteerHistoryLocked(64 << 10)
		item := pendingSteer(r, fmt.Sprint(i), strings.Repeat("x", 64<<10))
		r.reconcileSteerACKLocked(item, r.steer.token)
		steerChange(r, item, uint64(i+2), "delivered")
	}
	r.refreshSteerLocked()
	total := 0
	for _, item := range r.snapshot.Steer.Items {
		total += len(item.Text)
	}
	if len(r.snapshot.Steer.Items) > 8 || total > 256<<10 || len(r.steer.changes) > 16 {
		t.Fatal("steering history/buffer unbounded")
	}
}

func TestSteerSnapshotClonesAndOmitsPrivateRoot(t *testing.T) {
	r := steerProjectionRuntime(t)
	ack := pendingSteer(r, "request", "literal steering")
	r.reconcileSteerACKLocked(ack, r.steer.token)
	r.refreshSteerLocked()
	clone := r.snapshot.clone()
	clone.Steer.Items[0].Text = "tampered"
	if r.snapshot.Steer.Items[0].Text != "literal steering" {
		t.Fatal("snapshot shares steering slice")
	}
	raw, err := json.Marshal(r.snapshot)
	if err != nil || strings.Contains(string(raw), "PRIVATE-root") || strings.Contains(string(raw), "root_epoch") {
		t.Fatalf("private binding leaked: %s %v", raw, err)
	}
}

func TestSteerReplacementRetiresPublicAndPrivateControls(t *testing.T) {
	r := steerProjectionRuntime(t)
	ack := pendingSteer(r, "request", "old steering")
	r.reconcileSteerACKLocked(ack, r.steer.token)
	r.refreshSteerLocked()
	token := r.steer.token
	r.snapshot.SteerACK = &RuntimeSteerACK{Token: token, RequestID: ack.RequestID, ItemID: ack.ItemID, Status: "accepted"}
	r.compaction.active = true
	r.snapshot.Compaction = &RuntimeCompaction{State: "completed"}
	r.snapshot.CompactionACK = &RuntimeCompactionACK{}
	r.resetRunControlsForReplacementLocked()
	if r.snapshot.Steer != nil || r.snapshot.SteerACK != nil || r.snapshot.Compaction != nil || r.snapshot.CompactionACK != nil || r.compaction.active || len(r.steer.records) != 0 || r.steer.token != "" {
		t.Fatal("replacement inherited run controls")
	}
	if r.reconcileSteerACKLocked(ack, token) {
		t.Fatal("retired receipt rebound to replacement")
	}
}

func TestSteerQueueProjectionSharesNativeCapacityAndCompactionGuard(t *testing.T) {
	r := steerProjectionRuntime(t)
	r.queueSupported = true
	for i := range 8 {
		ack := pendingSteer(r, fmt.Sprint(i), "native steering")
		r.reconcileSteerACKLocked(ack, r.steer.token)
	}
	r.publishLocked()
	if r.snapshot.Queue.CanEnqueue || r.snapshot.Steer.CanSteer || len(r.snapshot.Queue.Items) != 0 {
		t.Fatal("steering capacity not shared separately with Queue next")
	}
	r.steer.records = nil
	r.publishLocked()
	if !r.snapshot.Queue.CanEnqueue {
		t.Fatal("empty native capacity unavailable")
	}
	for _, pending := range []bool{true, false} {
		r.compaction.pending, r.compaction.active = pending, !pending
		r.publishLocked()
		if r.snapshot.Queue.CanEnqueue || r.queueActiveLocked() {
			t.Fatal("compaction grants follow-up queue authority")
		}
	}
}

func TestSteerExactByteLimitRequiresCapacityForValidMinimum(t *testing.T) {
	for _, source := range []string{"steering", "followup", "review"} {
		t.Run(source, func(t *testing.T) {
			r := steerProjectionRuntime(t)
			r.queueSupported = true
			for i := range protocol.RPCQueueMaxTotalBytes / protocol.RPCQueueMaxTextBytes {
				text := strings.Repeat("x", protocol.RPCQueueMaxTextBytes)
				switch source {
				case "steering":
					ack := pendingSteer(r, fmt.Sprint("capacity-", i), text)
					r.reconcileSteerACKLocked(ack, r.steer.token)
				case "followup":
					r.queue.control.Items = append(r.queue.control.Items, protocol.QueueControlItem{ID: fmt.Sprint(i), Text: text, State: "pending"})
				case "review":
					r.queue.control.ReviewItems = append(r.queue.control.ReviewItems, protocol.QueueControlItem{ID: fmt.Sprint(i), Text: text, State: "held"})
				}
			}
			var last *string
			switch source {
			case "steering":
				last = &r.steer.records[len(r.steer.records)-1].Text
			case "followup":
				last = &r.queue.control.Items[len(r.queue.control.Items)-1].Text
			case "review":
				last = &r.queue.control.ReviewItems[len(r.queue.control.ReviewItems)-1].Text
			}
			// One remaining byte genuinely permits the smallest valid submission.
			*last = (*last)[:len(*last)-1]
			r.publishLocked()
			if !r.snapshot.Steer.CanSteer || !r.snapshot.Queue.CanEnqueue || !r.steerCapacityLocked("x") || r.steerCapacityLocked("xx") {
				t.Fatal("one-byte remaining capacity not projected or enforced exactly")
			}
			*last += "x"
			r.publishLocked()
			if r.snapshot.Steer.CanSteer || r.snapshot.Queue.CanEnqueue || r.steerCapacityLocked("x") {
				t.Fatal("exactly 256 KiB still grants admission for a valid submission")
			}
		})
	}
}
