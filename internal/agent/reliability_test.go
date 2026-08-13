package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
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
	for i := 0; i < repeatedToolNextThreshold; i++ {
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
		Model:      protocol.Model{Provider: "scripted", ID: "m"}, Auth: auth.NewMemoryStoreForTest(),
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
