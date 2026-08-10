package compact

import (
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func assistant(id, parent string, content []protocol.ContentBlock) protocol.Message {
	return protocol.NewAssistantMessage(id, parent, "p", "m", content, protocol.StopStop, nil)
}

func TestPlannerWithOptionsKeepsCompleteTurnsAndRealBoundary(t *testing.T) {
	msgs := []protocol.Message{
		{ID: "compaction-old", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("old summary")}},
		protocol.NewUserMessage("u1", "", "first"),
		assistant("a1", "u1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "c1", Name: "read"}}),
		protocol.NewToolResultMessage("t1", "a1", "c1", "read", []protocol.ContentBlock{protocol.NewTextBlock("result")}, false),
		assistant("a2", "t1", []protocol.ContentBlock{protocol.NewTextBlock("done")}),
		protocol.NewUserMessage("u2", "a2", "second"), assistant("a3", "u2", []protocol.ContentBlock{protocol.NewTextBlock("done2")}),
		protocol.NewUserMessage("u3", "a3", "third"), assistant("a4", "u3", []protocol.ContentBlock{protocol.NewTextBlock("done3")}),
	}
	plan := PlannerWithOptions(msgs, PlannerOptions{RetainTokens: 1, MinRetainedTurns: 2})
	if plan.KeepFrom != 5 || plan.BoundaryID != "a2" {
		t.Fatalf("plan=%+v", plan)
	}
	for _, msg := range msgs[plan.KeepFrom:] {
		if msg.ToolCallID == "c1" {
			t.Fatal("tool result split into retained tail")
		}
	}
}
