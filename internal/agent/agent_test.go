package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Scripted provider used in tests
// ---------------------------------------------------------------------------

type scriptedProvider struct {
	mu         sync.Mutex
	scripts    [][]protocol.StreamEvent // per Chat call
	call       int
	resolveErr error
	models     []protocol.Model
}

func (p *scriptedProvider) ID() string { return "scripted" }

func (p *scriptedProvider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.models, nil
}

func (p *scriptedProvider) Resolve(ctx context.Context, creds auth.Credential) error {
	return p.resolveErr
}

func (p *scriptedProvider) Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var evs []protocol.StreamEvent
	if len(p.scripts) > 0 {
		if p.call < len(p.scripts) {
			evs = p.scripts[p.call]
		} else {
			// Repeat the last script so tool loops can be driven until capped.
			evs = p.scripts[len(p.scripts)-1]
		}
	}
	p.call++
	return &sliceStream{evs: evs, ctx: ctx}, nil
}

type sliceStream struct {
	evs []protocol.StreamEvent
	i   int
	ctx context.Context
}

func (s *sliceStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.i >= len(s.evs) {
		return protocol.StreamEvent{}, io.EOF
	}
	ev := s.evs[s.i]
	s.i++
	return ev, nil
}

func (s *sliceStream) Close() error { return nil }

// ---------------------------------------------------------------------------
// Test tool
// ---------------------------------------------------------------------------

type testTool struct {
	name    string
	schema  protocol.ToolSchema
	runFunc func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult
}

func (t *testTool) Schema() protocol.ToolSchema { return t.schema }

func (t *testTool) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	return t.runFunc(ctx, args, host), nil
}

// ---------------------------------------------------------------------------
// Host
// ---------------------------------------------------------------------------

type testHost struct {
	cwd    string
	perm   permission.Service
	events []tools.ToolProgressEvent
}

func (h *testHost) CWD() string                    { return h.cwd }
func (h *testHost) Roots() []string                { return []string{h.cwd} }
func (h *testHost) Permission() permission.Service { return h.perm }
func (h *testHost) EmitProgress(e tools.ToolProgressEvent) {
	h.events = append(h.events, e)
}
func (h *testHost) Environ() []string { return nil }

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func setup(t *testing.T, prov provider.Provider, reg *tools.SimpleRegistry, permMode permission.Mode) (*Agent, *session.MemoryStore) {
	t.Helper()
	st := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	if reg == nil {
		reg = tools.NewRegistry()
	}
	perm := permission.NewService(permMode, nil)
	host := &testHost{cwd: t.TempDir(), perm: perm}
	a, err := New(Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "test system",
		Model:        protocol.Model{Provider: prov.ID(), ID: "m1", SupportsTools: true},
		Auth:         auth.NewMemoryStoreForTest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, st
}

func toolCallBlock(id, name string, args map[string]any) protocol.ContentBlock {
	raw, _ := json.Marshal(args)
	return protocol.ContentBlock{
		Type:       protocol.BlockToolCall,
		ToolCallID: id,
		Name:       name,
		Arguments:  raw,
	}
}

// TestSingleTextTurn: provider returns one text delta then done.
func TestSingleTextTurn(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "hello "},
		{Type: protocol.EvStreamTextDelta, Text: "world"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)

	var got string
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvTextDelta {
			got += ev.Text
		}
	})

	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
	msgs, _ := st.Messages()
	if len(msgs) != 2 { // user + assistant
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Role != protocol.RoleAssistant || msgs[1].StopReason != protocol.StopStop {
		t.Fatalf("bad assistant message: %+v", msgs[1])
	}
}

// TestToolRoundTrip: tool_use -> tool result -> final text.
func TestToolRoundTrip(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("file contents here")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(readTool); err != nil {
		t.Fatal(err)
	}

	_ = toolCallBlock("call-1", "read", map[string]any{"path": "a.txt"})
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDelta, ToolCallID: "call-1", ToolName: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "call-1", ToolName: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "done reading"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)

	var toolResults int
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvToolEnd {
			toolResults++
		}
	})

	if err := a.Prompt(context.Background(), "read a.txt"); err != nil {
		t.Fatal(err)
	}
	if toolResults != 1 {
		t.Fatalf("expected 1 tool end, got %d", toolResults)
	}
	msgs, _ := st.Messages()
	if len(msgs) != 4 { // user, assistant(tool_use), tool_result, assistant(final)
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	// Message 2 is the tool result.
	tr := msgs[2]
	if tr.Role != protocol.RoleTool || tr.ToolCallID != "call-1" {
		t.Fatalf("bad tool result message: %+v", tr)
	}
	if tr.IsError {
		t.Fatal("tool result should not be error")
	}
	// Final assistant contains "done reading".
	if msgs[3].Content[0].Text != "done reading" {
		t.Fatalf("final assistant text wrong: %+v", msgs[3].Content)
	}
}

