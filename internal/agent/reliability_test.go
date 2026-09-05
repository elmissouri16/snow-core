package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRepeatedToolCallReminderAppearsAtThirdIdenticalCall(t *testing.T) {
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			return tools.TextResult("same result")
		},
	}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	call := func(id string, args string) []protocol.StreamEvent {
		return []protocol.StreamEvent{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: "read", Arguments: json.RawMessage(args)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		}
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		call("one", `{"path":"x","offset":1}`),
		call("two", `{"offset":1,"path":"x"}`),
		call("three", `{"path":"x","offset":1}`),
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, _ := setup(t, provider, registry, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "repeat"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("provider requests=%d", len(provider.requests))
	}
	if len(provider.requests[2].InternalContext) != 0 {
		t.Fatalf("reminder arrived before third result: %+v", provider.requests[2].InternalContext)
	}
	last := provider.requests[3].InternalContext
	if len(last) != 1 || last[0].Source != "loop-guard" || !strings.Contains(last[0].Text, "repeating the exact same tool call") {
		t.Fatalf("missing loop reminder: %+v", last)
	}
}

func TestRepeatedToolCallDifferentArgumentsResetChain(t *testing.T) {
	a := &Agent{}
	a.observeRepeatedToolCall("read", json.RawMessage(`{"path":"a"}`))
	a.observeRepeatedToolCall("read", json.RawMessage(`{"path":"a"}`))
	a.observeRepeatedToolCall("read", json.RawMessage(`{"path":"b"}`))
	a.observeRepeatedToolCall("read", json.RawMessage(`{"path":"a"}`))
	if reminder := a.takeRepeatedToolReminder(); reminder != "" {
		t.Fatalf("unexpected reminder after reset: %q", reminder)
	}
	if a.repeatedTool.count != 1 {
		t.Fatalf("repeat count=%d", a.repeatedTool.count)
	}
}

func TestRepeatedToolDetailedReminderKeepsUTF8Valid(t *testing.T) {
	a := &Agent{}
	args, err := json.Marshal(map[string]string{"path": strings.Repeat("界", 300)})
	if err != nil {
		t.Fatal(err)
	}
	for range repeatedToolNextThreshold {
		a.observeRepeatedToolCall("read", args)
	}
	reminder := a.takeRepeatedToolReminder()
	if !strings.Contains(reminder, "consecutive_calls: 5") || strings.ToValidUTF8(reminder, "?") != reminder {
		t.Fatalf("invalid detailed reminder: %q", reminder)
	}
}

func TestNewRepairsInterruptedToolCallsWithRiskAwareResults(t *testing.T) {
	store := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	user := protocol.NewUserMessage("user", "", "run")
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
		t.Fatal(err)
	}
	assistant := protocol.NewAssistantMessage("assistant", user.ID, "scripted", "m", []protocol.ContentBlock{
		{Type: protocol.BlockToolCall, ToolCallID: "read-call", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)},
		{Type: protocol.BlockToolCall, ToolCallID: "bash-call", Name: "bash", Arguments: json.RawMessage(`{"command":"touch x"}`)},
	}, protocol.StopToolUse, nil)
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, ParentID: user.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	agent, err := New(Options{
		Provider: provider, Registry: tools.NewRegistry(), Session: store,
		Permission: permission.NewService(permission.ModeDeny, nil),
		Model:      protocol.Model{Provider: "scripted", ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[2].ToolCallID != "read-call" || messages[3].ToolCallID != "bash-call" {
		t.Fatalf("repaired messages: %+v", messages)
	}
	if !strings.Contains(messages[2].Content[0].Text, "may be retried") {
		t.Fatalf("read recovery=%q", messages[2].Content[0].Text)
	}
	if !strings.Contains(messages[3].Content[0].Text, "outcome is unknown") || !strings.Contains(messages[3].Content[0].Text, "ask the user") {
		t.Fatalf("bash recovery=%q", messages[3].Content[0].Text)
	}
	if repaired, err := repairInterruptedToolCalls(store, tools.NewRegistry()); err != nil || repaired != 0 {
		t.Fatalf("second recovery repaired=%d err=%v", repaired, err)
	}
}

func TestCompactionSummarizerReceivesPrunedHistoricalToolResult(t *testing.T) {
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "summary"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	appendMessage := func(message protocol.Message) {
		t.Helper()
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	appendMessage(protocol.NewUserMessage("old-user", "", "old turn"))
	appendMessage(protocol.NewAssistantMessage("old-call", "", "p", "m", []protocol.ContentBlock{{
		Type: protocol.BlockToolCall, ToolCallID: "large", Name: "bash", Arguments: json.RawMessage(`{"command":"x"}`),
	}}, protocol.StopToolUse, nil))
	appendMessage(protocol.NewToolResultMessage("large-result", "", "large", "bash", []protocol.ContentBlock{
		protocol.NewTextBlock(strings.Repeat("head", 3000) + strings.Repeat("tail", 3000)),
	}, false))
	appendMessage(protocol.NewAssistantMessage("old-final", "", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("old done")}, protocol.StopStop, nil))
	appendMessage(protocol.NewUserMessage("recent-user-1", "", "recent one"))
	appendMessage(protocol.NewAssistantMessage("recent-answer-1", "", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("one")}, protocol.StopStop, nil))
	appendMessage(protocol.NewUserMessage("recent-user-2", "", "recent two"))
	appendMessage(protocol.NewAssistantMessage("recent-answer-2", "", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("two")}, protocol.StopStop, nil))

	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("summary requests=%d", len(provider.requests))
	}
	found := false
	for _, message := range provider.requests[0].Messages {
		if message.Role == protocol.RoleTool && strings.Contains(message.Content[0].Text, "bytes omitted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("summary input was not pruned: %+v", provider.requests[0].Messages)
	}
	full, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full[2].Content[0].Text, strings.Repeat("head", 3000)) || strings.Contains(full[2].Content[0].Text, "bytes omitted") {
		t.Fatal("durable tool result was modified")
	}
}

