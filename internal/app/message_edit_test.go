package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func messageEditTestApp(t *testing.T, sqlite bool) *App {
	t.Helper()
	a, err := New(t.Context(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if sqlite {
		store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "edit.db"), a.CWD(), session.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.SetSession(store); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func seedMessageEditTurns(t *testing.T, a *App, n int) {
	t.Helper()
	for i := range n {
		user, assistant := fmt.Sprintf("user-%d", i), fmt.Sprintf("assistant-%d", i)
		for _, entry := range []session.Entry{
			{Type: session.EntryMeta, ID: fmt.Sprintf("turn-%d", i), Key: session.MetaAgentTurn, Value: "user"},
			{Type: session.EntryMessage, ID: user, Message: new(protocol.NewUserMessage(user, "", fmt.Sprintf("original %d", i)))},
			{Type: session.EntryMeta, ID: fmt.Sprintf("step-%d", i), Key: session.MetaAgentStep, Value: "provider"},
			{Type: session.EntryMessage, ID: assistant, Message: new(protocol.NewAssistantMessage(assistant, "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("answer")}, protocol.StopStop, nil))},
		} {
			if err := a.Session.Append(entry); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func prepareEdit(t *testing.T, a *App, turn string) protocol.RPCMessageEditPrepared {
	t.Helper()
	result, err := a.PrepareMessageEdit(t.Context(), protocol.RPCMessageEditPrepareParams{SessionID: a.Session.ID(), TurnID: turn})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMessageEditSameSessionAdmissionAndRunStats(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for selected := range 3 {
			t.Run(fmt.Sprintf("sqlite=%t/selected=%d", sqlite, selected), func(t *testing.T) {
				a := messageEditTestApp(t, sqlite)
				seedMessageEditTurns(t, a, 3)
				if err := a.RenameSession("keep title"); err != nil {
					t.Fatal(err)
				}
				oldStore, oldID, oldTip, oldPermission := a.Session, a.Session.ID(), a.Session.BranchTip(), a.Perm
				prepared := prepareEdit(t, a, fmt.Sprintf("turn-%d", selected))
				callbacks := 0
				var committed protocol.RPCMessageEditCommitted
				err := a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: oldID, EditToken: prepared.EditToken, Text: "replacement"}, func(result protocol.RPCMessageEditCommitted, messages []protocol.Message) error {
					callbacks++
					committed = result
					if !a.Agent.IsRunning() || len(messages) != selected*2+1 || messages[len(messages)-1].ID != result.UserEntryID || messages[len(messages)-1].Content[0].Text != "replacement" {
						t.Fatalf("admitted history=%+v result=%+v", messages, result)
					}
					entries, _ := a.Session.(session.BranchEntryStore).BranchEntries()
					if entries[len(entries)-2].ID != result.TurnID || entries[len(entries)-2].Key != session.MetaAgentTurn {
						t.Fatal("ACK precedes durable turn identity")
					}
					stats, err := a.Session.(session.AgentRunStatsStore).AgentRunStats()
					if err != nil || stats.Turns != uint64(selected+1) || stats.Steps != uint64(selected) {
						t.Fatalf("before provider stats=%+v err=%v", stats, err)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if callbacks != 1 || a.Session != oldStore || a.Session.ID() != oldID || a.Perm != oldPermission || committed.SourceTipID != oldTip || committed.BranchID == prepared.SourceBranchID {
					t.Fatal("session/authority/admission mismatch")
				}
				title, err := a.Agent.SessionTitle()
				if err != nil || title != "keep title" {
					t.Fatalf("title=%q err=%v", title, err)
				}
				stats, err := a.Session.(session.AgentRunStatsStore).AgentRunStats()
				if err != nil || stats.Turns != uint64(selected+1) {
					t.Fatalf("run stats=%+v err=%v", stats, err)
				}
				if err := a.SelectBranch(prepared.SourceBranchID); err != nil {
					t.Fatal(err)
				}
				messages, _ := a.Session.Messages()
				if len(messages) != 6 || a.Session.BranchTip() != oldTip {
					t.Fatal("original branch changed")
				}
			})
		}
	}
}

func TestMessageEditStaleAndSingleUseTokens(t *testing.T) {
	for _, kind := range []string{"tip", "branch", "session", "expired", "unknown", "replay"} {
		t.Run(kind, func(t *testing.T) {
			a := messageEditTestApp(t, false)
			seedMessageEditTurns(t, a, 1)
			prepared := prepareEdit(t, a, "turn-0")
			params := protocol.RPCMessageEditCommitParams{SessionID: a.Session.ID(), EditToken: prepared.EditToken, Text: "replacement"}
			switch kind {
			case "tip":
				_ = a.Session.Append(session.Entry{Type: session.EntryMeta, Key: "later", Value: "changed"})
			case "branch":
				if _, err := a.ForkBranch(a.Session.BranchTip()); err != nil {
					t.Fatal(err)
				}
			case "session":
				params.SessionID = "wrong-session"
			case "expired":
				authorization := a.messageEdits[prepared.EditToken]
				authorization.expires = time.Now().Add(-time.Second)
				a.messageEdits[prepared.EditToken] = authorization
			case "unknown":
				params.EditToken = "unknown"
			case "replay":
				if err := a.CommitMessageEdit(t.Context(), params, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			tip := a.Session.BranchTip()
			branches, _ := a.Session.(session.BranchStore).Branches()
			err := a.CommitMessageEdit(t.Context(), params, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
				t.Fatal("invalid token admitted")
				return nil
			})
			after, _ := a.Session.(session.BranchStore).Branches()
			if err == nil || a.Session.BranchTip() != tip || len(after) != len(branches) {
				t.Fatalf("stale mutation err=%v", err)
			}
		})
	}
}

func TestMessageEditPoolBoundAndConcurrentCommit(t *testing.T) {
	a := messageEditTestApp(t, false)
	seedMessageEditTurns(t, a, 1)
	prepared := prepareEdit(t, a, "turn-0")
	for range maxMessageEditTokens - 1 {
		prepareEdit(t, a, "turn-0")
	}
	if _, err := a.PrepareMessageEdit(t.Context(), protocol.RPCMessageEditPrepareParams{SessionID: a.Session.ID(), TurnID: "turn-0"}); err == nil {
		t.Fatal("pool exceeded bound")
	}
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_ = a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { admitted.Add(1); return nil })
		})
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("admitted %d", admitted.Load())
	}
}

func TestMessageEditPluginVetoBeforeBranch(t *testing.T) {
	for _, script := range []string{
		`snow.registerHook("before_session_change",()=>({block:"no rewind"}));`,
		`snow.registerHook("before_prompt",()=>({block:"no prompt"}));`,
		`snow.registerHook("before_prompt",()=>({text:"transformed"}));`,
	} {
		t.Run(script, func(t *testing.T) {
			a := lifecycleApp(t, script, []string{"hooks"})
			seedMessageEditTurns(t, a, 1)
			prepared := prepareEdit(t, a, "turn-0")
			generation := a.PluginGeneration()
			err := a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { t.Fatal("veto admitted"); return nil })
			branches, _ := a.Session.(session.BranchStore).Branches()
			if err == nil || errors.Is(err, ErrMessageEditOutcomeUnknown) || len(branches) != 1 || a.Session.BranchTip() != prepared.SourceTipID || a.PluginGeneration() != generation {
				t.Fatalf("veto state=%+v err=%v", branches, err)
			}
		})
	}
}

func TestMessageEditAcknowledgementFailureRetainsCommittedBranch(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			seedMessageEditTurns(t, a, 2)
			prepared := prepareEdit(t, a, "turn-0")
			err := a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
				return errors.New("projection/write unavailable")
			})
			if !errors.Is(err, ErrMessageEditOutcomeUnknown) {
				t.Fatalf("missing ambiguous outcome: %v", err)
			}
			messages, _ := a.Session.Messages()
			if len(messages) != 1 || messages[0].Content[0].Text != "replacement" || a.Session.(session.ActiveBranchStore).ActiveBranchID() == prepared.SourceBranchID || a.Agent.IsRunning() {
				t.Fatalf("committed history=%+v", messages)
			}
		})
	}
}

func TestMessageEditCancelAfterAdmissionRetainsBranch(t *testing.T) {
	a := messageEditTestApp(t, false)
	seedMessageEditTurns(t, a, 1)
	prepared := prepareEdit(t, a, "turn-0")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := a.CommitMessageEdit(ctx, protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) || a.Session.(session.ActiveBranchStore).ActiveBranchID() == prepared.SourceBranchID {
		t.Fatalf("cancellation rewound admitted branch: %v", err)
	}
}

func TestMessageEditPluginNotificationRunsAfterAdmissionAndPluginLocks(t *testing.T) {
	a := lifecycleApp(t, `snow.registerCommand({name:"read",description:"read",uses:["workflow"],async run(_,ctx){return String(await ctx.workflow.get({key:"marker"}))}});`, []string{"commands", "workflow"})
	seedMessageEditTurns(t, a, 1)
	prepared := prepareEdit(t, a, "turn-0")
	observed := make(chan error, 1)
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type != protocol.EvPluginSessionChanged {
			return
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, _, err := a.Agent.SessionIdentity(); err != nil {
			observed <- err
			return
		}
		_, err := a.RunPluginCommand(ctx, "guard:read", "")
		observed <- err
	})
	defer unsubscribe()
	if err := a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-observed:
		if err != nil {
			t.Fatalf("new host callback failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("plugin notification deadlocked on admission/session guard")
	}
}

func TestMessageEditRejectsNonterminalGoalWithoutChangingDeferral(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred=%t", deferred), func(t *testing.T) {
			a := messageEditTestApp(t, true)
			seedMessageEditTurns(t, a, 1)
			// Create only the persisted goal. App.CreateGoal also starts asynchronous
			// continuation, whose independent appends invalidate a read-only snapshot.
			if _, err := a.Goal.Create("unfinished", nil, false); err != nil {
				t.Fatal(err)
			}
			if err := a.Goal.Defer(deferred); err != nil {
				t.Fatal(err)
			}
			beforeGoal, err := a.Goal.Get()
			if err != nil {
				t.Fatal(err)
			}
			if beforeGoal == nil || beforeGoal.Status.Terminal() || a.Agent.IsRunning() {
				t.Fatalf("fixture must be idle with a nonterminal goal: %+v", beforeGoal)
			}
			beforeGoal = beforeGoal.Clone()
			beforeDeferred, err := a.Goal.Deferred()
			if err != nil || beforeDeferred != deferred {
				t.Fatalf("goal deferral fixture=%t want=%t err=%v", beforeDeferred, deferred, err)
			}
			before := a.Session.BranchTip()
			beforeMessages, err := a.Session.Messages()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.PrepareMessageEdit(t.Context(), protocol.RPCMessageEditPrepareParams{SessionID: a.Session.ID(), TurnID: "turn-0"}); err == nil || !strings.Contains(err.Error(), "rejects nonterminal goals") {
				t.Fatalf("expected nonterminal-goal rejection, got %v", err)
			}
			if a.Session.BranchTip() != before {
				t.Fatal("read-only preparation changed goal/history")
			}
			afterMessages, err := a.Session.Messages()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeMessages, afterMessages) {
				t.Fatal("read-only preparation changed messages")
			}
			afterGoal, err := a.Goal.Get()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeGoal, afterGoal) {
				t.Fatalf("read-only preparation changed goal: before=%+v after=%+v", beforeGoal, afterGoal)
			}
			afterDeferred, err := a.Goal.Deferred()
			if err != nil || afterDeferred != beforeDeferred {
				t.Fatalf("read-only preparation changed deferral: before=%t after=%t err=%v", beforeDeferred, afterDeferred, err)
			}
			if a.Agent.IsRunning() {
				t.Fatal("read-only preparation started goal continuation")
			}
		})
	}
}
