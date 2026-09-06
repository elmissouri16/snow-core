package subagent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type activityChild struct {
	mockChild
	started   chan struct{}
	release   chan struct{}
	pending   atomic.Bool
	followups atomic.Int32
	check     func()
}

func (c *activityChild) Prompt(ctx context.Context, _ string) error {
	c.running.Store(true)
	defer c.running.Store(false)
	close(c.started)
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *activityChild) EnqueueMailbox(m protocol.AgentMessage) error {
	c.pending.Store(true)
	return c.mockChild.EnqueueMailbox(m)
}
func (c *activityChild) PendingMailbox() bool {
	if c.check != nil {
		c.check()
	}
	return c.pending.Load()
}
func (c *activityChild) RunMailbox(context.Context) error {
	c.pending.Store(false)
	c.followups.Add(1)
	return nil
}

func TestWaitIncludesAcceptedFollowup(t *testing.T) {
	for _, stage := range []string{"queued", "dequeued", "consumed"} {
		t.Run(stage, func(t *testing.T) { testAcceptedFollowup(t, stage) })
	}
}

func testAcceptedFollowup(t *testing.T, stage string) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	m := New(t.Context(), Limits{MaxConcurrentThreads: 1, TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: time.Millisecond, MaxWait: time.Second})
	child := &activityChild{started: make(chan struct{}), release: make(chan struct{})}
	checked, proceed := make(chan struct{}), make(chan struct{})
	releaseCheck := sync.OnceFunc(func() { close(proceed) })
	if stage == "dequeued" {
		child.check = func() { close(checked); <-proceed }
	}
	terminal := make(chan struct{})
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	publish := func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvSubagentStatus && ev.Subagent != nil && ev.Subagent.Status == protocol.AgentCompleted && child.followups.Load() == 0 {
			close(terminal)
			<-unblock
		}
	}
	if err := m.Bind(root, ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) { return child, nil }), publish, st); err != nil {
		t.Fatal(err)
	}
	defer func() { release(); releaseCheck(); _ = m.Close(t.Context()) }()
	if err := m.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Spawn(t.Context(), m.RootCaller(), protocol.SpawnSubagentRequest{Name: "worker", Task: "initial", ForkTurns: "none"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.started:
	case <-time.After(time.Second):
		t.Fatal("child did not start")
	}
	if err := m.Followup(t.Context(), m.RootCaller(), "worker", "accepted followup"); err != nil {
		t.Fatal(err)
	}
	if stage == "consumed" {
		child.pending.Store(false)
	}
	close(child.release)
	select {
	case <-terminal:
	case <-time.After(time.Second):
		t.Fatal("no terminal event")
	}
	if stage == "dequeued" {
		m.slots <- struct{}{}
		release()
		select {
		case <-checked:
		case <-time.After(time.Second):
			t.Fatal("followup not dequeued")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := m.WaitAll(ctx)
	result, resultErr := m.WaitUntilAll(t.Context(), m.RootCaller(), time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || resultErr != nil || result.AllTerminal || !result.TimedOut || result.Queued != 1 {
		t.Fatalf("premature completion: WaitAll=%v result=%+v err=%v", err, result, resultErr)
	}
	release()
	releaseCheck()
	if stage == "dequeued" {
		<-m.slots
	}
	completed, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := m.WaitAll(completed); err != nil {
		t.Fatal(err)
	}
	want := int32(1)
	if stage == "consumed" {
		want = 0
	}
	if child.followups.Load() != want || m.HasActive() {
		t.Fatalf("followup not settled: calls=%d active=%v", child.followups.Load(), m.HasActive())
	}
}
