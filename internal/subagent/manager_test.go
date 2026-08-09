package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider/fake"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type mockChild struct {
	mu          sync.Mutex
	messages    []protocol.Message
	running     atomic.Bool
	delay       time.Duration
	active, max *atomic.Int32
	cancel      context.CancelFunc
}

func (c *mockChild) turn(ctx context.Context, text string) error {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	c.running.Store(true)
	n := c.active.Add(1)
	for {
		old := c.max.Load()
		if n <= old || c.max.CompareAndSwap(old, n) {
			break
		}
	}
	defer func() { c.active.Add(-1); c.running.Store(false); cancel() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.delay):
	}
	c.mu.Lock()
	c.messages = append(c.messages, protocol.NewAssistantMessage("a"+text, "", "fake", "m", []protocol.ContentBlock{protocol.NewTextBlock("done " + text)}, protocol.StopStop, nil))
	c.mu.Unlock()
	return nil
}
func (c *mockChild) Prompt(ctx context.Context, s string) error {
	c.mu.Lock()
	c.messages = append(c.messages, protocol.NewUserMessage("u"+s, "", s))
	c.mu.Unlock()
	return c.turn(ctx, s)
}
func (c *mockChild) RunMailbox(ctx context.Context) error { return c.turn(ctx, "follow") }
func (c *mockChild) EnqueueMailbox(m protocol.AgentMessage) error {
	c.mu.Lock()
	c.messages = append(c.messages, protocol.NewAgentMessage(m.ID, "", m))
	c.mu.Unlock()
	return nil
}
func (c *mockChild) PendingMailbox() bool { return false }
func (c *mockChild) AbortContext(context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	deadline := time.Now().Add(time.Second)
	for c.running.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return nil
}
func (c *mockChild) IsRunning() bool { return c.running.Load() }
func (c *mockChild) Messages() ([]protocol.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]protocol.Message(nil), c.messages...), nil
}
func (c *mockChild) ContextMessages() ([]protocol.Message, error) { return c.Messages() }
func (c *mockChild) Usage() (protocol.Usage, error)               { return protocol.Usage{}, nil }
func (c *mockChild) Subscribe(func(protocol.AgentEvent)) func()   { return func() {} }
func (c *mockChild) Close()                                       {}

