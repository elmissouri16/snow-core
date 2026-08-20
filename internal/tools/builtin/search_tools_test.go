package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type searchToolsRouter struct{ matches []tools.ToolMatch }

func (r searchToolsRouter) Search(_ context.Context, _ string, limit int) ([]tools.ToolMatch, error) {
	matches := append([]tools.ToolMatch(nil), r.matches...)
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}
func (r searchToolsRouter) DeferredCount() int { return len(r.matches) }
func (searchToolsRouter) Close() error         { return nil }

type searchToolsHost struct{ perm permission.Service }

func (searchToolsHost) CWD() string                          { return "" }
func (searchToolsHost) Roots() []string                      { return nil }
func (h searchToolsHost) Permission() permission.Service     { return h.perm }
func (searchToolsHost) EmitProgress(tools.ToolProgressEvent) {}
func (searchToolsHost) Environ() []string                    { return nil }

type searchToolsTestTool struct{ schema protocol.ToolSchema }

func (t searchToolsTestTool) Schema() protocol.ToolSchema { return t.schema }
func (searchToolsTestTool) Run(context.Context, json.RawMessage, tools.ToolHost) (tools.ToolResult, error) {
	return tools.TextResult("ok"), nil
}

func TestSearchToolsFiltersDeniedMatchesAndReturnsDiscoveryDetails(t *testing.T) {
	registry := tools.NewRegistry()
	for _, item := range []struct {
		name string
		risk permission.Risk
	}{{"read_catalog", permission.RiskRead}, {"write_catalog", permission.RiskWrite}} {
		schema := protocol.ToolSchema{
			Name: item.name, Description: item.name, Parameters: json.RawMessage(`{"type":"object"}`),
			Discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "catalog"},
		}
		if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: searchToolsTestTool{schema}, Source: tools.SourceSDK, Owner: "sdk", Risk: item.risk}); err != nil {
			t.Fatal(err)
		}
	}
	router := searchToolsRouter{matches: []tools.ToolMatch{
		{ID: "write_catalog", Description: "write"},
		{ID: "read_catalog", Description: "read"},
	}}
	tool := NewSearchTools(router, registry)
	result, err := tool.Run(context.Background(), json.RawMessage(`{"query":"catalog"}`), searchToolsHost{perm: permission.NewService(permission.ModeDeny, nil)})
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if !strings.Contains(result.Content[0].Text, "read_catalog") || strings.Contains(result.Content[0].Text, "write_catalog") {
		t.Fatalf("content = %s", result.Content[0].Text)
	}
	details, ok := result.Details.(tools.DiscoveryDetails)
	if !ok || len(details.Matches) != 1 || details.Matches[0].ID != "read_catalog" || details.CandidateCount != 2 {
		t.Fatalf("details = %+v", result.Details)
	}
}

func TestSearchToolsDeniedTopCandidatesDoNotHidePermittedResult(t *testing.T) {
	registry := tools.NewRegistry()
	matches := make([]tools.ToolMatch, 0, 21)
	for i := 0; i < 21; i++ {
		name := fmt.Sprintf("catalog_%02d", i)
		risk := permission.RiskWrite
		if i == 20 {
			risk = permission.RiskRead
		}
		schema := protocol.ToolSchema{Name: name, Parameters: json.RawMessage(`{"type":"object"}`), Discovery: &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "catalog"}}
		if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: searchToolsTestTool{schema}, Source: tools.SourceSDK, Owner: "sdk", Risk: risk}); err != nil {
			t.Fatal(err)
		}
		matches = append(matches, tools.ToolMatch{ID: name})
	}
	result, err := NewSearchTools(searchToolsRouter{matches: matches}, registry).Run(context.Background(), json.RawMessage(`{"query":"catalog"}`), searchToolsHost{perm: permission.NewService(permission.ModeDeny, nil)})
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "catalog_20") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSearchToolsValidatesArguments(t *testing.T) {
	tool := NewSearchTools(searchToolsRouter{}, tools.NewRegistry())
	for _, args := range []string{`{}`, `{"query":"x","limit":6}`} {
		result, err := tool.Run(context.Background(), json.RawMessage(args), searchToolsHost{perm: permission.NewService(permission.ModeAllow, nil)})
		if err != nil || !result.IsError {
			t.Fatalf("args %s: result = %+v, err = %v", args, result, err)
		}
	}
}
