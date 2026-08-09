package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestOpaqueProviderReasoningPersistsThroughToolFollowupWithoutAgentEvent(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			return tools.TextResult("contents")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(readTool); err != nil {
		t.Fatal(err)
	}
	wire := `{"type":"reasoning","id":"reasoning-1","summary":[],"encrypted_content":"encrypted-continuity"}`
	opaque := protocol.ContentBlock{Type: protocol.BlockProviderData, Name: "reasoning-1", Data: []byte(wire)}
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamThinkingDelta, Text: "safe summary"},
			{Type: protocol.EvStreamProviderData, ProviderData: &opaque},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "call-1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)
	var eventText string
	a.Subscribe(func(ev protocol.AgentEvent) {
		eventText += ev.Text + ev.Message + ev.ToolOutput
	})
	if err := a.Prompt(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests=%d", len(prov.requests))
	}
	followup := prov.requests[1].Messages
	if len(followup) < 2 || len(followup[1].Content) < 3 {
		t.Fatalf("follow-up transcript=%+v", followup)
	}
	assistant := followup[1]
	if assistant.Content[0].Type != protocol.BlockThinking || assistant.Content[1].Type != protocol.BlockProviderData || assistant.Content[2].Type != protocol.BlockToolCall {
		t.Fatalf("provider continuity order=%+v", assistant.Content)
	}
	if eventText == "" || contains(eventText, "encrypted-continuity") {
		t.Fatalf("opaque data leaked through AgentEvent: %q", eventText)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if messages[1].Content[1].Type != protocol.BlockProviderData || string(messages[1].Content[1].Data) != wire ||
		string(assistant.Content[1].Data) != wire {
		t.Fatal("complete opaque provider item did not persist exactly into the follow-up")
	}
}

func TestImageToolResultPersistsIntoProviderFollowup(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G'}
	imageTool := &testTool{
		name:   "mcp_screenshot",
		schema: protocol.ToolSchema{Name: "mcp_screenshot", Description: "capture", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			return tools.ToolResult{Content: []protocol.ContentBlock{
				protocol.NewTextBlock("MCP screenshot"),
				{Type: protocol.BlockImage, MIMEType: "image/png", Data: append([]byte(nil), image...)},
			}}
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(imageTool); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "call-image", ToolName: "mcp_screenshot", Arguments: json.RawMessage(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamTextDelta, Text: "done"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, _ := setup(t, prov, reg, permission.ModeAllow)
	if err := a.Prompt(context.Background(), "capture"); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests = %d", len(prov.requests))
	}
	var result *protocol.Message
	for i := range prov.requests[1].Messages {
		msg := &prov.requests[1].Messages[i]
		if msg.Role == protocol.RoleTool && msg.ToolCallID == "call-image" {
			result = msg
			break
		}
	}
	if result == nil || len(result.Content) != 2 || result.Content[0].Text != "MCP screenshot" ||
		result.Content[1].Type != protocol.BlockImage || result.Content[1].MIMEType != "image/png" || string(result.Content[1].Data) != string(image) {
		t.Fatalf("image tool follow-up = %+v", result)
	}
}

func contains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
