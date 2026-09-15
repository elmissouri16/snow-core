package protocol

import (
	json "encoding/json/v2"
	"strings"
	"testing"
)

func TestQueueControlEventCloneAndSafeClosedShape(t *testing.T) {
	original := AgentEvent{Type: EvQueueUpdated, QueueControl: &QueueControl{SessionID: "session", TurnID: "root", Revision: 3, Items: []QueueControlItem{}, ReviewItems: []QueueControlItem{{ID: "item", Text: "user text", State: "delivery_unknown"}}, Change: QueueControlChange{Kind: "delivery_unknown", ItemID: "item"}}}
	clone := original.Clone()
	clone.QueueControl.ReviewItems[0].Text = "changed"
	if original.QueueControl.ReviewItems[0].Text != "user text" {
		t.Fatal("queue state aliases observer snapshot")
	}
	wire, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolveRPCSchema(t, "agent-event.schema.json").Validate(jsonValue(t, original)); err != nil {
		t.Fatal(err)
	}
	var roundtrip AgentEvent
	if err := json.Unmarshal(wire, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.QueueControl.ReviewItems[0].State != "delivery_unknown" || roundtrip.QueueControl.Accepting {
		t.Fatal("unknown outcome became retryable")
	}
	invalid := strings.Replace(string(wire), `"delivery_unknown"`, `"retryable"`, 1)
	var value any
	if err := json.Unmarshal([]byte(invalid), &value); err != nil {
		t.Fatal(err)
	}
	if err := resolveRPCSchema(t, "agent-event.schema.json").Validate(value); err == nil {
		t.Fatal("open-ended queue lifecycle accepted")
	}
}
