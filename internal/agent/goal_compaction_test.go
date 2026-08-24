package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestExactBudgetGetsExactlyOneWrapTurn(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 1}}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}, {{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, c, _ := goalAgent(t, p)
	budget := int64(1)
	g, _ := c.Create("budgeted", &budget, false)
	a.ContinueGoal()
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.RLock()
		running := a.autoRunning
		a.mu.RUnlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("budget continuation did not stop")
		}
		time.Sleep(time.Millisecond)
	}
	got, _ := c.Get()
	if got.GoalID != g.GoalID || got.Status != protocol.GoalBudgetLimited || p.call != 2 {
		t.Fatalf("goal=%+v calls=%d", got, p.call)
	}
	if len(p.requests) < 2 || len(p.requests[1].InternalContext) == 0 || !strings.Contains(p.requests[1].InternalContext[0].Text, "budget has been reached") {
		t.Fatalf("requests=%+v", p.requests)
	}
}

func TestAutoCompactAdmittedBoundaryRequiresMatchingGoalSnapshot(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{AutoThresholdPercent: 90}
	first, _ := c.Create("first", nil, false)
	a.mu.Lock()
	a.goalAtTurn = first
	a.latestContextTokens = 90
	a.mu.Unlock()
	if _, err := c.Create("replacement", nil, true); err != nil {
		t.Fatal(err)
	}
	compacted, err := a.autoCompactAdmittedBoundary(context.Background(), nil)
	if err != nil || compacted {
		t.Fatalf("mismatched goal compacted=%v err=%v", compacted, err)
	}
}

func TestSetSessionClearsCompactionUsageCache(t *testing.T) {
	p := &scriptedProvider{}
	a, _, _ := goalAgent(t, p)
	a.mu.Lock()
	a.latestContextTokens = 90
	a.mu.Unlock()
	next := session.NewMemoryStore(session.Options{ID: "next"})
	if err := a.SetSession(next); err != nil {
		t.Fatal(err)
	}
	a.mu.RLock()
	got := a.latestContextTokens
	a.mu.RUnlock()
	if got != 0 {
		t.Fatalf("session switch retained context usage %d", got)
	}
}

func TestLatestPersistedContextTokensUsesOnlyPostCompactionUsage(t *testing.T) {
	summary := protocol.Message{ID: "compaction-marker", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("Conversation summary:\nold")}}
	retained := protocol.NewAssistantMessage("retained", "old-parent", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("old")}, protocol.StopStop, &protocol.Usage{Input: 99})
	post := protocol.NewAssistantMessage("post", "marker", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("new")}, protocol.StopStop, &protocol.Usage{Input: 91})
	if got := latestPersistedContextTokens([]protocol.Message{summary, retained}); got != 0 {
		t.Fatalf("retained pre-compaction usage=%d want 0", got)
	}
	if got := latestPersistedContextTokens([]protocol.Message{summary, retained, post}); got != 91 {
		t.Fatalf("post-compaction usage=%d want 91", got)
	}
}

func TestGoalAutoCompactsAtConfiguredContextThreshold(t *testing.T) {
	p := &scriptedProvider{}
	a, c, st := goalAgent(t, p)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 90}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("auto-compact-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	g, err := c.Create("compact then complete", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	p.scripts = [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: "working"}, {Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 90, Output: 5, Total: 95}}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "goal summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "done", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	var automaticStarted, automaticDone bool
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvCompactionStarted {
			automaticStarted = strings.Contains(event.Message, "90%")
		}
		if event.Type == protocol.EvCompactionDone && event.Compaction != nil {
			automaticDone = event.Compaction.Automatic
		}
	})
	a.ContinueGoal()
	deadline := time.Now().Add(3 * time.Second)
	for a.IsRunning() || func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.autoRunning }() {
		if time.Now().After(deadline) {
			t.Fatal("goal auto-compaction did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	got, _ := c.Get()
	if got.Status != protocol.GoalComplete || p.call != 4 {
		t.Fatalf("goal=%+v calls=%d requests=%+v", got, p.call, p.requests)
	}
	if !automaticStarted || !automaticDone {
		t.Fatalf("automatic events started=%v done=%v", automaticStarted, automaticDone)
	}
	projected, err := st.ContextMessages()
	if err != nil || len(projected) == 0 || projected[0].Role != protocol.RoleCustom {
		t.Fatalf("projected context=%+v err=%v", projected, err)
	}
}

func TestGoalAutoCompactsInsideSingleToolChain(t *testing.T) {
	p := &scriptedProvider{}
	a, c, st := goalAgent(t, p)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 90}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("chain-history-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	g, _ := c.Create("compact within tool chain", nil, false)
	p.scripts = [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 90, Output: 1, Total: 91}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "read", ToolName: "get_goal", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamTextDelta, Text: "goal summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "done", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	var compacted bool
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvCompactionDone && event.Compaction != nil && event.Compaction.Automatic {
			compacted = true
		}
	})
	a.ContinueGoal()
	deadline := time.Now().Add(3 * time.Second)
	for a.IsRunning() || func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.autoRunning }() {
		if time.Now().After(deadline) {
			t.Fatal("in-chain auto-compaction did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	goal, _ := c.Get()
	if goal.Status != protocol.GoalComplete || !compacted || p.call != 4 {
		t.Fatalf("goal=%+v compacted=%v calls=%d", goal, compacted, p.call)
	}
}

func TestGoalAutoCompactionBlocksWhenNoTurnsCanBeCompacted(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "working"},
		{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 90}},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, c, _ := goalAgent(t, p)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, Fallback: "local", AutoThresholdPercent: 90}
	g, _ := c.Create("cannot compact yet", nil, false)
	a.ContinueGoal()
	deadline := time.Now().Add(2 * time.Second)
	for a.IsRunning() || func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.autoRunning }() {
		if time.Now().After(deadline) {
			t.Fatal("no-candidate auto-compaction did not stop")
		}
		time.Sleep(time.Millisecond)
	}
	got, _ := c.Get()
	if got.GoalID != g.GoalID || got.Status != protocol.GoalBlocked || !strings.Contains(got.BlockedReason, "Automatic compaction failed") || p.call != 1 {
		t.Fatalf("goal=%+v calls=%d", got, p.call)
	}
}

