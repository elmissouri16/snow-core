package app

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func historyBinding(t *testing.T, a *App, target string) protocol.RPCHistoryControlBinding {
	t.Helper()
	snapshot, err := a.Session.(session.BranchVersionStore).BranchVersion(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.RPCHistoryControlBinding{SessionID: snapshot.Active.SessionID, SourceBranchID: snapshot.Active.BranchID, SourceTipID: snapshot.Active.TipID, TargetBranchID: target, TargetTipID: snapshot.Branch.TipID}
}

func TestManagedHistoryForkPassiveModeAndBoundPages(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
				t.Fatal(err)
			}
			seedMessageEditTurns(t, a, 2)
			tip := a.Session.BranchTip()
			if _, err := a.ForkBranch(""); err != nil {
				t.Fatal(err)
			}
			if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
				t.Fatal(err)
			}
			if err := a.Agent.WaitGoal(t.Context()); err != nil {
				t.Fatal(err)
			}
			p := &versionCountingProvider{Provider: a.Provider}
			if err := a.Agent.SetProvider(p); err != nil {
				t.Fatal(err)
			}
			before, err := a.RPCSettings()
			if err != nil {
				t.Fatal(err)
			}
			store, permission, epoch := a.Session, a.Perm, a.Agent.RootEpoch()
			binding := historyBinding(t, a, "main")
			result, err := a.ManagedBranchFork(t.Context(), protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "ordinary saved version"})
			if err != nil {
				t.Fatal(err)
			}
			if result.SessionID != store.ID() || result.BranchID == "main" || result.Branch.ParentID != "main" || result.TipID != tip || result.Mode != protocol.ModePlan || a.Agent.Mode() != result.Mode || result.RootEpoch <= epoch || result.RootEpoch != a.Agent.RootEpoch() {
				t.Fatalf("wrong fork: %+v", result)
			}
			if a.Session != store || a.Perm != permission || p.calls.Load() != 0 || a.Agent.IsRunning() {
				t.Fatal("fork executed or rebound runtime")
			}
			after, err := a.RPCSettings()
			if err != nil {
				t.Fatal(err)
			}
			if after.Provider != before.Provider || after.Model != before.Model || after.PermissionMode != before.PermissionMode || result.ReasoningEffort != a.Agent.BranchRestoreThinking(protocol.ModePlan) {
				t.Fatalf("settings changed: before=%+v after=%+v", before, after)
			}
			page, err := a.BranchMessagesPage(t.Context(), protocol.RPCBranchMessagesPageParams{SessionID: result.SessionID, BranchID: result.BranchID, TipID: result.TipID})
			if err != nil || page.Total != 4 {
				t.Fatalf("history page=%+v err=%v", page, err)
			}
			if _, err := a.ManagedBranchFork(t.Context(), protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "stale replay"}); !errors.Is(err, ErrHistoryControlRejected) {
				t.Fatalf("replay accepted: %v", err)
			}
		})
	}
}

func TestManagedHistoryRejectsCASNamesAndUnsafeTipWithoutMutation(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			seedMessageEditTurns(t, a, 2)
			binding := historyBinding(t, a, "main")
			before, _ := a.Agent.Branches()
			epoch := a.Agent.RootEpoch()
			for field := range 7 {
				p := protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "new branch"}
				switch field {
				case 0:
					p.SessionID = "other"
				case 1:
					p.SourceBranchID = "other"
				case 2:
					p.SourceTipID = "assistant-0"
				case 3:
					p.TargetBranchID = "other"
				case 4:
					p.TargetTipID = "assistant-0"
				case 5:
					p.Name = strings.Repeat("x", 65)
				case 6:
					p.Name = "bad\nname"
				}
				if _, err := a.ManagedBranchFork(t.Context(), p); !errors.Is(err, ErrHistoryControlRejected) || errors.Is(err, ErrHistoryControlUnknown) {
					t.Fatalf("field %d: %v", field, err)
				}
			}
			after, _ := a.Agent.Branches()
			if !reflect.DeepEqual(before, after) || a.Agent.RootEpoch() != epoch {
				t.Fatal("CAS/name rejection mutated branch state")
			}
			msg := protocol.NewAssistantMessage("pending", "", "fake", "fake-1", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "unpaired", Name: "read"}}, protocol.StopToolUse, nil)
			if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
				t.Fatal(err)
			}
			binding = historyBinding(t, a, "main")
			if _, err := a.ManagedBranchFork(t.Context(), protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "unsafe"}); !errors.Is(err, session.ErrInvalidForkBoundary) || !errors.Is(err, ErrHistoryControlRejected) {
				t.Fatalf("unpaired tip accepted: %v", err)
			}
		})
	}
}

