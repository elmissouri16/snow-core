package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmissouri16/snow-core/internal/tools"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func toolOnlyServer(calls *atomic.Int32) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "tool-only", Version: "1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`)}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		calls.Add(1)
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	return server
}

func seedLazyCache(t *testing.T, root string, decl Declaration, roots []string, now time.Time) {
	t.Helper()
	key, projectHash, fingerprint := cacheIdentity(decl, roots)
	cache := newCatalogCache(root)
	entry := cachedCatalog{
		ServerID: decl.Spec.ID, Scope: decl.Scope, ProjectIdentityHash: projectHash,
		ConfigurationFingerprint: fingerprint, CachedAt: now.UTC(), ProtocolVersion: "2025-11-25",
		ServerName: "cached-server", ServerVersion: "1", Capabilities: []string{"tools"},
		Tools: []cachedTool{{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`)}},
	}
	if err := cache.put(key, entry, now); err != nil {
		t.Fatal(err)
	}
	if err := cache.close(); err != nil {
		t.Fatal(err)
	}
}

func seedCatalog(t *testing.T, root string, decl Declaration, roots []string, now time.Time, capabilities []string, cachedTools []cachedTool) {
	t.Helper()
	key, projectHash, fingerprint := cacheIdentity(decl, roots)
	cache := newCatalogCache(root)
	entry := cachedCatalog{
		ServerID: decl.Spec.ID, Scope: decl.Scope, ProjectIdentityHash: projectHash,
		ConfigurationFingerprint: fingerprint, CachedAt: now.UTC(), ProtocolVersion: "2025-11-25",
		ServerName: "cached-server", ServerVersion: "1", Capabilities: capabilities, Tools: cachedTools,
	}
	if err := cache.put(key, entry, now); err != nil {
		t.Fatal(err)
	}
	if err := cache.close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTransportBoundsUnknownLengthResponseBody(t *testing.T) {
	for _, tt := range []struct {
		name      string
		size      int
		wantError bool
	}{{"exact limit", maxCacheBytes, false}, {"over limit", maxCacheBytes + 1, true}} {
		t.Run(tt.name, func(t *testing.T) {
			transport := headerTransport{base: testRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", tt.size)))}, nil
			})}
			request, err := http.NewRequest(http.MethodGet, "https://example.test/mcp", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if tt.wantError {
				if readErr == nil || !strings.Contains(readErr.Error(), "size limit") || len(data) > maxCacheBytes {
					t.Fatalf("read bytes=%d err=%v", len(data), readErr)
				}
			} else if readErr != nil || len(data) != tt.size {
				t.Fatalf("read bytes=%d err=%v", len(data), readErr)
			}
		})
	}
}