// TestToolDenied: permission deny produces error tool result and loop still finishes.
func TestToolDenied(t *testing.T) {
	writeTool := &testTool{
		name:   "write",
		schema: protocol.ToolSchema{Name: "write", Description: "write", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("SHOULD NOT RUN")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(writeTool)

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "write", Arguments: json.RawMessage(`{"path":"x"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "denied ok"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)

	if err := a.Prompt(context.Background(), "write x"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	tr := msgs[2]
	if !tr.IsError {
		t.Fatalf("expected denied tool result to be error: %+v", tr)
	}
}

// TestUnknownTool: unknown tool name yields error result without crashing.
func TestUnknownTool(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "nonexistent", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "recovered"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	if !msgs[2].IsError {
		t.Fatal("expected error result for unknown tool")
	}
}

// TestMalformedArguments: invalid JSON args produce error tool result.
func TestMalformedArguments(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("ran")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(readTool)

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{"bad json`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "done"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	if !msgs[2].IsError {
		t.Fatal("expected error result for malformed args")
	}
}

// TestMaxTurns: loop stops at turn cap.
func TestMaxTurns(t *testing.T) {
	loop := &testTool{
		name:   "loop",
		schema: protocol.ToolSchema{Name: "loop", Description: "l", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("loop")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(loop)

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "loop", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
	}}
	// Same script for all calls -> infinite tool loop, capped by MaxTurns=2.
	st := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	perm := permission.NewService(permission.ModeDeny, nil)
	host := &testHost{cwd: t.TempDir(), perm: perm}
	a, err := New(Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "s",
		Model:        protocol.Model{Provider: "scripted", ID: "m1"},
		MaxTurns:     2,
		Auth:         auth.NewMemoryStoreForTest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = a.Prompt(context.Background(), "x")
	if err == nil {
		t.Fatal("expected max turns error")
	}
	if err.Error() != "agent: max turns reached" {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestAbort: context cancellation aborts mid-stream and records aborted assistant.
func TestAbort(t *testing.T) {
	prov := &blockingProvider{}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	if err := a.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + aborted assistant), got %d", len(msgs))
	}
	asst := msgs[1]
	if asst.Role != protocol.RoleAssistant || asst.StopReason != protocol.StopAborted {
		t.Fatalf("expected aborted assistant: %+v", asst)
	}
}

// blockingProvider returns a stream that never yields, so ctx cancellation
// surfaces as the stream error path.
type blockingProvider struct {
	started chan struct{} // closed when Chat is entered
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{})}
}

func (p *blockingProvider) ID() string { return "blocking" }
func (p *blockingProvider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *blockingProvider) Resolve(ctx context.Context, creds auth.Credential) error { return nil }
func (p *blockingProvider) Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error) {
	if p.started != nil {
		select {
		case <-p.started:
		default:
			close(p.started)
		}
	}
	return &blockingStream{ctx: ctx}, nil
}

type blockingStream struct{ ctx context.Context }

func (s *blockingStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	<-ctx.Done()
	return protocol.StreamEvent{}, ctx.Err()
}
func (s *blockingStream) Close() error { return nil }

// TestEventOrder: tool_start before tool_end; turn_done last.
func TestEventOrder(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("ok")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(readTool)
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDelta, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "final"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, _ := setup(t, prov, reg, permission.ModeDeny)

	var order []string
	a.Subscribe(func(ev protocol.AgentEvent) {
		switch ev.Type {
		case protocol.EvToolStart:
			order = append(order, "tool_start")
		case protocol.EvToolEnd:
			order = append(order, "tool_end")
		case protocol.EvTurnDone:
			order = append(order, "turn_done")
		}
	})
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	want := []string{"tool_start", "tool_end", "turn_done"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("event order = %v, want %v", order, want)
	}
}

// TestEmptyPrompt rejected.
func TestEmptyPrompt(t *testing.T) {
	prov := &scriptedProvider{}
	a, _ := setup(t, prov, nil, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "   "); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

// TestAlreadyRunning: a second Prompt while a turn is in flight errors out.
func TestAlreadyRunning(t *testing.T) {
	prov := newBlockingProvider()
	a, _ := setup(t, prov, nil, permission.ModeDeny)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Prompt(ctx, "first")
	}()
	// Wait until the provider Chat is entered, then the running flag is set.
	<-prov.started
	// Give the agent a moment to publish the running state.
	err := a.Prompt(context.Background(), "second")
	cancel()
	if err == nil {
		t.Fatal("expected already-running error")
	}
	if err2 := <-done; err2 != nil {
		t.Fatalf("first prompt should abort cleanly, got %v", err2)
	}
}

// TestConcurrentPromptNoGhostMessage: a second Prompt while a turn is in
// flight must fail with "already running" WITHOUT persisting a ghost user
// message that would never be processed.
func TestConcurrentPromptNoGhostMessage(t *testing.T) {
	prov := newBlockingProvider()
	a, st := setup(t, prov, nil, permission.ModeDeny)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Prompt(ctx, "first")
	}()
	<-prov.started // first turn claimed the running flag

	err := a.Prompt(context.Background(), "second")
	if err == nil {
		t.Fatal("expected already-running error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("wrong error: %v", err)
	}

	// No ghost: exactly the first user message is persisted.
	msgs, _ := st.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 user message (no ghost), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != protocol.RoleUser || msgs[0].Content[0].Text != "first" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}

	// First turn aborts cleanly on cancel.
	cancel()
	if err2 := <-done; err2 != nil {
		t.Fatalf("first prompt should abort cleanly, got %v", err2)
	}
}

