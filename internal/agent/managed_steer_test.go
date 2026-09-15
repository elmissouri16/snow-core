package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func managedSteerParams(a *Agent, text string) protocol.RPCManagedSteerParams {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return protocol.RPCManagedSteerParams{SessionID: a.opts.Session.ID(), TurnID: a.turnID, RootEpoch: a.rootEpoch, RequestID: "explicit-request", Text: text}
}

func TestManagedSteerExactAdmissionAndCorrelation(t *testing.T) {
	p := newBlockingProvider()
	a, store := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	defer func() { a.Abort(); <-done }()
	params := managedSteerParams(a, "  /plan $review\nKeep literal text.  ")
	for name, mutate := range map[string]func(*protocol.RPCManagedSteerParams){
		"session": func(p *protocol.RPCManagedSteerParams) { p.SessionID += "-stale" },
		"root":    func(p *protocol.RPCManagedSteerParams) { p.TurnID += "-stale" },
		"epoch":   func(p *protocol.RPCManagedSteerParams) { p.RootEpoch++ },
	} {
		t.Run(name, func(t *testing.T) {
			bad := params
			mutate(&bad)
			if _, err := a.ManagedSteer(bad); !errors.Is(err, ErrManagedSteerStale) {
				t.Fatalf("stale admission: %v", err)
			}
		})
	}
	result, err := a.ManagedSteer(params)
	if err != nil || result.Status != "accepted" || result.ItemID == "" || result.RequestID != params.RequestID || result.SessionID != params.SessionID || result.TurnID != params.TurnID || result.RootEpoch != params.RootEpoch {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// Correlation is deliberately not replay deduplication. Only another
	// explicit invocation submits another item; there is no automatic retry.
	second, err := a.ManagedSteer(params)
	if err != nil || second.ItemID == result.ItemID {
		t.Fatalf("second explicit request result=%+v err=%v", second, err)
	}
	pending := a.PendingInputs()
	if len(pending.Items) != 2 || pending.Items[0].ID != result.ItemID || pending.Items[0].Kind != protocol.QueuedInputSteer || pending.Items[0].Text != params.Text {
		t.Fatalf("native pending=%+v", pending)
	}
	messages, err := store.Messages()
	if err != nil || len(messages) != 1 {
		t.Fatalf("ACK must not claim persistence: messages=%+v err=%v", messages, err)
	}
}

func TestManagedSteerRejectsOwnersAndLimits(t *testing.T) {
	p := newBlockingProvider()
	a, _ := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	defer func() { a.Abort(); <-done }()
	params := managedSteerParams(a, "steer")
	for name, change := range map[string]func() func(){
		"closed":           func() func() { a.closed = true; return func() { a.closed = false } },
		"idle":             func() func() { a.running = false; return func() { a.running = true } },
		"not accepting":    func() func() { a.queueAccepting = false; return func() { a.queueAccepting = true } },
		"cancel":           func() func() { a.autoStop = true; return func() { a.autoStop = false } },
		"goal owner":       func() func() { a.goalRun = &GoalRunHandle{}; return func() { a.goalRun = nil } },
		"compaction owner": func() func() { a.compactionRun = &CompactionRunHandle{}; return func() { a.compactionRun = nil } },
		"goal root":        func() func() { a.turnOrigin = "goal"; return func() { a.turnOrigin = "user" } },
		"compaction root":  func() func() { a.turnOrigin = "compact"; return func() { a.turnOrigin = "user" } },
		"automatic":        func() func() { a.autoRunning = true; return func() { a.autoRunning = false } },
		"transition":       func() func() { a.queueControl.ready = false; return func() { a.queueControl.ready = true } },
		"closed queue":     func() func() { a.queueControl.closed = true; return func() { a.queueControl.closed = false } },
	} {
		t.Run(name, func(t *testing.T) {
			a.mu.Lock()
			restore := change()
			a.mu.Unlock()
			_, err := a.ManagedSteer(params)
			a.mu.Lock()
			restore()
			a.mu.Unlock()
			if !errors.Is(err, ErrManagedSteerRejected) || len(a.PendingInputs().Items) != 0 {
				t.Fatalf("owner admission: %v", err)
			}
		})
	}
	for _, text := range []string{"", " \n", "a\x00b", string([]byte{255}), strings.Repeat("x", protocol.RPCManagedSteerMaxTextBytes+1)} {
		bad := params
		bad.Text = text
		if _, err := a.ManagedSteer(bad); !errors.Is(err, ErrManagedSteerRejected) {
			t.Fatalf("invalid text admission: %v", err)
		}
	}
	for _, request := range []string{"", " ", "bad\nrequest", strings.Repeat("x", protocol.RPCManagedSteerMaxIDBytes+1)} {
		bad := params
		bad.RequestID = request
		if _, err := a.ManagedSteer(bad); !errors.Is(err, ErrManagedSteerRejected) {
			t.Fatalf("invalid request admission: %v", err)
		}
	}
	a.queuePublishMu.Lock()
	_, err := a.ManagedSteer(params)
	a.queuePublishMu.Unlock()
	if !errors.Is(err, ErrManagedSteerRejected) {
		t.Fatalf("busy delivery admission: %v", err)
	}
	for range protocol.RPCQueueMaxItems {
		if err := a.FollowUp("native pending"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.ManagedSteer(params); !errors.Is(err, ErrManagedSteerRejected) {
		t.Fatalf("shared count limit: %v", err)
	}
	a.mu.Lock()
	a.queuedInputs = nil
	a.queueControl.review = []protocol.QueueControlItem{{ID: "held", Text: strings.Repeat("x", protocol.RPCQueueMaxTextBytes), State: "held"}}
	a.mu.Unlock()
	params.Text = strings.Repeat("x", protocol.RPCManagedSteerMaxTextBytes)
	for range 3 {
		if _, err := a.ManagedSteer(params); err != nil {
			t.Fatal(err)
		}
	}
	params.Text = "one byte beyond shared aggregate"
	if _, err := a.ManagedSteer(params); !errors.Is(err, ErrManagedSteerRejected) {
		t.Fatalf("shared aggregate limit: %v", err)
	}
}

func TestManagedSteerStaleRootRace(t *testing.T) {
	p := newBlockingProvider()
	a, _ := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	defer func() { a.Abort(); <-done }()
	for range 100 {
		params := managedSteerParams(a, "never retarget")
		start := make(chan struct{})
		var result protocol.RPCManagedSteerResult
		var err error
		var replacementPending []protocol.QueuedInput
		var wg sync.WaitGroup
		wg.Go(func() { <-start; result, err = a.ManagedSteer(params) })
		wg.Go(func() {
			<-start
			a.queuePublishMu.Lock()
			defer a.queuePublishMu.Unlock()
			a.mu.Lock()
			defer a.mu.Unlock()
			// Model retirement and successor admission in one identity-change
			// transaction. Accepted old-root work is retired, never copied.
			a.queuedInputs = nil
			a.admitTurnIdentityLocked("user")
			a.queueControl.turnID = a.turnID
			a.rootEpoch++
			replacementPending = slices.Clone(a.queuedInputs)
		})
		close(start)
		wg.Wait()
		if err != nil && !errors.Is(err, ErrManagedSteerStale) && !errors.Is(err, ErrManagedSteerRejected) {
			t.Fatal(err)
		}
		if err == nil && (result.TurnID != params.TurnID || result.RootEpoch != params.RootEpoch) {
			t.Fatalf("ACK retargeted: %+v", result)
		}
		if len(replacementPending) != 0 || len(a.PendingInputs().Items) != 0 {
			t.Fatal("old-root steering reached replacement")
		}
	}
}

func TestManagedSteerNativePriorityAndToolBatch(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "one", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "two", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	})
	reg := tools.NewRegistry()
	if err := reg.Register(&testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult { return tools.TextResult("ok") }}); err != nil {
		t.Fatal(err)
	}
	a, store := setup(t, p, reg, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	if err := a.FollowUp("follow"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ManagedSteer(managedSteerParams(a, "after complete batch")); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 7 || messages[2].Role != protocol.RoleTool || messages[3].Role != protocol.RoleTool || messageTextForTest(messages[4]) != "after complete batch" || messageTextForTest(messages[6]) != "follow" {
		t.Fatalf("native ordering: %+v", messages)
	}
}

func TestManagedSteerStopDeliverySingleWinner(t *testing.T) {
	for range 30 {
		p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}})
		a, store := setup(t, p, nil, permission.ModeDeny)
		var changes []protocol.QueueControlChange
		a.Subscribe(func(ev protocol.AgentEvent) {
			if ev.Type == protocol.EvQueueUpdated && ev.QueueControl != nil {
				changes = append(changes, ev.QueueControl.Change)
			}
		})
		done := make(chan error, 1)
		go func() { done <- a.Prompt(t.Context(), "initial") }()
		<-p.started
		result, err := a.ManagedSteer(managedSteerParams(a, "boundary"))
		if err != nil {
			t.Fatal(err)
		}
		a.queuePublishMu.Lock()
		close(p.release)
		clearedCh := make(chan protocol.InputQueue, 1)
		go func() { clearedCh <- a.ClearPendingInputs() }()
		a.queuePublishMu.Unlock()
		cleared := <-clearedCh
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := a.DrainEvents(t.Context()); err != nil {
			t.Fatal(err)
		}
		messages, err := store.Messages()
		if err != nil {
			t.Fatal(err)
		}
		persisted := slices.ContainsFunc(messages, func(m protocol.Message) bool {
			return m.Role == protocol.RoleUser && messageTextForTest(m) == "boundary"
		})
		discarded := slices.ContainsFunc(cleared.Items, func(i protocol.QueuedInput) bool { return i.ID == result.ItemID })
		acceptedEvents, deliveredEvents, discardedEvents := 0, 0, 0
		for _, change := range changes {
			if change.ItemID != result.ItemID {
				continue
			}
			switch change.Kind {
			case "steer_accepted":
				acceptedEvents++
			case "delivered":
				deliveredEvents++
			case "discarded":
				discardedEvents++
			}
		}
		if persisted == discarded || acceptedEvents != 1 || deliveredEvents+discardedEvents != 1 || (persisted && deliveredEvents != 1) || (discarded && discardedEvents != 1) {
			t.Fatalf("single winner: persisted=%v discarded=%v changes=%+v", persisted, discarded, changes)
		}
	}
}

func TestManagedSteerProviderFailureStillDeliversAcceptedNativeInput(t *testing.T) {
	p := newQueuedProvider([]protocol.StreamEvent{{Type: protocol.EvStreamError, Err: errors.New("provider failed")}})
	a, store := setup(t, p, nil, permission.ModeDeny)
	var changes []protocol.QueueControlChange
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.QueueControl != nil {
			changes = append(changes, ev.QueueControl.Change)
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	result, err := a.ManagedSteer(managedSteerParams(a, "native after failure"))
	if err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(messages, func(m protocol.Message) bool {
		return m.Role == protocol.RoleUser && messageTextForTest(m) == "native after failure"
	}) {
		t.Fatal("native accepted steering lost after provider error")
	}
	if !slices.ContainsFunc(changes, func(c protocol.QueueControlChange) bool {
		return c.Kind == "delivered" && c.ItemID == result.ItemID && c.UserEntryID != ""
	}) {
		t.Fatalf("missing durable delivery event: %+v", changes)
	}
}
