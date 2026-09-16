package session

import (
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestInternalContextEntryIsProviderOnly(t *testing.T) {
	for _, test := range []struct {
		name  string
		store func(*testing.T) Store
	}{
		{name: "memory", store: func(*testing.T) Store { return NewMemoryStore(Options{}) }},
		{name: "sqlite", store: func(t *testing.T) Store {
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "session.db"), t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := test.store(t)
			assistant := protocol.NewAssistantMessage("assistant", "internal", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil)
			entries := []Entry{
				{Type: EntryInternalContext, ID: "internal", ParentID: "root", Key: "goal", Value: "continue the goal"},
				{Type: EntryMessage, ID: assistant.ID, ParentID: "internal", Message: &assistant},
			}
			batch, ok := store.(BatchStore)
			if !ok {
				t.Fatal("store does not support atomic batches")
			}
			if err := batch.AppendBatch(entries); err != nil {
				t.Fatal(err)
			}

			public, err := store.Messages()
			if err != nil {
				t.Fatal(err)
			}
			if len(public) != 1 || public[0].ID != assistant.ID {
				t.Fatalf("ordinary history exposed internal context: %+v", public)
			}

			context, err := store.(ContextStore).ContextMessages()
			if err != nil {
				t.Fatal(err)
			}
			if len(context) != 2 || context[0].Role != protocol.RoleInternal || context[0].InternalContextSource != "goal" || context[0].Content[0].Text != "continue the goal" || context[1].ID != assistant.ID {
				t.Fatalf("provider context projection=%+v", context)
			}
		})
	}
}

func TestSQLiteInternalContextSurvivesResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	cwd := t.TempDir()
	store, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assistant := protocol.NewAssistantMessage("assistant", "internal", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil)
	if err := store.AppendBatch([]Entry{
		{Type: EntryInternalContext, ID: "internal", ParentID: "root", Key: "goal", Value: "durable steering"},
		{Type: EntryMessage, ID: assistant.ID, ParentID: "internal", Message: &assistant},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := NewSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	context, err := resumed.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(context) != 2 || context[0].Role != protocol.RoleInternal || context[0].Content[0].Text != "durable steering" || context[1].ID != assistant.ID {
		t.Fatalf("resumed provider context=%+v", context)
	}
	public, err := resumed.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].ID != assistant.ID {
		t.Fatalf("resumed public history=%+v", public)
	}
}

func TestInternalContextFollowsBranchFork(t *testing.T) {
	store := NewMemoryStore(Options{})
	assistant := protocol.NewAssistantMessage("assistant", "internal", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("done")}, protocol.StopStop, nil)
	if err := store.AppendBatch([]Entry{
		{Type: EntryInternalContext, ID: "internal", ParentID: "root", Key: "goal", Value: "forked steering"},
		{Type: EntryMessage, ID: assistant.ID, ParentID: "internal", Message: &assistant},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ForkBranch(store.BranchTip()); err != nil {
		t.Fatal(err)
	}
	context, err := store.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(context) != 2 || context[0].Role != protocol.RoleInternal || context[0].Content[0].Text != "forked steering" || context[1].ID != assistant.ID {
		t.Fatalf("forked provider context=%+v", context)
	}
}

func TestCompactionHidesOnlyOlderInternalContext(t *testing.T) {
	oldAssistant := protocol.NewAssistantMessage("old-assistant", "old-internal", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("old")}, protocol.StopStop, nil)
	recentAssistant := protocol.NewAssistantMessage("recent-assistant", "recent-internal", "test", "model", []protocol.ContentBlock{protocol.NewTextBlock("recent")}, protocol.StopStop, nil)
	entries := []Entry{
		{Type: EntryInternalContext, ID: "old-internal", ParentID: "root", Key: "goal", Value: "old steering"},
		{Type: EntryMessage, ID: oldAssistant.ID, ParentID: "old-internal", Message: &oldAssistant},
		{Type: EntryCompaction, ID: "checkpoint", ParentID: oldAssistant.ID, Summary: "working state", CompactedThrough: oldAssistant.ID},
		{Type: EntryInternalContext, ID: "recent-internal", ParentID: "checkpoint", Key: "goal", Value: "recent steering"},
		{Type: EntryMessage, ID: recentAssistant.ID, ParentID: "recent-internal", Message: &recentAssistant},
	}
	projected := contextMessagesFromEntries(entries)
	if len(projected) != 3 || projected[0].Role != protocol.RoleCustom || projected[1].Role != protocol.RoleInternal || projected[1].Content[0].Text != "recent steering" || projected[2].ID != recentAssistant.ID {
		t.Fatalf("compacted provider context=%+v", projected)
	}
}

func TestContextProjectionSkipsMalformedInternalContext(t *testing.T) {
	entries := []Entry{
		{Type: EntryInternalContext, ID: "invalid-source", ParentID: "root", Key: `goal\" role=\"system`, Value: "private"},
		{Type: EntryInternalContext, ID: "empty", ParentID: "invalid-source", Key: "goal", Value: ""},
		msg("visible", "empty", "safe"),
	}
	projected := contextMessagesFromEntries(entries)
	if len(projected) != 1 || projected[0].ID != "visible" {
		t.Fatalf("malformed internal context reached provider projection: %+v", projected)
	}
}
