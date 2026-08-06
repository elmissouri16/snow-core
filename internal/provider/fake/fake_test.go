package fake

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestReplayOrderMatchesScript(t *testing.T) {
	script := []Step{
		{Kind: StepThinking, Thinking: "let me think"},
		{Kind: StepText, Text: "hello "},
		{Kind: StepText, Text: "world"},
		{Kind: StepToolCall, ToolCallID: "tc1", ToolName: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
		{Kind: StepUsage, Usage: &protocol.Usage{Input: 10, Output: 5, Total: 15}},
		{Kind: StepDone, Stop: protocol.StopStop},
	}
	p := New(script)
	ctx := context.Background()
	es, err := p.Chat(ctx, auth.Credential{}, protocol.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer es.Close()

	got := []protocol.StreamEvent{}
	for {
		ev, err := es.Next(ctx)
		if err != nil {
			if errors.Is(err, ErrExhausted) {
				break
			}
			t.Fatalf("Next: %v", err)
		}
		got = append(got, ev)
	}

	wantTypes := []protocol.StreamEventType{
		protocol.EvStreamThinkingDelta,
		protocol.EvStreamTextDelta,
		protocol.EvStreamTextDelta,
		protocol.EvStreamToolCallDone,
		protocol.EvStreamUsage,
		protocol.EvStreamDone,
	}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events, want %d", len(got), len(wantTypes))
	}
	for i, w := range wantTypes {
		if got[i].Type != w {
			t.Fatalf("event %d type = %q, want %q", i, got[i].Type, w)
		}
	}
	if got[0].Text != "let me think" {
		t.Fatalf("thinking text = %q", got[0].Text)
	}
	if got[1].Text != "hello " || got[2].Text != "world" {
		t.Fatalf("text deltas = %q %q", got[1].Text, got[2].Text)
	}
	if got[3].ToolCallID != "tc1" || got[3].ToolName != "read" {
		t.Fatalf("tool call = %+v", got[3])
	}
	if got[4].Usage == nil || got[4].Usage.Input != 10 || got[4].Usage.Output != 5 {
		t.Fatalf("usage = %+v", got[4].Usage)
	}
	if got[5].StopReason != protocol.StopStop {
		t.Fatalf("stop = %q, want stop", got[5].StopReason)
	}
}

func TestToolCallArgumentsParsed(t *testing.T) {
	args := json.RawMessage(`{"path":"src/main.go","limit":20}`)
	script := []Step{
		{Kind: StepToolCall, ToolCallID: "tc9", ToolName: "read", Arguments: args},
		{Kind: StepDone},
	}
	p := New(script)
	es, err := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ev, err := es.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(ev.Arguments, &parsed); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if parsed.Path != "src/main.go" || parsed.Limit != 20 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestScriptReplaysOnEveryCall(t *testing.T) {
	script := []Step{{Kind: StepText, Text: "x"}, {Kind: StepDone}}
	p := New(script)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		es, err := p.Chat(ctx, auth.Credential{}, protocol.ChatRequest{})
		if err != nil {
			t.Fatal(err)
		}
		first, err := es.Next(ctx)
		if err != nil {
			t.Fatalf("call %d first event: %v", i, err)
		}
		if first.Type != protocol.EvStreamTextDelta || first.Text != "x" {
			t.Fatalf("call %d first = %+v", i, first)
		}
		es.Close()
	}
	if got := p.CallCount(); got != 3 {
		t.Fatalf("CallCount = %d, want 3", got)
	}
}

func TestNewWithModelsCatalog(t *testing.T) {
	models := []protocol.Model{
		{Provider: "fake", ID: "m1", SupportsTools: true},
		{Provider: "fake", ID: "m2"},
	}
	p := NewWithModels(models)
	got, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "m1" || got[1].ID != "m2" {
		t.Fatalf("models = %+v", got)
	}
}

func TestDefaultModels(t *testing.T) {
	p := New(nil)
	got, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("default models len = %d, want 1", len(got))
	}
	if got[0].Provider != "fake" || got[0].ID != "fake-1" || !got[0].SupportsTools {
		t.Fatalf("default model = %+v", got[0])
	}
	if p.ID() != "fake" {
		t.Fatalf("ID() = %q, want fake", p.ID())
	}
}

func TestResolveNil(t *testing.T) {
	p := New(nil)
	if err := p.Resolve(context.Background(), auth.Credential{}); err != nil {
		t.Fatalf("Resolve should always succeed: %v", err)
	}
}

func TestRecordedCalls(t *testing.T) {
	p := NewRecorded()
	ctx := context.Background()
	tools := []protocol.ToolSchema{{Name: "read", Description: "read a file"}}
	msgs := []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "hi"}}},
	}
	req1 := protocol.ChatRequest{Model: protocol.Model{ID: "fake-1"}, Messages: msgs, Tools: tools}
	req2 := protocol.ChatRequest{Model: protocol.Model{ID: "fake-1"}, Messages: msgs}

	es1, err := p.Chat(ctx, auth.Credential{}, req1)
	if err != nil {
		t.Fatal(err)
	}
	es1.Close()
	es2, err := p.Chat(ctx, auth.Credential{}, req2)
	if err != nil {
		t.Fatal(err)
	}
	es2.Close()

	calls := p.RecordedCalls()
	if len(calls) != 2 {
		t.Fatalf("RecordedCalls len = %d, want 2", len(calls))
	}
	if len(calls[0].Tools) != 1 || calls[0].Tools[0].Name != "read" {
		t.Fatalf("call 0 tools = %+v", calls[0].Tools)
	}
	if len(calls[0].Messages) != 1 || calls[0].Messages[0].Role != protocol.RoleUser {
		t.Fatalf("call 0 messages = %+v", calls[0].Messages)
	}
	// The recorded slice must be a copy: mutating the returned slice must
	// not affect the provider.
	calls[0].Messages = nil
	if len(p.RecordedCalls()[0].Messages) != 1 {
		t.Fatal("RecordedCalls must return a copy")
	}
}

func TestNonRecordedHasNoCalls(t *testing.T) {
	p := New(nil)
	if calls := p.RecordedCalls(); len(calls) != 0 {
		t.Fatalf("unexpected recorded calls: %d", len(calls))
	}
}

func TestStepErrorEvent(t *testing.T) {
	script := []Step{{Kind: StepError, Err: errors.New("boom")}}
	p := New(script)
	es, _ := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{})
	defer es.Close()
	ev, err := es.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != protocol.EvStreamError {
		t.Fatalf("type = %q, want error", ev.Type)
	}
	if ev.Err == nil || ev.Err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", ev.Err)
	}
}

func TestStreamCancel(t *testing.T) {
	p := New([]Step{{Kind: StepText, Text: "a"}, {Kind: StepText, Text: "b"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	es, _ := p.Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{})
	defer es.Close()
	if _, err := es.Next(ctx); err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