func TestLiveToolCatalogBoundsSingleAndMultiPage(t *testing.T) {
	tests := []struct {
		name     string
		pageSize int
		count    int
		schema   json.RawMessage
	}{
		{name: "single page count", count: maxCachedToolsPerServer + 1, schema: json.RawMessage(`{"type":"object"}`)},
		{name: "multiple pages count", pageSize: 7, count: maxCachedToolsPerServer + 1, schema: json.RawMessage(`{"type":"object"}`)},
		{name: "single oversized schema", count: 1, schema: json.RawMessage(`{"type":"object","description":"` + strings.Repeat("x", maxCachedSchemaBytes) + `"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "oversized", Version: "1"}, &sdkmcp.ServerOptions{PageSize: tt.pageSize})
			for i := range tt.count {
				server.AddTool(&sdkmcp.Tool{Name: fmt.Sprintf("tool_%d", i), InputSchema: tt.schema}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
					return &sdkmcp.CallToolResult{}, nil
				})
			}
			httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
			defer httpServer.Close()
			manager := NewManager(tools.NewRegistry(), Options{})
			manager.Initialize(context.Background(), []Declaration{{Spec: publicmcp.ServerSpec{ID: "oversized", URL: httpServer.URL}}})
			defer manager.Close()
			statuses := manager.Statuses()
			if len(statuses) != 1 || statuses[0].State != stateFailed.String() {
				t.Fatalf("statuses = %+v", statuses)
			}
		})
	}
}

func TestStrictNoBootstrapCacheMissDoesNoTransportWork(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer httpServer.Close()
	project := t.TempDir()
	decl := Declaration{Spec: publicmcp.ServerSpec{
		ID: "strict", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL,
		Lifecycle: publicmcp.LifecycleLazy, CacheBootstrap: publicmcp.CacheBootstrapExplicit,
	}, Scope: "project", ProjectIdentity: project}
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: t.TempDir()})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	if requests.Load() != 0 {
		t.Fatalf("strict cache miss made %d requests", requests.Load())
	}
	statuses := manager.Statuses()
	if len(statuses) != 1 || statuses[0].Connected || statuses[0].Cached || statuses[0].State != stateConfigured.String() || !strings.Contains(statuses[0].Message, "no valid MCP cache") {
		t.Fatalf("statuses = %+v", statuses)
	}
	cacheStatuses := manager.CacheStatuses([]Declaration{decl})
	if len(cacheStatuses) != 1 || cacheStatuses[0].State != publicmcp.CacheStateMissing || cacheStatuses[0].Valid {
		t.Fatalf("cache statuses = %+v", cacheStatuses)
	}
}

func TestStrictNoBootstrapAcceptsCachedEmptyCatalogWithoutTransport(t *testing.T) {
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer httpServer.Close()
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{
		ID: "strict-empty", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL,
		Lifecycle: publicmcp.LifecycleLazy, CacheBootstrap: publicmcp.CacheBootstrapExplicit,
	}, Scope: "project", ProjectIdentity: project}
	seedCatalog(t, root, decl, []string{project}, now, []string{"tools"}, nil)
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	if requests.Load() != 0 {
		t.Fatalf("strict cached empty catalog made %d requests", requests.Load())
	}
	statuses := manager.Statuses()
	if len(statuses) != 1 || statuses[0].Connected || !statuses[0].Cached || statuses[0].State != stateCached.String() || !strings.Contains(statuses[0].Message, "no activation descriptor") {
		t.Fatalf("statuses = %+v", statuses)
	}
}

func TestLazyKeepAliveConnectsOnFirstCallAndDoesNotArmIdle(t *testing.T) {
	var calls atomic.Int32
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "keep", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazyKeepAlive}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)
	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	rt := manager.runtimes["keep"]
	rt.mu.Lock()
	initialState := rt.state
	rt.mu.Unlock()
	if initialState != stateCached {
		t.Fatalf("initial state = %s", initialState)
	}
	if _, ok := registry.Get("mcp_keep_echo"); !ok {
		t.Fatal("cached keep-alive tool missing")
	}
	session, release, err := rt.acquire(context.Background())
	if err != nil || session == nil {
		t.Fatalf("acquire session=%v err=%v", session, err)
	}
	release()
	rt.mu.Lock()
	state, timer := rt.state, rt.idleTimer
	rt.mu.Unlock()
	if state != stateConnected || timer != nil {
		t.Fatalf("keep-alive state=%s idleTimer=%v", state, timer)
	}
}

func TestCacheStatusMismatchAndClearSupersededFingerprint(t *testing.T) {
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "managed", Command: "server-a", Lifecycle: publicmcp.LifecycleLazy}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root, Now: func() time.Time { return now }})
	defer manager.Close()
	statuses := manager.CacheStatuses([]Declaration{decl})
	if len(statuses) != 1 || !statuses[0].Valid || statuses[0].State != publicmcp.CacheStateValid || statuses[0].ToolCount != 1 {
		t.Fatalf("valid status = %+v", statuses)
	}
	changed := decl
	changed.Spec.Command = "server-b"
	statuses = manager.CacheStatuses([]Declaration{changed})
	if len(statuses) != 1 || statuses[0].State != publicmcp.CacheStateMismatch || statuses[0].Valid {
		t.Fatalf("mismatch status = %+v", statuses)
	}
	removed, err := manager.ClearCache(changed)
	if err != nil || removed != 1 {
		t.Fatalf("clear removed=%d err=%v", removed, err)
	}
	statuses = manager.CacheStatuses([]Declaration{decl})
	if len(statuses) != 1 || statuses[0].State != publicmcp.CacheStateMissing {
		t.Fatalf("post-clear status = %+v", statuses)
	}
}

func TestForcedRefreshCacheWriteFailureIsFatalAndRetainsPriorCache(t *testing.T) {
	var calls atomic.Int32
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC().Add(-time.Hour)
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "write-failure", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root, ForceRefresh: true})
	manager.cache.putErr = errors.New("injected durable write failure")
	manager.Initialize(context.Background(), []Declaration{decl})
	statuses := manager.Statuses()
	if len(statuses) != 1 || statuses[0].State != stateFailed.String() {
		t.Fatalf("refresh status = %+v", statuses)
	}
	manager.Close()
	inspector := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	defer inspector.Close()
	cacheStatuses := inspector.CacheStatuses([]Declaration{decl})
	if len(cacheStatuses) != 1 || !cacheStatuses[0].Valid || !cacheStatuses[0].CachedAt.Equal(now) {
		t.Fatalf("prior cache was not retained: %+v", cacheStatuses)
	}
}

func TestFailedForcedRefreshRetainsPriorCache(t *testing.T) {
	var calls atomic.Int32
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "retain", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)
	httpServer.Close()
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root, ForceRefresh: true})
	manager.Initialize(context.Background(), []Declaration{decl})
	manager.Close()
	inspector := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	defer inspector.Close()
	statuses := inspector.CacheStatuses([]Declaration{decl})
	if len(statuses) != 1 || !statuses[0].Valid || statuses[0].ToolCount != 1 {
		t.Fatalf("prior cache was not retained: %+v", statuses)
	}
}

func TestLazyValidCacheStartsWithoutTransportWork(t *testing.T) {
	var requests atomic.Int32
	var calls atomic.Int32
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	root, project := t.TempDir(), t.TempDir()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "cached", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)

	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: project, Roots: []string{project}, CacheRoot: root, Now: func() time.Time { return now }})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	if got := requests.Load(); got != 0 {
		t.Fatalf("cached initialization made %d HTTP requests", got)
	}
	statuses := manager.Statuses()
	if len(statuses) != 1 || statuses[0].Connected || !statuses[0].Cached || statuses[0].State != stateCached.String() {
		t.Fatalf("statuses = %+v", statuses)
	}
	if _, ok := registry.Get("mcp_cached_echo"); !ok {
		t.Fatalf("cached descriptor was not registered: %+v", registry.Descriptors())
	}
}

func TestLazyCachedResourcePromptBridgesStartWithoutTransportAndReconnect(t *testing.T) {
	var requests atomic.Int32
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return testServer() }, &sdkmcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "bridges", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	seedCatalog(t, root, decl, []string{project}, now, []string{"tools", "resources", "prompts"}, []cachedTool{{Name: "echo.value", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`)}})

	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	if got := requests.Load(); got != 0 {
		t.Fatalf("cached bridge initialization made %d HTTP requests", got)
	}
	for _, name := range []string{"mcp_bridges_list_resources", "mcp_bridges_read_resource", "mcp_bridges_list_prompts", "mcp_bridges_get_prompt"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("cached bridge %q missing: %+v", name, registry.Schemas())
		}
	}
	listResources, _ := registry.Get("mcp_bridges_list_resources")
	result, err := listResources.Run(context.Background(), json.RawMessage(`{"kind":"resources"}`), nil)
	if err != nil || result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "file:///note") {
		t.Fatalf("list resources result=%+v err=%v", result, err)
	}
	if requests.Load() == 0 {
		t.Fatal("bridge call did not lazily connect")
	}
	readResource, _ := registry.Get("mcp_bridges_read_resource")
	result, err = readResource.Run(context.Background(), json.RawMessage(`{"uri":"file:///note"}`), nil)
	if err != nil || result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "resource body") {
		t.Fatalf("read resource result=%+v err=%v", result, err)
	}
	listResources, _ = registry.Get("mcp_bridges_list_resources")
	result, err = listResources.Run(context.Background(), json.RawMessage(`{"kind":"templates","cursor":""}`), nil)
	if err != nil || result.IsError {
		t.Fatalf("list resource templates result=%+v err=%v", result, err)
	}
	getPrompt, _ := registry.Get("mcp_bridges_get_prompt")
	result, err = getPrompt.Run(context.Background(), json.RawMessage(`{"name":"review","arguments":{}}`), nil)
	if err != nil || result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[len(result.Content)-1].Text, "review this") {
		t.Fatalf("get prompt result=%+v err=%v", result, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt := manager.runtimes["bridges"]
		rt.mu.Lock()
		state := rt.state
		rt.mu.Unlock()
		if state == stateCached {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("bridge runtime did not idle-disconnect")
}

func TestLazyCachedBridgeRejectsRemovedCapability(t *testing.T) {
	var calls atomic.Int32
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "stale-bridge", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy}, Scope: "project", ProjectIdentity: project}
	seedCatalog(t, root, decl, []string{project}, now, []string{"resources"}, nil)
	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	var routingChanged atomic.Bool
	manager.SetCatalogChanged(func(candidate []tools.ToolDescriptor) error {
		routingChanged.Store(true)
		for _, descriptor := range candidate {
			if descriptor.Schema.Name == "mcp_stale-bridge_list_resources" {
				t.Fatal("routing candidate retained removed bridge")
			}
		}
		return nil
	})
	bridge, ok := registry.Get("mcp_stale-bridge_list_resources")
	if !ok {
		t.Fatal("cached resource bridge missing")
	}
	result, err := bridge.Run(context.Background(), json.RawMessage(`{}`), nil)
	if err != nil || !result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "cached capability changed") {
		t.Fatalf("stale bridge result=%+v err=%v", result, err)
	}
	if _, ok := registry.Get("mcp_stale-bridge_list_resources"); ok {
		t.Fatal("removed live capability remained registered")
	}
	if !routingChanged.Load() {
		t.Fatal("reconnect replacement did not rebuild routing indexes")
	}
}

