package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type queueFaultStore struct {
	*session.MemoryStore
	mode string
}

func (s *queueFaultStore) AppendBatch(entries []session.Entry) error {
	if s.mode == "committed" {
		if err := s.MemoryStore.AppendBatch(entries); err != nil {
			return err
		}
	}
	return errors.New("injected append outcome")
}
func (s *queueFaultStore) MessageEditEntryExists(ctx context.Context, id string) (bool, error) {
	if s.mode == "unknown" {
		return false, errors.New("probe unavailable")
	}
	return s.MemoryStore.MessageEditEntryExists(ctx, id)
}
func TestQueueControlAmbiguousAppendNeverPromisesUnsent(t *testing.T) {
	for _, mode := range []string{"absent", "committed", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			p := newQueuedProvider(replyEvents("first reply"))
			a, st := setup(t, p, nil, permission.ModeDeny)
			a.opts.Session = &queueFaultStore{MemoryStore: st, mode: mode}
			done := make(chan error, 1)
			go func() { done <- a.Prompt(t.Context(), "initial") }()
			<-p.started
			controlEnqueue(t, a, "second")
			close(p.release)
			err := <-done
			if err == nil {
				t.Fatal("lost persistence failure")
			}
			q := controlSnapshot(t, a)
			if len(q.Items) != 0 || q.Accepting || p.calls != 1 {
				t.Fatalf("unsafe retry state=%+v calls=%d", q, p.calls)
			}
			switch mode {
			case "absent":
				if len(q.ReviewItems) != 1 || q.ReviewItems[0].State != "held" || errors.Is(err, ErrQueueUnknown) {
					t.Fatalf("confirmed absence=%+v err=%v", q, err)
				}
			case "unknown":
				if len(q.ReviewItems) != 1 || q.ReviewItems[0].State != "delivery_unknown" || !errors.Is(err, ErrQueueUnknown) {
					t.Fatalf("unresolved outcome=%+v err=%v", q, err)
				}
			case "committed":
				if len(q.ReviewItems) != 0 || !errors.Is(err, ErrQueueUnknown) {
					t.Fatalf("committed work labeled unsent: %+v err=%v", q, err)
				}
				messages, _ := st.Messages()
				if len(messages) != 3 || messages[2].Content[0].Text != "second" {
					t.Fatalf("committed input lost: %+v", messages)
				}
			}
		})
	}
}

type queueReentrantHooks struct {
	a      *Agent
	t      *testing.T
	called bool
}

func (h *queueReentrantHooks) HasHook(phase string, _ bool) bool { return phase == "before_prompt" }
func (h *queueReentrantHooks) RunHooks(_ context.Context, req plugin.HookRequest) (plugin.HookRequest, []protocol.PluginTransform, error) {
	if req.Text == "second" {
		h.called = true
		q := controlSnapshot(h.t, h.a)
		if q.Items[0].State != "delivering" {
			h.t.Error("delivery not reserved")
		}
		_, err := h.a.MutateQueueControl("queue_update", protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.Items[0].ID, Text: "replace during delivery"})
		if !errors.Is(err, ErrQueueRejected) {
			h.t.Errorf("reentrant mutation=%v", err)
		}
	}
	return req, nil, nil
}
func TestQueueControlPluginCanReadAndRejectMutationDuringDelivery(t *testing.T) {
	p := newQueuedProvider(replyEvents("reply"))
	a, _ := setup(t, p, nil, permission.ModeDeny)
	hooks := &queueReentrantHooks{a: a, t: t}
	a.opts.PluginHooks = hooks
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	controlEnqueue(t, a, "second")
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !hooks.called {
		t.Fatal("queued hook not exercised")
	}
}