func rootAgent(t *testing.T, st session.Store) *agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Options{Provider: fake.NewRecorded(), Registry: tools.NewRegistry(), Session: st, Permission: permission.NewService(permission.ModeDeny, nil), Model: protocol.Model{Provider: "fake", ID: "m", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestResolveRoleAcceptsGeneralCompatibilityAlias(t *testing.T) {
	roles := map[string]Role{
		"default":  {Name: "default", Description: "general investigation"},
		"explorer": {Name: "explorer"},
		"worker":   {Name: "worker"},
	}
	for _, requested := range []string{"general", "general/default", "default/general", "DEFAULT"} {
		name, role, ok := resolveRole(roles, "default", requested)
		if !ok || name != "default" || role.Name != "default" {
			t.Fatalf("resolveRole(%q) = %q %+v %v, want canonical default", requested, name, role, ok)
		}
	}

	roles["general"] = Role{Name: "general", Description: "custom exact role"}
	name, role, ok := resolveRole(roles, "default", "general")
	if !ok || name != "general" || role.Description != "custom exact role" {
		t.Fatalf("exact configured general role did not override alias: %q %+v %v", name, role, ok)
	}

	err := availableRoleError(roles, "default", "missing")
	if err == nil || !strings.Contains(err.Error(), "available: default, explorer, general, worker") || !strings.Contains(err.Error(), `omit agent_type to use "default"`) {
		t.Fatalf("unknown role diagnostic = %v", err)
	}
}

func TestWaitUntilAllJoinsDescendantsAndExcludesCaller(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	var active, maxActive atomic.Int32
	m := New(context.Background(), Limits{MaxConcurrentThreads: 2, MaxAgentsPerSession: 8, MaxDepth: 2, Recursive: true, TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: 100 * time.Millisecond, MaxWait: time.Second, DefaultRole: "default", Roles: map[string]Role{"default": {Name: "default"}}})
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		return &mockChild{delay: 10 * time.Millisecond, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	parent, err := m.Spawn(context.Background(), m.RootCaller(), protocol.SpawnSubagentRequest{TaskName: "parent", Message: "parent", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := m.WaitUntilAll(context.Background(), m.RootCaller(), time.Second); err != nil || !result.AllTerminal || result.Terminal != 1 {
		t.Fatalf("root wait=%+v err=%v", result, err)
	}
	childCaller := Caller{ThreadID: parent.Agent.ThreadID, Path: parent.Agent.Path}
	if _, err := m.Spawn(context.Background(), childCaller, protocol.SpawnSubagentRequest{TaskName: "grandchild", Message: "grandchild", ForkTurns: "none"}); err != nil {
		t.Fatal(err)
	}
	result, err := m.WaitUntilAll(context.Background(), childCaller, time.Second)
	if err != nil || !result.AllTerminal || result.Running != 0 || result.Queued != 0 || result.Terminal != 1 {
		t.Fatalf("child wait=%+v err=%v", result, err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerExecutionSlotsAndStableList(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	var active, maxActive atomic.Int32
	m := New(context.Background(), Limits{MaxConcurrentThreads: 2, MaxAgentsPerSession: 8, MaxDepth: 1, TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: time.Millisecond, MaxWait: time.Second, DefaultRole: "default", Roles: map[string]Role{"default": {Name: "default"}}})
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		return &mockChild{delay: 20 * time.Millisecond, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	caller := m.RootCaller()
	for _, name := range []string{"one", "two", "three"} {
		if _, err := m.Spawn(context.Background(), caller, protocol.SpawnSubagentRequest{TaskName: name, Message: name, ForkTurns: "none"}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for m.HasActive() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.HasActive() {
		t.Fatal("children did not finish")
	}
	if got := maxActive.Load(); got != 2 {
		t.Fatalf("max child concurrency=%d, want 2", got)
	}
	list, err := m.List(context.Background(), caller, "")
	if err != nil {
		t.Fatal(err)
	}
	if list.ConcurrentLimit != 2 || list.Running != 0 || list.Queued != 0 || list.Terminal != 3 || list.AgentLimit != 8 {
		t.Fatalf("list summary=%+v", list)
	}
	want := []protocol.AgentPath{"/root", "/root/one", "/root/two", "/root/three"}
	for i, p := range want {
		if list.Agents[i].Agent.Path != p {
			t.Fatalf("order=%+v", list.Agents)
		}
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerSessionSwitchRejectsActiveAndRebindsIdle(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	var active, maxActive atomic.Int32
	m := New(context.Background(), Limits{MaxConcurrentThreads: 2, MaxAgentsPerSession: 4, MaxDepth: 1, TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: time.Millisecond, MaxWait: time.Second, DefaultRole: "default", Roles: map[string]Role{"default": {Name: "default"}}})
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		return &mockChild{delay: time.Second, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	caller := m.RootCaller()
	state, err := m.Spawn(context.Background(), caller, protocol.SpawnSubagentRequest{TaskName: "active", Message: "work", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	awaitToolState(t, m, string(state.Agent.Path), protocol.AgentRunning)
	next := session.NewMemoryStore(session.Options{})
	if err := m.SetStore(next); err == nil {
		t.Fatal("active child did not block session switch")
	}
	if _, err := m.Interrupt(context.Background(), caller, string(state.Agent.Path)); err != nil {
		t.Fatal(err)
	}
	awaitToolState(t, m, string(state.Agent.Path), protocol.AgentInterrupted)
	if err := m.SetStore(next); err != nil {
		t.Fatalf("idle child prevented session switch: %v", err)
	}
	if m.HasAgents() {
		t.Fatal("old child tree remained attached")
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerReservationRollbackAndInterruptReuse(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	defer root.Close()
	var active, maxActive atomic.Int32
	fails := atomic.Bool{}
	fails.Store(true)
	m := New(context.Background(), Limits{MaxConcurrentThreads: 2, MaxAgentsPerSession: 4, MaxDepth: 1, TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: time.Millisecond, MaxWait: time.Second, DefaultRole: "default", Roles: map[string]Role{"default": {Name: "default"}}})
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		if fails.Swap(false) {
			return nil, errors.New("boom")
		}
		return &mockChild{delay: time.Second, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	_ = m.Ready(context.Background())
	caller := m.RootCaller()
	req := protocol.SpawnSubagentRequest{TaskName: "retry", Message: "work", ForkTurns: "none"}
	if _, err := m.Spawn(context.Background(), caller, req); err == nil {
		t.Fatal("factory failure hidden")
	}
	if _, err := m.Spawn(context.Background(), caller, req); err != nil {
		t.Fatalf("reservation leaked: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		state, _ := m.Get(context.Background(), "retry")
		if state.Status == protocol.AgentRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not running")
		}
		time.Sleep(time.Millisecond)
	}
	previous, err := m.Interrupt(context.Background(), caller, "retry")
	if err != nil || previous != protocol.AgentRunning {
		t.Fatalf("interrupt=%s %v", previous, err)
	}
	if err := m.Followup(context.Background(), caller, "retry", "again"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
