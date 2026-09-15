package session

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func historyControlFixture(t *testing.T, sqlite bool) (Store, Store, protocol.RPCHistoryControlBinding) {
	t.Helper()
	store := editTestStore(t, sqlite)
	appendEditTurn(t, store, 0)
	targetTip := store.BranchTip()
	branch, err := store.(BranchStore).ForkBranch(targetTip)
	if err != nil {
		t.Fatal(err)
	}
	appendEditTurn(t, store, 1)
	binding := protocol.RPCHistoryControlBinding{SessionID: store.ID(), SourceBranchID: branch.ID, SourceTipID: store.BranchTip(), TargetBranchID: "main", TargetTipID: targetTip}
	var peer Store = store
	if sqlite {
		opened, err := OpenSQLiteStore(store.Path(), store.Header().CWD, Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = opened.Close() })
		peer = opened
	}
	return store, peer, binding
}

func historyControlPage(t *testing.T, store Store) protocol.RPCBranchesPage {
	t.Helper()
	page, err := store.(BranchVersionStore).BranchVersions(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func historyControlTry(t *testing.T, store Store, binding protocol.RPCHistoryControlBinding, operation string) error {
	t.Helper()
	bound := store.(HistoryControlStore)
	switch operation {
	case "fork":
		_, err := bound.ForkHistoryBranch(t.Context(), protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "managed fork"}, protocol.ModeDefault)
		return err
	case "rename":
		_, err := bound.RenameHistoryBranch(t.Context(), protocol.RPCManagedBranchRenameParams{RPCHistoryControlBinding: binding, OldName: "main", Name: "managed rename"})
		return err
	case "capture":
		_, err := bound.CaptureHistoryFork(t.Context(), protocol.RPCManagedSessionForkParams{RPCHistoryControlBinding: binding, Name: "managed detached"}, protocol.ModeDefault)
		return err
	default:
		t.Fatalf("unknown test operation %q", operation)
		return nil
	}
}

// A deterministic interleaving: preflight on one handle, independently committed
// writer on the other, then the admitted mutation using the original binding.
// The first handle intentionally retains its stale in-memory active cursor.
func TestHistoryControlRejectsIndependentWriterCAS(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, change := range []string{"active", "source tip", "target tip", "old name", "mode", "goal"} {
			for _, operation := range []string{"fork", "rename", "capture"} {
				if change == "old name" && operation != "rename" || change == "mode" && operation == "rename" {
					continue
				}
				t.Run(fmt.Sprintf("sqlite=%t/%s/%s", sqlite, change, operation), func(t *testing.T) {
					store, peer, binding := historyControlFixture(t, sqlite)
					preflight, err := store.(BranchVersionStore).BranchVersion(t.Context(), binding.TargetBranchID)
					if err != nil || preflight.Active.BranchID != binding.SourceBranchID {
						t.Fatalf("preflight: %+v %v", preflight, err)
					}
					branches := peer.(BranchManagementStore)
					switch change {
					case "active":
						if err := branches.SelectBranch(binding.TargetBranchID); err != nil {
							t.Fatal(err)
						}
					case "source tip":
						appendEditTurn(t, peer, 2)
					case "target tip":
						if err := branches.SelectBranch(binding.TargetBranchID); err != nil {
							t.Fatal(err)
						}
						appendEditTurn(t, peer, 2)
						if err := branches.SelectBranch(binding.SourceBranchID); err != nil {
							t.Fatal(err)
						}
					case "old name":
						if _, err := branches.RenameBranch(binding.TargetBranchID, "peer rename"); err != nil {
							t.Fatal(err)
						}
					case "mode", "goal":
						if err := branches.SelectBranch(binding.TargetBranchID); err != nil {
							t.Fatal(err)
						}
						if change == "mode" {
							if err := peer.(ThreadStateStore).SetCollaborationMode(protocol.ModePlan); err != nil {
								t.Fatal(err)
							}
						} else {
							if err := peer.(ThreadGoalStore).CreateGoal(protocol.ThreadGoal{GoalID: "history-test-goal", Objective: "do not fork", Status: protocol.GoalActive}, false); err != nil {
								t.Fatal(err)
							}
						}
						if err := branches.SelectBranch(binding.SourceBranchID); err != nil {
							t.Fatal(err)
						}
					}
					before := historyControlPage(t, peer)
					err = historyControlTry(t, store, binding, operation)
					if err == nil || errors.Is(err, ErrHistoryControlUnknown) {
						t.Fatalf("expected known rejection, got %v", err)
					}
					if change != "goal" && !errors.Is(err, ErrBranchVersionStale) {
						t.Fatalf("expected stale CAS, got %v", err)
					}
					if after := historyControlPage(t, peer); !reflect.DeepEqual(before, after) {
						t.Fatalf("rejection mutated branches: before=%+v after=%+v", before, after)
					}
				})
			}
		}
	}
}