func TestGoalAutoCompactionIgnoresBelowThresholdStaleUsageWhenLatestRequestOmitsUsage(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{AutoThresholdPercent: 99}
	g, _ := c.Create("latest request has no usage", nil, false)
	p.scripts = [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 90}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "read", ToolName: "get_goal", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "done", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}
	a.ContinueGoal()
	deadline := time.Now().Add(2 * time.Second)
	for a.IsRunning() || func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.autoRunning }() {
		if time.Now().After(deadline) {
			t.Fatal("stale-usage goal did not stop")
		}
		time.Sleep(time.Millisecond)
	}
	got, _ := c.Get()
	if got.Status != protocol.GoalComplete || p.call != 3 {
		t.Fatalf("goal=%+v calls=%d", got, p.call)
	}
}

func TestAbortDuringGoalAutoCompactionDefersWithoutBlocking(t *testing.T) {
	provider := &blockingSummaryProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, c, st := goalAgent(t, &scriptedProvider{})
	a.opts.Provider = provider
	a.model = protocol.Model{Provider: provider.ID(), ID: "m", SupportsTools: true, ContextWindow: 100}
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, Fallback: "local", AutoThresholdPercent: 90}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("abort-auto-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Create("abort compaction", nil, false); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.latestContextTokens = 90
	a.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := a.autoCompactGoalBoundary(context.Background()); done <- err }()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("automatic summary did not start")
	}
	abortDone := make(chan error, 1)
	go func() { abortDone <- a.StopGoal(context.Background(), true) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("auto compact err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("automatic summary did not cancel")
	}
	if err := <-abortDone; err != nil {
		t.Fatal(err)
	}
	goal, _ := c.Get()
	deferred, _ := c.Deferred()
	if goal.Status != protocol.GoalActive || !deferred {
		t.Fatalf("goal=%+v deferred=%v", goal, deferred)
	}
}

func TestGoalAutoCompactionCanBeDisabled(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	a.model.ContextWindow = 100
	a.opts.Compaction.AutoThresholdPercent = 0
	g, _ := c.Create("complete without compacting", nil, false)
	p.scripts = [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Total: 100}},
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "done", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}, {{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}
	a.ContinueGoal()
	deadline := time.Now().Add(2 * time.Second)
	for a.IsRunning() || func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.autoRunning }() {
		if time.Now().After(deadline) {
			t.Fatal("disabled auto-compaction goal did not stop")
		}
		time.Sleep(time.Millisecond)
	}
	if p.call != 2 {
		t.Fatalf("provider calls=%d, want no compaction call", p.call)
	}
}

func TestContinueGoalLaunchesAndStopsAtComplete(t *testing.T) {
	p := &scriptedProvider{}
	a, c, _ := goalAgent(t, p)
	g, _ := c.Create("complete", nil, false)
	p.scripts = [][]protocol.StreamEvent{{{Type: protocol.EvStreamToolCallDone, ToolCallID: "u", ToolName: "update_goal", Arguments: []byte(`{"goal_id":"` + g.GoalID + `","status":"complete"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}, {{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}
	a.ContinueGoal()
	deadline := time.Now().Add(2 * time.Second)
	for a.IsRunning() || func() bool { a.mu.RLock(); defer a.mu.RUnlock(); return a.autoRunning }() {
		if time.Now().After(deadline) {
			t.Fatal("continuation did not stop")
		}
		time.Sleep(time.Millisecond)
	}
	got, _ := c.Get()
	if got.Status != protocol.GoalComplete {
		t.Fatalf("goal=%+v", got)
	}
	if p.call != 2 {
		t.Fatalf("provider calls=%d", p.call)
	}
}
