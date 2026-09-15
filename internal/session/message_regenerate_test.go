package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func appendRegenerateTurn(t *testing.T, store Store, n int, tools bool) {
	t.Helper()
	user, reply := fmt.Sprintf("user-%d", n), fmt.Sprintf("reply-%d", n)
	entries := []Entry{
		{Type: EntryMeta, ID: fmt.Sprintf("turn-%d", n), Key: MetaAgentTurn, Value: "user"},
		{Type: EntryMessage, ID: user, Message: new(protocol.NewUserMessage(user, "", "same prompt"))},
	}
	if tools {
		preface, result, call := fmt.Sprintf("preface-%d", n), fmt.Sprintf("result-%d", n), fmt.Sprintf("call-%d", n)
		entries = append(entries, Entry{Type: EntryMessage, ID: preface, Message: new(protocol.NewAssistantMessage(preface, "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("working"), {Type: protocol.BlockToolCall, ToolCallID: call, Name: "bash", Arguments: []byte(`{}`)}}, protocol.StopToolUse, nil))}, Entry{Type: EntryMessage, ID: result, Message: new(protocol.NewToolResultMessage(result, "", call, "bash", []protocol.ContentBlock{protocol.NewTextBlock("effect happened")}, false))})
	}
	entries = append(entries, Entry{Type: EntryMessage, ID: reply, Message: new(protocol.NewAssistantMessage(reply, "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("same reply"), {Type: protocol.BlockProviderData, Data: []byte("never expose")}}, protocol.StopStop, nil))})
	for _, entry := range entries {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMessageRegenerateExactFirstMiddleLatestWithDuplicateText(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store := editTestStore(t, sqlite)
			for i := range 3 {
				appendRegenerateTurn(t, store, i, true)
			}
			oldTip := store.BranchTip()
			for selected := range 3 {
				byReply, err := ResolveMessageRegenerate(t.Context(), store, protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), EntryID: fmt.Sprintf("reply-%d", selected)})
				if err != nil {
					t.Fatal(err)
				}
				byTurn, err := ResolveMessageRegenerate(t.Context(), store, protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), TurnID: fmt.Sprintf("turn-%d", selected)})
				if err != nil || byTurn != byReply || byReply.EntryID != fmt.Sprintf("user-%d", selected) || byReply.ReplyEntryID != fmt.Sprintf("reply-%d", selected) || byReply.Text != "same prompt" {
					t.Fatalf("exact source=%+v turn=%+v err=%v", byReply, byTurn, err)
				}
				boundary := "root"
				if selected > 0 {
					boundary = fmt.Sprintf("reply-%d", selected-1)
				}
				if byReply.BoundaryID != boundary || store.BranchTip() != oldTip {
					t.Fatal("incorrect/read-write rewind boundary")
				}
			}
			for _, id := range []string{"user-0", "preface-0", "result-0", "turn-0", "root", "unknown", "same reply"} {
				if _, err := ResolveMessageRegenerate(t.Context(), store, protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), EntryID: id}); err == nil {
					t.Fatalf("nonterminal/exact assistant selector accepted %q", id)
				}
			}
		})
	}
}

func TestMessageRegenerateRejectsUnsafeOrAmbiguousTurn(t *testing.T) {
	for _, kind := range []string{"plan", "private-only", "tool-use", "aborted", "error", "pending", "extra-user", "transformed-user", "image-user", "goal-origin", "unbalanced-tools", "older-assistant"} {
		t.Run(kind, func(t *testing.T) {
			store := editTestStore(t, false).(*MemoryStore)
			appendRegenerateTurn(t, store, 0, true)
			user := store.entries[store.byID["user-0"]].Message
			reply := store.entries[store.byID["reply-0"]].Message
			switch kind {
			case "plan":
				reply.Content = []protocol.ContentBlock{{Type: protocol.BlockPlan, Text: "plan"}}
			case "private-only":
				reply.Content = []protocol.ContentBlock{{Type: protocol.BlockProviderData, Data: []byte("opaque")}}
			case "tool-use":
				reply.StopReason = protocol.StopToolUse
			case "aborted":
				reply.StopReason = protocol.StopAborted
			case "error":
				reply.Error = "failed"
			case "pending":
				reply.StopReason = protocol.StopPending
			case "extra-user":
				_ = store.Append(Entry{Type: EntryMessage, ID: "second-user", Message: new(protocol.NewUserMessage("second-user", "", "same prompt"))})
			case "transformed-user":
				user.PluginDetails = []byte(`{}`)
			case "image-user":
				user.Content = append(user.Content, protocol.ContentBlock{Type: protocol.BlockImage, Data: []byte("image")})
			case "goal-origin":
				store.entries[store.byID["turn-0"]].Value = "goal"
			case "unbalanced-tools":
				store.entries[store.byID["result-0"]].Message.ToolCallID = "unmatched"
			case "older-assistant":
				_ = store.Append(Entry{Type: EntryMessage, ID: "newer-reply", Message: new(protocol.NewAssistantMessage("newer-reply", "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("newer")}, protocol.StopStop, nil))})
			}
			before := store.BranchTip()
			if _, err := ResolveMessageRegenerate(t.Context(), store, protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), EntryID: "reply-0"}); err == nil {
				t.Fatal("unsafe turn accepted")
			}
			if store.BranchTip() != before {
				t.Fatal("unsafe preparation mutated store")
			}
		})
	}
}

func TestMessageRegenerateRequiresBoundedCurrentExactSession(t *testing.T) {
	store := editTestStore(t, false).(*MemoryStore)
	appendRegenerateTurn(t, store, 0, false)
	for _, params := range []protocol.RPCMessageRegeneratePrepareParams{
		{SessionID: "wrong", EntryID: "reply-0"},
		{SessionID: store.ID(), EntryID: "reply-0", TurnID: "turn-0"},
		{SessionID: store.ID(), TurnID: "user-0"},
	} {
		if _, err := ResolveMessageRegenerate(t.Context(), store, params); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ResolveMessageRegenerate(ctx, store, protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), EntryID: "reply-0"}); err == nil {
		t.Fatal("canceled preparation accepted")
	}
	store.entries[store.byID["reply-0"]].Message.Content[0].Text = strings.Repeat("x", messageEditMaxHistoryBytes+1)
	if _, err := ResolveMessageRegenerate(t.Context(), store, protocol.RPCMessageRegeneratePrepareParams{SessionID: store.ID(), EntryID: "reply-0"}); err == nil {
		t.Fatal("unbounded history accepted")
	}
}
