package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func prepareRegenerate(t *testing.T, a *App, reply string) protocol.RPCMessageRegeneratePrepared {
	t.Helper()
	prepared, err := a.PrepareMessageRegenerate(t.Context(), protocol.RPCMessageRegeneratePrepareParams{SessionID: a.Session.ID(), EntryID: reply})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestMessageRegenerateSameSessionFirstMiddleLatestAndReopen(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for selected := range 3 {
			t.Run(fmt.Sprintf("sqlite=%t/selected=%d", sqlite, selected), func(t *testing.T) {
				a := messageEditTestApp(t, sqlite)
				seedMessageEditTurns(t, a, 3)
				if err := a.RenameSession("preserved title"); err != nil {
					t.Fatal(err)
				}
				prepared := prepareRegenerate(t, a, fmt.Sprintf("assistant-%d", selected))
				originalStore, originalPermission, oldTip := a.Session, a.Perm, a.Session.BranchTip()
				acknowledged := false
				var result protocol.RPCMessageEditCommitted
				err := a.CommitMessageRegenerate(t.Context(), protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken}, func(committed protocol.RPCMessageEditCommitted, messages []protocol.Message) error {
					result = committed
					acknowledged = true
					if len(messages) != selected*2+1 || messages[len(messages)-1].Content[0].Text != prepared.Text || messages[len(messages)-1].ID != committed.UserEntryID || committed.TurnID == prepared.TurnID {
						t.Fatalf("regeneration duplicated/changed input: %+v %+v", messages, committed)
					}
					stats, err := a.Session.(session.AgentRunStatsStore).AgentRunStats()
					if err != nil || stats.Turns != uint64(selected+1) || stats.Steps != uint64(selected) {
						t.Fatalf("provider outran ACK/stats: %+v %v", stats, err)
					}
					return nil
				})
				if err != nil || !acknowledged || a.Session != originalStore || a.Perm != originalPermission || a.Session.ID() != prepared.SessionID || result.SourceTipID != oldTip {
					t.Fatalf("same-session regeneration err=%v", err)
				}
				title, err := a.Agent.SessionTitle()
				if err != nil || title != "preserved title" {
					t.Fatal("title changed")
				}
				if sqlite {
					reopened, err := session.OpenSQLiteStore(a.Session.Path(), a.CWD(), session.Options{})
					if err != nil {
						t.Fatal(err)
					}
					messages, err := reopened.Messages()
					if err != nil || len(messages) != selected*2+2 || messages[len(messages)-2].ID != result.UserEntryID || reopened.ActiveBranchID() != result.BranchID {
						t.Fatalf("reopened projection=%+v err=%v", messages, err)
					}
					_ = reopened.Close()
				}
				if err := a.SelectBranch(prepared.SourceBranchID); err != nil {
					t.Fatal(err)
				}
				messages, _ := a.Session.Messages()
				if len(messages) != 6 || a.Session.BranchTip() != oldTip {
					t.Fatal("original branch was lost")
				}
			})
		}
	}
}

func TestMessageRegenerateActionBoundAndStaleTokens(t *testing.T) {
	for _, kind := range []string{"edit-to-regenerate", "regenerate-to-edit", "expired", "tip", "session", "replay"} {
		t.Run(kind, func(t *testing.T) {
			a := messageEditTestApp(t, false)
			seedMessageEditTurns(t, a, 1)
			prepared := prepareRegenerate(t, a, "assistant-0")
			params := protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken}
			if kind == "edit-to-regenerate" {
				params.EditToken = prepareEdit(t, a, "turn-0").EditToken
			}
			if kind == "tip" {
				_ = a.Session.Append(session.Entry{Type: session.EntryMeta, Key: "changed", Value: "tip"})
			}
			if kind == "session" {
				params.SessionID = "wrong"
			}
			if kind == "expired" {
				authorization := a.messageEdits[params.EditToken]
				authorization.expires = time.Now().Add(-time.Second)
				a.messageEdits[params.EditToken] = authorization
			}
			if kind == "replay" {
				if err := a.CommitMessageRegenerate(t.Context(), params, func(protocol.RPCMessageEditCommitted, []protocol.Message) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			before := a.Session.BranchTip()
			callback := func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
				t.Fatal("invalid token admitted")
				return nil
			}
			var err error
			if kind == "regenerate-to-edit" {
				err = a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: params.SessionID, EditToken: params.EditToken, Text: "injected replacement"}, callback)
			} else {
				err = a.CommitMessageRegenerate(t.Context(), params, callback)
			}
			if err == nil || errors.Is(err, ErrMessageEditOutcomeUnknown) || a.Session.BranchTip() != before {
				t.Fatalf("action/stale rejection err=%v", err)
			}
			if _, exists := a.messageEdits[params.EditToken]; exists {
				t.Fatal("attempted token not consumed")
			}
		})
	}
}

