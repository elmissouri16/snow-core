package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func optimizedQueryStores(t *testing.T) map[string]Store {
	t.Helper()
	memory := NewMemoryStore(Options{})
	dir := t.TempDir()
	sqlite, err := NewSQLiteStore(filepath.Join(dir, "optimized.db"), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Store{"memory": memory, "sqlite": sqlite}
}

func TestContextMessagesRepeatedCompactionMatchesStores(t *testing.T) {
	for name, store := range optimizedQueryStores(t) {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			entries := []Entry{
				msg("old-a", "", "old a"),
				msg("old-b", "", "old b"),
				msg("keep-a", "", "keep a"),
				{Type: EntryCompaction, ID: "compact-one", Summary: "first", CompactedThrough: "old-b"},
				msg("keep-b", "", "keep b"),
				{Type: EntryCompaction, ID: "compact-two", Summary: "second", CompactedThrough: "keep-a"},
				msg("keep-c", "", "keep c"),
			}
			for _, entry := range entries {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			projected, err := store.(ContextStore).ContextMessages()
			if err != nil {
				t.Fatal(err)
			}
			if len(projected) != 3 || projected[0].ID != "compaction-compact-two" || projected[1].ID != "keep-b" || projected[2].ID != "keep-c" {
				t.Fatalf("projection=%+v", projected)
			}
			if !strings.Contains(projected[0].Content[0].Text, "second") || strings.Contains(projected[0].Content[0].Text, "first") {
				t.Fatalf("checkpoint=%q", projected[0].Content[0].Text)
			}
			branch, err := store.(BranchEntryStore).BranchEntries()
			if err != nil {
				t.Fatal(err)
			}
			fromBranch, compacted := store.(BranchContextProjector).ProjectBranchContext(branch)
			if !compacted || len(fromBranch) != len(projected) || fromBranch[0].ID != projected[0].ID || fromBranch[1].ID != projected[1].ID || fromBranch[2].ID != projected[2].ID {
				t.Fatalf("branch projection=%+v compacted=%t", fromBranch, compacted)
			}
			projected[1].Content[0].Text = "mutated"
			again, err := store.(ContextStore).ContextMessages()
			if err != nil || again[1].Content[0].Text != "keep b" {
				t.Fatalf("defensive projection=%+v err=%v", again, err)
			}
		})
	}
}

func TestBranchStateEntriesFiltersAndPreservesOrder(t *testing.T) {
	for name, store := range optimizedQueryStores(t) {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			ordinary := protocol.NewUserMessage("ordinary", "", "ignore")
			activation := protocol.NewToolResultMessage("activation", "ordinary", "call", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock("<skill_content name=\"review\">\nbody")}, false)
			entries := []Entry{
				{Type: EntryMessage, ID: ordinary.ID, Message: &ordinary},
				{Type: EntryMessage, ID: activation.ID, ParentID: ordinary.ID, Message: &activation},
				{Type: EntryMeta, ID: "ignored-meta", ParentID: activation.ID, Key: "ignored", Value: "value"},
				{Type: EntryMeta, ID: "deactivate", ParentID: "ignored-meta", Key: "agent_skill_deactivation", Value: "review"},
			}
			for _, entry := range entries {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			got, err := store.(BranchStateStore).BranchStateEntries([]string{"agent_skill_deactivation"}, []string{"activate_skill"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].ID != activation.ID || got[1].ID != "deactivate" {
				t.Fatalf("branch state=%+v", got)
			}
			got[0].Message.Content[0].Text = "mutated"
			again, err := store.(BranchStateStore).BranchStateEntries([]string{"agent_skill_deactivation"}, []string{"activate_skill"})
			if err != nil || again[0].Message.Content[0].Text == "mutated" {
				t.Fatalf("branch state aliases durable data: %+v err=%v", again, err)
			}
		})
	}
}

func TestLatestAssistantAndAggregateUsage(t *testing.T) {
	for name, store := range optimizedQueryStores(t) {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { _ = store.Close() })
			user := protocol.NewUserMessage("user", "", "start")
			older := protocol.NewAssistantMessage("older", "user", "p", "m", []protocol.ContentBlock{protocol.NewTextBlock("older answer")}, protocol.StopStop, &protocol.Usage{Input: 4, Output: 2, Total: 6})
			toolOnly := protocol.NewAssistantMessage("tool-only", "older", "p", "m", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "call", Name: "read"}}, protocol.StopToolUse, &protocol.Usage{Input: 3, Output: 1, Total: 4})
			latest := protocol.NewAssistantMessage("latest", "tool-only", "p", "m", []protocol.ContentBlock{{Type: protocol.BlockPlan, Text: "latest plan", PlanComplete: true}}, protocol.StopStop, nil)
			for _, message := range []protocol.Message{user, older, toolOnly, latest} {
				if err := store.Append(Entry{Type: EntryMessage, ID: message.ID, ParentID: message.ParentID, Message: &message}); err != nil {
					t.Fatal(err)
				}
			}
			message, found, err := store.(LatestAssistantStore).LatestAssistantMessage()
			if err != nil || !found || message.ID != latest.ID {
				t.Fatalf("latest=%+v found=%t err=%v", message, found, err)
			}
			aggregate, err := store.(interface {
				AggregateUsage() (protocol.Usage, error)
			}).AggregateUsage()
			if err != nil || aggregate.Input != 7 || aggregate.Output != 3 || aggregate.Total != 10 {
				t.Fatalf("usage=%+v err=%v", aggregate, err)
			}
		})
	}
}
