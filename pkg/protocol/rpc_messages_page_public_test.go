package protocol

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
)

func TestMessagesPagePublicHistoryWireAndSchemas(t *testing.T) {
	if !slices.Contains(KnownRPCCapabilities(), "messages_public_history") {
		t.Fatal("public history capability missing")
	}
	requestSchema := resolveRPCSchema(t, "request.schema.json")
	for _, public := range []bool{false, true} {
		params, err := json.Marshal(RPCMessagesPageParams{PublicHistory: public, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(params), "public_history") != public {
			t.Fatalf("opt-in encoding = %s", params)
		}
		request := RPCRequest{Type: "messages_page", Params: params}
		if err := requestSchema.Validate(jsonValue(t, request)); err != nil {
			t.Fatal(err)
		}
	}
	if err := requestSchema.Validate(map[string]any{"type": "messages_page", "params": map[string]any{"public_history": "true"}}); err == nil {
		t.Fatal("schema accepted nonboolean public_history")
	}
	tools, _ := ProjectHistoryTools([]Message{{ID: "owner", Role: RoleAssistant, Content: []ContentBlock{{Type: BlockToolCall, ToolCallID: "call", Name: "read"}}}})
	page := RPCMessagesPage{
		Messages:     []Message{{ID: "owner", Role: RoleAssistant, Content: []ContentBlock{}}},
		HistoryTools: tools, HistoryToolsTruncated: true, Total: 1,
	}
	response := RPCResponse{Type: "response", Command: "messages_page", Success: true, Data: page}
	schema := resolveRPCSchema(t, "response.schema.json")
	if err := schema.Validate(jsonValue(t, response)); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "history_tools_truncated") || !strings.Contains(string(data), "history_tools") {
		t.Fatalf("public tool fields missing: %s", data)
	}
	value := jsonValue(t, response).(map[string]any)
	tool := value["data"].(map[string]any)["history_tools"].(map[string]any)["owner"].([]any)[0].(map[string]any)
	tool["arguments"] = "PRIVATE"
	if err := schema.Validate(value); err == nil {
		t.Fatal("schema accepted private history tool fields")
	}
	delete(tool, "arguments")
	tool["status"] = "running"
	if err := schema.Validate(value); err == nil {
		t.Fatal("schema accepted guessed running status")
	}
	data, err = json.Marshal(RPCMessagesPage{Messages: []Message{}})
	if err != nil || strings.Contains(string(data), "history_tools") {
		t.Fatalf("legacy page changed optional fields: %s, %v", data, err)
	}
}