func TestLazyResourceSubscriptionPinsConnectionUntilUnsubscribe(t *testing.T) {
	var subscribed, unsubscribed atomic.Int32
	var failSecondUnsubscribe atomic.Bool
	failSecondUnsubscribe.Store(true)
	newServer := func() *sdkmcp.Server {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "subscriptions", Version: "1"}, &sdkmcp.ServerOptions{
			Capabilities: &sdkmcp.ServerCapabilities{Resources: &sdkmcp.ResourceCapabilities{Subscribe: true}},
			SubscribeHandler: func(context.Context, *sdkmcp.SubscribeRequest) error {
				subscribed.Add(1)
				return nil
			},
			UnsubscribeHandler: func(_ context.Context, req *sdkmcp.UnsubscribeRequest) error {
				if req.Params.URI == "file:///second" && failSecondUnsubscribe.CompareAndSwap(true, false) {
					return errors.New("temporary unsubscribe failure")
				}
				unsubscribed.Add(1)
				return nil
			},
		})
		server.AddResource(&sdkmcp.Resource{URI: "file:///watched", Name: "watched"}, func(context.Context, *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{}, nil
		})
		return server
	}
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return newServer() }, &sdkmcp.StreamableHTTPOptions{}))
	defer httpServer.Close()
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "subscribed", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	seedCatalog(t, root, decl, []string{project}, now, []string{"resources", "resources.subscribe"}, nil)
	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	bridge, ok := registry.Get("mcp_subscribed_resource_subscription")
	if !ok {
		t.Fatal("cached subscription bridge missing")
	}
	result, err := bridge.Run(context.Background(), json.RawMessage(`{"action":"subscribe","uri":"file:///watched"}`), nil)
	if err != nil || result.IsError || subscribed.Load() != 1 {
		t.Fatalf("subscribe result=%+v err=%v count=%d", result, err, subscribed.Load())
	}
	bridge, ok = registry.Get("mcp_subscribed_resource_subscription")
	if !ok {
		t.Fatal("live subscription bridge missing")
	}
	result, err = bridge.Run(context.Background(), json.RawMessage(`{"action":"subscribe","uri":"file:///watched"}`), nil)
	if err != nil || result.IsError || subscribed.Load() != 1 {
		t.Fatalf("duplicate subscribe result=%+v err=%v count=%d", result, err, subscribed.Load())
	}
	result, err = bridge.Run(context.Background(), json.RawMessage(`{"action":"subscribe","uri":"file:///second"}`), nil)
	if err != nil || result.IsError || subscribed.Load() != 2 {
		t.Fatalf("second subscribe result=%+v err=%v count=%d", result, err, subscribed.Load())
	}
	result, err = bridge.Run(context.Background(), json.RawMessage(`{"action":"unsubscribe","uri":"file:///unknown"}`), nil)
	if err != nil || !result.IsError || unsubscribed.Load() != 0 {
		t.Fatalf("unknown unsubscribe result=%+v err=%v count=%d", result, err, unsubscribed.Load())
	}
	time.Sleep(50 * time.Millisecond)
	rt := manager.runtimes["subscribed"]
	rt.mu.Lock()
	state, active := rt.state, rt.activeCalls
	rt.mu.Unlock()
	if state != stateConnected || active != 2 {
		t.Fatalf("subscribed runtime state=%s active=%d", state, active)
	}
	result, err = bridge.Run(context.Background(), json.RawMessage(`{"action":"unsubscribe","uri":"file:///watched"}`), nil)
	if err != nil || result.IsError || unsubscribed.Load() != 1 {
		t.Fatalf("unsubscribe result=%+v err=%v count=%d", result, err, unsubscribed.Load())
	}
	time.Sleep(30 * time.Millisecond)
	rt.mu.Lock()
	state, active = rt.state, rt.activeCalls
	rt.mu.Unlock()
	if state != stateConnected || active != 1 {
		t.Fatalf("remaining subscription did not pin runtime: state=%s active=%d", state, active)
	}
	result, err = bridge.Run(context.Background(), json.RawMessage(`{"action":"unsubscribe","uri":"file:///second"}`), nil)
	if err != nil || !result.IsError || unsubscribed.Load() != 1 {
		t.Fatalf("failed unsubscribe result=%+v err=%v count=%d", result, err, unsubscribed.Load())
	}
	rt.mu.Lock()
	state, active = rt.state, rt.activeCalls
	rt.mu.Unlock()
	if state != stateConnected || active != 1 {
		t.Fatalf("failed unsubscribe released lease: state=%s active=%d", state, active)
	}
	result, err = bridge.Run(context.Background(), json.RawMessage(`{"action":"unsubscribe","uri":"file:///second"}`), nil)
	if err != nil || result.IsError || unsubscribed.Load() != 2 {
		t.Fatalf("second unsubscribe result=%+v err=%v count=%d", result, err, unsubscribed.Load())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		state = rt.state
		active = rt.activeCalls
		rt.mu.Unlock()
		if state == stateCached && active == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("unsubscribed runtime state=%s active=%d", state, active)
}

