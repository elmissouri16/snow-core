package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	"github.com/snow-core/snow/pkg/protocol"
)

// e2eProvider is deliberately small, but otherwise behaves like a provider
// adapter: it receives the complete request, returns a stream, and supports
// multiple provider calls for one agent prompt. Unlike the unit-test provider
// in agent_test.go, it records requests so these tests can verify the complete
// model-facing conversation, including ordered tool results.
type e2eProvider struct {
	mu         sync.Mutex
	scripts    [][]protocol.StreamEvent
	requests   []protocol.ChatRequest
	resolveErr error
	chatErr    error
}

func (p *e2eProvider) ID() string { return "e2e" }

func (p *e2eProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return []protocol.Model{{Provider: p.ID(), ID: "e2e-model", SupportsTools: true}}, nil
}

func (p *e2eProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, p.resolveErr
}

func (p *e2eProvider) Chat(_ context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if p.resolveErr != nil {
		return nil, fmt.Errorf("provider resolve: %w", p.resolveErr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	// Copy the request and its slices before the caller can reuse them.
	copyReq := req
	copyReq.Messages = append([]protocol.Message(nil), req.Messages...)
	copyReq.Tools = append([]protocol.ToolSchema(nil), req.Tools...)
	p.requests = append(p.requests, copyReq)

	var events []protocol.StreamEvent
	if len(p.scripts) > 0 {
		index := len(p.requests) - 1
		if index >= len(p.scripts) {
			index = len(p.scripts) - 1
		}
		events = append([]protocol.StreamEvent(nil), p.scripts[index]...)
	}
	return &e2eStream{events: events}, nil
}

func (p *e2eProvider) Requests() []protocol.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]protocol.ChatRequest(nil), p.requests...)
}

type e2eStream struct {
	mu     sync.Mutex
	events []protocol.StreamEvent
	index  int
}

func (s *e2eStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index == len(s.events) {
		return protocol.StreamEvent{}, io.EOF
	}
	ev := s.events[s.index]
	s.index++
	return ev, nil
}

func (*e2eStream) Close() error { return nil }

func e2eCall(id, name string, args any) protocol.StreamEvent {
	data, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return protocol.StreamEvent{
		Type:       protocol.EvStreamToolCallDone,
		ToolCallID: id,
		ToolName:   name,
		Arguments:  data,
	}
}