func TestMessageRegenerateSharedAtomicFailureSemantics(t *testing.T) {
	for _, kind := range []string{"before-input", "commit-then-error", "acknowledgment", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			a := messageEditTestApp(t, false)
			store := &editFailureStore{MemoryStore: session.NewMemoryStore(session.Options{})}
			if err := a.SetSession(store); err != nil {
				t.Fatal(err)
			}
			seedMessageEditTurns(t, a, 1)
			prepared := prepareRegenerate(t, a, "assistant-0")
			store.failUser = kind == "before-input"
			store.commitThenFail = kind == "commit-then-error"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := a.CommitMessageRegenerate(ctx, protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
				if kind == "acknowledgment" {
					return errors.New("ACK unavailable")
				}
				if kind == "cancellation" {
					cancel()
					return nil
				}
				t.Fatal("failed input acknowledged")
				return nil
			})
			if err == nil {
				t.Fatal("injected failure succeeded")
			}
			if kind == "before-input" {
				if errors.Is(err, ErrMessageEditOutcomeUnknown) || store.ActiveBranchID() != prepared.SourceBranchID || store.BranchTip() != prepared.SourceTipID {
					t.Fatal("pre-input branch not restored")
				}
			} else {
				messages, _ := store.Messages()
				if store.ActiveBranchID() == prepared.SourceBranchID || len(messages) < 1 || messages[0].Content[0].Text != prepared.Text || (kind != "cancellation" && len(messages) != 1) {
					t.Fatalf("admitted original input was lost: branch=%s messages=%+v err=%v", store.ActiveBranchID(), messages, err)
				}
				if kind != "cancellation" && !errors.Is(err, ErrMessageEditOutcomeUnknown) {
					t.Fatal("ambiguous regeneration classified as rejection")
				}
			}
		})
	}
}

type regenerateFailProvider struct {
	provider.Provider
	called   bool
	messages []protocol.Message
}

func (p *regenerateFailProvider) Chat(_ context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.called = true
	p.messages = req.Messages
	return nil, errors.New("injected provider failure")
}
func TestMessageRegenerateProviderFailureRetainsOriginalInputAndExactPrefix(t *testing.T) {
	a := messageEditTestApp(t, false)
	seedMessageEditTurns(t, a, 3)
	prepared := prepareRegenerate(t, a, "assistant-1")
	fail := &regenerateFailProvider{Provider: a.Provider}
	if err := a.Agent.SetProvider(fail); err != nil {
		t.Fatal(err)
	}
	acknowledged := false
	err := a.CommitMessageRegenerate(t.Context(), protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
		if fail.called {
			t.Fatal("provider outran ACK")
		}
		acknowledged = true
		return nil
	})
	if err == nil || !acknowledged || !fail.called || a.Session.(session.ActiveBranchStore).ActiveBranchID() == prepared.SourceBranchID {
		t.Fatalf("provider failure rolled back regeneration: %v", err)
	}
	if len(fail.messages) != 3 || fail.messages[0].Content[0].Text != "original 0" || fail.messages[2].Content[0].Text != "original 1" || fail.messages[2].ID == prepared.EntryID {
		t.Fatalf("wrong regeneration provider prefix: %+v", fail.messages)
	}
}

func TestMessageRegeneratePluginVetoBeforeMutation(t *testing.T) {
	for _, script := range []string{`snow.registerHook("before_session_change",()=>({block:"no regenerate"}));`, `snow.registerHook("before_prompt",()=>({text:"changed by plugin"}));`} {
		t.Run(script, func(t *testing.T) {
			a := lifecycleApp(t, script, []string{"hooks"})
			seedMessageEditTurns(t, a, 1)
			prepared := prepareRegenerate(t, a, "assistant-0")
			err := a.CommitMessageRegenerate(t.Context(), protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
				t.Fatal("plugin veto admitted")
				return nil
			})
			branches, _ := a.Session.(session.BranchStore).Branches()
			if err == nil || errors.Is(err, ErrMessageEditOutcomeUnknown) || len(branches) != 1 || a.Session.BranchTip() != prepared.SourceTipID {
				t.Fatalf("plugin veto mutated regeneration: %v", err)
			}
		})
	}
}
