package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type fakeRouter struct {
	matches []tools.ToolMatch
	err     error
	count   int
}

func (r *fakeRouter) Search(context.Context, string, int) ([]tools.ToolMatch, error) {
	return append([]tools.ToolMatch(nil), r.matches...), r.err
}
func (r *fakeRouter) DeferredCount() int { return r.count }
func (*fakeRouter) Close() error         { return nil }

func routingTool(name string, discovery *protocol.ToolDiscovery, result tools.ToolResult) *testTool {
	return &testTool{
		name: name,
		schema: protocol.ToolSchema{
			Name:       name,
			Parameters: json.RawMessage(`{"type":"object"}`),
			Discovery:  discovery,
		},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult { return result },
	}
}

func deferredDiscovery(namespace string) *protocol.ToolDiscovery {
	return &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: namespace}
}

func newRoutingAgent(t *testing.T, prov *scriptedProvider, registry *tools.SimpleRegistry, router tools.Router, mode permission.Mode) *Agent {
	t.Helper()
	perm := permission.NewService(mode, nil)
	a, err := New(Options{
		Provider: prov, Registry: registry, Router: router,
		Session:    session.NewMemoryStore(session.Options{CWD: t.TempDir()}),
		Permission: perm, ToolHost: &testHost{cwd: t.TempDir(), perm: perm},
		Model: protocol.Model{Provider: prov.ID(), ID: "m1", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func schemaNames(schemas []protocol.ToolSchema) []string {
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Name)
	}
	return names
}

func TestRoutingExposesDirectAndTopFiveDeferred(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(routingTool("direct", nil, tools.TextResult("ok"))); err != nil {
		t.Fatal(err)
	}
	matches := make([]tools.ToolMatch, 0, 6)
	for i := 1; i <= 6; i++ {
		name := "deferred_" + string(rune('0'+i))
		if err := registry.Register(routingTool(name, deferredDiscovery("catalog"), tools.TextResult("ok"))); err != nil {
			t.Fatal(err)
		}
		matches = append(matches, tools.ToolMatch{ID: name, Score: float64(10 - i)})
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	agent := newRoutingAgent(t, provider, registry, &fakeRouter{matches: matches, count: 6}, permission.ModeAllow)
	var routing *protocol.ToolRouting
	agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvToolRouting {
			routing = event.ToolRouting
		}
	})
	if err := agent.Prompt(context.Background(), "catalog operation"); err != nil {
		t.Fatal(err)
	}
	want := []string{"direct", "deferred_1", "deferred_2", "deferred_3", "deferred_4", "deferred_5"}
	if got := schemaNames(provider.requests[0].Tools); !equalStrings(got, want) {
		t.Fatalf("schemas = %v, want %v", got, want)
	}
	if routing == nil || routing.SelectedCount != 5 || routing.ExposedCount != 6 || routing.SchemaBytes == 0 {
		t.Fatalf("routing event = %+v", routing)
	}
}

func TestRoutingFiltersKnownDeniedDeferredTools(t *testing.T) {
	registry := tools.NewRegistry()
	readTool := routingTool("deferred_read", deferredDiscovery("catalog"), tools.TextResult("ok"))
	networkTool := routingTool("deferred_network", deferredDiscovery("catalog"), tools.TextResult("ok"))
	for _, entry := range []struct {
		tool *testTool
		risk permission.Risk
	}{{readTool, permission.RiskRead}, {networkTool, permission.RiskNet}} {
		if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: entry.tool.schema, Tool: entry.tool, Source: tools.SourceSDK, Owner: "sdk", Risk: entry.risk}); err != nil {
			t.Fatal(err)
		}
	}
	router := &fakeRouter{count: 2, matches: []tools.ToolMatch{{ID: "deferred_network"}, {ID: "deferred_read"}}}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	agent := newRoutingAgent(t, provider, registry, router, permission.ModeDeny)
	if err := agent.Prompt(context.Background(), "catalog"); err != nil {
		t.Fatal(err)
	}
	if got := schemaNames(provider.requests[0].Tools); !equalStrings(got, []string{"deferred_read"}) {
		t.Fatalf("schemas = %v", got)
	}
}

func TestRoutingFailureFallsBackToEligibleCatalog(t *testing.T) {
	registry := tools.NewRegistry()
	for _, name := range []string{"one", "two"} {
		if err := registry.Register(routingTool(name, deferredDiscovery("catalog"), tools.TextResult("ok"))); err != nil {
			t.Fatal(err)
		}
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	agent := newRoutingAgent(t, provider, registry, &fakeRouter{count: 2, err: errors.New("index unavailable")}, permission.ModeAllow)
	var event protocol.AgentEvent
	agent.Subscribe(func(candidate protocol.AgentEvent) {
		if candidate.Type == protocol.EvToolRouting {
			event = candidate
		}
	})
	if err := agent.Prompt(context.Background(), "catalog"); err != nil {
		t.Fatal(err)
	}
	if got := schemaNames(provider.requests[0].Tools); !equalStrings(got, []string{"one", "two"}) {
		t.Fatalf("schemas = %v", got)
	}
	if event.ToolRouting == nil || !event.ToolRouting.Fallback || event.Message == "" {
		t.Fatalf("routing event = %+v", event)
	}
}

func TestSearchToolsExpandsNextContinuation(t *testing.T) {
	registry := tools.NewRegistry()
	base := routingTool("base_tool", deferredDiscovery("catalog"), tools.TextResult("ok"))
	discovered := routingTool("discovered_tool", deferredDiscovery("mail"), tools.TextResult("ok"))
	search := routingTool("search_tools", nil, tools.ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock(`{"tools":[{"id":"discovered_tool"}]}`)},
		Details: tools.DiscoveryDetails{Matches: []tools.ToolMatch{{ID: "discovered_tool"}}, CandidateCount: 1, LatencyMS: 2},
	})
	for _, tool := range []*testTool{base, discovered, search} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "search-1", ToolName: "search_tools", Arguments: json.RawMessage(`{"query":"mail"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	agent := newRoutingAgent(t, provider, registry, &fakeRouter{count: 2, matches: []tools.ToolMatch{{ID: "base_tool"}}}, permission.ModeAllow)
	if err := agent.Prompt(context.Background(), "catalog"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d", len(provider.requests))
	}
	wantFirst := []string{"search_tools", "base_tool"}
	wantSecond := []string{"search_tools", "base_tool", "discovered_tool"}
	if got := schemaNames(provider.requests[0].Tools); !equalStrings(got, wantFirst) {
		t.Fatalf("first schemas = %v, want %v", got, wantFirst)
	}
	if got := schemaNames(provider.requests[1].Tools); !equalStrings(got, wantSecond) {
		t.Fatalf("second schemas = %v, want %v", got, wantSecond)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