func TestManagedHistoryRenameExactTargetMetadataOnly(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			seedMessageEditTurns(t, a, 1)
			fork, err := a.ForkBranch("")
			if err != nil {
				t.Fatal(err)
			}
			binding := historyBinding(t, a, "main")
			snapshot, err := a.Session.(session.BranchVersionStore).BranchVersion(t.Context(), "main")
			if err != nil {
				t.Fatal(err)
			}
			before, _ := a.Session.(session.BranchEntryStore).BranchEntries()
			epoch := a.Agent.RootEpoch()
			p := protocol.RPCManagedBranchRenameParams{RPCHistoryControlBinding: binding, OldName: snapshot.Branch.Name, Name: "original renamed"}
			result, err := a.ManagedBranchRename(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}
			if result.Branch.ID != "main" || result.Branch.Name != p.Name || result.Branch.Active || result.BranchID != fork.ID || result.RootEpoch != epoch {
				t.Fatalf("rename=%+v", result)
			}
			after, _ := a.Session.(session.BranchEntryStore).BranchEntries()
			if !reflect.DeepEqual(before, after) || a.Session.(session.ActiveBranchStore).ActiveBranchID() != fork.ID || a.Agent.RootEpoch() != epoch {
				t.Fatal("rename changed history or epoch")
			}
			if _, err := a.ManagedBranchRename(t.Context(), p); !errors.Is(err, ErrHistoryControlRejected) {
				t.Fatalf("old-name CAS accepted: %v", err)
			}
		})
	}
}

func TestManagedHistoryDetachedSessionParentUnchanged(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("SNOW_SESSIONS_DIR", root)
			a := messageEditTestApp(t, sqlite)
			if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
				t.Fatal(err)
			}
			seedMessageEditTurns(t, a, 2)
			binding := historyBinding(t, a, "main")
			store, epoch := a.Session, a.Agent.RootEpoch()
			before, _ := a.Session.(session.BranchEntryStore).BranchEntries()
			branches, _ := a.Agent.Branches()
			settings, _ := a.RPCSettings()
			result, err := a.ManagedSessionFork(t.Context(), protocol.RPCManagedSessionForkParams{RPCHistoryControlBinding: binding, Name: "detached"})
			if err != nil {
				t.Fatal(err)
			}
			if result.SessionID == store.ID() || result.SourceSessionID != store.ID() || result.SourceTipID != binding.TargetTipID || result.Mode != protocol.ModePlan || result.RootEpoch != epoch {
				t.Fatalf("fork=%+v", result)
			}
			after, _ := a.Session.(session.BranchEntryStore).BranchEntries()
			branchesAfter, _ := a.Agent.Branches()
			settingsAfter, _ := a.RPCSettings()
			if a.Session != store || a.Agent.RootEpoch() != epoch || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(branches, branchesAfter) || !reflect.DeepEqual(settings, settingsAfter) {
				t.Fatal("detached fork mutated parent")
			}
			items, err := session.NewFileIndex(root).List(a.CWD())
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].ID != result.SessionID {
				t.Fatalf("index=%+v", items)
			}
			// Opening succeeds only after the creating handle's lease is released.
			child, err := session.OpenSQLiteStore(items[0].Path, a.CWD(), session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer child.Close()
			mode, err := child.CollaborationMode()
			if err != nil || mode != protocol.ModePlan {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if child.Header().ParentSessionID != store.ID() || child.Header().ForkEntryID != binding.TargetTipID {
				t.Fatal("child provenance lost")
			}
		})
	}
}

func TestManagedHistoryPreservesTerminalGoalIdentitySemantics(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			seedMessageEditTurns(t, a, 1)
			goals := a.Session.(session.ThreadGoalStore)
			g := protocol.ThreadGoal{GoalID: "finished-goal", Objective: "finished objective", Status: protocol.GoalComplete, CreatedAt: 1, UpdatedAt: 1, TokensUsed: 17}
			if err := goals.CreateGoal(g, false); err != nil {
				t.Fatal(err)
			}
			if err := goals.SetGoalContinuationDeferred(true); err != nil {
				t.Fatal(err)
			}
			result, err := a.ManagedBranchFork(t.Context(), protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: historyBinding(t, a, "main"), Name: "terminal goal fork"})
			if err != nil {
				t.Fatal(err)
			}
			copied, err := goals.Goal()
			if err != nil {
				t.Fatal(err)
			}
			deferred, err := goals.GoalContinuationDeferred()
			if err != nil {
				t.Fatal(err)
			}
			if copied == nil || copied.GoalID == g.GoalID || copied.BranchID != result.BranchID || copied.Objective != g.Objective || copied.Status != g.Status || copied.TokensUsed != g.TokensUsed || !deferred {
				t.Fatalf("goal copy=%+v deferred=%v", copied, deferred)
			}
			if a.Agent.IsRunning() {
				t.Fatal("terminal goal resumed")
			}
		})
	}
}

