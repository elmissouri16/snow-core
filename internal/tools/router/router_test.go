package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	bleve "github.com/blevesearch/bleve/v2"
	blevemapping "github.com/blevesearch/bleve/v2/mapping"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type testTool struct{ schema protocol.ToolSchema }

type embeddedBleveIndex interface{ bleve.Index }

type failingSearchIndex struct{ embeddedBleveIndex }

type closeCountingIndex struct {
	embeddedBleveIndex
	closes int
}

func (c *closeCountingIndex) Close() error {
	c.closes++
	return c.embeddedBleveIndex.Close()
}

func (f failingSearchIndex) SearchInContext(context.Context, *bleve.SearchRequest) (*bleve.SearchResult, error) {
	return nil, errors.New("forced namespace search failure")
}

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
	router := New([]tools.ToolDescriptor{
		deferredDescriptor("old_tool", "old", "legacy capability"),
		deferredDescriptor("other_tool", "other", "other capability"),
	})
	defer router.Close()
	if matches, err := router.Search(context.Background(), "legacy", 5); err != nil || len(matches) != 1 || matches[0].ID != "old_tool" {
		t.Fatalf("initial search = %+v, %v", matches, err)
	}
	if err := router.Refresh([]tools.ToolDescriptor{
		deferredDescriptor("new_tool", "new", "modern capability"),
		deferredDescriptor("next_tool", "next", "future capability"),
	}); err != nil {
		t.Fatal(err)
	}
	if router.DeferredCount() != 2 || router.namespaceCount != 2 {
		t.Fatalf("counts = tools %d namespaces %d", router.DeferredCount(), router.namespaceCount)
	}
	if matches, err := router.Search(context.Background(), "legacy", 5); err != nil || len(matches) != 0 {
		t.Fatalf("stale search = %+v, %v", matches, err)
	}
	if matches, err := router.Search(context.Background(), "modern", 5); err != nil || len(matches) != 1 || matches[0].ID != "new_tool" {
		t.Fatalf("refreshed search = %+v, %v", matches, err)
	}
}

func TestCloseClosesBothIndexesExactlyOnce(t *testing.T) {
	router := New([]tools.ToolDescriptor{
		deferredDescriptor("one", "one", "Find one."),
		deferredDescriptor("two", "two", "Find two."),
	})
	toolIndex := &closeCountingIndex{embeddedBleveIndex: router.index}
	namespaceIndex := &closeCountingIndex{embeddedBleveIndex: router.namespaceIndex}
	router.index = toolIndex
	router.namespaceIndex = namespaceIndex
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	if toolIndex.closes != 1 || namespaceIndex.closes != 1 {
		t.Fatalf("close counts = tool %d namespace %d", toolIndex.closes, namespaceIndex.closes)
	}
}

func TestNamespaceFirstPromotesScopedToolsAndKeepsGlobalRescue(t *testing.T) {
	router := New([]tools.ToolDescriptor{
		deferredDescriptor("commerce_inventory_lookup", "commerce", "Find inventory records for a store.", "stock"),
		deferredDescriptor("commerce_order_get", "commerce", "Read an order by identifier.", "purchase"),
		deferredDescriptor("analytics_commerce_lookup", "analytics", "Exact global commerce lookup report.", "report"),
		deferredDescriptor("support_record_lookup", "support", "Find support records.", "ticket"),
	})
	defer router.Close()

	matches, err := router.Search(context.Background(), "commerce lookup", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) < 2 {
		t.Fatalf("matches = %+v", matches)
	}
	if !slices.ContainsFunc(matches, func(match tools.ToolMatch) bool {
		return match.Namespace == "commerce"
	}) {
		t.Fatalf("selected namespace missing: %+v", matches)
	}
	if !slices.ContainsFunc(matches, func(match tools.ToolMatch) bool {
		return match.ID == "analytics_commerce_lookup"
	}) {
		t.Fatalf("global rescue tool missing: %+v", matches)
	}
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		if seen[match.ID] {
			t.Fatalf("duplicate result %q in %+v", match.ID, matches)
		}
		seen[match.ID] = true
	}
}

func TestReciprocalRankFusionPromotesScopedMatchAndRetainsGlobalMatch(t *testing.T) {
	global := []tools.ToolMatch{{ID: "global_exact", Namespace: "other"}}
	scoped := make([]tools.ToolMatch, 12)
	for i := range scoped {
		scoped[i] = tools.ToolMatch{ID: fmt.Sprintf("selected_%02d", i), Namespace: "selected"}
	}
	matches := fuseRankings(global, scoped, 20)
	globalRank := slices.IndexFunc(matches, func(match tools.ToolMatch) bool {
		return match.ID == "global_exact"
	})
	if globalRank < 0 || globalRank >= globalRescueWindow {
		t.Fatalf("global rescue rank = %d, matches = %+v", globalRank, matches)
	}
	if matches[0].Namespace != "selected" {
		t.Fatalf("scoped match was not promoted: %+v", matches)
	}

	limited := fuseRankings(global, scoped, 3)
	if !slices.ContainsFunc(limited, func(match tools.ToolMatch) bool { return match.ID == "global_exact" }) {
		t.Fatalf("global rescue missing at small limit: %+v", limited)
	}
}