func TestHistoryControlChecksToolBoundaryInsideStore(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store, peer, binding := historyControlFixture(t, sqlite)
			branches := peer.(BranchStore)
			if err := branches.SelectBranch(binding.TargetBranchID); err != nil {
				t.Fatal(err)
			}
			message := protocol.NewAssistantMessage("unfinished", "", "fake", "fake", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "pending", Name: "bash"}}, protocol.StopToolUse, nil)
			if err := peer.Append(Entry{ID: message.ID, Type: EntryMessage, Message: &message}); err != nil {
				t.Fatal(err)
			}
			binding.TargetTipID = peer.BranchTip()
			if err := branches.SelectBranch(binding.SourceBranchID); err != nil {
				t.Fatal(err)
			}
			before := historyControlPage(t, peer)
			for _, operation := range []string{"fork", "capture"} {
				if err := historyControlTry(t, store, binding, operation); !errors.Is(err, ErrInvalidForkBoundary) || errors.Is(err, ErrHistoryControlUnknown) {
					t.Fatalf("%s boundary: %v", operation, err)
				}
			}
			if after := historyControlPage(t, peer); !reflect.DeepEqual(before, after) {
				t.Fatal("unsafe history published a branch")
			}
		})
	}
}

func TestHistoryControlDetachedCaptureCannotRebindAtPublication(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			store, peer, binding := historyControlFixture(t, sqlite)
			branches := peer.(BranchStore)
			if err := branches.SelectBranch(binding.TargetBranchID); err != nil {
				t.Fatal(err)
			}
			if err := peer.(ThreadStateStore).SetCollaborationMode(protocol.ModePlan); err != nil {
				t.Fatal(err)
			}
			if err := branches.SelectBranch(binding.SourceBranchID); err != nil {
				t.Fatal(err)
			}
			// No artifacts, destination directory or child exists at capture time.
			capture, err := store.(HistoryControlStore).CaptureHistoryFork(t.Context(), protocol.RPCManagedSessionForkParams{RPCHistoryControlBinding: binding, Name: "captured detached"}, protocol.ModePlan)
			if err != nil {
				t.Fatal(err)
			}
			// The source reservation must already be released: a separate handle can
			// change both selected history and mode before we publish the child.
			if err := branches.SelectBranch(binding.TargetBranchID); err != nil {
				t.Fatal(err)
			}
			appendEditTurn(t, peer, 2)
			if err := peer.(ThreadStateStore).SetCollaborationMode(protocol.ModeDefault); err != nil {
				t.Fatal(err)
			}
			before := historyControlPage(t, peer)
			index, cwd := NewFileIndex(t.TempDir()), t.TempDir()
			child, fork, err := index.CreateHistoryFork(t.Context(), cwd, capture)
			if err != nil {
				t.Fatal(err)
			}
			path := child.Path()
			if err := child.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenSQLiteStore(path, cwd, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if mode, err := reopened.CollaborationMode(); err != nil || mode != protocol.ModePlan {
				t.Fatalf("publication reread mode: %s %v", mode, err)
			}
			header := reopened.Header()
			if fork.SourceSessionID != binding.SessionID || header.ParentSessionID != binding.SessionID || header.ParentBranchID != binding.TargetBranchID || header.ForkEntryID != binding.TargetTipID || reopened.BranchTip() != binding.TargetTipID {
				t.Fatalf("rebound provenance: fork=%+v header=%+v", fork, header)
			}
			messages, err := reopened.Messages()
			if err != nil || len(messages) != 2 || messages[1].ID != "assistant-0" {
				t.Fatalf("rebound source: %v %v", messages, err)
			}
			if after := historyControlPage(t, peer); !reflect.DeepEqual(before, after) {
				t.Fatal("publication mutated parent")
			}
		})
	}
}

func TestHistoryControlSQLiteStatementFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"fork", "rename"} {
		t.Run(operation, func(t *testing.T) {
			store, peer, binding := historyControlFixture(t, true)
			db := peer.(*SQLiteStore).db
			statement := `CREATE TRIGGER fail_history BEFORE INSERT ON session_branches WHEN NEW.branch_name='managed fork' BEGIN SELECT RAISE(ABORT,'injected history failure'); END`
			if operation == "rename" {
				statement = `CREATE TRIGGER fail_history BEFORE UPDATE OF branch_name ON session_branches WHEN NEW.branch_name='managed rename' BEGIN SELECT RAISE(ABORT,'injected history failure'); END`
			}
			if _, err := db.ExecContext(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
			before := historyControlPage(t, peer)
			err := historyControlTry(t, store, binding, operation)
			if err == nil || errors.Is(err, ErrHistoryControlUnknown) {
				t.Fatalf("statement rollback must establish rejection: %v", err)
			}
			if after := historyControlPage(t, peer); !reflect.DeepEqual(before, after) {
				t.Fatal("failed statement retained a partial mutation")
			}
		})
	}
}

func TestHistoryControlSQLiteCommitFailureIsUnknown(t *testing.T) {
	store, peer, binding := historyControlFixture(t, true)
	db := peer.(*SQLiteStore).db
	for _, statement := range []string{
		`CREATE TABLE history_commit_failure(id TEXT PRIMARY KEY, missing TEXT REFERENCES session_branches(branch_id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER fail_history_commit AFTER INSERT ON session_branches WHEN NEW.branch_name='managed fork' BEGIN INSERT INTO history_commit_failure VALUES(NEW.branch_id,'absent'); END`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	before := historyControlPage(t, peer)
	err := historyControlTry(t, store, binding, "fork")
	if !errors.Is(err, ErrHistoryControlUnknown) {
		t.Fatalf("commit failure incorrectly classified: %v", err)
	}
	if after := historyControlPage(t, store); !reflect.DeepEqual(before, after) {
		t.Fatal("failed COMMIT left uncommitted rows visible to same-handle probes")
	}
}
