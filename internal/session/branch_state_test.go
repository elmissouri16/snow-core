package session

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestBranchStateEntriesStayBranchLocalAndOrdered(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			var store Store
			var sqlitePath string
			switch kind {
			case "memory":
				store = NewMemoryStore(Options{ID: "branch-state"})
			case "sqlite":
				sqlitePath = filepath.Join(t.TempDir(), "branch-state.db")
				opened, err := NewSQLiteStore(sqlitePath, t.TempDir(), Options{ID: "branch-state"})
				if err != nil {
					t.Fatal(err)
				}
				store = opened
			}

			forkID := populateBranchStateFixture(t, store)
			assertBranchStateIDs(t, store, []string{"activation-shared", "activation-main"})
			if err := store.(BranchStore).SelectBranch(forkID); err != nil {
				t.Fatal(err)
			}
			assertBranchStateIDs(t, store, []string{"activation-shared", "deactivate-all", "reactivate-tool"})

			if kind == "sqlite" {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := NewSQLiteStore(sqlitePath, t.TempDir(), Options{ID: "branch-state"})
				if err != nil {
					t.Fatal(err)
				}
				store = reopened
				assertBranchStateIDs(t, store, []string{"activation-shared", "deactivate-all", "reactivate-tool"})
				if err := store.(BranchStore).SelectBranch("main"); err != nil {
					t.Fatal(err)
				}
				assertBranchStateIDs(t, store, []string{"activation-shared", "activation-main"})
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func populateBranchStateFixture(t *testing.T, store Store) string {
	t.Helper()
	appendMeta := func(id, key, value string) {
		t.Helper()
		parent := store.BranchTip()
		if err := store.Append(Entry{Type: EntryMeta, ID: id, ParentID: parent, Key: key, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	appendMeta("activation-shared", "skill_activation", "shared")
	parent := store.BranchTip()
	shared := protocol.NewUserMessage("shared-message", parent, "shared")
	if err := store.Append(Entry{Type: EntryMessage, ID: shared.ID, ParentID: parent, Message: &shared}); err != nil {
		t.Fatal(err)
	}
	fork, err := store.(BranchStore).ForkBranch(shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	appendMeta("deactivate-all", "skill_deactivation", "*")
	parent = store.BranchTip()
	reactivated := protocol.NewToolResultMessage("reactivate-tool", parent, "call", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock("<skill_content name=\"shared\">\ncontent\n</skill_content>")}, false)
	if err := store.Append(Entry{Type: EntryMessage, ID: reactivated.ID, ParentID: parent, Message: &reactivated}); err != nil {
		t.Fatal(err)
	}
	if err := store.(BranchStore).SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	appendMeta("activation-main", "skill_activation", "main-only")
	return fork.ID
}

func assertBranchStateIDs(t *testing.T, store Store, want []string) {
	t.Helper()
	got, err := store.(BranchStateStore).BranchStateEntries([]string{"skill_activation", "skill_deactivation"}, []string{"activate_skill"})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(got))
	for i := range got {
		ids[i] = got[i].ID
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("branch state IDs=%v want %v", ids, want)
	}
	branch, err := store.(BranchEntryStore).BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	filtered := make([]string, 0, len(branch))
	meta := makeStringSet([]string{"skill_activation", "skill_deactivation"})
	tools := makeStringSet([]string{"activate_skill"})
	for _, entry := range branch {
		if branchStateEntryMatches(entry, meta, tools) {
			filtered = append(filtered, entry.ID)
		}
	}
	if !reflect.DeepEqual(ids, filtered) {
		t.Fatalf("optimized state=%v full-branch filter=%v", ids, filtered)
	}
}