func TestInterruptedRecoveryCompletesPartialFinalBatchOnly(t *testing.T) {
	store := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	user := protocol.NewUserMessage("user", "", "run")
	_ = store.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user})
	assistant := protocol.NewAssistantMessage("assistant", user.ID, "p", "m", []protocol.ContentBlock{
		{Type: protocol.BlockToolCall, ToolCallID: "done", Name: "read"},
		{Type: protocol.BlockToolCall, ToolCallID: "missing", Name: "edit"},
	}, protocol.StopToolUse, nil)
	_ = store.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant})
	result := protocol.NewToolResultMessage("result", assistant.ID, "done", "read", []protocol.ContentBlock{protocol.NewTextBlock("ok")}, false)
	_ = store.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result})
	repaired, err := repairInterruptedToolCalls(store, tools.NewRegistry())
	if err != nil || repaired != 1 {
		t.Fatalf("repaired=%d err=%v", repaired, err)
	}
	messages, _ := store.Messages()
	if len(messages) != 4 || messages[3].ToolCallID != "missing" || !strings.Contains(messages[3].Content[0].Text, "outcome is unknown") {
		t.Fatalf("messages=%+v", messages)
	}
}

func TestLengthTruncatedToolCallsReceiveErrorsWithoutExecution(t *testing.T) {
	runs := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			runs++
			return tools.TextResult("unsafe")
		},
	}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDelta, ToolCallID: "truncated", ToolName: "read", Arguments: json.RawMessage(`{"path":`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopLength},
		},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	store, err := session.NewSQLiteStore(t.TempDir()+"/session.db", t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: store,
		Permission: permission.NewService(permission.ModeDeny, nil),
		Model:      protocol.Model{Provider: provider.ID(), ID: "m1", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("truncated tool executed %d times", runs)
	}
	messages, _ := store.Messages()
	if len(messages) != 4 || messages[1].StopReason != protocol.StopLength || !messages[2].IsError || !strings.Contains(messageTextForTest(messages[2]), "truncated") {
		t.Fatalf("truncated batch=%+v", messages)
	}
}

func TestRepeatedLengthTruncationStopsAfterOneCorrectiveRound(t *testing.T) {
	runs := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			runs++
			return tools.TextResult("unexpected")
		},
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "truncated", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopLength},
	}}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	a, _ := setup(t, provider, registry, permission.ModeAllow)
	err := a.Prompt(context.Background(), "repeat truncation")
	if err == nil || !strings.Contains(err.Error(), "repeated tool batches produced only synthetic") {
		t.Fatalf("Prompt error=%v", err)
	}
	if provider.call != 2 || runs != 0 {
		t.Fatalf("provider calls=%d tool runs=%d", provider.call, runs)
	}
}

