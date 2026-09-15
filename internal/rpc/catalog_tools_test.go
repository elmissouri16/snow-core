package rpc

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestServeCatalogPublicToolsOptInAndSchemas(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store, err := session.NewFileIndex(root).Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	owner := protocol.Message{ID: "owner", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{
		{Type: protocol.BlockThinking, Text: "PRIVATE_THINKING"},
		{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read", Arguments: []byte(`{"private":"PRIVATE_ARGS"}`)},
	}}
	result := protocol.Message{ID: "result", Role: protocol.RoleTool, ToolCallID: "call", PublicToolResult: &protocol.ToolResultPreview{Text: "explicit <public>\nresult"}, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "PRIVATE_CONTENT"}}}
	for _, message := range []protocol.Message{owner, result} {
		if err := store.Append(session.Entry{ID: message.ID, Type: session.EntryMessage, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	id := store.ID()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var input strings.Builder
	requests := resolveWireSchema(t, "catalog-request.schema.json")
	outputs := resolveWireSchema(t, "catalog-output.schema.json")
	for _, include := range []bool{false, true} {
		params, err := json.Marshal(protocol.RPCCatalogMessagesParams{SessionID: id, Limit: 1, IncludeTools: include})
		if err != nil {
			t.Fatal(err)
		}
		req, err := json.Marshal(protocol.RPCRequest{Type: "catalog_messages", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		if err := requests.Validate(decodedJSON(t, req)); err != nil {
			t.Fatal(err)
		}
		input.Write(req)
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	if err := ServeCatalog(t.Context(), strings.NewReader(input.String()), &output, cwd, root, "test"); err != nil {
		t.Fatal(err)
	}
	frames := catalogFrames(t, output.String())
	for _, frame := range frames {
		if err := outputs.Validate(frame); err != nil {
			t.Fatal(err)
		}
	}
	for _, secret := range []string{"PRIVATE", "tool_call_id", "public_tool_result", "arguments"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	legacy := frames[1]["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)
	if _, exists := legacy["tools"]; exists {
		t.Fatal("tools appeared without opt-in")
	}
	opted := frames[2]["data"].(map[string]any)["messages"].([]any)[0].(map[string]any)
	tools := opted["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["output"] != "explicit <public>\nresult" || tools[0].(map[string]any)["result_id"] != "result" {
		t.Fatalf("tools=%+v", tools)
	}
	// Additive schemas remain strict: no private DTO fields or guessed states.
	for _, field := range []string{"arguments", "thinking", "tool_display", "public_tool_result"} {
		tool := tools[0].(map[string]any)
		tool[field] = "secret"
		if err := outputs.Validate(frames[2]); err == nil {
			t.Fatalf("accepted %s", field)
		}
		delete(tool, field)
	}
	for _, status := range []string{"running", "canceled"} {
		tool := tools[0].(map[string]any)
		tool["status"] = status
		if err := outputs.Validate(frames[2]); err == nil {
			t.Fatalf("accepted history status %s", status)
		}
	}
	if err := requests.Validate(decodedJSON(t, []byte(`{"type":"catalog_messages","params":{"session_id":"s","include_tools":"true"}}`))); err == nil {
		t.Fatal("accepted nonboolean opt-in")
	}
}

func TestCatalogWireBoundIncludesToolsForEmptyTextOwner(t *testing.T) {
	owner := protocol.Message{ID: strings.Repeat("\x00", protocol.RPCHistoryMaxIDBytes), Role: protocol.RoleAssistant}
	for i := range protocol.RPCHistoryMaxTools {
		owner.Content = append(owner.Content, protocol.ContentBlock{Type: protocol.BlockToolCall, Name: "read", ToolCallID: string(rune('a' + i))})
	}
	messages := []protocol.Message{owner}
	for i, call := range owner.Content {
		// Distinct bounded result IDs with worst-case JSON escaping.
		messages = append(messages, protocol.Message{ID: strings.Repeat("\x00", protocol.RPCHistoryMaxIDBytes-2) + fmt.Sprintf("%02d", i), Role: protocol.RoleTool, ToolCallID: call.ToolCallID, PublicToolResult: &protocol.ToolResultPreview{Text: strings.Repeat("\x00", protocol.RPCHistoryMaxOutputBytes)}})
	}
	tools, _ := protocol.ProjectHistoryTools(messages)
	page := protocol.RPCCatalogMessagesPage{Messages: []protocol.RPCCatalogMessage{{ID: owner.ID, Role: "assistant", Tools: tools[owner.ID]}}, NextOffset: 1}
	response := Response{Type: "response", Command: "catalog_messages", Success: true, Data: page}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)+1 <= protocol.RPCCatalogMaxOutputBytes {
		t.Fatal("fixture does not exceed wire bound")
	}
	bounded, err := boundCatalogResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)+1 > protocol.RPCCatalogMaxOutputBytes {
		t.Fatalf("wire bytes=%d", len(encoded)+1)
	}
	got := bounded.Data.(protocol.RPCCatalogMessagesPage)
	if !got.ToolsTruncated || len(got.Messages) != 1 || got.NextOffset != 1 || got.HasMore || got.Messages[0].Text != "" || len(got.Messages[0].Tools) == 0 {
		t.Fatalf("bounded=%+v", got)
	}
	if err := resolveWireSchema(t, "catalog-output.schema.json").Validate(decodedJSON(t, encoded)); err != nil {
		t.Fatal(err)
	}
	for _, tool := range got.Messages[0].Tools {
		if tool.OwnerID != owner.ID || len(tool.ResultID) != protocol.RPCHistoryMaxIDBytes {
			t.Fatal("collision-truncated identity")
		}
	}
}
