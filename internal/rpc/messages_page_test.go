package rpc

import (
	"encoding/base64"
	jsonv1 "encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessagesPageHydratesStableAppendOnlySnapshot(t *testing.T) {
	messages := linkedMessages(7)
	params := protocol.RPCMessagesPageParams{Limit: 2, MaxBytes: defaultMessagesPageBytes}
	var got []protocol.Message
	for {
		page, err := buildMessagesPage("history", messages, params)
		if err != nil {
			t.Fatal(err)
		}
		if page.Start != len(got) || page.Total != 7 {
			t.Fatalf("page bounds = start %d total %d, loaded %d", page.Start, page.Total, len(got))
		}
		got = append(got, page.Messages...)
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatalf("terminal next cursor = %q", page.NextCursor)
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatal("non-terminal page omitted cursor")
		}
		cursorPayload, err := base64.RawURLEncoding.DecodeString(page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(cursorPayload), "message-") {
			t.Fatalf("cursor exposes durable message ids: %s", cursorPayload)
		}
		params.Cursor = page.NextCursor
		// Appends after the first page must not move this snapshot's end.
		messages = append(messages, protocol.NewUserMessage("appended", messages[len(messages)-1].ID, "later"))
	}
	if len(got) != 7 {
		t.Fatalf("loaded %d messages, want 7", len(got))
	}
	for i, message := range got {
		if message.ID != messages[i].ID || message.ParentID != messages[i].ParentID {
			t.Fatalf("message %d ancestry changed: %+v", i, message)
		}
	}
}

func TestMessagesPageHydratesLargeHistoryWithinFrameLimit(t *testing.T) {
	messages := linkedMessages(5000)
	params := protocol.RPCMessagesPageParams{Limit: maxMessagesPageLimit, MaxBytes: minMessagesPageBytes}
	loaded := 0
	for {
		page, err := buildMessagesPage("large-history", messages, params)
		if err != nil {
			t.Fatal(err)
		}
		size, err := messagesPageFrameSize("large-history", page)
		if err != nil {
			t.Fatal(err)
		}
		if size > maxMessagesPageBytes {
			t.Fatalf("page frame %d exceeds %d", size, maxMessagesPageBytes)
		}
		if page.Start != loaded {
			t.Fatalf("page starts at %d, want %d", page.Start, loaded)
		}
		loaded += len(page.Messages)
		if !page.HasMore {
			break
		}
		params.Cursor = page.NextCursor
	}
	if loaded != len(messages) {
		t.Fatalf("loaded %d messages, want %d", loaded, len(messages))
	}
}

func TestMessagesPageRejectsCursorAfterBranchChange(t *testing.T) {
	messages := linkedMessages(4)
	first, err := buildMessagesPage("history", messages, protocol.RPCMessagesPageParams{Limit: 2, MaxBytes: defaultMessagesPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	changed := linkedMessages(4)
	changed[3].ID = "other-tip"
	_, err = buildMessagesPage("history", changed, protocol.RPCMessagesPageParams{Cursor: first.NextCursor, Limit: 2, MaxBytes: defaultMessagesPageBytes})
	if err == nil || !strings.Contains(err.Error(), "active session branch") {
		t.Fatalf("branch-change error = %v", err)
	}
}

func TestMessagesPageBoundsEncodedFramesAndAlwaysMakesProgress(t *testing.T) {
	messages := linkedMessages(3)
	messages[0].Content[0].Text = strings.Repeat("<>&\u2028", minMessagesPageBytes/4)
	messages[1].Content[0].Text = strings.Repeat("y", minMessagesPageBytes)
	page, err := buildMessagesPage("history", messages, protocol.RPCMessagesPageParams{Limit: 3, MaxBytes: minMessagesPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || !page.HasMore {
		t.Fatalf("first page = %+v", page)
	}
	size, err := messagesPageFrameSize("history", page)
	if err != nil {
		t.Fatal(err)
	}
	if size > maxMessagesPageBytes {
		t.Fatalf("frame size %d exceeds hard limit %d", size, maxMessagesPageBytes)
	}
	legacyFrame, err := jsonv1.Marshal(Response{ID: "history", Type: "response", Command: "messages_page", Success: true, Data: page})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyFrame)+1 > size {
		t.Fatalf("page estimate %d is smaller than shared writer output %d", size, len(legacyFrame)+1)
	}
}

func TestMessagesPageDefersIncompleteToolPairFromSnapshot(t *testing.T) {
	messages := []protocol.Message{
		protocol.NewUserMessage("user", "", "question"),
		protocol.NewAssistantMessage(
			"assistant", "user", "provider", "model",
			[]protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "read"}},
			protocol.StopToolUse, nil,
		),
	}
	page, err := buildMessagesPage("history", messages, protocol.RPCMessagesPageParams{Limit: 32, MaxBytes: defaultMessagesPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Messages) != 1 || page.Messages[0].ID != "user" {
		t.Fatalf("incomplete tool pair leaked into snapshot: %+v", page)
	}
}

func TestMessagesPagePreservesToolPairsAndPublicProjection(t *testing.T) {
	assistant := protocol.NewAssistantMessage(
		"assistant", "user", "provider", "model",
		[]protocol.ContentBlock{
			{Type: protocol.BlockText, Text: "calling"},
			{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "read"},
			{Type: protocol.BlockProviderData, Data: []byte("private")},
		},
		protocol.StopToolUse, nil,
	)
	result := protocol.NewToolResultMessage("result", "assistant", "call-1", "read", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "done"}}, false)
	messages := publicMessages([]protocol.Message{
		protocol.NewUserMessage("user", "", "question"),
		assistant,
		result,
	})
	var loaded []protocol.Message
	params := protocol.RPCMessagesPageParams{Limit: 1, MaxBytes: defaultMessagesPageBytes}
	for {
		page, err := buildMessagesPage("history", messages, params)
		if err != nil {
			t.Fatal(err)
		}
		loaded = append(loaded, page.Messages...)
		if !page.HasMore {
			break
		}
		params.Cursor = page.NextCursor
	}
	if len(loaded) != 3 || loaded[1].Content[1].ToolCallID != loaded[2].ToolCallID {
		t.Fatalf("tool pairing changed: %+v", loaded)
	}
	for _, block := range loaded[1].Content {
		if block.Type == protocol.BlockProviderData {
			t.Fatal("provider-private continuity crossed RPC projection")
		}
	}
}

func TestMessagesPageRejectsMalformedCursor(t *testing.T) {
	_, err := buildMessagesPage("history", linkedMessages(2), protocol.RPCMessagesPageParams{Cursor: "not base64!", Limit: 1, MaxBytes: defaultMessagesPageBytes})
	if err == nil || !strings.Contains(err.Error(), "cursor is invalid") {
		t.Fatalf("cursor error = %v", err)
	}
}

func linkedMessages(count int) []protocol.Message {
	messages := make([]protocol.Message, 0, count)
	parent := ""
	for i := range count {
		id := "message-" + string(rune('a'+i))
		message := protocol.NewUserMessage(id, parent, id)
		messages = append(messages, message)
		parent = id
	}
	return messages
}
