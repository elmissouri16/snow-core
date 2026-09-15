package protocol

import (
	json "encoding/json/v2"
	"testing"
)

func TestMessageRegeneratableReplyLocalEligibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		modify func(*Message)
		want   bool
	}{
		{name: "terminal", want: true},
		{name: "length", modify: func(m *Message) { m.StopReason = StopLength }, want: true},
		{name: "continuity-with-text", modify: func(m *Message) {
			m.Content = append(m.Content, ContentBlock{Type: BlockProviderData, Data: []byte("private")}, ContentBlock{Type: BlockThinking, Text: "private"})
		}, want: true},
		{name: "tool-preface", modify: func(m *Message) { m.StopReason = StopToolUse }},
		{name: "plan", modify: func(m *Message) { m.Content = append(m.Content, ContentBlock{Type: BlockPlan, Text: "plan"}) }},
		{name: "image", modify: func(m *Message) { m.Content = append(m.Content, ContentBlock{Type: BlockImage, Data: []byte("image")}) }},
		{name: "future-block", modify: func(m *Message) { m.Content = append(m.Content, ContentBlock{Type: "future"}) }},
		{name: "assistant-tool-id", modify: func(m *Message) { m.ToolCallID = "call" }},
		{name: "assistant-tool-name", modify: func(m *Message) { m.ToolName = "tool" }},
		{name: "failure-bit", modify: func(m *Message) { m.IsError = true }},
		{name: "failure-detail", modify: func(m *Message) { m.Error = "private error" }},
		{name: "aborted", modify: func(m *Message) { m.StopReason = StopAborted }},
		{name: "error-stop", modify: func(m *Message) { m.StopReason = StopError }},
		{name: "pending", modify: func(m *Message) { m.StopReason = StopPending }},
		{name: "unknown-stop", modify: func(m *Message) { m.StopReason = "future" }},
		{name: "missing-stop", modify: func(m *Message) { m.StopReason = "" }},
		{name: "private-only", modify: func(m *Message) { m.Content = []ContentBlock{{Type: BlockThinking, Text: "private"}} }},
		{name: "whitespace", modify: func(m *Message) { m.Content[0].Text = " \n" }},
		{name: "user", modify: func(m *Message) { m.Role = RoleUser }},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := NewAssistantMessage("reply", "", "fake", "fake-1", []ContentBlock{NewTextBlock("reply")}, StopStop, nil)
			if test.modify != nil {
				test.modify(&message)
			}
			if got := message.IsRegeneratableReply(); got != test.want {
				t.Fatalf("eligibility=%t want=%t", got, test.want)
			}
		})
	}
}

func TestMessagePublicRegenerationMetadataRoundTripAndSchema(t *testing.T) {
	schema := resolveRPCSchema(t, "message.schema.json")
	for _, reason := range []StopReason{StopStop, StopLength, StopToolUse, StopError, StopAborted, StopPending} {
		for _, failed := range []bool{false, true} {
			original := Message{ID: "reply", Role: RoleAssistant, StopReason: reason, IsError: failed, Content: []ContentBlock{NewTextBlock("public reply")}}
			wire, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Message
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.StopReason != reason || decoded.IsError != failed || decoded.IsRegeneratableReply() != original.IsRegeneratableReply() {
				t.Fatalf("lost public lifecycle: %s", wire)
			}
			if err := schema.Validate(jsonValue(t, original)); err != nil {
				t.Fatal(err)
			}
		}
	}
}