func TestLazyAcquireSharesConnectionAndIdleDisconnects(t *testing.T) {
	var calls atomic.Int32
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()

	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "shared", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	rt := manager.runtimes["shared"]

	start := make(chan struct{})
	sessions := make(chan *sdkmcp.ClientSession, 2)
	releases := make(chan func(), 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			session, release, err := rt.acquire(context.Background())
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			sessions <- session
			releases <- release
		})
	}
	close(start)
	wg.Wait()
	close(sessions)
	var first *sdkmcp.ClientSession
	for session := range sessions {
		if first == nil {
			first = session
		} else if first != session {
			t.Fatal("simultaneous cold acquires received different sessions")
		}
	}
	close(releases)
	for release := range releases {
		release()
		release() // lease release is idempotent
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		state, active := rt.state, rt.activeCalls
		rt.mu.Unlock()
		if state == stateCached && active == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	rt.mu.Lock()
	state, active := rt.state, rt.activeCalls
	rt.mu.Unlock()
	t.Fatalf("idle state = %s, active calls = %d", state, active)
}

func TestLazyFailedConnectionIsSharedByCurrentWaiters(t *testing.T) {
	entered := make(chan struct{})
	releaseServer := make(chan struct{})
	var once sync.Once
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(entered) })
		<-releaseServer
		http.Error(w, "server-secret-must-not-leak", http.StatusInternalServerError)
	}))
	defer httpServer.Close()

	root, project := t.TempDir(), t.TempDir()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "failure", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL + "?token=url-secret", Lifecycle: publicmcp.LifecycleLazy}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, time.Now().UTC())
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	rt := manager.runtimes["failure"]

	const waiters = 6
	start := make(chan struct{})
	errs := make(chan error, waiters)
	for range waiters {
		go func() {
			<-start
			_, _, err := rt.acquire(context.Background())
			errs <- err
		}()
	}
	close(start)
	<-entered
	time.Sleep(25 * time.Millisecond)
	close(releaseServer)
	for range waiters {
		err := <-errs
		if err == nil || !strings.Contains(err.Error(), "connect failed") {
			t.Fatalf("shared error = %v", err)
		}
		if strings.Contains(err.Error(), "url-secret") || strings.Contains(err.Error(), "server-secret") {
			t.Fatalf("connection error leaked a secret: %v", err)
		}
	}
	rt.mu.Lock()
	generation := rt.generation
	rt.mu.Unlock()
	if generation != 1 {
		t.Fatalf("shared failure started %d connection generations", generation)
	}
}

