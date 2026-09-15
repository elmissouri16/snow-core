package web

import (
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimeSavedToolsStayWithTheirOwningMessage(t *testing.T) {
	owner := protocol.Message{ID: "owner", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "Before"},
		{Type: protocol.BlockPlan, Text: "Plan"},
		{Type: protocol.BlockText, Text: "After"},
		{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read"},
	}}
	result := protocol.Message{ID: "result", ParentID: "owner", Role: protocol.RoleTool, ToolCallID: "call", ToolName: "read", PublicToolResult: &protocol.ToolResultPreview{Text: "public <script>text</script>"}, Content: []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "never public"}}}
	r := &liveRuntime{assistant: -1, plan: -1}
	r.projectHistory(protocol.RPCMessagesPage{Messages: []protocol.Message{owner, result}})
	if len(r.snapshot.Messages) != 3 {
		t.Fatalf("lost mixed saved segments: %+v", r.snapshot.Messages)
	}
	for i, message := range r.snapshot.Messages {
		if i != 2 && len(message.Tools) != 0 {
			t.Fatal("tool attached more than once")
		}
	}
	saved := r.snapshot.Messages[2].Tools
	if len(saved) != 1 || saved[0].OwnerID != "owner" || saved[0].ResultID != "result" || saved[0].Output != "public <script>text</script>" || !saved[0].OutputAvailable {
		t.Fatalf("wrong public tool projection: %+v", saved)
	}
	copy := r.snapshot.clone()
	copy.Messages[2].Tools[0].Output = "modified"
	if r.snapshot.Messages[2].Tools[0].Output == "modified" {
		t.Fatal("snapshot clone aliases tool history")
	}
	other := &liveRuntime{assistant: -1, plan: -1}
	other.projectHistory(protocol.RPCMessagesPage{Messages: []protocol.Message{owner, result}})
	if saved[0].ID != other.snapshot.Messages[2].Tools[0].ID {
		t.Fatal("saved tool identity changed on reload")
	}
}

func TestRuntimeToolOnlyOwnerDoesNotUseTruncatedSourceIdentity(t *testing.T) {
	prefix := strings.Repeat("x", 128)
	messages := []protocol.Message{
		{ID: prefix + "-one", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "Earlier owner"}}},
		{ID: prefix + "-two", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "pending", Name: "write"}}},
	}
	r := &liveRuntime{assistant: -1, plan: -1}
	r.projectHistory(protocol.RPCMessagesPage{Messages: messages})
	if len(r.snapshot.Messages) != 2 || len(r.snapshot.Messages[0].Tools) != 0 || len(r.snapshot.Messages[1].Tools) != 1 {
		t.Fatalf("empty owner merged with unrelated message: %+v", r.snapshot.Messages)
	}
	if tool := r.snapshot.Messages[1].Tools[0]; tool.Status != "unresolved" || tool.OutputAvailable || tool.OwnerID != messages[1].ID {
		t.Fatalf("missing result misrepresented: %+v", tool)
	}
}

func TestRuntimeLegacyToolOutputIsNotInferred(t *testing.T) {
	messages := []protocol.Message{
		{ID: "owner", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read", Arguments: []byte(`{"private":"argument"}`)}}},
		{ID: "result", Role: protocol.RoleTool, ToolCallID: "call", ToolName: "read", Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "legacy content"}}, ToolDisplay: &protocol.ToolDisplay{Output: "private display"}},
	}
	r := &liveRuntime{assistant: -1, plan: -1}
	r.projectHistory(protocol.RPCMessagesPage{Messages: messages})
	tool := r.snapshot.Messages[0].Tools[0]
	if tool.OutputAvailable || tool.Output != "" || tool.Status != "completed" {
		t.Fatalf("legacy text inferred or recorded result lost: %+v", tool)
	}
}