func TestNamespaceFilterDoesNotChangeMetadataScores(t *testing.T) {
	catalog := []tools.ToolDescriptor{
		deferredDescriptor("large_target", "large", "Shared needle capability.", "needle"),
		deferredDescriptor("small_target", "small", "Shared needle capability.", "needle"),
	}
	for i := 0; i < 40; i++ {
		catalog = append(catalog, deferredDescriptor(fmt.Sprintf("large_filler_%02d", i), "large", "Unrelated filler capability.", "filler"))
	}
	router := New(catalog)
	defer router.Close()

	router.mu.RLock()
	matches, err := router.searchToolIndex(context.Background(), "needle", []string{"large", "small"}, 20)
	router.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0].ID != "large_target" || matches[1].ID != "small_target" {
		t.Fatalf("namespace filter distorted metadata ranking: %+v", matches)
	}
	if matches[0].Score != matches[1].Score {
		t.Fatalf("equal metadata scores differ: %+v", matches)
	}
}

func TestOneNamespaceAndNamespaceFailurePreserveGlobalOrder(t *testing.T) {
	catalog := []tools.ToolDescriptor{
		deferredDescriptor("catalog_find", "catalog", "Find catalog data.", "lookup"),
		deferredDescriptor("catalog_update", "catalog", "Update catalog data.", "write"),
	}
	router := New(catalog)
	defer router.Close()

	router.mu.RLock()
	global, err := router.searchToolIndex(context.Background(), "catalog data", nil, 20)
	router.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := router.Search(context.Background(), "catalog data", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMatchOrder(global, matches) {
		t.Fatalf("one namespace changed global order: global=%+v matches=%+v", global, matches)
	}

	multi := New(append(catalog, deferredDescriptor("github_find", "github", "Find source records.", "repo")))
	defer multi.Close()
	multi.mu.RLock()
	global, err = multi.searchToolIndex(context.Background(), "find records", nil, 20)
	multi.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	multi.namespaceIndex = failingSearchIndex{embeddedBleveIndex: multi.namespaceIndex}
	matches, err = multi.Search(context.Background(), "find records", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMatchOrder(global, matches) {
		t.Fatalf("namespace failure changed global order: global=%+v matches=%+v", global, matches)
	}
}

func TestNamespaceSummariesAreBoundedAndDeterministic(t *testing.T) {
	catalog := make([]tools.ToolDescriptor, 0, 800)
	for i := 0; i < cap(catalog); i++ {
		catalog = append(catalog, deferredDescriptor(
			fmt.Sprintf("catalog_tool_%04d", i),
			"very_large_catalog",
			strings.Repeat(fmt.Sprintf("description-%04d ", i), 300),
			fmt.Sprintf("keyword-%04d", i), "duplicate",
		))
	}
	forward := buildNamespaceDocuments(catalog)["very_large_catalog"]
	slices.Reverse(catalog)
	reverse := buildNamespaceDocuments(catalog)["very_large_catalog"]
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatal("namespace summary depends on catalog order")
	}
	if size := namespaceDocumentSize(forward); size > namespaceSummaryMaxSize {
		t.Fatalf("summary size = %d, max %d", size, namespaceSummaryMaxSize)
	}
	if !utf8FieldsValid(forward) {
		t.Fatal("bounded namespace summary contains invalid UTF-8")
	}
	if strings.Count(strings.Join(forward.Keywords, " "), "duplicate") != 1 {
		t.Fatalf("keywords were not deduplicated: %q", forward.Keywords)
	}

	unicodeDoc := buildNamespaceDocuments([]tools.ToolDescriptor{
		deferredDescriptor("unicode_tool", "unicode", "x"+strings.Repeat("界", namespaceDescriptionsMaxSize), "keyword"),
	})["unicode"]
	if !utf8FieldsValid(unicodeDoc) || namespaceDocumentSize(unicodeDoc) > namespaceSummaryMaxSize {
		t.Fatalf("multibyte summary was not safely bounded: size=%d", namespaceDocumentSize(unicodeDoc))
	}
	if len(unicodeDoc.Description) >= namespaceDescriptionsMaxSize {
		t.Fatalf("test did not exercise a partial-rune boundary: description bytes=%d", len(unicodeDoc.Description))
	}

	oversizedNamespace := strings.Repeat("n", namespaceFieldMaxSize+1)
	if _, ok := buildNamespaceDocuments([]tools.ToolDescriptor{
		deferredDescriptor("oversized_namespace_tool", oversizedNamespace, "Find data.", "find"),
	})[oversizedNamespace]; ok {
		t.Fatal("oversized namespace produced an unbounded summary")
	}
}

func TestTiedSearchOrderIsStableAcrossCatalogOrder(t *testing.T) {
	catalog := []tools.ToolDescriptor{
		deferredDescriptor("alpha_lookup", "alpha", "Find a shared record.", "shared"),
		deferredDescriptor("beta_lookup", "beta", "Find a shared record.", "shared"),
		deferredDescriptor("gamma_lookup", "gamma", "Find a shared record.", "shared"),
		deferredDescriptor("delta_lookup", "delta", "Find a shared record.", "shared"),
	}
	forward := New(catalog)
	defer forward.Close()
	slices.Reverse(catalog)
	reverse := New(catalog)
	defer reverse.Close()

	forwardMatches, err := forward.Search(context.Background(), "shared record", 20)
	if err != nil {
		t.Fatal(err)
	}
	reverseMatches, err := reverse.Search(context.Background(), "shared record", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMatchOrder(forwardMatches, reverseMatches) {
		t.Fatalf("catalog order changed tied ranking: forward=%+v reverse=%+v", forwardMatches, reverseMatches)
	}
}

func TestRefreshFailureRetainsPreviousIndexPair(t *testing.T) {
	calls := 0
	factory := func(mapping blevemapping.IndexMapping) (bleve.Index, error) {
		calls++
		if calls == 4 {
			return nil, errors.New("forced namespace build failure")
		}
		return bleve.NewMemOnly(mapping)
	}
	router := newWithFactory([]tools.ToolDescriptor{
		deferredDescriptor("old_find", "old", "Find legacy records."),
		deferredDescriptor("other_find", "other", "Find other records."),
	}, factory)
	defer router.Close()
	if router.initErr != nil {
		t.Fatal(router.initErr)
	}

	err := router.Refresh([]tools.ToolDescriptor{
		deferredDescriptor("new_find", "new", "Find modern records."),
		deferredDescriptor("next_find", "next", "Find future records."),
	})
	if err == nil {
		t.Fatal("expected refresh failure")
	}
	matches, searchErr := router.Search(context.Background(), "legacy", 20)
	if searchErr != nil || len(matches) == 0 || matches[0].ID != "old_find" {
		t.Fatalf("previous pair not retained: matches=%+v err=%v", matches, searchErr)
	}
	if matches, searchErr = router.Search(context.Background(), "modern", 20); searchErr != nil || len(matches) != 0 {
		t.Fatalf("failed refresh leaked new catalog: matches=%+v err=%v", matches, searchErr)
	}
}

func TestConcurrentSearchAndRefresh(t *testing.T) {
	catalogA := []tools.ToolDescriptor{
		deferredDescriptor("alpha_find", "alpha", "Find alpha records."),
		deferredDescriptor("beta_find", "beta", "Find beta records."),
	}
	catalogB := []tools.ToolDescriptor{
		deferredDescriptor("gamma_find", "gamma", "Find gamma records."),
		deferredDescriptor("delta_find", "delta", "Find delta records."),
	}
	router := New(catalogA)
	defer router.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = router.Search(context.Background(), "find records", 20)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if err := router.Refresh(catalogB); err != nil {
				t.Errorf("refresh B: %v", err)
				return
			}
			if err := router.Refresh(catalogA); err != nil {
				t.Errorf("refresh A: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

func sameMatchOrder(left, right []tools.ToolMatch) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].ID != right[i].ID || left[i].Score != right[i].Score {
			return false
		}
	}
	return true
}

func utf8FieldsValid(doc indexDocument) bool {
	if !utf8.ValidString(doc.NamespaceID + doc.Namespace + doc.Name + doc.Description) {
		return false
	}
	for _, keyword := range doc.Keywords {
		if !utf8.ValidString(keyword) {
			return false
		}
	}
	return true
}

func BenchmarkSearch(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools_%d", size), func(b *testing.B) {
			catalog := make([]tools.ToolDescriptor, 0, size)
			for i := 0; i < size; i++ {
				namespace := fmt.Sprintf("catalog_%02d", i%10)
				catalog = append(catalog, deferredDescriptor(fmt.Sprintf("catalog_tool_%d", i), namespace, "Find and update catalog inventory records.", "inventory", "record"))
			}
			router := New(catalog)
			defer router.Close()

			b.Run("global", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					router.mu.RLock()
					_, err := router.searchToolIndex(context.Background(), "catalog 03 update inventory", nil, 20)
					router.mu.RUnlock()
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("namespace_first", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := router.Search(context.Background(), "catalog 03 update inventory", 20); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
