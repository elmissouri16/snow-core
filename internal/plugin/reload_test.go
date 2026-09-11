package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/tools"
	public "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type reloadFixture struct {
	id, name      string
	calls, closed atomic.Int32
}

func (p *reloadFixture) Manifest() public.Manifest {
	return public.Manifest{ID: p.id, Name: p.id, Version: "1", ProtocolVersion: public.ProtocolVersion}
}
func (p *reloadFixture) Register(_ context.Context, r public.Registrar) error {
	r.Subscribe(public.EventSessionUpdated, func(public.Event) { p.calls.Add(1) })
	return r.RegisterTool(public.ToolDefinition{Name: p.name, Description: "test", Parameters: json.RawMessage(`{"type":"object"}`), Risk: "read", Executor: func(context.Context, public.ToolContext, json.RawMessage) (public.ToolResult, error) {
		return public.ToolResult{}, nil
	}})
}
func (p *reloadFixture) Close(context.Context) error { p.closed.Add(1); return nil }

func TestJavaScriptReplacementKeepsStableOwnerAndObservers(t *testing.T) {
	reg := tools.NewRegistry()
	manager := NewManager(reg)
	old, neighbor := &reloadFixture{id: "demo", name: "old"}, &reloadFixture{id: "neighbor", name: "keep"}
	if err := manager.LoadJavaScript(old, "first"); err != nil {
		t.Fatal(err)
	}
	if err := manager.LoadGo(neighbor); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	next := &reloadFixture{id: "demo", name: "new"}
	candidate, err := manager.PrepareJavaScript(t.Context(), "demo", next, "second")
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close(context.Background())
	manager.Emit(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if old.calls.Load() != 1 || neighbor.calls.Load() != 1 || next.calls.Load() != 0 {
		t.Fatal("candidate observed before commit")
	}
	if _, ok := reg.Descriptor("plugin_demo_new"); ok {
		t.Fatal("candidate tool published before commit")
	}
	rejected := errors.New("router rejected")
	if _, err := manager.CommitJavaScript(t.Context(), candidate, func([]tools.ToolDescriptor) error { return rejected }); !errors.Is(err, rejected) {
		t.Fatalf("prepare error=%v", err)
	}
	manager.Emit(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if old.calls.Load() != 2 || next.calls.Load() != 0 {
		t.Fatal("failed commit lost observer")
	}
	cleanup, err := manager.CommitJavaScript(t.Context(), candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	manager.Emit(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if old.calls.Load() != 2 || next.calls.Load() != 1 || neighbor.calls.Load() != 3 {
		t.Fatalf("old/new/neighbor=%d/%d/%d", old.calls.Load(), next.calls.Load(), neighbor.calls.Load())
	}
	if old.closed.Load() != 1 || next.closed.Load() != 0 || neighbor.closed.Load() != 0 {
		t.Fatal("wrong runtime retired")
	}
	if _, ok := reg.Descriptor("plugin_demo_new"); !ok {
		t.Fatal("cleanup unregistered replacement")
	}
	if _, ok := reg.Descriptor("plugin_demo_old"); ok {
		t.Fatal("old descriptor survived")
	}
	if _, ok := reg.Descriptor("plugin_neighbor_keep"); !ok {
		t.Fatal("neighbor lost")
	}
	if _, err := manager.CommitJavaScript(t.Context(), candidate, nil); err == nil {
		t.Fatal("candidate committed twice")
	}
}
func TestJavaScriptReplacementRejectsStaleAndGoOwner(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	if err := manager.LoadJavaScript(&reloadFixture{id: "demo", name: "tool"}, "first"); err != nil {
		t.Fatal(err)
	}
	if err := manager.LoadGo(&reloadFixture{id: "static", name: "tool"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	if _, err := manager.PrepareJavaScript(t.Context(), "static", &reloadFixture{id: "static", name: "new"}, "new"); err == nil {
		t.Fatal("Go owner reloaded")
	}
	first, err := manager.PrepareJavaScript(t.Context(), "demo", &reloadFixture{id: "demo", name: "first"}, "first")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	second, err := manager.PrepareJavaScript(t.Context(), "demo", &reloadFixture{id: "demo", name: "second"}, "second")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	cleanup, err := manager.CommitJavaScript(t.Context(), first, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = cleanup(t.Context())
	if _, err := manager.CommitJavaScript(t.Context(), second, nil); err == nil {
		t.Fatal("stale candidate replaced new generation")
	}
}

func (p *reloadFixture) FreezeForReload() (func(), error) { return func() {}, nil }

func TestJavaScriptReloadFreezeRejectsSnapshottedDelivery(t *testing.T) {
	manager := NewManager(tools.NewRegistry())
	if err := manager.LoadJavaScript(&reloadFixture{id: "demo", name: "old"}, "first"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(t.Context())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unsubscribe := manager.Subscribe(public.EventSessionUpdated, func(public.Event) { close(entered); <-release })
	go func() { manager.Emit(protocol.AgentEvent{Type: protocol.EvSessionUpdated}); close(done) }()
	<-entered
	resume, err := manager.FreezeJavaScriptDelivery()
	close(release)
	<-done
	unsubscribe()
	if err == nil {
		resume()
		t.Fatal("snapshotted callbacks crossed reload boundary")
	}
	resume, err = manager.FreezeJavaScriptDelivery()
	if err != nil {
		t.Fatal(err)
	}
	resume()
	resume() // All cleanup paths may safely release.
}

func TestJavaScriptReloadReadersSeeWholeCatalog(t *testing.T) {
	reg := tools.NewRegistry()
	manager := NewManager(reg)
	if err := manager.LoadJavaScript(&reloadFixture{id: "demo", name: "old"}, "first"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close(t.Context())
	stop, done := make(chan struct{}), make(chan struct{})
	var partial atomic.Bool
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			catalog := reg.Descriptors()
			if len(catalog) != 1 || catalog[0].PluginID != "demo" {
				partial.Store(true)
			}
		}
	}()
	defer func() {
		close(stop)
		<-done
		if partial.Load() {
			t.Error("reader observed partial replacement")
		}
	}()
	for i := range 30 {
		name := "old"
		if i%2 == 0 {
			name = "new"
		}
		candidate, err := manager.PrepareJavaScript(t.Context(), "demo", &reloadFixture{id: "demo", name: name}, name)
		if err != nil {
			t.Fatal(err)
		}
		cleanup, err := manager.CommitJavaScript(t.Context(), candidate, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := cleanup(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
