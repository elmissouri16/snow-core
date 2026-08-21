package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/tools"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type testGoPlugin struct {
	id         string
	registered atomic.Int32
	closed     atomic.Int32
}

func (p *testGoPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: p.id, Name: "Test", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (p *testGoPlugin) Register(_ context.Context, r publicplugin.Registrar) error {
	p.registered.Add(1)
	return r.RegisterTool(publicplugin.ToolDefinition{
		Name: "echo", Description: "echo", Parameters: json.RawMessage(`{"type":"object"}`), Risk: "read",
		Executor: func(ctx context.Context, tc publicplugin.ToolContext, args json.RawMessage) (publicplugin.ToolResult, error) {
			if tc.Context != ctx || tc.CWD == "" {
				panic("missing tool context")
			}
			return publicplugin.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(string(args))}}, nil
		},
	})
}
func (p *testGoPlugin) Close(context.Context) error { p.closed.Add(1); return nil }

type blockingPlugin struct {
	started chan struct{}
	release chan struct{}
	closed  atomic.Int32
}

func (p *blockingPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "blocking", Name: "Blocking", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (p *blockingPlugin) Register(_ context.Context, registrar publicplugin.Registrar) error {
	close(p.started)
	<-p.release
	return registrar.RegisterTool(publicplugin.ToolDefinition{
		Name: "ready", Description: "ready", Parameters: json.RawMessage(`{"type":"object"}`), Risk: "read",
		Executor: func(context.Context, publicplugin.ToolContext, json.RawMessage) (publicplugin.ToolResult, error) {
			return publicplugin.ToolResult{}, nil
		},
	})
}
func (p *blockingPlugin) Close(context.Context) error { p.closed.Add(1); return nil }

type failingClosePlugin struct{}

func (failingClosePlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "failing-close", Name: "Failing close", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (failingClosePlugin) Register(context.Context, publicplugin.Registrar) error {
	return errors.New("register sentinel")
}
func (failingClosePlugin) Close(context.Context) error { return errors.New("close sentinel") }

func TestManagerInitializationJoinsRollbackCloseError(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	if err := manager.LoadGo(failingClosePlugin{}); err != nil {
		t.Fatal(err)
	}
	err := manager.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "register sentinel") || !strings.Contains(err.Error(), "close sentinel") {
		t.Fatalf("Initialize error = %v, want registration and rollback-close errors", err)
	}
	if second := manager.Initialize(context.Background()); second == nil || second.Error() != err.Error() {
		t.Fatalf("second Initialize error = %v, want stable %v", second, err)
	}
}

func TestManagerCloseWaitsForInitialization(t *testing.T) {
	registry := tools.NewRegistry()
	manager := NewManager(registry)
	plugin := &blockingPlugin{started: make(chan struct{}), release: make(chan struct{})}
	if err := manager.LoadGo(plugin); err != nil {
		t.Fatal(err)
	}
	initialized := make(chan error, 1)
	go func() { initialized <- manager.Initialize(context.Background()) }()
	select {
	case <-plugin.started:
	case <-time.After(2 * time.Second):
		t.Fatal("plugin initialization did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- manager.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("close returned during initialization: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(plugin.release)
	if err := <-initialized; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if plugin.closed.Load() != 1 {
		t.Fatalf("close count = %d", plugin.closed.Load())
	}
	if _, ok := registry.Get("plugin_blocking_ready"); ok {
		t.Fatal("tool remained registered after close")
	}
}

func TestManagerEmitWithoutObserversDoesNotClone(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	ev := protocol.AgentEvent{Type: protocol.EvToolEnd, ToolOutput: strings.Repeat("x", 64<<10)}
	if allocs := testing.AllocsPerRun(100, func() { manager.Emit(ev) }); allocs != 0 {
		t.Fatalf("Emit without observers allocated %.1f times per event", allocs)
	}
}

func TestManagerCloseDoesNotWaitForInFlightEmit(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	manager.Subscribe(publicplugin.EventToolEnd, func(publicplugin.Event) {
		calls.Add(1)
		close(started)
		<-release
	})
	emitted := make(chan struct{})
	go func() {
		manager.Emit(protocol.AgentEvent{Type: protocol.EvToolEnd})
		close(emitted)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("event handler did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- manager.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close waited for a best-effort event observer")
	}
	close(release)
	select {
	case <-emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("event emit did not finish")
	}
	manager.Emit(protocol.AgentEvent{Type: protocol.EvToolEnd})
	if calls.Load() != 1 {
		t.Fatalf("post-close handler calls = %d", calls.Load())
	}
}

func TestManagerObserverCanCloseReentrantly(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	manager.Subscribe(publicplugin.EventToolEnd, func(publicplugin.Event) {
		closed <- manager.Close(context.Background())
	})
	go manager.Emit(protocol.AgentEvent{Type: protocol.EvToolEnd})
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reentrant close deadlocked")
	}
}

func TestManagerAcceptsNilContexts(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg, ManagerOptions{CWD: t.TempDir(), SessionID: "session"})
	p := &testGoPlugin{id: "nil-context"}
	if err := m.LoadGo(p); err != nil {
		t.Fatal(err)
	}
	if err := m.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	d, ok := reg.Descriptor("plugin_nil-context_echo")
	if !ok {
		t.Fatal("missing nil-context plugin tool")
	}
	if _, err := d.Tool.Run(nil, json.RawMessage(`{"value":"ok"}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(nil); err != nil {
		t.Fatal(err)
	}
}

func TestManagerGoPluginLifecycleAndMetadata(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg, ManagerOptions{CWD: t.TempDir(), SessionID: "session"})
	p := &testGoPlugin{id: "demo"}
	if err := m.LoadGo(p); err != nil {
		t.Fatal(err)
	}
	if err := m.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	name := "plugin_demo_echo"
	d, ok := reg.Descriptor(name)
	if !ok {
		t.Fatalf("missing descriptor %q", name)
	}
	if d.Source != tools.SourceGoPlugin || d.Owner != "plugin:demo" || d.Risk != "read" || d.OriginalName != "echo" {
		t.Fatalf("descriptor = %+v", d)
	}
	if _, ok := reg.Get(name); !ok {
		t.Fatal("tool not registered")
	}
	var got publicplugin.Event
	m.Subscribe(publicplugin.EventToolStart, func(e publicplugin.Event) { got = e })
	m.Emit(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: name, Message: "running"})
	if got.Type != publicplugin.EventToolStart || got.Version != publicplugin.ProtocolVersion {
		t.Fatalf("event = %+v", got)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.closed.Load() != 1 {
		t.Fatalf("close count = %d", p.closed.Load())
	}
	if _, ok := reg.Get(name); ok {
		t.Fatal("tool remained after close")
	}
}

type deferredGoPlugin struct{}

func (deferredGoPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "mail", Name: "Mail", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (deferredGoPlugin) Register(_ context.Context, registrar publicplugin.Registrar) error {
	return registrar.RegisterTool(publicplugin.ToolDefinition{
		Name: "find", Description: "Find messages", Parameters: json.RawMessage(`{"type":"object"}`), Risk: "read",
		Discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Keywords: []string{"unread", "email"}},
		Executor: func(context.Context, publicplugin.ToolContext, json.RawMessage) (publicplugin.ToolResult, error) {
			return publicplugin.ToolResult{}, nil
		},
	})
}
func (deferredGoPlugin) Close(context.Context) error { return nil }

func TestManagerPropagatesDeferredDiscoveryMetadata(t *testing.T) {
	registry := tools.NewRegistry()
	manager := NewManager(registry)
	if err := manager.LoadGo(deferredGoPlugin{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	desc, ok := registry.Descriptor("plugin_mail_find")
	if !ok || !tools.IsDeferred(desc) {
		t.Fatalf("descriptor = %+v", desc)
	}
	if desc.Schema.Discovery.Namespace != "mail" || len(desc.Schema.Discovery.Keywords) != 2 {
		t.Fatalf("discovery = %+v", desc.Schema.Discovery)
	}
}

func TestManagerRollsBackFailedRegistration(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg, ManagerOptions{})
	p := &testGoPlugin{id: "bad"}
	bad := &duplicatePlugin{testGoPlugin: p}
	if err := m.LoadGo(bad); err != nil {
		t.Fatal(err)
	}
	firstErr := m.Initialize(context.Background())
	if firstErr == nil {
		t.Fatal("expected duplicate registration error")
	}
	secondErr := m.Initialize(context.Background())
	if secondErr == nil || secondErr.Error() != firstErr.Error() {
		t.Fatalf("retry error = %v, want %v", secondErr, firstErr)
	}
	if p.registered.Load() != 1 {
		t.Fatalf("register count = %d, want 1", p.registered.Load())
	}
	if len(reg.Descriptors()) != 0 {
		t.Fatalf("descriptors after rollback = %d", len(reg.Descriptors()))
	}
	if p.closed.Load() != 1 {
		t.Fatalf("close count = %d", p.closed.Load())
	}
}

type duplicatePlugin struct{ *testGoPlugin }

func (p *duplicatePlugin) Register(ctx context.Context, r publicplugin.Registrar) error {
	if err := p.testGoPlugin.Register(ctx, r); err != nil {
		return err
	}
	return r.RegisterTool(publicplugin.ToolDefinition{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`), Executor: func(context.Context, publicplugin.ToolContext, json.RawMessage) (publicplugin.ToolResult, error) {
		return publicplugin.ToolResult{}, nil
	}})
}

