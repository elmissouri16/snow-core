package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type controlProvider struct {
	*fake.Provider
	calls atomic.Int32
}

func (p *controlProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if p.calls.Add(1) == 1 {
		return p.Provider.Chat(ctx, req)
	}
	return fake.New(nil).Chat(ctx, req)
}

// The gate schedules cancellation just before the real manager tool acquires
// root admission; it neither holds a lock nor ignores cancellation.
type cancellationGatedManagerTool struct {
	tool      tools.Tool
	entered   chan struct{}
	delegated chan struct{}
}

func (t *cancellationGatedManagerTool) Schema() protocol.ToolSchema { return t.tool.Schema() }
func (t *cancellationGatedManagerTool) Run(ctx context.Context, raw json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	close(t.entered)
	<-ctx.Done()
	close(t.delegated)
	return t.tool.Run(ctx, raw, host)
}
func cancellationControlFixture(t *testing.T) (*agent.Agent, *Manager, *cancellationGatedManagerTool) {
	t.Helper()
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "cancellation.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c, err := goalpkg.New(st, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(c) {
		if err := reg.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	p := &controlProvider{Provider: fake.New([]fake.Step{{Kind: fake.StepToolCall, ToolCallID: "list", ToolName: "list_agents", Arguments: []byte(`{}`)}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}})}
	root, err := agent.New(agent.Options{Provider: p, Registry: reg, Session: st, Goal: c, Permission: permission.NewService(permission.ModeAllow, nil), Model: protocol.Model{Provider: "fake", ID: "fake-1", SupportsTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	m := New(t.Context(), Limits{})
	if err := m.Bind(root, ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) { return nil, errors.New("no child required") }), root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	var actual tools.Tool
	for _, tool := range Tools(m, m.RootCaller()) {
		if tool.Schema().Name == "list_agents" {
			actual = tool
		}
	}
	gate := &cancellationGatedManagerTool{tool: actual, entered: make(chan struct{}), delegated: make(chan struct{})}
	if err := reg.Register(gate); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create("inspect agent status", nil, false); err != nil {
		t.Fatal(err)
	}
	root.ContinueGoal()
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	return root, m, gate
}
func TestControlsCancelManagerAdmission(t *testing.T) {
	for _, operation := range []string{"plan", "compact", "branch", "fork", "prompt"} {
		t.Run(operation, func(t *testing.T) {
			root, m, _ := cancellationControlFixture(t)
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "plan":
					err = root.SetMode(protocol.ModePlan)
				case "compact":
					_, err = root.Compact(t.Context())
				case "branch":
					err = root.SelectBranch("main")
				case "fork":
					_, err = root.Fork("")
				case "prompt":
					err = root.Prompt(t.Context(), "replacement")
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("%s: %v", operation, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("control and manager tool deadlocked")
			}
			if operation == "prompt" {
				_ = root.StopGoal(t.Context(), true)
			}
			root.Close()
			if err := m.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