func TestCanceledLazyWaiterStillGetsIdleDisconnect(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	releaseServer := make(chan struct{})
	var once sync.Once
	base := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return toolOnlyServer(&calls) }, &sdkmcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-releaseServer
		base.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	root, project := t.TempDir(), t.TempDir()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "canceled", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, time.Now().UTC())
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: root})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	rt := manager.runtimes["canceled"]

	ctx, cancel := context.WithCancel(t.Context())
	waiter := make(chan error, 1)
	go func() {
		_, _, err := rt.acquire(ctx)
		waiter <- err
	}()
	<-entered
	cancel()
	if err := <-waiter; err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("canceled waiter error = %v", err)
	}
	close(releaseServer)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		state := rt.state
		rt.mu.Unlock()
		if state == stateCached {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	rt.mu.Lock()
	state := rt.state
	rt.mu.Unlock()
	t.Fatalf("unleased successful connection remained in state %s", state)
}

func TestLazyResourcePromptCatalogDisconnectsAfterBootstrap(t *testing.T) {
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return testServer() }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	project := t.TempDir()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "fallback", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: t.TempDir()})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	rt := manager.runtimes["fallback"]
	session, release, err := rt.acquire(context.Background())
	if err != nil || session == nil {
		t.Fatalf("fallback acquire: session=%v err=%v", session, err)
	}
	release()
	time.Sleep(40 * time.Millisecond)
	rt.mu.Lock()
	state, eligible := rt.state, rt.lazyEligible
	rt.mu.Unlock()
	if state != stateCached || !eligible {
		t.Fatalf("lazy bridge state=%s lazyEligible=%v", state, eligible)
	}
	for _, name := range []string{"mcp_fallback_list_resources", "mcp_fallback_read_resource", "mcp_fallback_list_prompts", "mcp_fallback_get_prompt"} {
		if _, ok := manager.registry.Get(name); !ok {
			t.Fatalf("cached bridge %q missing after disconnect", name)
		}
	}
}

