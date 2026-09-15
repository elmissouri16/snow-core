package protocol

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

func TestToolResultPreviewCloneAndSchema(t *testing.T) {
	event := AgentEvent{Type: EvToolEnd, ToolResult: &ToolResultPreview{Text: "public", Truncated: true}}
	clone := event.Clone()
	clone.ToolResult.Text = "changed"
	clone.ToolResult.Truncated = false
	if event.ToolResult.Text != "public" || !event.ToolResult.Truncated {
		t.Fatal("event clone aliases public preview")
	}
	schema := resolveRPCSchema(t, "agent-event.schema.json")
	if err := schema.Validate(jsonValue(t, event)); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(jsonValue(t, AgentEvent{Type: EvToolEnd})); err != nil {
		t.Fatal("legacy event rejected", err)
	}
	for _, invalid := range []any{
		map[string]any{"text": "text"},
		map[string]any{"text": 5, "truncated": false},
		map[string]any{"text": "text", "truncated": false, "private": "secret"},
		map[string]any{"text": strings.Repeat("x", 8193), "truncated": false},
	} {
		if err := schema.Validate(map[string]any{"type": "tool_end", "tool_result": invalid}); err == nil {
			t.Fatal("invalid public preview accepted")
		}
	}
	data, err := json.Marshal(AgentEvent{Type: EvToolEnd})
	if err != nil || strings.Contains(string(data), "tool_result") {
		t.Fatal("absent preview not omitted", string(data), err)
	}
}
