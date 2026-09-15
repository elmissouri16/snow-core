package web

import (
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimePlanOnlyToolsRetainAssistantArticle(t *testing.T) {
	r := &liveRuntime{assistant: -1, plan: -1}
	r.projectHistory(protocol.RPCMessagesPage{Messages: []protocol.Message{{
		ID: "owner", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
			{Type: protocol.BlockPlan, Text: "Plan text"},
			{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read"},
		},
	}}})
	if len(r.snapshot.Messages) != 2 || r.snapshot.Messages[0].Role != "plan" || len(r.snapshot.Messages[0].Tools) != 0 || r.snapshot.Messages[1].Role != "assistant" || len(r.snapshot.Messages[1].Tools) != 1 {
		t.Fatalf("tools disappeared into a plan-only article: %+v", r.snapshot.Messages)
	}
}

func TestRuntimeAuthoritativeEmptyHistoryMapPreventsPartialPairing(t *testing.T) {
	r := &liveRuntime{assistant: -1, plan: -1}
	r.projectHistory(protocol.RPCMessagesPage{
		Messages:     []protocol.Message{{ID: "owner", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "Text"}, {Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read"}}}},
		HistoryTools: map[string][]protocol.RPCHistoryTool{}, HistoryToolsTruncated: true,
	})
	if !r.snapshot.HistoryToolsTruncated || len(r.snapshot.Messages) != 1 || len(r.snapshot.Messages[0].Tools) != 0 {
		t.Fatalf("raw partial page overrode authoritative omission: %+v", r.snapshot)
	}
}
