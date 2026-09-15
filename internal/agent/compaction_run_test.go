package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func seedCompactionRun(t *testing.T, st session.Store) {
	t.Helper()
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("compact-%d", i), st.BranchTip(), fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, ParentID: msg.ParentID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompactionRunCancelBeforeReleaseAndCapturedCompletion(t *testing.T) {
	p := &scriptedProvider{}
	a, st := setup(t, p, nil, permission.ModeDeny)
	seedCompactionRun(t, st)
	before := st.BranchTip()
	h, err := a.StartCompaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if h.ID() == "" || h.Turn() != a.ActiveTurnSnapshot() || h.SessionID() != st.ID() || !a.ActiveTurnSnapshot().Running {
		t.Fatal("missing captured admission")
	}
	if _, ready := h.Completion(); ready {
		t.Fatal("completion before release")
	}
	if len(p.requests) != 0 || st.BranchTip() != before {
		t.Fatal("work before ACK")
	}
	if _, err := a.StartCompaction(t.Context()); err == nil {
		t.Fatal("parallel compaction admitted")
	}
	h.Cancel()
	if err := h.Wait(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	h.Release() // A late ACK cannot revive cancellation.
	h.Release()
	completion, ready := h.Completion()
	if !ready || completion.Status != "canceled" || len(p.requests) != 0 || st.BranchTip() != before {
		t.Fatalf("completion=%+v", completion)
	}
	next, err := a.StartCompaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if next.ID() == h.ID() || next.Done() == h.Done() || next.Turn().Sequence <= h.Turn().Sequence {
		t.Fatal("identity/waiter replaced")
	}
	if err := h.Wait(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	again, _ := h.Completion()
	if again != completion {
		t.Fatal("later admission mutated captured completion")
	}
	next.Cancel()
	_ = next.Wait(t.Context())
}

func TestCompactionRunReleaseThenStop(t *testing.T) {
	p := &blockingSummaryProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, st := setup(t, p, nil, permission.ModeDeny)
	seedCompactionRun(t, st)
	h, err := a.StartCompaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.started:
		t.Fatal("provider started before release")
	default:
	}
	h.Release()
	select {
	case <-p.started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not started")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := a.AbortContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if a.ActiveTurnSnapshot().Running {
		t.Fatal("Stop left owner active")
	}
}

func TestCompactionRunRejectsActiveControlsWithoutStopping(t *testing.T) {
	p := &scriptedProvider{}
	a, st := setup(t, p, nil, permission.ModeDeny)
	h, err := a.StartCompaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Cancel(); _ = h.Wait(context.Background()) })
	for name, control := range map[string]func() error{
		"prompt":    func() error { return a.Prompt(t.Context(), "reject") },
		"mode":      func() error { return a.SetMode(protocol.ModePlan) },
		"model":     func() error { return a.SetModel(protocol.Model{Provider: p.ID(), ID: "other"}) },
		"session":   func() error { return a.SetSession(st) },
		"compact":   func() error { _, _, err := a.CompactWithTurn(t.Context()); return err },
		"steer":     func() error { return a.Steer("reject") },
		"follow_up": func() error { return a.FollowUp("reject") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := control(); err == nil {
				t.Fatal("accepted control during manual compaction")
			}
			select {
			case <-h.Done():
				t.Fatal("rejected control secretly stopped run")
			default:
			}
			if a.ActiveTurnSnapshot().ID != h.ID() || len(p.requests) != 0 {
				t.Fatal("rejected control changed owner")
			}
		})
	}
}

type compactionCleanupStore struct {
	*session.MemoryStore
	started, release chan struct{}
	once             sync.Once
}

func (s *compactionCleanupStore) AppendBatch(entries []session.Entry) error {
	for _, e := range entries {
		if e.Message != nil && e.Message.Role == protocol.RoleAgent {
			s.once.Do(func() { close(s.started) })
			<-s.release
			break
		}
	}
	return s.MemoryStore.AppendBatch(entries)
}

