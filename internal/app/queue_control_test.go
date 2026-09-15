package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type reviewQueueProvider struct {
	provider.Provider
	started chan struct{}
	once    sync.Once
}

func (p *reviewQueueProvider) Chat(ctx context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestQueueReviewRejectsLifecycleTransitionsBeforeMutation(t *testing.T) {
	a := messageEditTestApp(t, false)
	p := &reviewQueueProvider{started: make(chan struct{})}
	// Explicit provider methods used by the agent are limited to ID and Chat.
	p.Provider = a.Provider
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
	q, err = a.QueueEnqueue(protocol.RPCQueueEnqueueParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: "retained"})
	if err != nil {
		t.Fatal(err)
	}
	a.Agent.Abort()
	<-done
	q, err = a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
	if err != nil || len(q.ReviewItems) != 1 {
		t.Fatalf("review=%+v err=%v", q, err)
	}
	original, tip := a.Session, a.Session.BranchTip()
	branches, err := a.Agent.Branches()
	if err != nil {
		t.Fatal(err)
	}
	target := session.NewMemoryStore(session.Options{})
	defer target.Close()
	for name, operation := range map[string]func() error{
		"quiet switch": func() error {
			unlock := a.Agent.LockAdmission()
			defer unlock()
			return a.Agent.SetSessionQuietAdmitted(target)
		},
		"app switch":    func() error { return a.SetSession(target) },
		"new session":   func() error { _, err := a.CreateSession(); return err },
		"open session":  func() error { _, err := a.OpenSession("different"); return err },
		"select branch": func() error { return a.SelectBranch(branches[0].ID) },
		"fork branch":   func() error { _, err := a.ForkBranch(""); return err },
		"fork session":  func() error { _, err := a.ForkSession(t.Context(), protocol.SessionForkOptions{}); return err },
		"fork worktree": func() error { _, err := a.ForkWorktree(t.Context(), protocol.SessionWorktreeForkOptions{}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, agent.ErrQueueRejected) {
				t.Fatalf("transition should reject controlled review, got %v", err)
			}
			after, err := a.QueueList(protocol.RPCQueueListParams{SessionID: q.SessionID, TurnID: q.TurnID})
			if err != nil || !reflect.DeepEqual(q, after) || a.Session != original || a.Session.BranchTip() != tip {
				t.Fatalf("review/session mutated: %+v err=%v", after, err)
			}
			current, err := a.Agent.Branches()
			if err != nil || !reflect.DeepEqual(branches, current) {
				t.Fatal("branches mutated")
			}
		})
	}
	if _, err := a.QueueRemove(protocol.RPCQueueRemoveParams{SessionID: target.ID(), TurnID: q.TurnID, Revision: q.Revision, ItemID: q.ReviewItems[0].ID}); !errors.Is(err, agent.ErrQueueStale) {
		t.Fatalf("old review retargeted: %v", err)
	}
	if _, err := a.QueueRemove(protocol.RPCQueueRemoveParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.ReviewItems[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSession(target); err != nil {
		t.Fatalf("explicit discard did not release lifecycle: %v", err)
	}
}
