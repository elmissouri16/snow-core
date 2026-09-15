package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type versionCountingProvider struct {
	provider.Provider
	calls atomic.Int32
}

func (p *versionCountingProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.calls.Add(1)
	return p.Provider.Chat(ctx, req)
}
func seedVersionSpans(t *testing.T, a *App) {
	t.Helper()
	seedMessageEditTurns(t, a, 1)
	for i := 1; i < 3; i++ {
		user := fmt.Sprintf("user-%d", i)
		marker, err := session.NewAgentInputSpanEntry(fmt.Sprintf("span-%d", i), session.AgentInputSpan{Version: 1, RootTurnID: "turn-0", QueueID: fmt.Sprintf("queue-%d", i), UserEntryID: user, Kind: protocol.QueuedInputFollowUp})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range []session.Entry{marker, {ID: user, Type: session.EntryMessage, Message: new(protocol.NewUserMessage(user, "", fmt.Sprintf("queued %d", i)))}, {ID: fmt.Sprintf("step-%d", i), Type: session.EntryMeta, Key: session.MetaAgentStep, Value: "provider"}, {ID: fmt.Sprintf("assistant-%d", i), Type: session.EntryMessage, Message: new(protocol.NewAssistantMessage(fmt.Sprintf("assistant-%d", i), "", "fake", "fake-1", []protocol.ContentBlock{protocol.NewTextBlock("queued answer")}, protocol.StopStop, nil))}} {
			if err := a.Session.Append(entry); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func versionBinding(t *testing.T, a *App, target, tip string) protocol.RPCBranchRestorePrepareParams {
	t.Helper()
	page, err := a.BranchesPage(t.Context(), protocol.RPCBranchesPageParams{SessionID: a.Session.ID()})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.RPCBranchRestorePrepareParams{SessionID: a.Session.ID(), SourceBranchID: page.ActiveBranchID, SourceTipID: page.ActiveTipID, TargetBranchID: target, TargetTipID: tip}
}
func noVersionAck(protocol.RPCBranchRestoreCommitted, []protocol.Message) (func() error, error) {
	return func() error { return nil }, nil
}
func TestBranchRestoreOriginalQueuedVersionsWithoutExecution(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, regenerate := range []bool{false, true} {
			for selected := range 3 {
				t.Run(fmt.Sprintf("sqlite=%t/regenerate=%t/selected=%d", sqlite, regenerate, selected), func(t *testing.T) {
					a := messageEditTestApp(t, sqlite)
					seedVersionSpans(t, a)
					originalTip := a.Session.BranchTip()
					p := &versionCountingProvider{Provider: a.Provider}
					if err := a.Agent.SetProvider(p); err != nil {
						t.Fatal(err)
					}
					if regenerate {
						prepared, err := a.PrepareMessageRegenerate(t.Context(), protocol.RPCMessageRegeneratePrepareParams{SessionID: a.Session.ID(), EntryID: fmt.Sprintf("assistant-%d", selected)})
						if err != nil {
							t.Fatal(err)
						}
						if err := a.CommitMessageRegenerate(t.Context(), protocol.RPCMessageRegenerateCommitParams{SessionID: a.Session.ID(), EditToken: prepared.EditToken}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { return nil }); err != nil {
							t.Fatal(err)
						}
					} else {
						prepared, err := a.PrepareMessageEdit(t.Context(), protocol.RPCMessageEditPrepareParams{SessionID: a.Session.ID(), EntryID: fmt.Sprintf("user-%d", selected)})
						if err != nil {
							t.Fatal(err)
						}
						if err := a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: a.Session.ID(), EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { return nil }); err != nil {
							t.Fatal(err)
						}
					}
					before, _ := a.BranchesPage(t.Context(), protocol.RPCBranchesPageParams{SessionID: a.Session.ID()})
					settings, _ := a.RPCSettings()
					permission := a.Perm
					calls := p.calls.Load()
					preview, err := a.BranchMessagesPage(t.Context(), protocol.RPCBranchMessagesPageParams{SessionID: a.Session.ID(), BranchID: "main", TipID: originalTip, Limit: 2})
					if err != nil || preview.Total != 6 || len(preview.Messages) != 2 || preview.NextCursor == "" {
						t.Fatalf("preview=%+v err=%v", preview, err)
					}
					page, err := a.BranchMessagesPage(t.Context(), protocol.RPCBranchMessagesPageParams{SessionID: a.Session.ID(), BranchID: "main", TipID: originalTip, Limit: 2, Cursor: preview.NextCursor})
					if err != nil || page.Start != 2 || page.Messages[0].ID != "user-1" {
						t.Fatalf("page=%+v err=%v", page, err)
					}
					if a.Session.BranchTip() != before.ActiveTipID {
						t.Fatal("preview selected target")
					}
					prepared, err := a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", originalTip))
					if err != nil {
						t.Fatal(err)
					}
					callbacks := 0
					err = a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, func(result protocol.RPCBranchRestoreCommitted, messages []protocol.Message) (func() error, error) {
						if a.Session.BranchTip() != before.ActiveTipID || len(messages) != 6 {
							t.Fatal("publication not preflighted on unchanged source")
						}
						return func() error {
							callbacks++
							if a.Agent.IsRunning() || a.Session.BranchTip() != originalTip || result.BranchID != "main" {
								t.Fatal("ack not exact idle target")
							}
							return nil
						}, nil
					})
					if err != nil {
						t.Fatal(err)
					}
					afterSettings, _ := a.RPCSettings()
					if callbacks != 1 || p.calls.Load() != calls || !reflect.DeepEqual(settings, afterSettings) || a.Perm != permission {
						t.Fatal("restore replayed execution or changed settings/permission")
					}
					after, _ := a.BranchesPage(t.Context(), protocol.RPCBranchesPageParams{SessionID: a.Session.ID()})
					if len(after.Branches) != len(before.Branches) {
						t.Fatal("restore removed/created a branch")
					}
					if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, noVersionAck); !errors.Is(err, ErrBranchRestoreRejected) {
						t.Fatal("token replay accepted")
					}
				})
			}
		}
	}
}
func TestBranchRestoreTokenStalenessExpiryAndBounds(t *testing.T) {
	a := messageEditTestApp(t, false)
	seedMessageEditTurns(t, a, 1)
	tip := a.Session.BranchTip()
	if _, err := a.ForkBranch(""); err != nil {
		t.Fatal(err)
	}
	binding := versionBinding(t, a, "main", tip)
	prepared, err := a.PrepareBranchRestore(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	auth := a.branchRestores[prepared.RestoreToken]
	auth.expires = time.Now().Add(-time.Second)
	a.branchRestores[prepared.RestoreToken] = auth
	if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, noVersionAck); !errors.Is(err, ErrBranchRestoreRejected) {
		t.Fatal("expired token accepted")
	}
	prepared, err = a.PrepareBranchRestore(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Session.Append(session.Entry{ID: "changed-tip", Type: session.EntryMeta, Key: "test", Value: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, noVersionAck); !errors.Is(err, ErrBranchRestoreRejected) {
		t.Fatal("source changed after preparation")
	}
	binding = versionBinding(t, a, "main", tip)
	for range maxBranchRestoreTokens {
		if _, err := a.PrepareBranchRestore(t.Context(), binding); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.PrepareBranchRestore(t.Context(), binding); !errors.Is(err, ErrBranchRestoreRejected) {
		t.Fatal("unbounded authorization pool")
	}
}

type versionFailureStore struct {
	session.Store
	session.BranchVersionStore
	mutate, unknownProbe bool
}

func (s *versionFailureStore) RestoreBranchVersion(ctx context.Context, p protocol.RPCBranchRestorePrepareParams, mode protocol.CollaborationMode) error {
	if s.mutate {
		if err := s.BranchVersionStore.RestoreBranchVersion(ctx, p, mode); err != nil {
			return err
		}
	}
	return errors.New("private storage diagnostics")
}
func (s *versionFailureStore) ProbeBranchVersion(ctx context.Context) (session.BranchVersionIdentity, error) {
	if s.unknownProbe {
		return session.BranchVersionIdentity{}, errors.New("private probe diagnostics")
	}
	return s.BranchVersionStore.ProbeBranchVersion(ctx)
}
func TestBranchRestoreAmbiguousOutcomesAndAckFailure(t *testing.T) {
	for _, outcome := range []string{"absent", "committed", "unprobeable", "ack"} {
		t.Run(outcome, func(t *testing.T) {
			a := messageEditTestApp(t, false)
			seedMessageEditTurns(t, a, 1)
			tip := a.Session.BranchTip()
			if _, err := a.ForkBranch(""); err != nil {
				t.Fatal(err)
			}
			prepared, err := a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", tip))
			if err != nil {
				t.Fatal(err)
			}
			if outcome != "ack" {
				wrapper := &versionFailureStore{Store: a.Session, BranchVersionStore: a.Session.(session.BranchVersionStore), mutate: outcome == "committed", unknownProbe: outcome == "unprobeable"}
				a.Session = wrapper
				unlock := a.Agent.LockAdmission()
				err = a.Agent.SetSessionQuietAdmitted(wrapper)
				unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			ack := noVersionAck
			if outcome == "ack" {
				ack = func(protocol.RPCBranchRestoreCommitted, []protocol.Message) (func() error, error) {
					return func() error { return errors.New("write failed") }, nil
				}
			}
			err = a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, ack)
			if outcome == "absent" {
				if !errors.Is(err, ErrBranchRestoreRejected) || errors.Is(err, ErrBranchRestoreUnknown) {
					t.Fatalf("confirmed absence=%v", err)
				}
			} else if !errors.Is(err, ErrBranchRestoreUnknown) {
				t.Fatalf("ambiguous mutation=%v", err)
			}
		})
	}
}

func TestBranchRestoreReadOnlyWhileRunningAndQueueGuards(t *testing.T) {
	a := messageEditTestApp(t, false)
	seedMessageEditTurns(t, a, 1)
	original := a.Session.BranchTip()
	if _, err := a.ForkBranch(""); err != nil {
		t.Fatal(err)
	}
	prepared, err := a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", original))
	if err != nil {
		t.Fatal(err)
	}
	p := &reviewQueueProvider{Provider: a.Provider, started: make(chan struct{})}
	if err := a.Agent.SetProvider(p); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(t.Context(), "active") }()
	<-p.started
	preview, err := a.BranchMessagesPage(t.Context(), protocol.RPCBranchMessagesPageParams{SessionID: a.Session.ID(), BranchID: "main", TipID: original})
	if err != nil || preview.Total != 2 || !a.Agent.IsRunning() {
		t.Fatalf("preview preempted active run: %v", err)
	}
	_, turn, _ := a.Agent.ActiveTurn()
	q, err := a.QueueList(protocol.RPCQueueListParams{SessionID: a.Session.ID(), TurnID: turn})
	if err != nil {
		t.Fatal(err)
	}
	q, err = a.QueueEnqueue(protocol.RPCQueueEnqueueParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: "held"})
	if err != nil {
		t.Fatal(err)
	}
	binding := versionBinding(t, a, "main", original)
	if _, err := a.PrepareBranchRestore(t.Context(), binding); !errors.Is(err, ErrBranchRestoreRejected) {
		t.Fatal("active queue admitted restore")
	}
	a.Agent.Abort()
	<-done
	if _, err := a.PrepareBranchRestore(t.Context(), binding); !errors.Is(err, ErrBranchRestoreRejected) {
		t.Fatal("held review admitted preparation")
	}
	if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, noVersionAck); !errors.Is(err, ErrBranchRestoreRejected) {
		t.Fatal("held review admitted commit")
	}
	q, err = a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
	if err != nil || len(q.ReviewItems) != 1 {
		t.Fatal("rejection stranded review")
	}
	if _, err := a.QueueRemove(protocol.RPCQueueRemoveParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.ReviewItems[0].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", original)); err != nil {
		t.Fatal("explicit discard failed to release restore")
	}
}
func TestBranchRestoreRejectsTargetNonterminalGoalAndPublicationPreflight(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			seedMessageEditTurns(t, a, 1)
			tip := a.Session.BranchTip()
			fork, err := a.ForkBranch("")
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", tip))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, func(protocol.RPCBranchRestoreCommitted, []protocol.Message) (func() error, error) {
				return nil, errors.New("oversized public response")
			}); !errors.Is(err, ErrBranchRestoreRejected) {
				t.Fatal("publication preflight was not a no-change rejection")
			}
			if a.Session.(session.ActiveBranchStore).ActiveBranchID() != fork.ID {
				t.Fatal("publication preflight selected target")
			}
			prepared, err = a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", tip))
			if err != nil {
				t.Fatal(err)
			}
			branches := a.Session.(session.BranchStore)
			if err := branches.SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			if err := a.Session.(session.ThreadGoalStore).CreateGoal(protocol.ThreadGoal{GoalID: "old-goal", Objective: "must not resume", Status: protocol.GoalPaused, CreatedAt: 1, UpdatedAt: 1}, false); err != nil {
				t.Fatal(err)
			}
			if err := branches.SelectBranch(fork.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := a.PrepareBranchRestore(t.Context(), versionBinding(t, a, "main", tip)); !errors.Is(err, ErrBranchRestoreRejected) {
				t.Fatal("target goal accepted by prepare")
			}
			if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, noVersionAck); !errors.Is(err, ErrBranchRestoreRejected) {
				t.Fatal("target goal created after prepare accepted by commit")
			}
		})
	}
}