func TestCompactionRunDoneWaitsForFinalMailboxPersistence(t *testing.T) {
	p := &blockingSummaryProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, memory := setup(t, p, nil, permission.ModeDeny)
	store := &compactionCleanupStore{MemoryStore: memory, started: make(chan struct{}), release: make(chan struct{})}
	if err := a.SetSession(store); err != nil {
		t.Fatal(err)
	}
	seedCompactionRun(t, store)
	h, err := a.StartCompaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	h.Release()
	<-p.started
	if err := a.EnqueueMailbox(protocol.AgentMessage{ID: "final-mail", Author: protocol.AgentPath("/root/tester"), Recipient: protocol.RootAgentPath, Kind: protocol.AgentMessageNormal, Content: "pending mailbox", CreatedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	select {
	case <-store.started:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup not reached")
	}
	if !a.ActiveTurnSnapshot().Running {
		t.Fatal("released admission before cleanup")
	}
	select {
	case <-h.Done():
		t.Fatal("Done closed before cleanup")
	default:
	}
	if _, ok := h.Completion(); ok {
		t.Fatal("completion before cleanup")
	}
	if _, err := a.StartCompaction(t.Context()); err == nil {
		t.Fatal("admitted during cleanup")
	}
	close(store.release)
	if err := h.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	completion, ready := h.Completion()
	if !ready || (completion.Status != "completed" && completion.Status != "fallback") {
		t.Fatalf("completion=%+v", completion)
	}
	messages, err := memory.Messages()
	if err != nil || messages[len(messages)-1].Role != protocol.RoleAgent {
		t.Fatal("Done preceded persisted final mailbox")
	}
}

func TestCompactionRunBoundedResultClassifications(t *testing.T) {
	for _, fallback := range []string{"local", "error"} {
		t.Run(fallback, func(t *testing.T) {
			p := &scriptedProvider{resolveErr: errors.New("private provider detail must not reach public completion")}
			a, st := setup(t, p, nil, permission.ModeDeny)
			a.opts.Compaction.Fallback = fallback
			seedCompactionRun(t, st)
			h, err := a.StartCompaction(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			h.Release()
			err = h.Wait(t.Context())
			completion, ready := h.Completion()
			if !ready {
				t.Fatal("Wait preceded completion")
			}
			if fallback == "local" {
				if err != nil || completion.Status != "fallback" || !completion.UsedFallback || completion.SummarizedMessages == 0 {
					t.Fatalf("completion=%+v err=%v", completion, err)
				}
			} else if err == nil || completion.Status != "failed" {
				t.Fatalf("completion=%+v err=%v", completion, err)
			}
		})
	}
}

func TestCompactionRunRejectsPendingAndRecoveredWork(t *testing.T) {
	for _, state := range []string{"native", "review", "pending", "delivering", "automatic"} {
		t.Run(state, func(t *testing.T) {
			p := &scriptedProvider{}
			a, _ := setup(t, p, nil, permission.ModeDeny)
			a.mu.Lock()
			switch state {
			case "native":
				a.queuedInputs = []protocol.QueuedInput{{ID: "queued"}}
			case "review":
				a.queueControl.review = []protocol.QueueControlItem{{ID: "review"}}
			case "automatic":
				a.autoRunning = true
			default:
				a.queueControl.items = []protocol.QueueControlItem{{ID: "queued", State: state}}
			}
			a.mu.Unlock()
			if _, err := a.StartCompaction(t.Context()); err == nil {
				t.Fatal("accepted conflicting work")
			}
			a.mu.Lock()
			if state == "automatic" && (!a.autoRunning || a.autoStop) {
				t.Error("compaction stopped automatic owner")
			}
			a.autoRunning = false
			a.mu.Unlock()
			if len(p.requests) != 0 {
				t.Fatal("rejected run started provider")
			}
		})
	}
}
