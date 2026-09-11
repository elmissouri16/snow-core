package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/tools/builtin"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type fixedPluginPolicy struct{ denied []string }

func (*fixedPluginPolicy) HasHook(string, bool) bool { return false }
func (*fixedPluginPolicy) RunHooks(_ context.Context, r plugin.HookRequest) (plugin.HookRequest, []protocol.PluginTransform, error) {
	return r, nil, nil
}
func (p *fixedPluginPolicy) ToolPolicy() func(string) error {
	denied := slices.Clone(p.denied)
	return func(name string) error {
		if slices.Contains(denied, name) {
			return errors.New("plugin denied " + name)
		}
		return nil
	}
}
func TestPluginPolicyFiltersSchemasDiscoveryFallbackAndDispatch(t *testing.T) {
	registry := tools.NewRegistry()
	ran := false
	for _, name := range []string{"permitted", "restricted"} {
		tool := routingTool(name, deferredDiscovery("catalog"), tools.TextResult("ok"))
		tool.runFunc = func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			ran = true
			return tools.TextResult("ran")
		}
		if err := registry.RegisterDescriptor(tools.ToolDescriptor{Tool: tool, Schema: tool.Schema(), Risk: permission.RiskRead, Effect: tools.EffectReadOnly, Source: tools.SourceSDK, Owner: "sdk"}); err != nil {
			t.Fatal(err)
		}
	}
	router := &fakeRouter{count: 2, matches: []tools.ToolMatch{{ID: "restricted"}, {ID: "permitted"}}}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a := newRoutingAgent(t, provider, registry, router, permission.ModeAllow)
	defer a.Close()
	a.opts.PluginHooks = &fixedPluginPolicy{denied: []string{"restricted"}}
	if err := a.Prompt(t.Context(), "catalog"); err != nil {
		t.Fatal(err)
	}
	if got := schemaNames(provider.requests[0].Tools); !slices.Equal(got, []string{"permitted"}) {
		t.Fatalf("restricted schema: %v", got)
	}
	if matches := a.fallbackDeferred("catalog", 5); len(matches) != 1 || matches[0].ID != "permitted" {
		t.Fatalf("fallback leak: %+v", matches)
	}
	search := builtin.NewSearchTools(router, registry)
	host := &progressHost{agent: a, ToolHost: a.opts.ToolHost}
	result, err := search.Run(t.Context(), json.RawMessage(`{"query":"catalog"}`), host)
	if err != nil || len(result.Content) != 1 || strings.Contains(result.Content[0].Text, "restricted") {
		t.Fatalf("discovery leak: %+v %v", result, err)
	}
	_, err = a.InvokePluginTool(t.Context(), plugin.Invocation{PluginID: "other", Name: "manual"}, "restricted", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "plugin denied") || ran {
		t.Fatalf("dispatch bypass: ran=%v err=%v", ran, err)
	}
}
