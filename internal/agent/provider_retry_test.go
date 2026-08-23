package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type retryStartupProvider struct {
	mu         sync.Mutex
	failures   int
	calls      int
	retryAfter time.Duration
}

func (*retryStartupProvider) ID() string                                           { return "retry-startup" }
func (*retryStartupProvider) ListModels(context.Context) ([]protocol.Model, error) { return nil, nil }
func (p *retryStartupProvider) Chat(ctx context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call <= p.failures {
		return nil, &providerpkg.AdvisedError{Err: errors.New("temporary outage"), Advice: providerpkg.RetryAdvice{Kind: providerpkg.RetryTransient, RetryAfter: p.retryAfter}}
	}
	return &sliceStream{ctx: ctx, evs: []protocol.StreamEvent{{Type: protocol.EvStreamTextDelta, Text: "recovered"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}, nil
}

func fastRetryProfile(attempts int) RetryProfile {
	return RetryProfile{MaxAttempts: attempts, MaxElapsed: time.Second, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
}

func TestProviderRetryRecoversRepeatedStartupFailuresAndEmitsNonterminalEvents(t *testing.T) {
	p := &retryStartupProvider{failures: 3}
	a, _ := setup(t, p, nil, permission.ModeDeny)
	a.opts.MaxTurns = 1
	a.opts.Retry.Normal = fastRetryProfile(5)
	var retries, finalErrors int
	a.Subscribe(func(event protocol.AgentEvent) {
		switch event.Type {
		case protocol.EvProviderRetry:
			retries++
			if event.ProviderRetry == nil || event.ProviderRetry.Phase != "pre_activity" {
				t.Errorf("retry event=%+v", event)
			}
		case protocol.EvError:
			finalErrors++
		}
	})
	if err := a.Prompt(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.calls != 4 || retries != 3 || finalErrors != 0 {
		t.Fatalf("calls=%d retries=%d errors=%d", p.calls, retries, finalErrors)
	}
}

func TestRetryAfterBeyondRemainingBudgetStopsWithoutRetryingEarly(t *testing.T) {
	p := &retryStartupProvider{failures: 1, retryAfter: 50 * time.Millisecond}
	a, _ := setup(t, p, nil, permission.ModeDeny)
	a.opts.Retry.Normal = RetryProfile{MaxAttempts: 3, MaxElapsed: 10 * time.Millisecond, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
	if err := a.Prompt(context.Background(), "respect server delay"); err == nil {
		t.Fatal("expected failure when Retry-After exceeds remaining budget")
	}
	if p.calls != 1 {
		t.Fatalf("provider calls=%d", p.calls)
	}
}

func TestSuccessfulProviderRoundResetsConsecutiveFailureBackoff(t *testing.T) {
	transient := responsesapi.NewResponseError("test", 0, "network", "network_error", "")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamError, Err: transient}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "read-1", ToolName: "read_once", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamError, Err: transient}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	reg := tools.NewRegistry()
	if err := reg.Register(&testTool{name: "read_once", schema: protocol.ToolSchema{Name: "read_once", Parameters: json.RawMessage(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult { return tools.TextResult("ok") }}); err != nil {
		t.Fatal(err)
	}
	a, _ := setup(t, p, reg, permission.ModeAllow)
	a.opts.Retry.Normal = fastRetryProfile(2)
	var attempts []int
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.ProviderRetry != nil {
			attempts = append(attempts, event.ProviderRetry.Attempt)
		}
	})
	if err := a.Prompt(context.Background(), "reset retries"); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0] != 2 || attempts[1] != 2 {
		t.Fatalf("retry attempts=%v", attempts)
	}
}

func TestProviderRetryExhaustionPublishesOneFinalError(t *testing.T) {
	p := &retryStartupProvider{failures: 10}
	a, _ := setup(t, p, nil, permission.ModeDeny)
	a.opts.Retry.Normal = fastRetryProfile(3)
	var retries, finalErrors int
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvProviderRetry {
			retries++
		}
		if event.Type == protocol.EvError {
			finalErrors++
		}
	})
	if err := a.Prompt(context.Background(), "fail"); err == nil {
		t.Fatal("expected retry exhaustion")
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 || retries != 2 || finalErrors != 1 {
		t.Fatalf("calls=%d retries=%d final_errors=%d", p.calls, retries, finalErrors)
	}
}

func TestCompactionProviderFailureUsesCentralRetryPolicy(t *testing.T) {
	transient := responsesapi.NewResponseError("test", 0, "network", "network_error", "")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamError, Err: transient}},
		{{Type: protocol.EvStreamTextDelta, Text: "# Working State Checkpoint\nrecovered"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, _ := setup(t, p, nil, permission.ModeDeny)
	a.opts.Retry.Normal = fastRetryProfile(2)
	retries := 0
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvProviderRetry {
			retries++
		}
	})
	summary, err := a.summarizeForCompaction(context.Background(), []protocol.Message{protocol.NewUserMessage("u", "", "old")})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "recovered") || p.call != 2 || retries != 1 {
		t.Fatalf("summary=%q calls=%d retries=%d", summary, p.call, retries)
	}
}