func TestBranchRestoreTargetModeIsBoundAndAppliedWithoutExecution(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		t.Run(fmt.Sprint(sqlite), func(t *testing.T) {
			a := messageEditTestApp(t, sqlite)
			if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
				t.Fatal(err)
			}
			seedMessageEditTurns(t, a, 1)
			targetTip := a.Session.BranchTip()
			fork, err := a.ForkBranch("")
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
				t.Fatal(err)
			}
			// Legacy Default-mode transitions can briefly schedule an empty goal
			// worker. Join it rather than weakening restore's strict idle gate.
			if err := a.Agent.WaitGoal(t.Context()); err != nil {
				t.Fatal(err)
			}
			provider := &versionCountingProvider{Provider: a.Provider}
			if err := a.Agent.SetProvider(provider); err != nil {
				t.Fatal(err)
			}
			binding := versionBinding(t, a, "main", targetTip)
			prepared, err := a.PrepareBranchRestore(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			// Mode changes do not need to append messages; the authorization must still
			// reject a different saved target mode under the same exact branch tip.
			branches := a.Session.(session.BranchStore)
			state := a.Session.(session.ThreadStateStore)
			if err := branches.SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			if err := state.SetCollaborationMode(protocol.ModeDefault); err != nil {
				t.Fatal(err)
			}
			if err := branches.SelectBranch(fork.ID); err != nil {
				t.Fatal(err)
			}
			if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, noVersionAck); !errors.Is(err, ErrBranchRestoreRejected) {
				t.Fatal("changed target mode accepted")
			}
			if err := branches.SelectBranch("main"); err != nil {
				t.Fatal(err)
			}
			if err := state.SetCollaborationMode(protocol.ModePlan); err != nil {
				t.Fatal(err)
			}
			if err := branches.SelectBranch(fork.ID); err != nil {
				t.Fatal(err)
			}
			prepared, err = a.PrepareBranchRestore(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.CommitBranchRestore(t.Context(), protocol.RPCBranchRestoreCommitParams{SessionID: a.Session.ID(), RestoreToken: prepared.RestoreToken}, func(value protocol.RPCBranchRestoreCommitted, _ []protocol.Message) (func() error, error) {
				if value.Mode != protocol.ModePlan || a.Agent.Mode() != protocol.ModeDefault {
					t.Fatal("ACK preflight did not preserve old mode")
				}
				return func() error {
					if a.Agent.Mode() != value.Mode {
						t.Fatal("ACK mode disagrees with runtime")
					}
					return nil
				}, nil
			}); err != nil {
				t.Fatal(err)
			}
			mode, err := state.CollaborationMode()
			if err != nil || mode != protocol.ModePlan || provider.calls.Load() != 0 || a.Agent.IsRunning() {
				t.Fatalf("mode restore executed work or lost durable mode: %s %v", mode, err)
			}
		})
	}
}
