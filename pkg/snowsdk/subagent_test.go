package snowsdk

import (
	"context"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSDKSubagentSurface(t *testing.T) {
	s, err := Open(context.Background(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, AutoApprove: true, EnableSubagents: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := make(chan protocol.AgentEvent, 32)
	s.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Agent != nil {
			select {
			case events <- ev.Clone():
			default:
			}
		}
	})
	if err := s.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	models := s.SubagentModels()
	if len(models) != 1 || models[0].Provider != "fake" || models[0].ID != "fake-1" {
		t.Fatalf("subagent models = %+v", models)
	}
	state, err := s.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "sdk", Task: "inspect", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	waited, err := s.WaitSubagentsUntilAll(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !waited.AllTerminal || waited.Running != 0 || waited.Queued != 0 || waited.Terminal != 1 {
		t.Fatalf("wait until all=%+v", waited)
	}
	state, err = s.Subagent("sdk")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != protocol.AgentCompleted {
		t.Fatalf("status=%s", state.Status)
	}
	list := s.Subagents()
	if len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
	found := false
	for !found {
		select {
		case ev := <-events:
			found = ev.Agent != nil && ev.Agent.Path == "/root/sdk"
		case <-time.After(time.Second):
			t.Fatal("missing attributed event")
		}
	}
	if _, err := s.SubagentUsage(); err != nil {
		t.Fatal(err)
	}
}