func TestPartialStreamRecoveryNeverDispatchesIncompleteToolCall(t *testing.T) {
	transient := responsesapi.NewResponseError("test", 0, "stream lost", "stream_truncated", "")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: "partial"}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "write-1", ToolName: "write_once", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamError, Err: transient}},
		{{Type: protocol.EvStreamTextDelta, Text: "recovered"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	runs := 0
	reg := tools.NewRegistry()
	if err := reg.Register(&testTool{name: "write_once", schema: protocol.ToolSchema{Name: "write_once", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		runs++
		return tools.TextResult("written")
	}}); err != nil {
		t.Fatal(err)
	}
	a, store := setup(t, p, reg, permission.ModeAllow)
	a.opts.Retry.Normal = fastRetryProfile(2)
	var phase string
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.ProviderRetry != nil {
			phase = event.ProviderRetry.Phase
		}
	})
	if err := a.Prompt(context.Background(), "recover safely"); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || phase != "recovery" || len(p.requests) != 2 {
		t.Fatalf("runs=%d phase=%q requests=%d", runs, phase, len(p.requests))
	}
	messages, _ := store.Messages()
	for _, message := range messages {
		if message.Role == protocol.RoleTool {
			t.Fatalf("incomplete failed-stream tool was dispatched: %+v", messages)
		}
	}
	foundRecovery := false
	for _, fragment := range p.requests[1].InternalContext {
		if fragment.Source == "provider-recovery" {
			foundRecovery = true
		}
	}
	if !foundRecovery {
		t.Fatalf("missing recovery context: %+v", p.requests[1].InternalContext)
	}
}

func TestCompletedMutatingToolIsNotHostReplayedAfterLaterProviderFailure(t *testing.T) {
	transient := responsesapi.NewResponseError("test", 0, "upstream unavailable", "network_error", "")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "write-1", ToolName: "write_once", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamError, Err: transient}},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	runs := 0
	reg := tools.NewRegistry()
	if err := reg.Register(&testTool{name: "write_once", schema: protocol.ToolSchema{Name: "write_once", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		runs++
		return tools.TextResult("written")
	}}); err != nil {
		t.Fatal(err)
	}
	a, _ := setup(t, p, reg, permission.ModeAllow)
	a.opts.Retry.Normal = fastRetryProfile(2)
	if err := a.Prompt(context.Background(), "write once"); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || len(p.requests) != 3 {
		t.Fatalf("tool runs=%d requests=%d", runs, len(p.requests))
	}
	foundResult := false
	for _, message := range p.requests[2].Messages {
		if message.Role == protocol.RoleTool && message.ToolCallID == "write-1" {
			foundResult = true
		}
	}
	if !foundResult {
		t.Fatalf("completed tool result missing from recovery request: %+v", p.requests[2].Messages)
	}
}
