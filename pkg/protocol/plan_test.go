package protocol

import (
	"encoding/json"
	"testing"
)

func TestParseCollaborationMode(t *testing.T) {
	for input, want := range map[string]CollaborationMode{"": ModeDefault, "default": ModeDefault, "plan": ModePlan} {
		got, err := ParseCollaborationMode(input)
		if err != nil || got != want {
			t.Fatalf("ParseCollaborationMode(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := ParseCollaborationMode("execute"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestPlanBlockAndEventJSONRoundTrip(t *testing.T) {
	msg := NewAssistantMessage("a", "", "p", "m", []ContentBlock{{Type: BlockPlan, Text: "# Do it"}}, StopStop, nil)
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Message
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Content) != 1 || decoded.Content[0].Type != BlockPlan || decoded.Content[0].Text != "# Do it" {
		t.Fatalf("decoded = %+v", decoded)
	}
	ev := AgentEvent{Type: EvPlanCompleted, Plan: &PlanItem{ID: "p1", Text: "# Do it"}}
	raw, _ = json.Marshal(ev)
	if string(raw) == "" || !json.Valid(raw) {
		t.Fatalf("event JSON = %s", raw)
	}
}
