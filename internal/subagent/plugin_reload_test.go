package subagent

import (
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"testing"
)

func TestPluginReloadChecksDormantChildFingerprintsAndWork(t *testing.T) {
	child := &runtime{state: protocol.SubagentState{Agent: protocol.AgentRef{Path: "/root/child"}, Status: protocol.AgentCompleted, PluginTools: map[string]string{"plugin_demo_echo": "old"}}}
	m := &Manager{byID: map[string]*runtime{"child": child}}
	if err := m.CheckPluginReloadAdmitted("demo"); err == nil {
		t.Fatal("open selected child allowed")
	}
	if err := m.CheckPluginReloadAdmitted("other"); err != nil {
		t.Fatalf("unrelated child rejected: %v", err)
	}
	child.state.Status = protocol.AgentClosed
	if err := m.CheckPluginReloadAdmitted("demo"); err != nil {
		t.Fatal(err)
	}
	if child.state.PluginTools["plugin_demo_echo"] != "old" {
		t.Fatal("persisted fingerprint rewritten")
	}
	child.state.Status = protocol.AgentQueued
	if err := m.CheckPluginReloadAdmitted("other"); err == nil {
		t.Fatal("queued work allowed")
	}
	child.state.Status = protocol.AgentCompleted
	child.pendingTasks = 1
	if err := m.CheckPluginReloadAdmitted("other"); err == nil {
		t.Fatal("pending work allowed")
	}
	child.pendingTasks = 0
	m.reserved = map[protocol.AgentPath]struct{}{"/root/new": {}}
	if err := m.CheckPluginReloadAdmitted("other"); err == nil {
		t.Fatal("child reservation allowed")
	}
}