func TestLazyEmptyCatalogUsesEagerFallback(t *testing.T) {
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return sdkmcp.NewServer(&sdkmcp.Implementation{Name: "empty", Version: "1"}, &sdkmcp.ServerOptions{
			Capabilities: &sdkmcp.ServerCapabilities{Tools: &sdkmcp.ToolCapabilities{ListChanged: true}},
		})
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	project := t.TempDir()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "empty", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: t.TempDir()})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	time.Sleep(40 * time.Millisecond)
	rt := manager.runtimes["empty"]
	rt.mu.Lock()
	state, eligible := rt.state, rt.lazyEligible
	rt.mu.Unlock()
	if state != stateConnected || eligible {
		t.Fatalf("empty fallback state=%s lazyEligible=%v", state, eligible)
	}
}

func TestLazyEmptyCatalogBecomesIdleEligibleAfterFirstTool(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "growing", Version: "1"}, &sdkmcp.ServerOptions{
		Capabilities: &sdkmcp.ServerCapabilities{Tools: &sdkmcp.ToolCapabilities{ListChanged: true}},
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{}))
	defer httpServer.Close()
	project := t.TempDir()
	decl := Declaration{Spec: publicmcp.ServerSpec{ID: "growing", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Lifecycle: publicmcp.LifecycleLazy, IdleTimeoutMS: 10}, Scope: "project", ProjectIdentity: project}
	manager := NewManager(tools.NewRegistry(), Options{CWD: project, Roots: []string{project}, CacheRoot: t.TempDir()})
	manager.Initialize(context.Background(), []Declaration{decl})
	defer manager.Close()
	rt := manager.runtimes["growing"]
	server.AddTool(&sdkmcp.Tool{Name: "first", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		state, eligible := rt.state, rt.lazyEligible
		rt.mu.Unlock()
		if state == stateCached && eligible {
			if _, ok := manager.registry.Get("mcp_growing_first"); !ok {
				t.Fatal("first dynamic tool missing after refresh")
			}
			statuses := manager.Statuses()
			if len(statuses) != 1 || strings.Contains(statuses[0].Message, "eager fallback") {
				t.Fatalf("fallback warning remained after dynamic tool: %+v", statuses)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("runtime remained eager after gaining an activation descriptor")
}

func TestResourceSubscriptionBoundsBeforeTransport(t *testing.T) {
	rt := &serverRuntime{state: stateConnected, subscriptions: make(map[string]struct{})}
	session := new(sdkmcp.ClientSession)
	rt.session = session
	for i := range maxResourceSubscriptions {
		rt.subscriptions[fmt.Sprintf("file:///%d", i)] = struct{}{}
	}
	if err := rt.subscribeResource(context.Background(), session, "file:///overflow"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("subscription limit error = %v", err)
	}
	if err := rt.subscribeResource(context.Background(), session, "file:///"+strings.Repeat("x", maxSubscriptionURIBytes)); err == nil || !strings.Contains(err.Error(), "URI") {
		t.Fatalf("subscription URI limit error = %v", err)
	}
}

func TestCacheExpiryPartitioningAndSymlinkRejection(t *testing.T) {
	now := time.Now().UTC()
	projectA, projectB := t.TempDir(), t.TempDir()
	spec := publicmcp.ServerSpec{ID: "partitioned", Command: "server", Lifecycle: publicmcp.LifecycleLazy}
	globalA := Declaration{Spec: spec, Scope: "global", ProjectIdentity: projectA}
	projectDeclA := Declaration{Spec: spec, Scope: "project", ProjectIdentity: projectA}
	projectDeclB := Declaration{Spec: spec, Scope: "project", ProjectIdentity: projectB}
	keyGlobal, _, _ := cacheIdentity(globalA, []string{projectA})
	keyProjectA, _, _ := cacheIdentity(projectDeclA, []string{projectA})
	keyProjectB, _, _ := cacheIdentity(projectDeclB, []string{projectB})
	if keyGlobal == keyProjectA || keyProjectA == keyProjectB || keyGlobal == keyProjectB {
		t.Fatal("scope/project cache identities collided")
	}
	withArgs := projectDeclA
	withArgs.Spec.Args = []string{"package-a", "--mode=fast", "--token=first-secret"}
	argsKeyA, _, _ := cacheIdentity(withArgs, []string{projectA})
	withArgs.Spec.Args = []string{"low-entropy-positional-secret", "--mode=slow", "--token=first-secret"}
	argsKeyB, _, _ := cacheIdentity(withArgs, []string{projectA})
	if argsKeyA != argsKeyB {
		t.Fatal("positional or flag value influenced cache identity")
	}
	withArgs.Spec.Args = []string{"package-a", "--different=fast", "--token=first-secret"}
	shapeKey, _, _ := cacheIdentity(withArgs, []string{projectA})
	if shapeKey == argsKeyA {
		t.Fatal("argument flag shape was ignored")
	}
	withArgs.Spec.Args = []string{"package-a", "--mode=fast", "--token=second-secret"}
	secretKey, _, _ := cacheIdentity(withArgs, []string{projectA})
	if secretKey != argsKeyA {
		t.Fatal("credential value influenced cache identity")
	}

	root := t.TempDir()
	key, projectHash, fingerprint := cacheIdentity(projectDeclA, []string{projectA})
	cache := newCatalogCache(root)
	expired := cachedCatalog{ServerID: spec.ID, Scope: "project", ProjectIdentityHash: projectHash, ConfigurationFingerprint: fingerprint, CachedAt: now.Add(-defaultCacheAge - time.Hour), Capabilities: []string{"tools"}, Tools: []cachedTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	if err := cache.put(key, expired, now.Add(-defaultCacheAge-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.get(key, now); ok {
		t.Fatal("expired cache entry was reused")
	}
	_ = cache.close()

	symlinkRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(symlinkRoot, "cache")); err != nil {
		t.Fatal(err)
	}
	bad := newCatalogCache(symlinkRoot)
	if _, _, err := bad.get(key, now); err == nil {
		t.Fatal("symlink cache directory was accepted")
	}
}

func TestCacheDoesNotPersistCredentialValues(t *testing.T) {
	root, project := t.TempDir(), t.TempDir()
	now := time.Now().UTC()
	decl := Declaration{Spec: publicmcp.ServerSpec{
		ID: "private", Command: "server", Args: []string{"--token=argument-secret", "positional-secret"},
		Env: map[string]string{"API_TOKEN": "environment-secret"}, Headers: map[string]string{"Authorization": "header-secret"},
		Lifecycle: publicmcp.LifecycleLazy,
	}, Scope: "project", ProjectIdentity: project}
	seedLazyCache(t, root, decl, []string{project}, now)
	data, err := os.ReadFile(filepath.Join(root, "cache", cacheFilename))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"argument-secret", "positional-secret", "environment-secret", "header-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("cache persisted secret %q", secret)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "cache", cacheFilename)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache file mode: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(filepath.Join(root, "cache")); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory mode: info=%v err=%v", info, err)
	}
}