func TestManagedHistoryNonterminalGoalsRejectEveryOperation(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, targetGoal := range []bool{false, true} {
			t.Run(fmt.Sprintf("sqlite=%v/target=%v", sqlite, targetGoal), func(t *testing.T) {
				a := messageEditTestApp(t, sqlite)
				seedMessageEditTurns(t, a, 1)
				fork, err := a.ForkBranch("")
				if err != nil {
					t.Fatal(err)
				}
				branches := a.Session.(session.BranchStore)
				if targetGoal {
					if err := branches.SelectBranch("main"); err != nil {
						t.Fatal(err)
					}
				}
				goals := a.Session.(session.ThreadGoalStore)
				if err := goals.CreateGoal(protocol.ThreadGoal{GoalID: "paused", Objective: "never resume", Status: protocol.GoalPaused, CreatedAt: 1, UpdatedAt: 1}, false); err != nil {
					t.Fatal(err)
				}
				if err := goals.SetGoalContinuationDeferred(true); err != nil {
					t.Fatal(err)
				}
				if targetGoal {
					if err := branches.SelectBranch(fork.ID); err != nil {
						t.Fatal(err)
					}
				}
				binding := historyBinding(t, a, "main")
				before, _ := a.Agent.Branches()
				epoch := a.Agent.RootEpoch()
				for _, op := range historyControlOperations(a, binding) {
					if err := op(t); !errors.Is(err, ErrHistoryControlRejected) || errors.Is(err, ErrHistoryControlUnknown) {
						t.Fatalf("nonterminal admitted: %v", err)
					}
				}
				after, _ := a.Agent.Branches()
				if !reflect.DeepEqual(before, after) || a.Agent.RootEpoch() != epoch {
					t.Fatal("goal rejection mutated history")
				}
				if targetGoal {
					if err := branches.SelectBranch("main"); err != nil {
						t.Fatal(err)
					}
				}
				g, _ := goals.Goal()
				deferred, _ := goals.GoalContinuationDeferred()
				if g.Status != protocol.GoalPaused || !deferred {
					t.Fatal("rejection changed goal or deferral")
				}
			})
		}
	}
}

func historyControlOperations(a *App, binding protocol.RPCHistoryControlBinding) []func(*testing.T) error {
	return []func(*testing.T) error{
		func(t *testing.T) error {
			_, err := a.ManagedBranchFork(t.Context(), protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: binding, Name: "new fork"})
			return err
		},
		func(t *testing.T) error {
			_, err := a.ManagedSessionFork(t.Context(), protocol.RPCManagedSessionForkParams{RPCHistoryControlBinding: binding, Name: "detached"})
			return err
		},
		func(t *testing.T) error {
			_, err := a.ManagedBranchRename(t.Context(), protocol.RPCManagedBranchRenameParams{RPCHistoryControlBinding: binding, OldName: "main", Name: "renamed"})
			return err
		},
	}
}

func TestManagedHistoryRunningAndReviewRejectionsDoNotConsumeQueue(t *testing.T) {
	a := messageEditTestApp(t, false)
	p := &reviewQueueProvider{Provider: a.Provider, started: make(chan struct{})}
	if err := a.Agent.SetProvider(p); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(t.Context(), "initial") }()
	<-p.started
	_, turn, _ := a.Agent.ActiveTurn()
	q, err := a.QueueList(protocol.RPCQueueListParams{SessionID: a.Session.ID(), TurnID: turn})
	if err != nil {
		t.Fatal(err)
	}
	q, err = a.QueueEnqueue(protocol.RPCQueueEnqueueParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: "retain exactly"})
	if err != nil {
		t.Fatal(err)
	}
	for _, review := range []bool{false, true} {
		if review {
			a.Agent.Abort()
			<-done
		}
		q, err = a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
		if err != nil {
			t.Fatal(err)
		}
		before, _ := a.Agent.Branches()
		epoch := a.Agent.RootEpoch()
		for _, op := range historyControlOperations(a, historyBinding(t, a, "main")) {
			if err := op(t); !errors.Is(err, ErrHistoryControlRejected) {
				t.Fatalf("queued history control admitted: %v", err)
			}
		}
		afterQueue, err := a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
		if err != nil {
			t.Fatal(err)
		}
		after, _ := a.Agent.Branches()
		if !reflect.DeepEqual(q, afterQueue) || !reflect.DeepEqual(before, after) || a.Agent.RootEpoch() != epoch {
			t.Fatal("rejection consumed queue or changed history")
		}
		if !review && !a.Agent.IsRunning() {
			t.Fatal("rejection stopped running turn")
		}
	}
}
