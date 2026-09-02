package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	providerfake "github.com/elmissouri16/snow-core/internal/provider/fake"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestAppPlanModeAndModeTools(t *testing.T) {
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), CollaborationMode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Agent.Mode() != protocol.ModePlan {
		t.Fatalf("mode = %q", a.Agent.Mode())
	}
	for _, name := range []string{"ask_user", "request_user_input", "update_plan"} {
		if _, ok := a.Registry.Get(name); !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
}

type blockingChildProvider struct {
	*providerfake.Provider
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingChildProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
		return p.Provider.Chat(ctx, req)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAppPlanTransitionRejectsActiveUnsafeChild(t *testing.T) {
	enabled := true
	a, err := New(t.Context(), Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow", CWD: t.TempDir(), Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	blocking := &blockingChildProvider{Provider: providerfake.New(nil), started: make(chan struct{}), release: make(chan struct{})}
	a.runtimeSelection.mu.Lock()
	a.runtimeSelection.providers["fake"] = blocking
	a.runtimeSelection.mu.Unlock()
	if _, err := a.SpawnSubagent(t.Context(), protocol.SpawnSubagentRequest{Name: "worker", Task: "inspect", Role: "general", ForkTurns: "none"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("child provider did not start")
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err == nil || !strings.Contains(err.Error(), "mutation-capable child work is active") {
		t.Fatalf("direct Plan transition error=%v", err)
	}
	if a.Agent.Mode() != protocol.ModeDefault {
		t.Fatalf("rejected direct transition changed mode to %s", a.Agent.Mode())
	}
	if err := a.Agent.PromptWithMode(t.Context(), "plan atomically", protocol.ModePlan); err == nil || !strings.Contains(err.Error(), "mutation-capable child work is active") {
		t.Fatalf("atomic Plan transition error=%v", err)
	}
	if a.Agent.Mode() != protocol.ModeDefault {
		t.Fatalf("rejected atomic transition changed mode to %s", a.Agent.Mode())
	}
	close(blocking.release)
	if err := a.Subagents.WaitAll(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type modeSnapshotPlugin struct{ got chan publicplugin.Event }

func (p *modeSnapshotPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "mode-snapshot", Name: "mode snapshot", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (p *modeSnapshotPlugin) Register(_ context.Context, registrar publicplugin.Registrar) error {
	registrar.Subscribe(publicplugin.EventModeChanged, func(event publicplugin.Event) { p.got <- event })
	return nil
}
func (*modeSnapshotPlugin) Close(context.Context) error { return nil }

func TestPluginReceivesInitialModeSnapshot(t *testing.T) {
	plugin := &modeSnapshotPlugin{got: make(chan publicplugin.Event, 1)}
	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), CollaborationMode: "plan", GoPlugins: []publicplugin.Plugin{plugin}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	select {
	case event := <-plugin.got:
		if event.Payload.Mode == nil || event.Payload.Mode.Mode != protocol.ModePlan {
			t.Fatalf("event = %+v", event)
		}
	default:
		t.Fatal("plugin did not receive initial mode snapshot")
	}
}

func TestAppResumesPersistedPlanMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	a, err := New(context.Background(), Options{Provider: "fake", SessionPath: path, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.Prompt(context.Background(), "remember mode"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := New(context.Background(), Options{Provider: "fake", SessionPath: path, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.Agent.Mode() != protocol.ModePlan {
		t.Fatalf("resumed mode = %q", resumed.Agent.Mode())
	}
}
