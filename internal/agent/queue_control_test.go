package agent

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func controlSnapshot(t *testing.T, a *Agent) protocol.QueueControl {
	t.Helper()
	a.mu.RLock()
	p := protocol.RPCQueueListParams{SessionID: a.queueControl.sessionID, TurnID: a.queueControl.turnID}
	a.mu.RUnlock()
	q, err := a.QueueControlSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func controlEnqueue(t *testing.T, a *Agent, text string) protocol.QueueControl {
	t.Helper()
	q := controlSnapshot(t, a)
	out, err := a.MutateQueueControl("queue_enqueue", protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func replyEvents(text string) []protocol.StreamEvent {
	return []protocol.StreamEvent{{Type: protocol.EvStreamTextDelta, Text: text}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}
}

func TestQueueControlDeliveryPreservesRootAndHistoricalActions(t *testing.T) {
	p := newQueuedProvider(replyEvents("first reply"))
	p.later = replyEvents("next reply")
	a, st := setup(t, p, nil, permission.ModeDeny)
	var mu sync.Mutex
	var changes []protocol.QueueControl
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.QueueControl != nil {
			mu.Lock()
			changes = append(changes, *ev.QueueControl.Clone())
			mu.Unlock()
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	initial := controlSnapshot(t, a)
	q := controlEnqueue(t, a, "second")
	if q.TurnID != initial.TurnID || !q.Accepting {
		t.Fatal("queue lost exact admitted root")
	}
	stale := protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision - 1, ItemID: q.Items[0].ID, Text: "stale"}
	if _, err := a.MutateQueueControl("queue_update", stale); !errors.Is(err, ErrQueueStale) {
		t.Fatal(err)
	}
	stale.Revision = q.Revision
	stale.Text = "edited second"
	if _, err := a.MutateQueueControl("queue_update", stale); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[2].Content[0].Text != "edited second" {
		t.Fatalf("messages=%+v", messages)
	}
	for _, message := range messages {
		if message.Role == protocol.RoleUser {
			source, err := session.ResolveMessageEdit(t.Context(), st, protocol.RPCMessageEditPrepareParams{SessionID: st.ID(), EntryID: message.ID})
			if err != nil || source.TurnID != initial.TurnID {
				t.Fatalf("exact user source=%+v err=%v", source, err)
			}
		} else if message.Role == protocol.RoleAssistant {
			source, err := session.ResolveMessageRegenerate(t.Context(), st, protocol.RPCMessageRegeneratePrepareParams{SessionID: st.ID(), EntryID: message.ID})
			if err != nil || source.TurnID != initial.TurnID {
				t.Fatalf("exact reply source=%+v err=%v", source, err)
			}
		}
	}
	if _, err := session.ResolveMessageRegenerate(t.Context(), st, protocol.RPCMessageRegeneratePrepareParams{SessionID: st.ID(), TurnID: initial.TurnID}); err == nil {
		t.Fatal("ambiguous root regeneration guessed latest span")
	}
	source, err := session.ResolveMessageEdit(t.Context(), st, protocol.RPCMessageEditPrepareParams{SessionID: st.ID(), TurnID: initial.TurnID})
	if err != nil || source.EntryID != messages[0].ID {
		t.Fatalf("root edit must remain initial input: %+v %v", source, err)
	}
	if err := a.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	delivered := false
	revision := uint64(0)
	for _, state := range changes {
		if state.Revision <= revision {
			t.Fatal("queue control publication reordered")
		}
		revision = state.Revision
		if c := state.Change; c.Kind == "delivered" {
			delivered = true
			if c.ItemID != q.Items[0].ID || c.UserEntryID != messages[2].ID || c.PreviousUserEntryID != messages[0].ID || c.PrecedingReplyEntryID != messages[1].ID || c.SpanID == "" {
				t.Fatalf("delivery identities=%+v", c)
			}
		}
	}
	if !delivered {
		t.Fatal("missing authoritative delivery")
	}
}

func TestQueueControlFailureAndCancelRetainWithoutReplay(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamError, Err: errors.New("provider failed")}})
			a, st := setup(t, p, nil, permission.ModeDeny)
			done := make(chan error, 1)
			go func() { done <- a.Prompt(t.Context(), "initial") }()
			<-p.started
			q := controlEnqueue(t, a, "review me")
			if cancel {
				a.Abort()
			} else {
				close(p.release)
			}
			<-done
			state := controlSnapshot(t, a)
			if state.Accepting || len(state.Items) != 0 || len(state.ReviewItems) != 1 || state.ReviewItems[0].State != "held" || state.ReviewItems[0].ID != q.Items[0].ID || len(a.PendingInputs().Items) != 0 {
				t.Fatalf("held state=%+v pending=%+v", state, a.PendingInputs())
			}
			if p.calls != 1 {
				t.Fatalf("failure replayed accepted queue: calls=%d", p.calls)
			}
			messages, _ := st.Messages()
			for _, m := range messages {
				if m.Role == protocol.RoleUser && m.Content[0].Text == "review me" {
					t.Fatal("held work persisted")
				}
			}
			if err := a.Prompt(t.Context(), "unrelated"); !errors.Is(err, ErrPromptRejected) {
				t.Fatalf("held work overwritten: %v", err)
			}
			tip := st.BranchTip()
			again := controlSnapshot(t, a)
			if !reflect.DeepEqual(state, again) || st.BranchTip() != tip {
				t.Fatal("queue_list mutated state")
			}
			if _, err := a.MutateQueueControl("queue_remove", protocol.RPCQueueUpdateParams{SessionID: state.SessionID, TurnID: state.TurnID, Revision: state.Revision, ItemID: state.ReviewItems[0].ID}); err != nil {
				t.Fatal(err)
			}
			if len(controlSnapshot(t, a).ReviewItems) != 0 {
				t.Fatal("explicit discard failed")
			}
		})
	}
}

func TestQueueControlBoundsAdmissionAndBusyRejection(t *testing.T) {
	p := newQueuedProvider(replyEvents("reply"))
	a, _ := setup(t, p, nil, permission.ModeDeny)
	if _, err := a.MutateQueueControl("queue_enqueue", protocol.RPCQueueUpdateParams{SessionID: "s", TurnID: "t", Text: "x"}); err == nil {
		t.Fatal("pre-admission enqueue accepted")
	}
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	defer func() { a.Abort(); <-done }()
	q := controlSnapshot(t, a)
	for _, text := range []string{"", "\x00", string([]byte{0xff}), strings.Repeat("x", protocol.RPCQueueMaxTextBytes+1)} {
		if _, err := a.MutateQueueControl("queue_enqueue", protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: text}); !errors.Is(err, ErrQueueRejected) {
			t.Fatalf("invalid input err=%v", err)
		}
	}
	for range protocol.RPCQueueMaxItems {
		q = controlEnqueue(t, a, "pending")
	}
	if _, err := a.MutateQueueControl("queue_enqueue", protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: "overflow"}); !errors.Is(err, ErrQueueRejected) {
		t.Fatal(err)
	}
	a.queuePublishMu.Lock()
	if _, err := a.MutateQueueControl("queue_remove", protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.Items[0].ID}); !errors.Is(err, ErrQueueRejected) {
		t.Fatal(err)
	}
	if state := controlSnapshot(t, a); !reflect.DeepEqual(q, state) {
		t.Fatal("busy/list changed state")
	}
	a.queuePublishMu.Unlock()
}
