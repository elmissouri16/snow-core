package subagent

import (
	"context"
	"encoding/json"
	"io"
	goruntime "runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	"github.com/snow-core/snow/pkg/protocol"
)

type shellSequenceProvider struct {
	mu      sync.Mutex
	command string
	calls   int
}

func (p *shellSequenceProvider) ID() string { return "shell-test" }

func (p *shellSequenceProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return []protocol.Model{{Provider: p.ID(), ID: "shell-model", SupportsTools: true}}, nil
}

func (p *shellSequenceProvider) Resolve(_ context.Context, c auth.Credential) (auth.Credential, error) {
	return c, nil
}

func (p *shellSequenceProvider) Chat(ctx context.Context, _ auth.Credential, _ protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	command := p.command
	p.mu.Unlock()
	if call > 0 {
		return &shellEventStream{ctx: ctx, events: []protocol.StreamEvent{
			{Type: protocol.EvStreamTextDelta, Text: "shell complete"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		}}, nil
	}
	args, _ := json.Marshal(map[string]any{"command": command})
	return &shellEventStream{ctx: ctx, events: []protocol.StreamEvent{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "shell-1", ToolName: "bash", Arguments: args},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}}, nil
}

type shellEventStream struct {
	ctx    context.Context
	events []protocol.StreamEvent
	index  int
}

func (s *shellEventStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if err := s.ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	if err := ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	if s.index >= len(s.events) {
		return protocol.StreamEvent{}, io.EOF
	}
	ev := s.events[s.index]
	s.index++
	return ev, nil
}

func (*shellEventStream) Close() error { return nil }

type trackingBash struct {
	inner  *builtin.Bash
	active *atomic.Int32
	max    *atomic.Int32
}

func (b *trackingBash) Schema() protocol.ToolSchema { return b.inner.Schema() }

func (b *trackingBash) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	n := b.active.Add(1)
	for {
		old := b.max.Load()
		if n <= old || b.max.CompareAndSwap(old, n) {
			break
		}
	}
	defer b.active.Add(-1)
	return b.inner.Run(ctx, args, host)
}

type shellHost struct {
	cwd  string
	perm permission.Service
}

func (h *shellHost) CWD() string                          { return h.cwd }
func (h *shellHost) Roots() []string                      { return []string{h.cwd} }
func (h *shellHost) Permission() permission.Service       { return h.perm }
func (h *shellHost) EmitProgress(tools.ToolProgressEvent) {}
func (h *shellHost) Environ() []string                    { return nil }

func shellCommand(long bool) string {
	if goruntime.GOOS == "windows" {
		seconds := 3
		if long {
			seconds = 20
		}
		return "ping -n " + strconv.Itoa(seconds+1) + " 127.0.0.1 >NUL && echo shell"
	}
	if long {
		return "sleep 5; printf shell"
	}
	return "sleep 0.15; printf shell"
}

func TestShellChildrenOverlapAndInterruptCleanup(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("process-group cleanup has different Windows semantics")
	}
	rootStore := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, rootStore)
	defer root.Close()

	rootPerm := permission.NewService(permission.ModeAllow, nil)
	var active, maxActive atomic.Int32
	cwd := t.TempDir()
	factory := ChildFactoryFunc(func(_ context.Context, spec ChildSpec) (ChildRuntime, error) {
		command := shellCommand(spec.State.Agent.Path == "/root/interrupt")
		reg := tools.NewRegistry()
		bash := builtin.NewBash()
		bash.Timeout = 10 * time.Second
		if err := reg.Register(&trackingBash{inner: bash, active: &active, max: &maxActive}); err != nil {
			return nil, err
		}
		store := session.NewMemoryStore(session.Options{CWD: cwd})
		prov := &shellSequenceProvider{command: command}
		child, err := agent.New(agent.Options{
			Provider: prov, Registry: reg, Session: store, Permission: rootPerm,
			ToolHost: &shellHost{cwd: cwd, perm: rootPerm},
			Model:    protocol.Model{Provider: prov.ID(), ID: "shell-model", SupportsTools: true},
			Identity: spec.State.Agent.Clone(),
		})
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		return child, nil
	})

	m := New(context.Background(), Limits{
		MaxConcurrentThreads: 3,
		MaxAgentsPerSession:  8,
		MaxDepth:             1,
		TaskTimeout:          time.Second,
		MinWait:              time.Millisecond,
		DefaultWait:          20 * time.Millisecond,
		MaxWait:              time.Second,
		DefaultRole:          "default",
		Roles:                map[string]Role{"default": {Name: "default"}},
	})
	defer m.Close(context.Background())
	if err := m.Bind(root, factory, root.Publish, rootStore); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}

	caller := m.RootCaller()
	for _, name := range []string{"one", "two"} {
		if _, err := m.Spawn(context.Background(), caller, protocol.SpawnSubagentRequest{TaskName: name, Message: "run shell", ForkTurns: "none"}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for maxActive.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if maxActive.Load() < 2 {
		t.Fatalf("shell children did not overlap; max active=%d", maxActive.Load())
	}
	awaitToolState(t, m, "one", protocol.AgentCompleted)
	awaitToolState(t, m, "two", protocol.AgentCompleted)
	if active.Load() != 0 {
		t.Fatalf("shell processes still active after completion: %d", active.Load())
	}

	if _, err := m.Spawn(context.Background(), caller, protocol.SpawnSubagentRequest{TaskName: "interrupt", Message: "run long shell", ForkTurns: "none"}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for active.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active.Load() == 0 {
		t.Fatal("interrupt child never started bash")
	}
	if _, err := m.Interrupt(context.Background(), caller, "interrupt"); err != nil {
		t.Fatal(err)
	}
	awaitToolState(t, m, "interrupt", protocol.AgentInterrupted)
	deadline = time.Now().Add(2 * time.Second)
	for active.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active.Load() != 0 {
		t.Fatalf("interrupt left process active: %d", active.Load())
	}
	if err := m.Followup(context.Background(), caller, "interrupt", "retry after interrupt"); err != nil {
		t.Fatal(err)
	}
	awaitToolState(t, m, "interrupt", protocol.AgentCompleted)
}

var _ provider.Provider = (*shellSequenceProvider)(nil)
