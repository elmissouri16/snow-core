package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

type registryTestTool struct{ schema ToolSchema }

func (t registryTestTool) Schema() ToolSchema { return t.schema }
func (registryTestTool) Run(context.Context, json.RawMessage, ToolHost) (ToolResult, error) {
	return TextResult("ok"), nil
}

func TestDiscoveryDefaultsToAlways(t *testing.T) {
	registry := NewRegistry()
	tool := registryTestTool{schema: protocol.ToolSchema{Name: "existing", Parameters: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	desc, ok := registry.Descriptor("existing")
	if !ok || IsDeferred(desc) {
		t.Fatalf("descriptor = %+v, want direct", desc)
	}
}

func TestDeferredPluginDerivesNamespaceAndClonesMetadata(t *testing.T) {
	registry := NewRegistry()
	discovery := &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Keywords: []string{"mail"}}
	schema := protocol.ToolSchema{Name: "find", Parameters: json.RawMessage(`{"type":"object"}`), Discovery: discovery}
	err := registry.RegisterDescriptor(ToolDescriptor{
		Schema: schema, Tool: registryTestTool{schema: schema}, Source: SourceGoPlugin,
		Owner: "plugin:gmail", PluginID: "gmail", OriginalName: "find", Risk: permission.RiskRead,
	})
	if err != nil {
		t.Fatal(err)
	}
	discovery.Keywords[0] = "mutated"
	desc, ok := registry.Descriptor("plugin_gmail_find")
	if !ok {
		t.Fatal("missing namespaced descriptor")
	}
	if !IsDeferred(desc) || desc.Schema.Discovery.Namespace != "gmail" || desc.Schema.Discovery.Keywords[0] != "mail" {
		t.Fatalf("discovery = %+v", desc.Schema.Discovery)
	}
	desc.Schema.Discovery.Keywords[0] = "also-mutated"
	again, _ := registry.Descriptor("plugin_gmail_find")
	if again.Schema.Discovery.Keywords[0] != "mail" {
		t.Fatal("registry metadata was not defensively cloned")
	}
}

func TestDeferredDiscoveryValidation(t *testing.T) {
	tests := []struct {
		name      string
		discovery *protocol.ToolDiscovery
	}{
		{name: "missing namespace", discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred}},
		{name: "bad namespace", discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "Bad.Space"}},
		{name: "bad mode", discovery: &protocol.ToolDiscovery{Mode: "sometimes", Namespace: "valid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			schema := protocol.ToolSchema{Name: "future_tool", Parameters: json.RawMessage(`{"type":"object"}`), Discovery: tt.discovery}
			if err := registry.Register(registryTestTool{schema: schema}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCanExposeUsesOptionalPermissionPolicy(t *testing.T) {
	desc := ToolDescriptor{Schema: protocol.ToolSchema{Name: "network_tool"}, Risk: permission.RiskNet}
	deny := permission.NewService(permission.ModeDeny, nil)
	if CanExpose(deny, desc) {
		t.Fatal("deny mode exposed a network tool")
	}
	allow := permission.NewService(permission.ModeAllow, nil)
	if !CanExpose(allow, desc) {
		t.Fatal("allow mode hid a tool")
	}
	ask := permission.NewService(permission.ModeAsk, nil)
	if !CanExpose(ask, desc) {
		t.Fatal("undecided ask-mode tool should remain visible")
	}
	ask.Remember(permission.Request{Tool: "network_tool", Risk: permission.RiskNet}, permission.DecisionDeny)
	if CanExpose(ask, desc) {
		t.Fatal("remembered denial should hide deferred tool")
	}
}
