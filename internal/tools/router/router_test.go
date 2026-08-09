package router

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type testTool struct{ schema protocol.ToolSchema }

func (t testTool) Schema() protocol.ToolSchema { return t.schema }
func (testTool) Run(context.Context, json.RawMessage, tools.ToolHost) (tools.ToolResult, error) {
	return tools.TextResult("ok"), nil
}

func deferredDescriptor(id, namespace, description string, keywords ...string) tools.ToolDescriptor {
	schema := protocol.ToolSchema{
		Name:        id,
		Description: description,
		Parameters:  json.RawMessage(`{"type":"object","properties":{"secret_schema_marker":{"type":"string"}}}`),
		Discovery: &protocol.ToolDiscovery{
			Mode:      protocol.ToolDiscoveryDeferred,
			Namespace: namespace,
			Keywords:  keywords,
		},
	}
	return tools.ToolDescriptor{Schema: schema, Tool: testTool{schema: schema}, Source: tools.SourceSDK, Owner: "test", Risk: permission.RiskRead, OriginalName: id}
}

func TestSearchRanksNamesKeywordsAndDescriptions(t *testing.T) {
	router := New([]tools.ToolDescriptor{
		deferredDescriptor("shopify_inventory_adjust", "shopify", "Increase or decrease variant stock quantity.", "inventory", "stock", "sku"),
		deferredDescriptor("shopify_product_get", "shopify", "Read product title and merchandising data.", "product", "catalog"),
		deferredDescriptor("github_branch_create", "github", "Create a source control branch in a repository.", "repo", "branch"),
	})
	defer router.Close()

	tests := []struct {
		query string
		want  string
	}{
		{query: "adjust inventory by sku", want: "shopify_inventory_adjust"},
		{query: "create github branch", want: "github_branch_create"},
		{query: "read product catalog", want: "shopify_product_get"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			matches, err := router.Search(context.Background(), tt.query, 3)
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) == 0 || matches[0].ID != tt.want {
				t.Fatalf("matches = %+v, want first %q", matches, tt.want)
			}
		})
	}
}

func TestSearchHonorsLimitAndOmitsSchemasFromIndex(t *testing.T) {
	router := New([]tools.ToolDescriptor{
		deferredDescriptor("one", "catalog", "Find a catalog item.", "catalog"),
		deferredDescriptor("two", "catalog", "Update a catalog item.", "catalog"),
		deferredDescriptor("three", "catalog", "Delete a catalog item.", "catalog"),
	})
	defer router.Close()
	matches, err := router.Search(context.Background(), "catalog", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	fields, err := router.index.Fields()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field == "parameters" || field == "secret_schema_marker" {
			t.Fatalf("full schema leaked into index fields: %v", fields)
		}
	}
}

func TestSearchCancellationAndClose(t *testing.T) {
	router := New([]tools.ToolDescriptor{deferredDescriptor("one", "catalog", "Find catalog data.")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := router.Search(ctx, "catalog", 5); err == nil {
		t.Fatal("expected canceled search")
	}
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Search(context.Background(), "catalog", 5); err == nil {
		t.Fatal("expected closed router error")
	}
	if err := router.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestRefreshReplacesDeferredCatalog(t *testing.T) {
	router := New([]tools.ToolDescriptor{deferredDescriptor("old_tool", "old", "legacy capability")})
	defer router.Close()
	if matches, err := router.Search(context.Background(), "legacy", 5); err != nil || len(matches) != 1 || matches[0].ID != "old_tool" {
		t.Fatalf("initial search = %+v, %v", matches, err)
	}
	if err := router.Refresh([]tools.ToolDescriptor{deferredDescriptor("new_tool", "new", "modern capability")}); err != nil {
		t.Fatal(err)
	}
	if router.DeferredCount() != 1 {
		t.Fatalf("count = %d", router.DeferredCount())
	}
	if matches, err := router.Search(context.Background(), "legacy", 5); err != nil || len(matches) != 0 {
		t.Fatalf("stale search = %+v, %v", matches, err)
	}
	if matches, err := router.Search(context.Background(), "modern", 5); err != nil || len(matches) != 1 || matches[0].ID != "new_tool" {
		t.Fatalf("refreshed search = %+v, %v", matches, err)
	}
}

func BenchmarkSearch(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools_%d", size), func(b *testing.B) {
			catalog := make([]tools.ToolDescriptor, 0, size)
			for i := 0; i < size; i++ {
				catalog = append(catalog, deferredDescriptor(fmt.Sprintf("catalog_tool_%d", i), "catalog", "Find and update catalog inventory records.", "inventory", "record"))
			}
			router := New(catalog)
			defer router.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := router.Search(context.Background(), "update inventory record", 20); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