func TestManagerExternalRiskDefaultsToExec(t *testing.T) {
	registry := tools.NewRegistry()
	manager := NewManager(registry)
	managed := &managedPlugin{id: "default-risk", owner: ownerFor("default-risk"), external: &ExternalHost{}}
	if err := manager.registerExternal(managed, ExternalInitResult{Tools: []publicplugin.ExternalToolDefinition{{
		Name: "inspect", Description: "inspect", Parameters: json.RawMessage(`{"type":"object"}`),
	}}}); err != nil {
		t.Fatal(err)
	}
	descriptor, ok := registry.Descriptor("plugin_default-risk_inspect")
	if !ok || descriptor.Risk != "exec" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
}

func TestManagerCancellationStillGracefullyClosesExternalPlugin(t *testing.T) {
	bin := buildV2Plugin(t)
	reg := tools.NewRegistry()
	m := NewManager(reg, ManagerOptions{CWD: t.TempDir()})
	if err := m.LoadExternal(publicplugin.PluginSpec{ID: "v2", Command: []string{bin}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	host := m.plugins[0].external
	if host == nil {
		t.Fatal("external host was not retained")
	}

	cancel()
	select {
	case <-host.waitDone:
		t.Fatal("canceling the app context killed the plugin before graceful shutdown")
	case <-time.After(250 * time.Millisecond):
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatalf("graceful close after app cancellation: %v", err)
	}
}

func TestManagerExternalRegistrationUsesSingleNamespace(t *testing.T) {
	bin := buildV2Plugin(t)
	reg := tools.NewRegistry()
	m := NewManager(reg, ManagerOptions{CWD: t.TempDir()})
	if err := m.LoadExternal(publicplugin.PluginSpec{ID: "v2", Command: []string{bin}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	descriptor, ok := reg.Descriptor("plugin_v2_echo")
	if !ok {
		t.Fatalf("missing namespaced tool: %v", reg.Schemas())
	}
	if descriptor.Risk != "read" || len(descriptor.Capabilities) != 2 || strings.Join(descriptor.Capabilities, ",") != "base,echo" {
		t.Fatalf("external metadata = %+v", descriptor)
	}
	if _, ok := reg.Get("plugin_v2_plugin_v2_echo"); ok {
		t.Fatal("tool was double-namespaced")
	}

	// The fixture subscribes only to tool_start. Unsupported delta events must
	// not cross the process boundary.
	m.Emit(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "skip"})
	m.Emit(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "plugin_v2_echo"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		result, err := descriptor.Tool.Run(context.Background(), json.RawMessage(`{}`), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Content) == 1 && result.Content[0].Text == "echo:1" {
			details, ok := result.Details.(json.RawMessage)
			if !ok || !strings.Contains(string(details), `"source":"v2"`) {
				t.Fatalf("details = %#v", result.Details)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("event count did not settle: %+v", result.Content)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
