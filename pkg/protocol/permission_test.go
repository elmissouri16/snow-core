package protocol

import (
	"encoding/json"
	"testing"
)

func TestPermissionEventCloneOwnsStructuredAnalysis(t *testing.T) {
	event := AgentEvent{Type: EvPermissionRequest, Permission: &Permission{Request: PermissionRequest{
		Tool: "bash", Args: json.RawMessage(`{"command":"cat /tmp/input"}`), Paths: []string{"/tmp/input"},
		Effects:      []PermissionEffect{{Type: "filesystem", Capability: "filesystem.read.external", Operation: "read", Resource: "/tmp/input"}},
		Capabilities: []string{"filesystem.read.external"}, Rememberable: true, ScopeLabel: "read /tmp/input",
	}}}
	clone := event.Clone()
	clone.Permission.Request.Args[0] = '['
	clone.Permission.Request.Paths[0] = "changed"
	clone.Permission.Request.Effects[0].Resource = "changed"
	clone.Permission.Request.Capabilities[0] = "changed"
	if string(event.Permission.Request.Args) != `{"command":"cat /tmp/input"}` || event.Permission.Request.Paths[0] != "/tmp/input" || event.Permission.Request.Effects[0].Resource != "/tmp/input" || event.Permission.Request.Capabilities[0] != "filesystem.read.external" {
		t.Fatalf("clone mutated source: %+v", event.Permission.Request)
	}
}
