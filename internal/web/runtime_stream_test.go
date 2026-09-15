package web

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// This fixture has no process: transport tests exercise the actual manager's
// publication/subscription locking without modifying existing RPC workers.
func streamRuntimeFixture(t *testing.T, projectID string) (*RuntimeManager, *liveRuntime) {
	t.Helper()
	m := NewRuntimeManager(t.Context(), "", "")
	ctx, cancel := context.WithCancel(m.ctx)
	r := &liveRuntime{ctx: ctx, cancel: cancel, instanceID: "instance-one", assistant: -1, plan: -1,
		started: make(chan struct{}), drained: make(chan struct{}),
		snapshot: RuntimeSnapshot{ProjectID: projectID, InstanceID: "instance-one", SessionID: "session-one", Status: "idle", Revision: 1}}
	close(r.started)
	close(r.drained)
	m.workers[projectID] = r
	t.Cleanup(func() { _ = m.Close() })
	return m, r
}

func TestRuntimeSubscribeAtomicLatestAndIndependentLifetime(t *testing.T) {
	m, r := streamRuntimeFixture(t, "project")
	initial, sub, err := m.Subscribe("project", "instance-one")
	if err != nil || initial.Revision != 1 {
		t.Fatalf("subscribe: %+v %v", initial, err)
	}
	defer sub.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := range 10000 {
			r.mu.Lock()
			r.snapshot.SessionName = fmt.Sprint(i)
			r.publishLocked()
			r.mu.Unlock()
		}
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("an unread observer blocked publication")
	}
	if len(sub.Changes()) != 1 {
		t.Fatal("notifications were not coalesced into one wakeup")
	}
	<-sub.Changes()
	latest, ok := sub.Snapshot()
	if !ok || latest.Revision != 10001 || latest.SessionName != "9999" || initial.SessionName != "" {
		t.Fatalf("latest/initial snapshot isolation: %+v %+v", latest, initial)
	}
	sub.Close()
	sub.Close()
	if _, ok := sub.Snapshot(); ok || r.ctx.Err() != nil {
		t.Fatal("unsubscribe retained authority or canceled the worker")
	}
}

func TestRuntimeSubscribeRaceGenerationAndLimits(t *testing.T) {
	m, r := streamRuntimeFixture(t, "project")
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 500 {
			r.mu.Lock()
			r.snapshot.SessionName = fmt.Sprint(r.snapshot.Revision + 1)
			r.publishLocked()
			r.mu.Unlock()
		}
	})
	for range 500 {
		initial, sub, err := m.Subscribe("project", "instance-one")
		if err != nil {
			t.Fatal(err)
		}
		if initial.Revision > 1 && initial.SessionName != fmt.Sprint(initial.Revision) {
			t.Fatal("initial snapshot and revision are not atomic")
		}
		sub.Close()
	}
	wg.Wait()
	var subscriptions []RuntimeSubscription
	for range runtimeSubscriberLimit {
		_, sub, err := m.Subscribe("project", "instance-one")
		if err != nil {
			t.Fatal(err)
		}
		subscriptions = append(subscriptions, sub)
		defer sub.Close()
	}
	if _, _, err := m.Subscribe("project", "instance-one"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("subscriber admission is unbounded")
	}
	r.mu.Lock()
	r.instanceID, r.snapshot.InstanceID = "instance-two", "instance-two"
	r.publishLocked()
	r.mu.Unlock()
	if _, ok := subscriptions[0].Snapshot(); ok {
		t.Fatal("old subscription followed a switched session")
	}
	if _, _, err := m.Subscribe("project", "instance-one"); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatal("stale reconnect acquired replacement authority")
	}
	subscriptions[0].Close()
	_, current, err := m.Subscribe("project", "instance-two")
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	r.stop()
	if _, ok := current.Snapshot(); ok {
		t.Fatal("closing runtime remains observable as live")
	}
}

func TestRuntimeHistoryProjectionIDsDistinctAndRepeatable(t *testing.T) {
	page := protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "original-message", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "before"}, {Type: protocol.BlockPlan, Text: "plan"}, {Type: protocol.BlockText, Text: "after"},
	}}}}
	r := &liveRuntime{assistant: -1, plan: -1}
	r.projectHistory(page)
	if len(r.snapshot.Messages) != 3 {
		t.Fatalf("lost interleaved blocks: %+v", r.snapshot.Messages)
	}
	seen := map[string]bool{}
	for _, message := range r.snapshot.Messages {
		if message.ID == "" || seen[message.ID] || message.SourceID != "original-message" {
			t.Fatalf("unstable/duplicate identity: %+v", message)
		}
		seen[message.ID] = true
	}
	other := &liveRuntime{assistant: -1, plan: -1}
	other.projectHistory(page)
	if !reflect.DeepEqual(r.snapshot.Messages, other.snapshot.Messages) {
		t.Fatal("reloading history changed projected identities")
	}
}

func TestRuntimeLiveMessageIdentitySurvivesTrimAndReusedPlanID(t *testing.T) {
	r := &liveRuntime{instanceID: "instance", assistant: -1, plan: -1}
	for range runtimeHistoryCount {
		r.addMessage(RuntimeMessage{Role: "user", Text: "prompt"})
	}
	previous := slices.Clone(r.snapshot.Messages)
	r.addMessage(RuntimeMessage{Role: "assistant", Text: "answer"})
	for i, message := range r.snapshot.Messages[:runtimeHistoryCount-1] {
		if message.ID == "" || message.ID != previous[i+1].ID {
			t.Fatal("trim rekeyed an existing live message")
		}
	}
	plan := &protocol.PlanItem{ID: "reused", Text: "complete"}
	r.projectPlan(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: plan})
	first := r.snapshot.Messages[r.plan].ID
	r.projectPlan(protocol.AgentEvent{Type: protocol.EvPlanDelta, Plan: plan, Text: "delta"})
	if r.snapshot.Messages[r.plan].ID != first {
		t.Fatal("plan deltas replaced their stable display identity")
	}
	r.projectPlan(protocol.AgentEvent{Type: protocol.EvPlanCompleted, Plan: plan})
	r.projectPlan(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: plan})
	if r.snapshot.Messages[r.plan].ID == first {
		t.Fatal("a later plan reused a prior display identity")
	}
}