func newBuiltinE2EAgent(t *testing.T, root string, p provider.Provider, st session.Store, mode permission.Mode, asker permission.Asker) *Agent {
	t.Helper()
	reg := tools.NewRegistry()
	if err := builtin.RegisterBuiltins(reg, builtin.Options{
		Roots:       []string{root},
		BashTimeout: 5 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	perm := permission.NewService(mode, asker)
	host := &testHost{cwd: root, perm: perm}
	a, err := New(Options{
		Provider:     p,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "end-to-end system prompt",
		Thinking:     protocol.ThinkingHigh,
		Model: protocol.Model{
			Provider:         p.ID(),
			ID:               "e2e-model",
			SupportsTools:    true,
			SupportsThinking: true,
			ThinkingLevels:   []protocol.ThinkingLevel{protocol.ThinkingHigh},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type e2eUserInputHost struct {
	*testHost
	emit     func(protocol.UserInputRequest)
	requests []protocol.UserInputRequest
}

func (h *e2eUserInputHost) RequestUserInput(_ context.Context, request protocol.UserInputRequest) (protocol.UserInputResponse, error) {
	h.requests = append(h.requests, request)
	if h.emit != nil {
		h.emit(request)
	}
	return protocol.UserInputResponse{RequestID: request.ID, Answers: []protocol.UserInputAnswer{
		{QuestionID: "format", Answer: "JSON"},
		{QuestionID: "notes", Answer: "keep comments"},
	}}, nil
}

func TestAgentEndToEndAskUserContinuesWithAnswers(t *testing.T) {
	root := t.TempDir()
	p := &e2eProvider{scripts: [][]protocol.StreamEvent{
		{
			e2eCall("ask-1", "ask_user", map[string]any{"questions": []map[string]any{
				{"id": "format", "header": "Format", "question": "Which format?", "options": []map[string]string{{"label": "JSON", "description": "Machine readable"}, {"label": "Text", "description": "Human readable"}}},
				{"id": "notes", "header": "Notes", "question": "Anything else?"},
			}}),
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "using JSON"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	reg := tools.NewRegistry()
	if err := builtin.RegisterBuiltins(reg, builtin.Options{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	perm := permission.NewService(permission.ModeDeny, nil)
	host := &e2eUserInputHost{testHost: &testHost{cwd: root, perm: perm}}
	st := session.NewMemoryStore(session.Options{CWD: root})
	a, err := New(Options{
		Provider: p, Registry: reg, Session: st, Permission: perm, ToolHost: host,
		Model: protocol.Model{Provider: p.ID(), ID: "e2e-model", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	host.emit = a.EmitUserInputRequest
	var requestEvent *protocol.UserInputRequest
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvUserInputRequest {
			requestEvent = event.UserInput
		}
	})
	if err := a.Prompt(context.Background(), "prepare output"); err != nil {
		t.Fatal(err)
	}

	if len(host.requests) != 1 || host.requests[0].ID != "ask-1" || host.requests[0].ToolCallID != "ask-1" {
		t.Fatalf("host requests = %+v", host.requests)
	}
	if requestEvent == nil || requestEvent.ID != "ask-1" || len(requestEvent.Questions) != 2 {
		t.Fatalf("request event = %+v", requestEvent)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[2].Role != protocol.RoleTool || messages[2].IsError {
		t.Fatalf("messages = %+v", messages)
	}
	if got, want := messages[2].Content[0].Text, `{"answers":[{"id":"format","answer":"JSON"},{"id":"notes","answer":"keep comments"}]}`; got != want {
		t.Fatalf("tool result = %s, want %s", got, want)
	}
	requests := p.Requests()
	if len(requests) != 2 || requests[1].Messages[2].Content[0].Text != messages[2].Content[0].Text {
		t.Fatalf("provider continuation = %+v", requests)
	}
	if requests[0].Tools[6].Name != "ask_user" || requests[0].Tools[6].Discovery != nil {
		t.Fatalf("ask_user schema = %+v", requests[0].Tools[6])
	}
}

// TestAgentEndToEndBuiltinWorkflow exercises the real agent, session, tool
// registry, permission gate, and all six directly invoked file/shell tools in one streamed
// multi-tool turn. The second provider request is asserted to contain every
// ordered tool result, which catches regressions that unit-testing tools or the
// provider separately cannot detect.
func TestAgentEndToEndBuiltinWorkflow(t *testing.T) {
	root := t.TempDir()
	p := &e2eProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamThinkingDelta, Text: "planning"},
			e2eCall("write-1", "write", map[string]any{
				"path": "note.txt", "content": "before\nbefore\n",
			}),
			e2eCall("edit-1", "edit", map[string]any{
				"path": "note.txt", "old_str": "before", "new_str": "after", "replace_all": true,
			}),
			e2eCall("read-1", "read", map[string]any{"path": "note.txt"}),
			e2eCall("bash-1", "bash", map[string]any{"command": "printf shell-output"}),
			e2eCall("glob-1", "glob", map[string]any{"pattern": "*.txt"}),
			e2eCall("grep-1", "grep", map[string]any{"pattern": "after", "glob": "*.txt"}),
			{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 11, Output: 7, Total: 18}},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "all tools completed"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	st := session.NewMemoryStore(session.Options{CWD: root})
	a := newBuiltinE2EAgent(t, root, p, st, permission.ModeAllow, nil)

	var events []protocol.AgentEvent
	a.Subscribe(func(ev protocol.AgentEvent) { events = append(events, ev) })
	if err := a.Prompt(context.Background(), "prepare and inspect note.txt"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after\nafter\n" {
		t.Fatalf("final note = %q, want edited contents", data)
	}

	msgs, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 9 { // user, tool-call assistant, six results, final assistant
		t.Fatalf("message count = %d, want 9: %+v", len(msgs), msgs)
	}
	if msgs[1].StopReason != protocol.StopToolUse {
		t.Fatalf("tool-call assistant stop = %q", msgs[1].StopReason)
	}
	if len(msgs[1].Content) != 7 || msgs[1].Content[0].Type != protocol.BlockThinking {
		t.Fatalf("assistant content = %+v, want thinking plus six calls", msgs[1].Content)
	}
	wantCalls := []string{"write-1", "edit-1", "read-1", "bash-1", "glob-1", "grep-1"}
	for i, id := range wantCalls {
		if msgs[i+2].Role != protocol.RoleTool || msgs[i+2].ToolCallID != id {
			t.Fatalf("result %d = %+v, want ordered result %q", i, msgs[i+2], id)
		}
		if msgs[i+2].IsError {
			t.Fatalf("tool result %q unexpectedly failed: %+v", id, msgs[i+2])
		}
	}
	if msgs[4].Content[0].Text != "after\nafter\n" {
		t.Fatalf("read result = %q, want edited file", msgs[4].Content[0].Text)
	}
	if !strings.Contains(msgs[5].Content[0].Text, "shell-output") {
		t.Fatalf("bash result = %q, want command output", msgs[5].Content[0].Text)
	}
	if !strings.Contains(msgs[6].Content[0].Text, "note.txt") || !strings.Contains(msgs[7].Content[0].Text, "note.txt:1: after") {
		t.Fatalf("search results = glob %q, grep %q", msgs[6].Content[0].Text, msgs[7].Content[0].Text)
	}
	if msgs[1].Usage == nil || msgs[1].Usage.Total != 18 {
		t.Fatalf("first assistant usage = %+v, want total 18", msgs[1].Usage)
	}
	if msgs[8].Content[0].Text != "all tools completed" {
		t.Fatalf("final assistant = %+v", msgs[8])
	}
	if len(p.Requests()) != 2 {
		t.Fatalf("provider calls = %d, want initial + continuation", len(p.Requests()))
	}
	continuation := p.Requests()[1]
	if continuation.Thinking != protocol.ThinkingHigh || continuation.System != "end-to-end system prompt" {
		t.Fatalf("continuation request lost options: %+v", continuation)
	}
	if len(continuation.Messages) != 8 {
		t.Fatalf("continuation messages = %d, want user + assistant + six results", len(continuation.Messages))
	}
	for i, id := range wantCalls {
		if continuation.Messages[i+2].ToolCallID != id {
			t.Fatalf("provider result %d = %q, want %q", i, continuation.Messages[i+2].ToolCallID, id)
		}
	}
	if len(continuation.Tools) != 9 {
		t.Fatalf("provider tool schemas = %d, want Default-mode builtins", len(continuation.Tools))
	}
	if len(events) == 0 || events[len(events)-1].Type != protocol.EvTurnDone {
		t.Fatalf("last event = %+v, want turn_done", events)
	}
	if events[len(events)-1].Usage == nil || events[len(events)-1].Usage.Total != 18 || events[len(events)-1].Usage.Requests != 1 {
		t.Fatalf("turn usage = %+v, want total=18 requests=1", events[len(events)-1].Usage)
	}
	usage, err := a.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total != 18 || usage.Requests != 1 {
		t.Fatalf("session usage = %+v, want persisted total=18 requests=1", usage)
	}
}

type e2eAsker struct {
	decision permission.Decision
	request  permission.Request
}

func (a *e2eAsker) Ask(_ context.Context, req permission.Request) (permission.Decision, error) {
	a.request = req
	return a.decision, nil
}

// TestAgentEndToEndPermissionMatrix verifies that the policy is enforced at
// the agent/tool boundary, not only inside the permission package. A denied
// mutation still gets a model-visible error result and the turn continues.
func TestAgentEndToEndPermissionMatrix(t *testing.T) {
	cases := []struct {
		name      string
		mode      permission.Mode
		decision  permission.Decision
		wantWrite bool
		wantError bool
		wantAsk   bool
	}{
		{name: "deny", mode: permission.ModeDeny, wantError: true},
		{name: "allow", mode: permission.ModeAllow, wantWrite: true},
		{name: "ask allow", mode: permission.ModeAsk, decision: permission.DecisionAllow, wantWrite: true, wantAsk: true},
		{name: "ask deny", mode: permission.ModeAsk, decision: permission.DecisionDeny, wantError: true, wantAsk: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := &e2eProvider{scripts: [][]protocol.StreamEvent{
				{
					e2eCall("write-1", "write", map[string]any{"path": "guarded.txt", "content": "changed"}),
					{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
				},
				{
					{Type: protocol.EvStreamTextDelta, Text: "continued"},
					{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
				},
			}}
			var asker *e2eAsker
			if tc.mode == permission.ModeAsk {
				asker = &e2eAsker{decision: tc.decision}
			}
			st := session.NewMemoryStore(session.Options{CWD: root})
			a := newBuiltinE2EAgent(t, root, p, st, tc.mode, asker)
			if err := a.Prompt(context.Background(), "write guarded.txt"); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(filepath.Join(root, "guarded.txt"))
			wrote := statErr == nil
			if wrote != tc.wantWrite {
				t.Fatalf("file exists = %v, want %v (stat err %v)", wrote, tc.wantWrite, statErr)
			}
			msgs, err := st.Messages()
			if err != nil {
				t.Fatal(err)
			}
			if len(msgs) != 4 {
				t.Fatalf("messages = %d, want 4", len(msgs))
			}
			if msgs[2].IsError != tc.wantError {
				t.Fatalf("tool error = %v, want %v: %+v", msgs[2].IsError, tc.wantError, msgs[2])
			}
			if msgs[3].Content[0].Text != "continued" {
				t.Fatalf("continuation = %+v", msgs[3].Content)
			}
			if tc.wantAsk && asker.request.Tool != "write" {
				t.Fatalf("ask request = %+v, want write", asker.request)
			}
			if !tc.wantAsk && asker != nil {
				t.Fatal("unexpected asker")
			}
		})
	}
}

// TestAgentEndToEndJSONLResumeAndContinuation verifies the durable path: a
// completed turn is closed, reopened from its JSONL file, and then continued
// by a new agent. It also verifies the provider receives the restored history.
func TestAgentEndToEndJSONLResumeAndContinuation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")

	firstProvider := &e2eProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "first answer"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	firstStore, err := session.NewJSONLStore(path, root, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first := newBuiltinE2EAgent(t, root, firstProvider, firstStore, permission.ModeAllow, nil)
	if err := first.Prompt(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	secondProvider := &e2eProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "second answer"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	secondStore, err := session.NewJSONLStore(path, root, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	second := newBuiltinE2EAgent(t, root, secondProvider, secondStore, permission.ModeAllow, nil)
	if err := second.Prompt(context.Background(), "follow-up question"); err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()

	msgs, err := secondStore.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("resumed messages = %d, want 4: %+v", len(msgs), msgs)
	}
	if msgs[0].Content[0].Text != "first question" || msgs[1].Content[0].Text != "first answer" {
		t.Fatalf("restored history = %+v", msgs[:2])
	}
	if msgs[2].Content[0].Text != "follow-up question" || msgs[3].Content[0].Text != "second answer" {
		t.Fatalf("continued history = %+v", msgs[2:])
	}
	requests := secondProvider.Requests()
	if len(requests) != 1 || len(requests[0].Messages) != 3 {
		t.Fatalf("resume request = %+v, want restored history plus current prompt", requests)
	}
	if requests[0].Messages[0].Content[0].Text != "first question" || requests[0].Messages[2].Content[0].Text != "follow-up question" {
		t.Fatalf("provider did not receive restored/current messages: %+v", requests[0].Messages)
	}
}

// TestAgentEndToEndProviderFailureCases covers failures at each provider
// boundary. These are intentionally end-to-end assertions: the public Prompt
// call must return the error, publish turn_done, and persist an actionable
// assistant error when a stream has already started.
func TestAgentEndToEndProviderFailureCases(t *testing.T) {
	cases := []struct {
		name       string
		provider   func() *e2eProvider
		wantError  string
		wantAsst   bool
		wantReason protocol.StopReason
	}{
		{
			name: "resolve",
			provider: func() *e2eProvider {
				return &e2eProvider{resolveErr: errors.New("credentials unavailable")}
			},
			wantError: "provider resolve",
		},
		{
			name: "chat",
			provider: func() *e2eProvider {
				return &e2eProvider{chatErr: errors.New("upstream unavailable")}
			},
			wantError: "provider chat",
		},
		{
			name: "stream",
			provider: func() *e2eProvider {
				return &e2eProvider{scripts: [][]protocol.StreamEvent{{
					{Type: protocol.EvStreamTextDelta, Text: "partial"},
					{Type: protocol.EvStreamError, Err: errors.New("stream broke")},
				}}}
			},
			wantError: "stream broke", wantAsst: true, wantReason: protocol.StopError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			st := session.NewMemoryStore(session.Options{CWD: root})
			a := newBuiltinE2EAgent(t, root, tc.provider(), st, permission.ModeAllow, nil)
			var sawDone bool
			a.Subscribe(func(ev protocol.AgentEvent) {
				if ev.Type == protocol.EvTurnDone {
					sawDone = true
				}
			})
			err := a.Prompt(context.Background(), "trigger failure")
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Prompt error = %v, want substring %q", err, tc.wantError)
			}
			if !sawDone {
				t.Fatal("missing turn_done after provider failure")
			}
			msgs, msgErr := st.Messages()
			if msgErr != nil {
				t.Fatal(msgErr)
			}
			if tc.wantAsst {
				if len(msgs) != 2 || msgs[1].StopReason != tc.wantReason || msgs[1].Error == "" {
					t.Fatalf("failure assistant = %+v", msgs)
				}
			} else if len(msgs) != 1 {
				t.Fatalf("messages after pre-stream failure = %+v, want only user", msgs)
			}
		})
	}
}

// TestAgentEndToEndAbortDuringBash verifies cancellation across the complete
// agent → permission → builtin bash path.
func TestAgentEndToEndAbortDuringBash(t *testing.T) {
	root := t.TempDir()
	p := &e2eProvider{scripts: [][]protocol.StreamEvent{
		{
			e2eCall("bash-1", "bash", map[string]any{"command": "sleep 10"}),
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "should not be reached"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	st := session.NewMemoryStore(session.Options{CWD: root})
	a := newBuiltinE2EAgent(t, root, p, st, permission.ModeAllow, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	err := a.Prompt(ctx, "run a cancellable command")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled prompt = %v, want context deadline exceeded", err)
	}
	msgs, msgErr := st.Messages()
	if msgErr != nil {
		t.Fatal(msgErr)
	}
	if len(msgs) != 4 || !msgs[2].IsError {
		t.Fatalf("canceled session = %+v, want tool-call plus tool error", msgs)
	}
}

// TestAgentEndToEndEOFRequiresDone verifies that truncated provider streams
// cannot be mistaken for a successful assistant response.
func TestAgentEndToEndEOFRequiresDone(t *testing.T) {
	root := t.TempDir()
	p := &e2eProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "implicit stop"},
	}}}
	st := session.NewMemoryStore(session.Options{CWD: root})
	a := newBuiltinE2EAgent(t, root, p, st, permission.ModeAllow, nil)
	if err := a.Prompt(context.Background(), "eof"); err == nil || !strings.Contains(err.Error(), "terminal done event") {
		t.Fatalf("Prompt error = %v, want missing terminal event", err)
	}
	msgs, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].StopReason != protocol.StopError || msgs[1].Error == "" || msgs[1].Content[0].Text != "implicit stop" {
		t.Fatalf("EOF result = %+v, want durable failed assistant", msgs)
	}
}

// Ensure compile-time coverage of the provider contract used by this suite.
var _ provider.Provider = (*e2eProvider)(nil)
