package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/artifact"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

func appendCompleteTurns(t *testing.T, store *session.MemoryStore, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		user := protocol.NewUserMessage(fmt.Sprintf("pressure-user-%d", i), "", fmt.Sprintf("old user %d", i))
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
			t.Fatal(err)
		}
		assistant := protocol.NewAssistantMessage(fmt.Sprintf("pressure-assistant-%d", i), user.ID, "scripted", "m1", []protocol.ContentBlock{protocol.NewTextBlock("old answer")}, protocol.StopStop, nil)
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, ParentID: user.ID, Message: &assistant}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrdinaryTurnAutoCompactsInsideToolChain(t *testing.T) {
	tool := &testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult("tool output")
	}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 80}}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "read-1", ToolName: "read", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, registry, permission.ModeDeny)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 80}
	appendCompleteTurns(t, store, 4)
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if p.call != 3 || !strings.Contains(p.requests[1].System, "continuation context") || p.requests[2].Messages[0].Role != protocol.RoleCustom {
		t.Fatalf("calls=%d requests=%+v", p.call, p.requests)
	}
}

func TestOversizedToolResultSpillsToPrivateArtifact(t *testing.T) {
	full := strings.Repeat("head", 5000) + " NEEDLE " + strings.Repeat("tail", 5000)
	tool := &testTool{name: "read", schema: protocol.ToolSchema{Name: "read", Parameters: []byte(`{"type":"object"}`)}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult(full)
	}}
	registry := tools.NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "large", ToolName: "read", Arguments: []byte(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, registry, permission.ModeDeny)
	artifacts, err := artifact.NewLocalStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	a.opts.Artifacts = artifacts
	a.opts.Compaction.ToolResultInlineBytes = 4096
	if err := a.Prompt(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	var preview string
	for _, message := range messages {
		if message.Role == protocol.RoleTool {
			preview = message.Content[0].Text
		}
	}
	if len(preview) >= len(full) || !strings.Contains(preview, "bytes omitted") || !strings.Contains(preview, "artifact-") {
		t.Fatalf("preview bytes=%d text=%q", len(preview), preview)
	}
	start := strings.Index(preview, "artifact-")
	ref := preview[start : start+len("artifact-")+32]
	got, err := artifacts.ReadText(context.Background(), store.ID(), ref)
	if err != nil || got != full {
		t.Fatalf("artifact bytes=%d err=%v", len(got), err)
	}
}

func TestContextOverflowCompactsAndRetriesOnce(t *testing.T) {
	overflow := errors.New("maximum context length exceeded")
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamError, Err: overflow}},
		{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "recovered"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, store := setup(t, p, nil, permission.ModeDeny)
	a.model.ContextWindow = 100
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "local", AutoThresholdPercent: 80}
	appendCompleteTurns(t, store, 4)
	var errorsSeen int
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvError {
			errorsSeen++
		}
	})
	if err := a.Prompt(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	if p.call != 3 || p.requests[2].Messages[0].Role != protocol.RoleCustom || errorsSeen != 0 {
		t.Fatalf("calls=%d errors=%d retry=%+v", p.call, errorsSeen, p.requests)
	}
	if !provider.IsContextWindowExceeded(overflow) {
		t.Fatal("overflow classifier rejected known diagnostic")
	}
}
