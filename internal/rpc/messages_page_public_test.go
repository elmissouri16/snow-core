package rpc

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func publicPageParams(limit int) protocol.RPCMessagesPageParams {
	return protocol.RPCMessagesPageParams{PublicHistory: true, Limit: limit, MaxBytes: defaultMessagesPageBytes}
}

func publicPageHistory() []protocol.Message {
	return []protocol.Message{
		{ID: "owner", Role: protocol.RoleAssistant, Provider: "PRIVATE-PROVIDER", Model: "PRIVATE-MODEL", Error: "PRIVATE-ERROR", Content: []protocol.ContentBlock{
			{Type: protocol.BlockProviderData, Data: []byte("PRIVATE-CONTINUITY")},
			{Type: protocol.BlockThinking, Text: "PRIVATE-THINKING"},
			{Type: protocol.BlockText, Text: "public answer", Data: []byte("PRIVATE-DATA"), Arguments: []byte(`{"private":"PRIVATE-ARGS"}`)},
			{Type: protocol.BlockPlan, Text: "public plan", PlanComplete: true, Data: []byte("PRIVATE-PLAN-DATA")},
			{Type: protocol.BlockImage, Data: []byte("PRIVATE-IMAGE")},
			{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read", Text: "PRIVATE-CALL-TEXT", Arguments: []byte(`{"private":"PRIVATE-ARGS"}`)},
		}},
		{ID: "result", ParentID: "owner", Role: protocol.RoleTool, ToolCallID: "call", ToolName: "read", IsError: true,
			Content:     []protocol.ContentBlock{protocol.NewTextBlock("PRIVATE-RAW-RESULT")},
			ToolDisplay: &protocol.ToolDisplay{Output: "PRIVATE-DISPLAY"}, PluginDetails: []byte(`{"private":"PRIVATE-PLUGIN"}`),
			PublicToolResult: &protocol.ToolResultPreview{Text: "public result"}},
		protocol.NewUserMessage("user", "result", "next question"),
	}
}

func TestPublicMessagesPageIncludesTrailingUnresolvedOwner(t *testing.T) {
	messages := publicPageHistory()[:1]
	page, err := buildMessagesPage("history", messages, publicPageParams(1))
	if err != nil {
		t.Fatal(err)
	}
	tools := page.HistoryTools["owner"]
	if page.Total != 1 || len(page.Messages) != 1 || len(tools) != 1 || tools[0].Status != "unresolved" || tools[0].OutputAvailable || tools[0].Output != "" {
		t.Fatalf("incomplete public history = %+v", page)
	}
	legacy, err := buildMessagesPage("history", publicMessages(messages), protocol.RPCMessagesPageParams{Limit: 1, MaxBytes: defaultMessagesPageBytes})
	if err != nil || legacy.Total != 0 || len(legacy.Messages) != 0 || legacy.HistoryTools != nil {
		t.Fatalf("legacy stable-pair behavior changed: %+v, %v", legacy, err)
	}
}

func TestPublicMessagesPageCompletesOwnerBeyondPageEnd(t *testing.T) {
	messages := publicPageHistory()
	page, err := buildMessagesPage("history", messages, publicPageParams(1))
	if err != nil {
		t.Fatal(err)
	}
	tools := page.HistoryTools["owner"]
	if len(page.Messages) != 1 || page.Messages[0].ID != "owner" || page.Total != 3 || !page.HasMore || len(tools) != 1 || tools[0].Status != "failed" || tools[0].ResultID != "result" || tools[0].Output != "public result" {
		t.Fatalf("page-end result not definitively paired: %+v", page)
	}
	want, _ := protocol.ProjectHistoryTools(messages)
	if !reflect.DeepEqual(page.HistoryTools, want) {
		t.Fatalf("RPC/catalog tool projection differs: got %+v want %+v", page.HistoryTools, want)
	}
	params := publicPageParams(1)
	params.Cursor = page.NextCursor
	next, err := buildMessagesPage("history", messages, params)
	if err != nil || len(next.Messages) != 1 || next.Messages[0].ID != "result" || len(next.HistoryTools) != 0 {
		t.Fatalf("result-only page should not repeat owner tools: %+v, %v", next, err)
	}
	// Complete-interval scanning must see a duplicate even after a valid result.
	duplicate := messages[1].Clone()
	duplicate.ID = "duplicate"
	ambiguous := []protocol.Message{messages[0], messages[1], duplicate, messages[2]}
	page, err = buildMessagesPage("history", ambiguous, publicPageParams(1))
	if err != nil {
		t.Fatal(err)
	}
	if tool := page.HistoryTools["owner"][0]; tool.Status != "unresolved" || tool.OutputAvailable || tool.ResultID != "" {
		t.Fatalf("page-end ambiguity falsely resolved: %+v", tool)
	}
	// A user boundary ends ownership even when a matching result follows it.
	page, err = buildMessagesPage("history", []protocol.Message{messages[0], messages[2], messages[1]}, publicPageParams(1))
	if err != nil || page.HistoryTools["owner"][0].Status != "unresolved" {
		t.Fatalf("crossed ownership boundary: %+v, %v", page, err)
	}
}

func TestPublicMessagesPageAllowlistAndStableToolIDs(t *testing.T) {
	messages := publicPageHistory()
	page, err := buildMessagesPage("history", messages, publicPageParams(32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"PRIVATE", `"arguments"`, `"data"`, `"tool_display"`, `"plugin_details"`, `"provider"`, `"thinking"`, `"model"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public history leaked %s: %s", forbidden, encoded)
		}
	}
	if len(page.Messages[1].Content) != 0 || page.Messages[1].PublicToolResult == nil || page.Messages[1].PublicToolResult.Text != "public result" {
		t.Fatalf("tool-result allowlist = %+v", page.Messages[1])
	}
	rawTools, _ := protocol.ProjectHistoryTools(messages)
	safeTools, _ := protocol.ProjectHistoryTools(page.Messages)
	if !reflect.DeepEqual(rawTools, safeTools) {
		t.Fatalf("stripping private blocks changed tool identity: raw %+v safe %+v", rawTools, safeTools)
	}
	page.Messages[1].PublicToolResult.Text = "changed"
	if messages[1].PublicToolResult.Text != "public result" {
		t.Fatal("public DTO aliases durable preview")
	}
	messages[1].PublicToolResult = nil
	page, err = buildMessagesPage("history", messages, publicPageParams(32))
	if err != nil || page.Messages[1].PublicToolResult != nil || page.HistoryTools["owner"][0].OutputAvailable {
		t.Fatalf("legacy/private result fabricated public output: %+v, %v", page, err)
	}
}

func TestMessagesPageCursorBindsPublicHistoryMode(t *testing.T) {
	for _, public := range []bool{false, true} {
		params := protocol.RPCMessagesPageParams{PublicHistory: public, Limit: 1, MaxBytes: defaultMessagesPageBytes}
		messages := linkedMessages(3)
		first, err := buildMessagesPage("history", messages, params)
		if err != nil {
			t.Fatal(err)
		}
		params.Cursor = first.NextCursor
		params.PublicHistory = !public
		if _, err := buildMessagesPage("history", messages, params); err == nil || !strings.Contains(err.Error(), "public_history mode") {
			t.Fatalf("mode switch from %v accepted: %v", public, err)
		}
		params.PublicHistory = public
		messages = append(messages, protocol.NewUserMessage("later", messages[2].ID, "append"))
		next, err := buildMessagesPage("history", messages, params)
		if err != nil || next.Start != 1 || next.Total != 3 {
			t.Fatalf("same-mode snapshot changed: %+v, %v", next, err)
		}
	}
}

func TestPublicMessagesPageBoundsToolsAndEncodedFrame(t *testing.T) {
	messages := publicPageHistory()[:1]
	messages[0].Content = nil
	for i := range protocol.RPCHistoryMaxTools + 1 {
		callID := fmt.Sprintf("call-%d", i)
		messages[0].Content = append(messages[0].Content, protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read"})
		messages = append(messages, protocol.Message{ID: fmt.Sprintf("result-%d", i), Role: protocol.RoleTool, ToolCallID: callID, ToolName: "read", PublicToolResult: &protocol.ToolResultPreview{Text: strings.Repeat("\x00", protocol.RPCHistoryMaxOutputBytes)}})
	}
	params := publicPageParams(128)
	params.MaxBytes = minMessagesPageBytes
	page, err := buildMessagesPage("history", messages, params)
	if err != nil {
		t.Fatal(err)
	}
	// The oversized first owner still makes progress under the existing soft
	// byte-budget rule, but its tool history participates in the frame size.
	if len(page.Messages) != 1 || len(page.HistoryTools["owner"]) != protocol.RPCHistoryMaxTools || !page.HistoryToolsTruncated {
		t.Fatalf("tool budget or soft frame budget not enforced: messages %d tools %d truncated %v", len(page.Messages), len(page.HistoryTools["owner"]), page.HistoryToolsTruncated)
	}
	outputBytes := 0
	for _, tool := range page.HistoryTools["owner"] {
		outputBytes += len(tool.Output)
	}
	if outputBytes != protocol.RPCHistoryMaxTotalOutputBytes {
		t.Fatalf("total projected output bytes = %d", outputBytes)
	}
	size, err := messagesPageFrameSize("history", page)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := jsonv1.Marshal(Response{ID: "history", Type: "response", Command: "messages_page", Success: true, Data: page})
	if err != nil || size != len(frame)+1 || size > maxMessagesPageBytes {
		t.Fatalf("complete encoded-frame bound: size %d encoded %d err %v", size, len(frame)+1, err)
	}
	withoutTools := page
	withoutTools.HistoryTools = nil
	baseSize, err := messagesPageFrameSize("history", withoutTools)
	if err != nil || baseSize >= minMessagesPageBytes || size <= minMessagesPageBytes {
		t.Fatalf("fixture failed to exercise tool wire budget: without %d with %d err %v", baseSize, size, err)
	}
}

func TestPublicMessagesPageRejectsHardFrameOverflowIncludingTools(t *testing.T) {
	messages := publicPageHistory()[:2]
	messages[0].Content[2].Text = strings.Repeat("x", maxMessagesPageBytes-20*1024)
	messages[1].PublicToolResult.Text = strings.Repeat("\x00", protocol.RPCHistoryMaxOutputBytes)
	if _, err := buildMessagesPage("history", messages, publicPageParams(1)); err == nil || !strings.Contains(err.Error(), "byte frame limit") {
		t.Fatalf("tool history overflow escaped hard frame bound: %v", err)
	}
}

func TestPublicMessagesPagePreservesUnknownToolOutcome(t *testing.T) {
	for _, isError := range []bool{false, true} {
		for _, preview := range []*protocol.ToolResultPreview{nil, {Text: "PRIVATE_UNKNOWN_PREVIEW", Truncated: true}} {
			messages := publicPageHistory()[:2]
			messages[1].ToolOutcomeUnknown = true
			messages[1].IsError, messages[1].PublicToolResult = isError, preview
			projected := publicHistoryMessages(messages)
			if !projected[1].ToolOutcomeUnknown || projected[1].IsError != isError || projected[1].PublicToolResult != nil || len(projected[1].Content) != 0 {
				t.Fatalf("allowlist dropped recovery provenance or exposed output: %+v", projected[1])
			}
			page, err := buildMessagesPage("history", messages, publicPageParams(2))
			if err != nil {
				t.Fatal(err)
			}
			tool := page.HistoryTools["owner"][0]
			if tool.Status != "unresolved" || tool.ResultID != "" || tool.OutputAvailable || tool.Output != "" || tool.Truncated {
				t.Fatalf("RPC invented a definitive outcome: %+v", tool)
			}
			if len(page.Messages) != 2 || !page.Messages[1].ToolOutcomeUnknown || page.Messages[1].PublicToolResult != nil {
				t.Fatalf("wire message lost recovery provenance: %+v", page.Messages)
			}
			encoded, err := json.Marshal(page)
			if err != nil || strings.Contains(string(encoded), "PRIVATE") {
				t.Fatalf("RPC leaked recovery output: %s, %v", encoded, err)
			}
		}
	}
}
