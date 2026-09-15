package protocol

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

func TestMessagePublicToolResultClone(t *testing.T) {
	message := Message{Role: RoleTool, PublicToolResult: &ToolResultPreview{Text: "public", Truncated: true}}
	clone := message.Clone()
	clone.PublicToolResult.Text = "changed"
	clone.PublicToolResult.Truncated = false
	if message.PublicToolResult.Text != "public" || !message.PublicToolResult.Truncated {
		t.Fatal("message clone aliases explicit public provenance")
	}
	message.PublicToolResult = nil
	message.Content = []ContentBlock{NewTextBlock("NOT-PUBLIC-WITHOUT-PROVENANCE")}
	message.ToolDisplay = &ToolDisplay{Output: "PRIVATE-DISPLAY"}
	if message.Clone().PublicToolResult != nil {
		t.Fatal("clone fabricated public provenance for legacy/private history")
	}
}

func TestMessagePublicToolResultJSONAndSchema(t *testing.T) {
	schema := resolveRPCSchema(t, "message.schema.json")
	for _, preview := range []*ToolResultPreview{nil, {}, {Text: "public", Truncated: true}} {
		message := NewToolResultMessage("result", "parent", "call", "read", []ContentBlock{NewTextBlock("legacy content")}, false)
		message.PublicToolResult = preview
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"public_tool_result"`) != (preview != nil) {
			t.Fatalf("optional provenance encoding: %s", data)
		}
		var decoded Message
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if preview == nil {
			if decoded.PublicToolResult != nil {
				t.Fatal("legacy JSON fabricated public provenance")
			}
		} else if decoded.PublicToolResult == nil || *decoded.PublicToolResult != *preview {
			t.Fatalf("public provenance did not round trip: %s", data)
		}
		if err := schema.Validate(jsonValue(t, message)); err != nil {
			t.Fatalf("message schema rejected optional provenance: %v", err)
		}
	}
	for _, invalid := range []any{
		nil,
		map[string]any{"text": "text"},
		map[string]any{"text": 5, "truncated": false},
		map[string]any{"text": "text", "truncated": false, "private": "secret"},
		map[string]any{"text": strings.Repeat("x", 8193), "truncated": false},
	} {
		message := map[string]any{
			"id": "result", "role": "tool_result", "content": []any{}, "ts": 0,
			"public_tool_result": invalid,
		}
		if err := schema.Validate(message); err == nil {
			t.Fatalf("message schema accepted invalid public provenance: %#v", invalid)
		}
	}
}
