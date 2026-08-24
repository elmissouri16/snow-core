package session

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestContextProjectionPackedClonePreservesDefensiveOwnership(t *testing.T) {
	source := protocol.Message{
		ID: "first", ParentID: "root", Role: protocol.RoleAssistant,
		Content: []protocol.ContentBlock{{
			Type: protocol.BlockToolCall, Text: "payload", Data: []byte{1, 2, 3},
			ToolCallID: "call", Name: "tool", Arguments: json.RawMessage(`{"path":"one"}`),
		}},
		Provider: "provider", Model: "model", StopReason: protocol.StopToolUse,
		Usage:       &protocol.Usage{Input: 10, Output: 2, Total: 12, Cost: &protocol.Cost{Currency: "USD", Total: 0.5}},
		ToolDisplay: &protocol.ToolDisplay{Started: true, StartMessage: "start", Progress: []string{"one", "two"}, Output: "done"},
	}
	second := protocol.Message{
		ID: "second", ParentID: "first", Role: protocol.RoleTool,
		Content:     []protocol.ContentBlock{{Type: protocol.BlockText, Text: "second"}},
		ToolDisplay: &protocol.ToolDisplay{Progress: []string{"separate"}},
	}
	entries := []Entry{
		{Type: EntryMessage, ID: source.ID, ParentID: source.ParentID, Message: &source},
		{Type: EntryMessage, ID: second.ID, ParentID: second.ParentID, Message: &second},
	}

	projected := contextMessagesFromEntries(entries)
	if len(projected) != 2 || !reflect.DeepEqual(projected[0], source.Clone()) || !reflect.DeepEqual(projected[1], second.Clone()) {
		t.Fatalf("projection changed message values: %+v", projected)
	}

	projected[0].Content[0].Text = "mutated"
	projected[0].Content[0].Data[0] = 9
	projected[0].Content[0].Arguments[0] = '['
	projected[0].Content = append(projected[0].Content, protocol.NewTextBlock("appended"))
	projected[0].Usage.Input = 999
	projected[0].Usage.Cost.Total = 999
	projected[0].ToolDisplay.Progress[0] = "mutated"
	projected[0].ToolDisplay.Progress = append(projected[0].ToolDisplay.Progress, "appended")
	if projected[1].Content[0].Text != "second" || projected[1].ToolDisplay.Progress[0] != "separate" {
		t.Fatalf("packed projection fields alias across messages: %+v", projected[1])
	}

	again := contextMessagesFromEntries(entries)
	if !reflect.DeepEqual(again[0], source.Clone()) || !reflect.DeepEqual(again[1], second.Clone()) {
		t.Fatalf("caller mutation leaked into durable projection: %+v", again)
	}
}

func TestContextProjectionSkipsCompactionIndexForOrdinaryBranch(t *testing.T) {
	entries := []Entry{
		msg("a", "", "one"),
		{Type: EntryMeta, ID: "meta", ParentID: "a", Key: "key", Value: "value"},
		msg("b", "meta", "two"),
	}
	last, boundary := latestContextCompaction(entries)
	if last != -1 || boundary != -1 {
		t.Fatalf("ordinary branch boundary = (%d, %d), want (-1, -1)", last, boundary)
	}
	projected := contextMessagesFromEntries(entries)
	if len(projected) != 2 || projected[0].ID != "a" || projected[1].ID != "b" {
		t.Fatalf("ordinary projection = %+v", projected)
	}
}
