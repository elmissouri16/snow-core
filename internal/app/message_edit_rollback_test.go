package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type editFailureStore struct {
	*session.MemoryStore
	failUser, failRollback, failHistory, failMode bool
	commitThenFail, failProbe                     bool
}

func (s *editFailureStore) AppendWithInitialTitle(entry session.Entry, title string) error {
	if s.failUser {
		return errors.New("injected user persistence failure")
	}
	err := s.MemoryStore.AppendWithInitialTitle(entry, title)
	if err == nil && s.commitThenFail {
		return errors.New("injected failure after durable user commit")
	}
	return err
}
func (s *editFailureStore) SelectBranch(id string) error {
	if s.failRollback && id == "main" {
		return errors.New("injected branch restore failure")
	}
	return s.MemoryStore.SelectBranch(id)
}
func (s *editFailureStore) Messages() ([]protocol.Message, error) {
	if s.failHistory && s.ActiveBranchID() != "main" {
		return nil, errors.New("injected history failure")
	}
	return s.MemoryStore.Messages()
}
func (s *editFailureStore) CollaborationMode() (protocol.CollaborationMode, error) {
	if s.failMode && s.ActiveBranchID() != "main" {
		return "", errors.New("injected mode restore failure")
	}
	return s.MemoryStore.CollaborationMode()
}

func (s *editFailureStore) MessageEditEntryExists(ctx context.Context, id string) (bool, error) {
	if s.failProbe {
		return false, errors.New("probe unavailable")
	}
	return s.MemoryStore.MessageEditEntryExists(ctx, id)
}

func TestMessageEditPreTurnRollbackAndAmbiguousFailure(t *testing.T) {
	for _, kind := range []string{"user-append", "rollback", "history", "fork-rollback", "commit-then-error", "unavailable-probe"} {
		t.Run(kind, func(t *testing.T) {
			a := messageEditTestApp(t, false)
			store := &editFailureStore{MemoryStore: session.NewMemoryStore(session.Options{ID: "edit-failure"})}
			if err := a.SetSession(store); err != nil {
				t.Fatal(err)
			}
			seedMessageEditTurns(t, a, 1)
			prepared := prepareEdit(t, a, "turn-0")
			switch kind {
			case "user-append":
				store.failUser = true
			case "rollback":
				store.failUser = true
				store.failRollback = true
			case "history":
				store.failHistory = true
			case "commit-then-error":
				store.commitThenFail = true
			case "unavailable-probe":
				store.failUser = true
				store.failProbe = true
			case "fork-rollback":
				store.failMode = true
				store.failRollback = true
			}
			err := a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
				t.Fatal("injected failure acknowledged")
				return nil
			})
			if err == nil {
				t.Fatal("injected failure succeeded")
			}
			if kind == "user-append" {
				branches, _ := store.Branches()
				if errors.Is(err, ErrMessageEditOutcomeUnknown) || store.ActiveBranchID() != prepared.SourceBranchID || store.BranchTip() != prepared.SourceTipID || len(branches) != 1 {
					t.Fatalf("rollback incomplete: %v branches=%+v", err, branches)
				}
			} else if !errors.Is(err, ErrMessageEditOutcomeUnknown) {
				t.Fatalf("mutation failure presented as rejection: %v", err)
			}
			if kind == "commit-then-error" {
				messages, _ := store.Messages()
				if store.ActiveBranchID() == prepared.SourceBranchID || len(messages) != 1 || messages[0].Content[0].Text != "replacement" {
					t.Fatal("durable user was rolled back after append error")
				}
			}
			store.failHistory = false
			store.failMode = false
			store.failRollback = false
			store.failUser = false
			if a.Agent.IsRunning() {
				t.Fatal("admission leaked running state")
			}
		})
	}
}

type editSQLiteCommitFailure struct {
	*session.SQLiteStore
	fail bool
}

func (s *editSQLiteCommitFailure) AppendWithInitialTitle(entry session.Entry, title string) error {
	if err := s.SQLiteStore.AppendWithInitialTitle(entry, title); err != nil {
		return err
	}
	if s.fail {
		return errors.New("SQLite post-commit refresh failure")
	}
	return nil
}
func TestMessageEditSQLiteAppendCommitThenErrorRetainsDurableBranch(t *testing.T) {
	a := messageEditTestApp(t, false)
	db, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "edit.db"), a.CWD(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	store := &editSQLiteCommitFailure{SQLiteStore: db}
	if err := a.SetSession(store); err != nil {
		t.Fatal(err)
	}
	seedMessageEditTurns(t, a, 1)
	prepared := prepareEdit(t, a, "turn-0")
	store.fail = true
	err = a.CommitMessageEdit(t.Context(), protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: prepared.EditToken, Text: "replacement"}, func(protocol.RPCMessageEditCommitted, []protocol.Message) error {
		t.Fatal("ambiguous append acknowledged")
		return nil
	})
	if !errors.Is(err, ErrMessageEditOutcomeUnknown) {
		t.Fatalf("append failure classified as rejection: %v", err)
	}
	messages, err := store.Messages()
	if err != nil || len(messages) != 1 || messages[0].Content[0].Text != "replacement" || store.ActiveBranchID() == prepared.SourceBranchID {
		t.Fatalf("lost durable branch: %+v %v", messages, err)
	}
	reopened, err := session.OpenSQLiteStore(store.Path(), a.CWD(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	durable, err := reopened.Messages()
	if err != nil || len(durable) != 1 || durable[0].ID != messages[0].ID || reopened.ActiveBranchID() != store.ActiveBranchID() {
		t.Fatalf("lost reopened branch: %+v %v", durable, err)
	}
}
