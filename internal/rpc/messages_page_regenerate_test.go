package rpc

import (
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPublicHistoryPreservesRegenerationLifecycleWithoutPrivateData(t *testing.T) {
	for _, test := range []struct {
		name    string
		modify  func(*protocol.Message)
		want    bool
		failure bool
	}{
		{name: "terminal", want: true},
		{name: "length", modify: func(m *protocol.Message) { m.StopReason = protocol.StopLength }, want: true},
		{name: "error-detail", modify: func(m *protocol.Message) { m.Error = "PRIVATE-ERROR" }, failure: true},
		{name: "failure-bit", modify: func(m *protocol.Message) { m.IsError = true }, failure: true},
		{name: "image", modify: func(m *protocol.Message) {
			m.Content = append(m.Content, protocol.ContentBlock{Type: protocol.BlockImage, Data: []byte("PRIVATE-IMAGE")})
		}},
		{name: "future-block", modify: func(m *protocol.Message) {
			m.Content = append(m.Content, protocol.ContentBlock{Type: "PRIVATE-FUTURE", Text: "PRIVATE-CONTENT"})
		}},
		{name: "assistant-tool-id", modify: func(m *protocol.Message) { m.ToolCallID = "PRIVATE-CALL-ID" }},
		{name: "assistant-tool-name", modify: func(m *protocol.Message) { m.ToolName = "PRIVATE-TOOL" }},
		{name: "plan", modify: func(m *protocol.Message) {
			m.Content = append(m.Content, protocol.ContentBlock{Type: protocol.BlockPlan, Text: "public plan"})
		}},
		{name: "tool-preface", modify: func(m *protocol.Message) { m.StopReason = protocol.StopToolUse }},
		{name: "tool-call", modify: func(m *protocol.Message) {
			m.Content = append(m.Content, protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read", Arguments: []byte(`{"secret":"PRIVATE-ARGUMENTS"}`)})
		}},
		{name: "aborted", modify: func(m *protocol.Message) { m.StopReason = protocol.StopAborted }},
		{name: "pending", modify: func(m *protocol.Message) { m.StopReason = protocol.StopPending }},
		{name: "error-stop", modify: func(m *protocol.Message) { m.StopReason = protocol.StopError }},
		{name: "unknown-stop", modify: func(m *protocol.Message) { m.StopReason = "PRIVATE-UNKNOWN-STOP" }},
		{name: "private-only", modify: func(m *protocol.Message) { m.Content = m.Content[1:] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := protocol.NewAssistantMessage("reply", "", "PRIVATE-PROVIDER", "PRIVATE-MODEL", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "public reply", Data: []byte("PRIVATE-TEXT-DATA"), Arguments: []byte(`{"secret":"PRIVATE-TEXT-ARGUMENTS"}`)}, {Type: protocol.BlockThinking, Text: "PRIVATE-THOUGHT"}, {Type: protocol.BlockProviderData, Data: []byte("PRIVATE-CONTINUITY")}}, protocol.StopStop, nil)
			original.PluginDetails = []byte(`{"private":"PRIVATE-PLUGIN"}`)
			if test.modify != nil {
				test.modify(&original)
			}
			before, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			page, err := buildMessagesPage("history", []protocol.Message{original}, publicPageParams(1))
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Messages) != 1 {
				t.Fatal("public history lost reply")
			}
			projected := page.Messages[0]
			if projected.IsRegeneratableReply() != test.want || projected.IsError != test.failure || projected.Error != "" {
				t.Fatalf("unsafe public lifecycle: %+v", projected)
			}
			if projected.IsRegeneratableReply() && !original.IsRegeneratableReply() {
				t.Fatal("redaction created false regeneration eligibility")
			}
			if test.want && projected.StopReason != original.StopReason {
				t.Fatal("terminal metadata lost")
			}
			wire, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "PRIVATE") {
				t.Fatalf("private metadata leaked: %s", wire)
			}
			after, _ := json.Marshal(original)
			if string(before) != string(after) {
				t.Fatal("public projection mutated durable source")
			}
			if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, mustWireJSON(t, Response{ID: "history", Type: "response", Command: "messages_page", Success: true, Data: page}))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func mustWireJSON(t *testing.T, value any) []byte {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
