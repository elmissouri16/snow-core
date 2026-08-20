package subagent

import (
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestForkContextSanitizesAndSelectsTurns(t *testing.T) {
	messages := []protocol.Message{
		protocol.NewUserMessage("u1", "", "first"),
		protocol.NewAssistantMessage("a1", "u1", "p", "m", []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "secret"}, {Type: protocol.BlockText, Text: "answer1"}, {Type: protocol.BlockToolCall, ToolCallID: "complete", Name: "read"}, {Type: protocol.BlockToolCall, ToolCallID: "dangling", Name: "read"}}, protocol.StopToolUse, nil),
		protocol.NewToolResultMessage("r1", "a1", "complete", "read", []protocol.ContentBlock{protocol.NewTextBlock("ok")}, false),
		protocol.NewUserMessage("u2", "r1", "second"),
		protocol.NewAssistantMessage("a2", "u2", "p", "m", []protocol.ContentBlock{{Type: protocol.BlockPlan, Text: "partial"}, {Type: protocol.BlockText, Text: "answer2"}}, protocol.StopStop, nil),
		{ID: "mail", Role: protocol.RoleAgent, Content: []protocol.ContentBlock{protocol.NewTextBlock("old collaboration")}},
	}
	st, err := ForkContext(messages, "1", "/tmp", "child")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Role != protocol.RoleUser || got[0].Content[0].Text != "second" {
		t.Fatalf("got=%+v", got)
	}
	if len(got[1].Content) != 1 || got[1].Content[0].Text != "answer2" {
		t.Fatalf("assistant=%+v", got[1])
	}
	if got[0].ParentID == "" || got[1].ParentID != got[0].ID {
		t.Fatalf("parents not rebuilt: %+v", got)
	}

	all, err := ForkContext(messages, "all", "/tmp", "child2")
	if err != nil {
		t.Fatal(err)
	}
	defer all.Close()
	msgs, _ := all.Messages()
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == protocol.BlockThinking || b.ToolCallID == "dangling" {
				t.Fatalf("unsanitized %+v", b)
			}
		}
		if m.Role == protocol.RoleAgent {
			t.Fatal("old mail copied")
		}
	}
}

func TestParseForkTurns(t *testing.T) {
	for _, v := range []string{"none", "all", "1", "12", ""} {
		if _, err := ParseForkTurns(v); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
	for _, v := range []string{"0", "-1", "x"} {
		if _, err := ParseForkTurns(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}
