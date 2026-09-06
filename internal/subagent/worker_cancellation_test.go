package subagent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type cancellationChild struct {
	mockChild
	check func()
	calls atomic.Int32
}

func (c *cancellationChild) PendingMailbox() bool {
	c.check()
	return true
}
func (c *cancellationChild) RunMailbox(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.calls.Add(1)
	return nil
}

func TestInterruptedTaskSettlesAtEveryWorkerBoundary(t *testing.T) {
	for _, phase := range []string{"before-dequeue", "before-slot", "after-slot"} {
		t.Run(phase, func(t *testing.T) {
			st := session.NewMemoryStore(session.Options{})
			root := rootAgent(t, st)
			defer root.Close()
			m := New(t.Context(), Limits{MaxConcurrentThreads: 1, TaskTimeout: time.Second})
			checked, proceed := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(proceed) })
			child := &cancellationChild{check: func() {
				close(checked)
				<-proceed
			}}
			var interruptOnce sync.Once
			publish := func(ev protocol.AgentEvent) {
				if phase == "after-slot" && ev.Subagent != nil && ev.Subagent.Status == protocol.AgentRunning {
					interruptOnce.Do(func() {
						if _, err := m.Interrupt(t.Context(), m.RootCaller(), "queued"); err != nil {
							t.Error(err)
						}
					})
				}
			}
			if err := m.Bind(root, ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) { return child, nil }), publish, st); err != nil {
				t.Fatal(err)
			}
			defer func() { release(); _ = m.Close(t.Context()) }()
			if err := m.Ready(t.Context()); err != nil {
				t.Fatal(err)
			}
			// Seed one accepted queued task so every cancellation boundary can
			// be scheduled deterministically without stack inspection or sleeps.
			state := protocol.SubagentState{Agent: protocol.AgentRef{
				ThreadID: "queued", Path: "/root/queued", ParentPath: "/root",
				ParentThreadID: m.RootCaller().ThreadID, Role: "general", Depth: 1,
			}, Status: protocol.AgentQueued, Generation: 1}
			r := &runtime{state: state, record: session.SubagentRecord{State: state}, child: child,
				tasks: make(chan childTask, 64), pendingTasks: 1, followupQueued: true,
				workerStarted: true, workerStop: make(chan struct{}), workerDone: make(chan struct{})}
			r.tasks <- childTask{onlyIfPending: true, followup: true}
			m.mu.Lock()
			m.byID[state.Agent.ThreadID], m.byPath[state.Agent.Path] = r, r
			m.order = append(m.order, state.Agent.ThreadID)
			m.mu.Unlock()
			if phase == "before-dequeue" {
				if _, err := m.Interrupt(t.Context(), m.RootCaller(), "queued"); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "before-slot" {
				m.slots <- struct{}{}
			}
			m.wg.Go(func() { m.worker(r, r.workerStop, r.workerDone) })
			if phase != "before-dequeue" {
				select {
				case <-checked:
				case <-time.After(time.Second):
					t.Fatal("worker did not reach mailbox check")
				}
				if phase == "before-slot" {
					if _, err := m.Interrupt(t.Context(), m.RootCaller(), "queued"); err != nil {
						t.Fatal(err)
					}
				}
				release()
				if phase == "before-slot" {
					<-m.slots
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := m.WaitAll(ctx); err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			settled := r.pendingTasks == 0 && r.cancel == nil && !r.interruptRequested
			r.mu.Unlock()
			if !settled || m.HasActive() || child.calls.Load() != 0 {
				t.Fatalf("interruption did not settle: settled=%v active=%v calls=%d", settled, m.HasActive(), child.calls.Load())
			}
			if _, err := m.CloseAgent(ctx, m.RootCaller(), "queued"); err != nil {
				t.Fatal(err)
			}
			if err := m.Followup(ctx, m.RootCaller(), "queued", "reuse after interruption"); err != nil {
				t.Fatal(err)
			}
			if err := m.WaitAll(ctx); err != nil {
				t.Fatal(err)
			}
			if child.calls.Load() != 1 {
				t.Fatalf("reused child calls=%d", child.calls.Load())
			}
			unlock := root.LockAdmission()
			err := m.SetStoreAdmitted(session.NewMemoryStore(session.Options{}))
			unlock()
			if err != nil {
				t.Fatalf("settled child blocked session switch: %v", err)
			}
		})
	}
}
