package plugin

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/snow-core/snow/internal/tools"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
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
	if err := m.Initialize(context.Background()); err == nil {
		t.Fatal("expected duplicate registration error")
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
	if _, ok := reg.Get("plugin_v2_echo"); !ok {
		t.Fatalf("missing namespaced tool: %v", reg.Schemas())
	}
	if _, ok := reg.Get("plugin_v2_plugin_v2_echo"); ok {
		t.Fatal("tool was double-namespaced")
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