func TestRepeatedCallLimitBatchStopsAfterOneCorrectiveRound(t *testing.T) {
	runs := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			runs++
			return tools.TextResult("ok")
		},
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "first", ToolName: "read", Arguments: json.RawMessage(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "over-limit", ToolName: "read", Arguments: json.RawMessage(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
	}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	a, _ := setup(t, provider, registry, permission.ModeAllow)
	a.opts.CallLimit = 1
	err := a.Prompt(context.Background(), "repeat limit")
	if err == nil || !strings.Contains(err.Error(), "repeated tool batches produced only synthetic") {
		t.Fatalf("Prompt error=%v", err)
	}
	if provider.call != 3 || runs != 1 {
		t.Fatalf("provider calls=%d tool runs=%d", provider.call, runs)
	}
}

func TestCompleteCallsOverrideIncorrectStopReason(t *testing.T) {
	runs := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			runs++
			return tools.TextResult("ok")
		},
	}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "call", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, provider, registry, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}
	messages, _ := store.Messages()
	if runs != 1 || len(messages) != 4 || messages[1].StopReason != protocol.StopToolUse || messages[2].IsError {
		t.Fatalf("normalized batch runs=%d messages=%+v", runs, messages)
	}
}

func TestEmptyToolUseStopsWithProtocolError(t *testing.T) {
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}}}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	err := a.Prompt(context.Background(), "loop")
	if err == nil || !strings.Contains(err.Error(), "without any tool calls") {
		t.Fatalf("Prompt error=%v", err)
	}
	messages, _ := store.Messages()
	if provider.call != 1 || len(messages) != 2 || messages[1].StopReason != protocol.StopError {
		t.Fatalf("provider calls=%d messages=%+v", provider.call, messages)
	}
}

func TestToolCallsRequireIDAndName(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		tool string
	}{
		{name: "missing id", tool: "read"},
		{name: "missing name", id: "call"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
				{Type: protocol.EvStreamToolCallDone, ToolCallID: tc.id, ToolName: tc.tool, Arguments: json.RawMessage(`{}`)},
				{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
			}}}
			a, store := setup(t, provider, nil, permission.ModeDeny)
			err := a.Prompt(context.Background(), "invalid identity")
			if err == nil || !strings.Contains(err.Error(), "without both an ID and name") {
				t.Fatalf("Prompt error=%v", err)
			}
			messages, _ := store.Messages()
			if len(messages) != 2 || messages[1].StopReason != protocol.StopError {
				t.Fatalf("invalid identity messages=%+v", messages)
			}
			for _, block := range messages[1].Content {
				if block.Type == protocol.BlockToolCall {
					t.Fatalf("invalid tool call persisted: %+v", messages[1])
				}
			}
		})
	}
}

func TestToolCallLimitAppliesAcrossProviderBatches(t *testing.T) {
	runs := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			runs++
			return tools.TextResult("ok")
		},
	}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	call := func(id string) []protocol.StreamEvent {
		return []protocol.StreamEvent{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: id, ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		}
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		call("first"), call("second"), {{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, provider, registry, permission.ModeDeny)
	a.opts.CallLimit = 1
	if err := a.Prompt(context.Background(), "two rounds"); err != nil {
		t.Fatal(err)
	}
	messages, _ := store.Messages()
	if runs != 1 {
		t.Fatalf("tool runs=%d, want one across the admitted turn", runs)
	}
	foundLimit := false
	for _, message := range messages {
		if message.Role == protocol.RoleTool && message.IsError && strings.Contains(messageTextForTest(message), "call limit 1") {
			foundLimit = true
		}
	}
	if !foundLimit {
		t.Fatalf("missing cross-batch limit result: %+v", messages)
	}
}

type transientStartError struct{}

func (transientStartError) Error() string   { return "temporary startup failure" }
func (transientStartError) Transient() bool { return true }

type startFailureProvider struct {
	calls  int
	first  error
	cancel context.CancelFunc
}

func (*startFailureProvider) ID() string { return "start-failure" }
func (*startFailureProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *startFailureProvider) Chat(ctx context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	p.calls++
	if p.cancel != nil {
		p.cancel()
		return nil, ctx.Err()
	}
	if p.calls == 1 && p.first != nil {
		return nil, p.first
	}
	return &sliceStream{ctx: ctx, evs: []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}, nil
}

func TestSynchronousProviderCancellationPersistsAbortedBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	provider := &startFailureProvider{cancel: cancel}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	if err := a.Prompt(ctx, "cancel during startup"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	messages, _ := store.Messages()
	if len(messages) != 2 || messages[1].StopReason != protocol.StopAborted {
		t.Fatalf("startup cancellation messages=%+v", messages)
	}
}

func TestTransientProviderStartupFailureRetriesOnce(t *testing.T) {
	provider := &startFailureProvider{first: transientStartError{}}
	a, _ := setup(t, provider, nil, permission.ModeDeny)
	a.opts.MaxTurns = 1
	if err := a.Prompt(context.Background(), "retry startup"); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls=%d, want one bounded retry", provider.calls)
	}
}

