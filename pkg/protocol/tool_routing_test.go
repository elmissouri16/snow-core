package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolDiscoveryAndRoutingEventJSON(t *testing.T) {
	schema := ToolSchema{
		Name: "mail_find", Parameters: json.RawMessage(`{"type":"object"}`),
		Discovery: &ToolDiscovery{Mode: ToolDiscoveryDeferred, Namespace: "mail", Keywords: []string{"unread"}},
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"mode":"deferred"`) || !strings.Contains(string(encoded), `"namespace":"mail"`) {
		t.Fatalf("schema JSON = %s", encoded)
	}

	event := AgentEvent{Type: EvToolRouting, ToolRouting: &ToolRouting{
		Trigger: "automatic", ToolIDs: []string{"mail_find"}, CandidateCount: 3,
		SelectedCount: 1, ExposedCount: 7, SchemaBytes: 1234, LatencyMS: 2,
	}}
	encoded, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "user query") || !strings.Contains(string(encoded), `"tool_routing"`) {
		t.Fatalf("event JSON = %s", encoded)
	}
}
