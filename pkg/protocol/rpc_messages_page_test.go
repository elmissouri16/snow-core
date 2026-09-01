package protocol

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestMessagesPageWireContract(t *testing.T) {
	page := RPCMessagesPage{
		Messages:   []Message{{ID: "m1", Role: RoleUser, Content: []ContentBlock{}}},
		NextCursor: "cursor",
		Start:      0,
		Total:      2,
		HasMore:    true,
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"messages", "next_cursor", "start", "total", "has_more"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("messages_page omitted %q: %s", field, encoded)
		}
	}
	if !slices.Contains(KnownRPCCommands(), "messages_page") {
		t.Fatal("messages_page command is not advertised")
	}
	if !slices.Contains(KnownRPCCapabilities(), "messages_page") {
		t.Fatal("messages_page capability is not advertised")
	}
}
