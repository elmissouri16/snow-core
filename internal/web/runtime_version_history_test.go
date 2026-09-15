package web

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func versionHistoryToolsPage() protocol.RPCBranchMessagesPage {
	return protocol.RPCBranchMessagesPage{SessionID: "session", BranchID: "target", TipID: "tip", Start: 1, Total: 3, NextCursor: "older-page", Messages: []protocol.Message{{ID: "assistant", Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "public reply"}, {Type: protocol.BlockToolCall, Name: "read", ToolCallID: "reused-call-id"}}}}, HistoryTools: map[string][]protocol.RPCHistoryTool{"assistant": {{ID: "stable-public-tool", OwnerID: "assistant", ResultID: "result-outside-page", Tool: "read", Status: "completed", Output: "authoritative public result", OutputAvailable: true}}}, HistoryToolsTruncated: true}
}

func TestVersionHistoryUsesAuthoritativeCrossPageToolsAndOwnsProjection(t *testing.T) {
	page := versionHistoryToolsPage()
	if !validVersionHistory(page, "session", "target", "tip", "") {
		t.Fatal("valid cross-page public provenance rejected")
	}
	projected := projectVersionHistory(page)
	if !projected.HistoryTruncated || !projected.HistoryToolsTruncated || len(projected.Messages) != 1 || len(projected.Messages[0].Tools) != 1 {
		t.Fatalf("tool metadata omitted: %+v", projected)
	}
	tool := projected.Messages[0].Tools[0]
	if tool.Status != "completed" || tool.ResultID != "result-outside-page" || tool.Output != "authoritative public result" {
		t.Fatalf("cross-page result reconstructed or lost: %+v", tool)
	}
	page.HistoryTools["assistant"][0].Output = "source changed"
	if projected.Messages[0].Tools[0].Output != "authoritative public result" {
		t.Fatal("source map aliases projected tool slice")
	}
	r := &liveRuntime{ctx: t.Context(), cancel: func() {}, instanceID: "old", assistant: -1, plan: -1, snapshot: RuntimeSnapshot{InstanceID: "old", SessionID: "session", Status: "idle"}}
	committed := protocol.RPCBranchRestoreCommitted{SessionID: "session", BranchID: "target", TipID: "tip", Mode: protocol.ModeDefault, History: page}
	restored, err := r.publishVersionRestore("old", committed, 0)
	if err != nil || !restored.HistoryToolsTruncated || restored.Messages[0].Tools[0].ResultID != "result-outside-page" {
		t.Fatalf("restore omitted authoritative tool metadata: %+v %v", restored, err)
	}
}

func TestVersionHistoryMissingOrEmptyMapNeverUsesPartialRawFallback(t *testing.T) {
	for _, explicitEmpty := range []bool{false, true} {
		page := versionHistoryToolsPage()
		page.HistoryTools = nil
		if explicitEmpty {
			page.HistoryTools = map[string][]protocol.RPCHistoryTool{}
		}
		page.Messages = append(page.Messages, protocol.Message{ID: "result", Role: protocol.RoleTool, ToolCallID: "reused-call-id", ToolName: "read", PublicToolResult: &protocol.ToolResultPreview{Text: "must not reconstruct"}})
		projected := projectVersionHistory(page)
		encoded, _ := json.Marshal(projected)
		if len(projected.Messages[0].Tools) != 0 || strings.Contains(string(encoded), "must not reconstruct") || !projected.HistoryToolsTruncated {
			t.Fatalf("missing authoritative map permitted raw fallback: %s", encoded)
		}
	}
}

func TestVersionHistoryRejectsUnboundedOrForeignToolMetadata(t *testing.T) {
	for _, mutate := range []func(*protocol.RPCBranchMessagesPage){
		func(p *protocol.RPCBranchMessagesPage) { p.HistoryTools["foreign"] = p.HistoryTools["assistant"] },
		func(p *protocol.RPCBranchMessagesPage) { p.HistoryTools["assistant"][0].OwnerID = "foreign" },
		func(p *protocol.RPCBranchMessagesPage) {
			p.HistoryTools["assistant"] = append(p.HistoryTools["assistant"], p.HistoryTools["assistant"][0])
		},
		func(p *protocol.RPCBranchMessagesPage) {
			p.HistoryTools["assistant"][0].Output = strings.Repeat("x", protocol.RPCHistoryMaxOutputBytes+1)
		},
		func(p *protocol.RPCBranchMessagesPage) { p.HistoryTools["assistant"][0].Status = "running" },
		func(p *protocol.RPCBranchMessagesPage) { p.HistoryTools["assistant"][0].OutputAvailable = false },
		func(p *protocol.RPCBranchMessagesPage) { p.HistoryTools["assistant"][0].ResultID = "" },
		func(p *protocol.RPCBranchMessagesPage) {
			p.HistoryTools["assistant"] = slices.Repeat(p.HistoryTools["assistant"], protocol.RPCHistoryMaxTools+1)
		},
	} {
		page := versionHistoryToolsPage()
		mutate(&page)
		if validVersionHistory(page, "session", "target", "tip", "") {
			t.Fatal("malformed public tool provenance accepted")
		}
	}
}
