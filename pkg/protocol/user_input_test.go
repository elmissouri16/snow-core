package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserInputEventJSONShape(t *testing.T) {
	event := AgentEvent{Type: EvUserInputRequest, UserInput: &UserInputRequest{
		ID: "call-1", ToolCallID: "call-1",
		Questions: []UserInputQuestion{{
			ID: "format", Header: "Format", Question: "Which format?",
			Options: []UserInputOption{{Label: "JSON", Description: "Machine readable"}, {Label: "Text", Description: "Human readable"}},
		}},
	}}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type":"user_input_request"`, `"user_input":{"id":"call-1"`, `"tool_call_id":"call-1"`, `"options":[{"label":"JSON"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("event JSON %s missing %s", data, want)
		}
	}
}
