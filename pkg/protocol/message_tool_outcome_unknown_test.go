package protocol

import (
	json "encoding/json/v2"
	"strings"
	"testing"
)

func TestMessageToolOutcomeUnknownJSONCloneAndSchema(t *testing.T) {
	schema := resolveRPCSchema(t, "message.schema.json")
	for _, unknown := range []bool{false, true} {
		message := NewToolResultMessage("recovery", "owner", "call", "write", []ContentBlock{}, true)
		message.ToolOutcomeUnknown = unknown
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"tool_outcome_unknown"`) != unknown {
			t.Fatalf("optional outcome encoding: %s", data)
		}
		var decoded Message
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ToolOutcomeUnknown != unknown || message.Clone().ToolOutcomeUnknown != unknown {
			t.Fatalf("outcome provenance lost: %s", data)
		}
		if err := schema.Validate(jsonValue(t, message)); err != nil {
			t.Fatalf("schema rejected outcome provenance: %v", err)
		}
	}
	for _, invalid := range []any{nil, "true", 1, map[string]any{}} {
		message := map[string]any{"id": "recovery", "role": "tool_result", "content": []any{}, "ts": 0, "tool_outcome_unknown": invalid}
		if err := schema.Validate(message); err == nil {
			t.Fatalf("schema accepted invalid outcome provenance: %#v", invalid)
		}
	}
}
