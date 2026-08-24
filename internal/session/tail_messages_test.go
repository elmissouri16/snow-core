package session

import (
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTailMessagesReturnsFinalToolBatch(t *testing.T) {
	factories := map[string]func(*testing.T) Store{
		"memory": func(t *testing.T) Store { return NewMemoryStore(Options{}) },
		"sqlite": func(t *testing.T) Store {
			dir := t.TempDir()
			store, err := NewSQLiteStore(filepath.Join(dir, "session.db"), dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			store := factory(t)
			t.Cleanup(func() { _ = store.Close() })
			user := protocol.NewUserMessage("user", "", "start")
			assistant := protocol.NewAssistantMessage("assistant", "user", "provider", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read"}}, protocol.StopToolUse, nil)
			tool := protocol.NewToolResultMessage("tool", "meta", "call", "read", []protocol.ContentBlock{protocol.NewTextBlock("done")}, false)
			entries := []Entry{
				{Type: EntryMessage, ID: user.ID, Message: &user},
				{Type: EntryMessage, ID: assistant.ID, ParentID: user.ID, Message: &assistant},
				{Type: EntryMeta, ID: "meta", ParentID: assistant.ID, Key: "ignored", Value: "state"},
				{Type: EntryMessage, ID: tool.ID, ParentID: "meta", Message: &tool},
			}
			for _, entry := range entries {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			tail := store.(TailMessageStore)
			got, err := tail.TailMessages()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].ID != assistant.ID || got[1].ID != tool.ID {
				t.Fatalf("tail messages = %+v", got)
			}
			got[0].Content[0].Name = "mutated"
			again, err := tail.TailMessages()
			if err != nil {
				t.Fatal(err)
			}
			if again[0].Content[0].Name != "read" {
				t.Fatal("tail projection aliases durable data")
			}

			next := protocol.NewUserMessage("next", tool.ID, "continue")
			if err := store.Append(Entry{Type: EntryMessage, ID: next.ID, ParentID: tool.ID, Message: &next}); err != nil {
				t.Fatal(err)
			}
			got, err = tail.TailMessages()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].ID != next.ID {
				t.Fatalf("tail after user = %+v", got)
			}
		})
	}
}