// TestToolLoopCancelStopsRemaining: cancelling the context during the first
// tool execution must stop the remaining tool calls (no second run) and
// surface the context error from Prompt.
func TestToolLoopCancelStopsRemaining(t *testing.T) {
	runCount := 0
	var once sync.Once
	cancelFirst := func() {}

	tool := &testTool{
		// Named "read" so the permission gate (RiskRead) always allows it
		// under deny mode; the loop-cancel behavior is what we exercise.
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			runCount++
			once.Do(func() {
				cancelFirst()
			})
			return tools.TextResult("ran")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "c2", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}}}
	a, _ := setup(t, prov, reg, permission.ModeDeny)

	ctx, cancel := context.WithCancel(context.Background())
	cancelFirst = cancel

	err := a.Prompt(ctx, "run tools")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prompt = %v, want context.Canceled", err)
	}
	if runCount != 1 {
		t.Fatalf("tool ran %d times, want exactly 1 (second call must be skipped)", runCount)
	}
}

// TestToolCallLimitEmitsErrorResults: when CallLimit is exceeded, the skipped
// tool call must still get an error tool_result so no tool_call is left
// dangling without a result.
func TestToolCallLimitEmitsErrorResults(t *testing.T) {
	ran := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			ran++
			return tools.TextResult("ok")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c2", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "finished"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	st := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	perm := permission.NewService(permission.ModeDeny, nil)
	host := &testHost{cwd: t.TempDir(), perm: perm}
	a, err := New(Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "s",
		Model:        protocol.Model{Provider: "scripted", ID: "m1"},
		CallLimit:    1,
		Auth:         auth.NewMemoryStoreForTest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("tool ran %d times, want 1 (limit)", ran)
	}
	msgs, _ := st.Messages()
	// user, assistant(tool_use), tool_result(c1), tool_result(c2 error), assistant(final)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(msgs), msgs)
	}
	// Both tool calls must have results (no dangling tool_call).
	if msgs[2].Role != protocol.RoleTool || msgs[2].ToolCallID != "c1" {
		t.Fatalf("msgs[2] = %+v, want tool_result for c1", msgs[2])
	}
	if msgs[3].Role != protocol.RoleTool || msgs[3].ToolCallID != "c2" {
		t.Fatalf("msgs[3] = %+v, want tool_result for c2", msgs[3])
	}
	// The skipped (limited) call must be an error result.
	if !msgs[3].IsError {
		t.Fatalf("skipped call result should be IsError: %+v", msgs[3])
	}
	// The executed call is a normal result.
	if msgs[2].IsError {
		t.Fatalf("executed call result should not be IsError: %+v", msgs[2])
	}
	if msgs[4].Role != protocol.RoleAssistant || msgs[4].Content[0].Text != "finished" {
		t.Fatalf("final assistant message wrong: %+v", msgs[4])
	}
}
