package session

import (
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAgentTurnCountFollowsSameDatabaseBranchAncestry(t *testing.T) {
	for name, store := range optimizedQueryStores(t) {
		t.Run(name, func(t *testing.T) {
			defer store.Close()
			if err := store.Append(Entry{Type: EntryMeta, ID: "turn-1", Key: MetaAgentTurn, Value: "user"}); err != nil {
				t.Fatal(err)
			}
			branch, err := store.(BranchStore).ForkBranch("turn-1")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.(BranchStore).SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			for _, entry := range []Entry{
				{Type: EntryMeta, ID: "turn-main-2", Key: MetaAgentTurn, Value: "goal"},
				{Type: EntryMeta, ID: "turn-main-3", Key: MetaAgentTurn, Value: "user"},
			} {
				if err := store.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			assertTurnCount(t, store, 3)

			if err := store.(BranchStore).SelectBranch(branch.ID); err != nil {
				t.Fatal(err)
			}
			assertTurnCount(t, store, 1)
			if err := store.Append(Entry{Type: EntryMeta, ID: "turn-child-2", Key: MetaAgentTurn, Value: "user"}); err != nil {
				t.Fatal(err)
			}
			assertTurnCount(t, store, 2)

			if err := store.(BranchStore).SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			assertTurnCount(t, store, 3)
		})
	}
}

func TestAgentTurnCountSurvivesPhysicalExactEntryForkAndReopen(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	index := NewFileIndex(root)
	source, err := index.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	firstUser := protocol.NewUserMessage("user-1", "turn-1", "first")
	secondUser := protocol.NewUserMessage("user-2", "turn-2", "second")
	for _, entry := range []Entry{
		{Type: EntryMeta, ID: "turn-1", Key: MetaAgentTurn, Value: "user"},
		{Type: EntryMessage, ID: firstUser.ID, ParentID: firstUser.ParentID, Message: &firstUser},
		{Type: EntryMeta, ID: "turn-2", Key: MetaAgentTurn, Value: "goal"},
		{Type: EntryMessage, ID: secondUser.ID, ParentID: secondUser.ParentID, Message: &secondUser},
	} {
		if err := source.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	assertTurnCount(t, source, 2)

	forked, result, err := index.CreateFork(cwd, source, protocol.SessionForkOptions{
		FromEntryID: "user-1",
		Name:        "turn-count-fork",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTurnCount(t, forked, 1)
	path := result.SessionPath
	if err := forked.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStore(path, cwd, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertTurnCount(t, reopened, 1)
}

func assertTurnCount(t *testing.T, store Store, want uint64) {
	t.Helper()
	got, err := store.(TurnCountStore).CountAgentTurns()
	if err != nil || got != want {
		t.Fatalf("turn count=%d want=%d err=%v", got, want, err)
	}
}
