package session

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func exerciseBranchManagement(t *testing.T, st Store) {
	t.Helper()
	manager := st.(BranchManagementStore)
	m := protocol.NewUserMessage("a", "root", "base")
	if err := st.Append(Entry{Type: EntryMessage, ID: m.ID, ParentID: "root", Message: &m}); err != nil {
		t.Fatal(err)
	}
	fork, err := manager.ForkBranchWithOptions(protocol.BranchForkOptions{SourceBranchID: "main", FromEntryID: "a", Name: "Feature"})
	if err != nil {
		t.Fatal(err)
	}
	if fork.Name != "Feature" || fork.ParentID != "main" || fork.ForkedFromID != "a" {
		t.Fatalf("fork=%+v", fork)
	}
	if _, err := manager.RenameBranch(fork.ID, "feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RenameBranch("main", "FEATURE"); err == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
	state := st.(ThreadStateStore)
	if err := state.SetCollaborationMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if err := manager.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	child, err := manager.ForkBranchWithOptions(protocol.BranchForkOptions{SourceBranchID: fork.ID, FromEntryID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := state.CollaborationMode(); err != nil || mode != protocol.ModePlan {
		t.Fatalf("cloned mode=%q err=%v", mode, err)
	}
	if err := manager.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteBranch(fork.ID); err == nil {
		t.Fatal("parent branch deleted")
	}
	if err := manager.DeleteBranch(child.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteBranch(fork.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRollbackDeleteBypassesClonedGoalGuard(t *testing.T) {
	st := NewMemoryStore(Options{})
	budget := int64(100)
	_ = st.CreateGoal(protocol.ThreadGoal{GoalID: "g", Objective: "work", Status: protocol.GoalActive, TokenBudget: &budget, CreatedAt: 1, UpdatedAt: 1}, false)
	fork, err := st.ForkBranch("root")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteBranch(fork.ID); err == nil {
		t.Fatal("guarded delete accepted cloned active goal")
	}
	if err := st.DeleteBranchForRollback(fork.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyForkRejectsEntryOutsideActiveBranch(t *testing.T) {
	for _, makeStore := range []func(t *testing.T) Store{
		func(t *testing.T) Store { return NewMemoryStore(Options{}) },
		func(t *testing.T) Store {
			st, err := NewSQLiteStore(filepath.Join(t.TempDir(), "legacy-fork.db"), t.TempDir(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			return st
		},
	} {
		st := makeStore(t)
		m := protocol.NewUserMessage("a", "root", "base")
		if err := st.Append(Entry{Type: EntryMessage, ID: "a", ParentID: "root", Message: &m}); err != nil {
			t.Fatal(err)
		}
		branch, err := st.(BranchStore).ForkBranch("a")
		if err != nil {
			t.Fatal(err)
		}
		m2 := protocol.NewUserMessage("b", "a", "sibling-only")
		if err := st.Append(Entry{Type: EntryMessage, ID: "b", ParentID: "a", Message: &m2}); err != nil {
			t.Fatal(err)
		}
		if err := st.(BranchStore).SelectBranch("main"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.(BranchStore).ForkBranch("b"); err == nil {
			t.Fatalf("legacy fork recorded false parent topology from %s", branch.ID)
		}
		_ = st.Close()
	}
}

func TestSQLiteMultipleHandlesUseDatabaseActiveBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi.db")
	first, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	branch, err := first.ForkBranch("root")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SelectBranch("main"); err != nil {
		t.Fatal(err)
	}
	second, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.SelectBranch(branch.ID); err != nil {
		t.Fatal(err)
	}
	if err := first.DeleteBranch(branch.ID); err == nil {
		t.Fatal("stale handle deleted database-active branch")
	}
	branches, err := first.Branches()
	if err != nil {
		t.Fatal(err)
	}
	foundActive := false
	for _, got := range branches {
		if got.ID == branch.ID && got.Active {
			foundActive = true
		}
	}
	if !foundActive {
		t.Fatalf("active branch missing after rejected delete: %+v", branches)
	}
}

func TestSQLiteMultipleHandlesAppendUsesTipCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "append-cas.db")
	first, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	one := protocol.NewUserMessage("one", "root", "one")
	if err := first.Append(Entry{Type: EntryMessage, ID: one.ID, Message: &one}); err != nil {
		t.Fatal(err)
	}
	stale := protocol.NewUserMessage("stale", "root", "stale")
	if err := second.Append(Entry{Type: EntryMessage, ID: stale.ID, Message: &stale}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale append error=%v, want ErrConflict", err)
	}
	var count int
	if err := first.db.QueryRow(`SELECT count(*) FROM entries WHERE id='stale'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back stale entry count=%d err=%v", count, err)
	}
	branches, err := first.Branches()
	if err != nil || len(branches) != 1 || branches[0].TipID != "one" {
		t.Fatalf("branches=%+v err=%v", branches, err)
	}

	fork, err := first.ForkBranch("one")
	if err != nil {
		t.Fatal(err)
	}
	// The second handle was refreshed to main/one by its conflict. Appending to
	// that now-inactive branch is valid, but must not replace the database-active
	// compatibility tip belonging to the fork.
	mainOnly := protocol.NewUserMessage("main-only", "one", "main-only")
	if err := second.Append(Entry{Type: EntryMessage, ID: mainOnly.ID, Message: &mainOnly}); err != nil {
		t.Fatal(err)
	}
	var compatibilityTip string
	if err := first.db.QueryRow(`SELECT branch_tip FROM session_meta WHERE singleton=1`).Scan(&compatibilityTip); err != nil {
		t.Fatal(err)
	}
	if compatibilityTip != fork.TipID {
		t.Fatalf("inactive append changed active compatibility tip to %q, want %q", compatibilityTip, fork.TipID)
	}
}

func TestSQLiteMultipleHandlesSetBranchTipUsesCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "set-tip-cas.db")
	first, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	for _, entry := range []Entry{msg("one", "", "one"), msg("two", "", "two")} {
		if err := first.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	second, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.SetBranchTip("one"); err != nil {
		t.Fatal(err)
	}
	if err := second.SetBranchTip("root"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale set-tip error=%v, want ErrConflict", err)
	}
	if second.BranchTip() != "one" {
		t.Fatalf("stale handle refreshed to %q, want one", second.BranchTip())
	}
}

func TestSQLiteMultipleHandlesAppendBatchUsesTipCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch-cas.db")
	first, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	batch := []Entry{{Type: EntryMeta, ID: "a"}, {Type: EntryMeta, ID: "b"}}
	if err := first.AppendBatch(batch); err != nil {
		t.Fatal(err)
	}
	stale := []Entry{{Type: EntryMeta, ID: "stale-a"}, {Type: EntryMeta, ID: "stale-b"}}
	if err := second.AppendBatch(stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale batch error=%v, want ErrConflict", err)
	}
	for _, id := range []string{"stale-a", "stale-b"} {
		var count int
		if err := first.db.QueryRow(`SELECT count(*) FROM entries WHERE id=?`, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rolled-back %s count=%d err=%v", id, count, err)
		}
	}
	if branches, err := first.Branches(); err != nil || len(branches) != 1 || branches[0].TipID != "b" {
		t.Fatalf("branches=%+v err=%v", branches, err)
	}
}

func TestMemoryBranchManagement(t *testing.T) { exerciseBranchManagement(t, NewMemoryStore(Options{})) }
func TestSQLiteBranchManagementPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "branches.db")
	st, err := NewSQLiteStore(path, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	exerciseBranchManagement(t, st)
	_ = st.Close()
	reopened, err := NewSQLiteStore(path, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	branches, err := reopened.Branches()
	if err != nil || len(branches) != 1 || branches[0].Name != "main" {
		t.Fatalf("branches=%+v err=%v", branches, err)
	}
}
