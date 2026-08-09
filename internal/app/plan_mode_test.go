package app

import (
	"context"
	"path/filepath"
	"testing"

	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
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