func TestCancellationDuringProviderStartupBackoffPersistsAbort(t *testing.T) {
	provider := &startFailureProvider{first: transientStartError{}}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	retrying := make(chan struct{}, 1)
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvProviderRetry && event.ProviderRetry != nil {
			select {
			case retrying <- struct{}{}:
			default:
			}
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Prompt(ctx, "cancel retry") }()
	select {
	case <-retrying:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("startup retry did not enter backoff")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Prompt error=%v, want context.Canceled", err)
	}
	messages, _ := store.Messages()
	if provider.calls != 1 || len(messages) != 2 || messages[1].StopReason != protocol.StopAborted {
		t.Fatalf("calls=%d messages=%+v", provider.calls, messages)
	}
}

func TestTerminalProviderErrorPersistsFailedBoundary(t *testing.T) {
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "partial"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopError},
	}}}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	err := a.Prompt(context.Background(), "fail")
	if err == nil || !strings.Contains(err.Error(), "provider stopped with error") {
		t.Fatalf("Prompt error=%v", err)
	}
	messages, _ := store.Messages()
	if len(messages) != 2 || messages[1].StopReason != protocol.StopError || messages[1].Error == "" {
		t.Fatalf("terminal error messages=%+v", messages)
	}
}

func TestTerminalProviderAbortPersistsAbortedBoundary(t *testing.T) {
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "partial"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopAborted},
	}}}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "abort"); err != nil {
		t.Fatal(err)
	}
	messages, _ := store.Messages()
	if len(messages) != 2 || messages[1].StopReason != protocol.StopAborted {
		t.Fatalf("terminal abort messages=%+v", messages)
	}
}

func TestInvalidTerminalStopReasonsAreRejected(t *testing.T) {
	for _, stop := range []protocol.StopReason{protocol.StopPending, protocol.StopReason("future_unknown")} {
		t.Run(string(stop), func(t *testing.T) {
			provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
				{Type: protocol.EvStreamToolCallDone, ToolCallID: "call", ToolName: "read", Arguments: json.RawMessage(`{}`)},
				{Type: protocol.EvStreamDone, StopReason: stop},
			}}}
			a, store := setup(t, provider, nil, permission.ModeDeny)
			err := a.Prompt(context.Background(), "invalid terminal")
			if err == nil || !strings.Contains(err.Error(), "invalid terminal stop reason") {
				t.Fatalf("Prompt error=%v", err)
			}
			messages, _ := store.Messages()
			if len(messages) != 2 || messages[1].StopReason != protocol.StopError {
				t.Fatalf("invalid terminal messages=%+v", messages)
			}
			for _, block := range messages[1].Content {
				if block.Type == protocol.BlockToolCall {
					t.Fatalf("invalid terminal persisted tool call: %+v", messages[1])
				}
			}
		})
	}
}

func TestProviderContextDropsFailedLegacyToolGroup(t *testing.T) {
	store := session.NewMemoryStore(session.Options{})
	user := protocol.NewUserMessage("user", "", "start")
	failed := protocol.NewAssistantMessage("failed", user.ID, "p", "m", []protocol.ContentBlock{{
		Type: protocol.BlockToolCall, ToolCallID: "legacy-call", Name: "read", Arguments: json.RawMessage(`{}`),
	}}, protocol.StopError, nil)
	failed.Error = "legacy failure"
	result := protocol.NewToolResultMessage("result", failed.ID, "legacy-call", "read", []protocol.ContentBlock{protocol.NewTextBlock("legacy repair")}, true)
	next := protocol.NewUserMessage("next", result.ID, "continue")
	for _, message := range []protocol.Message{user, failed, result, next} {
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := contextMessagesFromStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].ID != user.ID || messages[1].ID != next.ID {
		t.Fatalf("provider projection exposed failed legacy group: %+v", messages)
	}
}
