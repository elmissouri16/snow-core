package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmissouri16/snow-core/internal/tools"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
)

func TestManagerCloseWaitsForAdmittedConnections(t *testing.T) {
	manager := NewManager(tools.NewRegistry(), Options{})
	manager.mu.Lock()
	manager.connectWG.Add(1) // model ConnectAll admission under the manager lock
	manager.mu.Unlock()
	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned during connection: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	manager.mu.Lock()
	manager.connectErr = errors.New("late connection close sentinel")
	manager.mu.Unlock()
	manager.connectWG.Done()
	if err := <-closed; err == nil || !strings.Contains(err.Error(), "late connection close sentinel") {
		t.Fatalf("Close error = %v", err)
	}
}

func TestManagerCloseSerializesWithRuntimeRefresh(t *testing.T) {
	manager := NewManager(tools.NewRegistry(), Options{})
	runtime := &serverRuntime{manager: manager, spec: publicmcp.ServerSpec{ID: "race"}, owner: "mcp:race"}
	manager.runtimes["race"] = runtime
	runtime.mu.Lock() // model an in-flight refresh holding the lifecycle lock
	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned during refresh: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	runtime.mu.Unlock()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if !runtime.closed {
		t.Fatal("runtime was not marked closed")
	}
	if err := runtime.refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("post-close refresh error = %v", err)
	}
}

func TestConnectAllRejectsDuplicateServerIDsBeforeConnecting(t *testing.T) {
	manager := NewManager(tools.NewRegistry(), Options{})
	manager.ConnectAll(context.Background(), []publicmcp.ServerSpec{
		{ID: "duplicate", Disabled: true},
		{ID: "duplicate", Disabled: true},
	})
	statuses := manager.Statuses()
	if len(statuses) != 1 || !strings.Contains(statuses[0].Message, "duplicate") {
		t.Fatalf("statuses = %+v", statuses)
	}
	if len(manager.runtimes) != 0 {
		t.Fatalf("duplicate declarations created runtimes: %d", len(manager.runtimes))
	}
}

func testServer() *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "1.0.0"}, &sdkmcp.ServerOptions{Instructions: "Use echo for exact repetition."})
	server.AddTool(&sdkmcp.Tool{Name: "echo.value", Description: strings.Repeat("Echo a value with detailed usage guidance. ", 12), InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`)}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: args.Value}}, StructuredContent: map[string]any{"echoed": args.Value}}, nil
	})
	server.AddResource(&sdkmcp.Resource{URI: "file:///note", Name: "note", MIMEType: "text/plain"}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: "resource body"}}}, nil
	})
	server.AddPrompt(&sdkmcp.Prompt{Name: "review", Description: "Review code."}, func(_ context.Context, _ *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{Description: "Review code.", Messages: []*sdkmcp.PromptMessage{{Role: sdkmcp.Role("user"), Content: &sdkmcp.TextContent{Text: "review this"}}}}, nil
	})
	return server
}

func TestManagerBridgeNameCollisionGetsStableSuffix(t *testing.T) {
	server := testServer()
	server.AddTool(&sdkmcp.Tool{Name: "list_resources", Description: "Remote collision.", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "remote"}}}, nil
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: t.TempDir(), HostVersion: "test"})
	manager.ConnectAll(context.Background(), []publicmcp.ServerSpec{{ID: "demo", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL}})
	defer manager.Close()
	statuses := manager.Statuses()
	if len(statuses) != 1 || !statuses[0].Connected {
		t.Fatalf("statuses=%+v", statuses)
	}
	if _, ok := registry.Get("mcp_demo_list_resources"); !ok {
		t.Fatal("remote collision tool missing")
	}
	if _, ok := registry.Get("mcp_demo_list_resources_2"); !ok {
		t.Fatalf("resource bridge suffix missing; schemas=%+v", registry.Descriptors())
	}
}

func TestManagerStreamableHTTPNegotiatesLatestAndBridgesCapabilities(t *testing.T) {
	ctx := t.Context()
	server := testServer()
	t.Setenv("SNOW_MCP_TEST_TOKEN", "test-token")
	var sawHeader atomic.Bool
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(req *http.Request) *sdkmcp.Server {
		sawHeader.Store(req.Header.Get("Authorization") == "Bearer test-token")
		return server
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()

	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: t.TempDir(), Roots: []string{t.TempDir()}, HostVersion: "test"})
	manager.ConnectAll(ctx, []publicmcp.ServerSpec{{ID: "demo", Transport: publicmcp.TransportStreamableHTTP, URL: httpServer.URL, Headers: map[string]string{"Authorization": "Bearer ${SNOW_MCP_TEST_TOKEN}"}}})
	defer manager.Close()

	statuses := manager.Statuses()
	if len(statuses) != 1 || !statuses[0].Connected {
		t.Fatalf("statuses = %+v", statuses)
	}
	if statuses[0].ProtocolVersion != "2026-07-28" {
		t.Fatalf("protocol = %q, want latest 2026-07-28", statuses[0].ProtocolVersion)
	}
	if !sawHeader.Load() {
		t.Fatal("Streamable HTTP transport did not apply configured header")
	}
	if !strings.Contains(manager.CatalogPrompt(), "Use echo for exact repetition") {
		t.Fatalf("catalog prompt = %q", manager.CatalogPrompt())
	}

	tool, ok := registry.Get("mcp_demo_echo_value")
	if !ok {
		t.Fatalf("registered tools = %+v", registry.Descriptors())
	}
	result, err := tool.Run(ctx, json.RawMessage(`{"value":"hello"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "hello") {
		t.Fatalf("tool result = %+v, err = %v", result, err)
	}
	desc, _ := registry.Descriptor("mcp_demo_echo_value")
	if !tools.IsDeferred(desc) || desc.Source != tools.SourceMCP {
		t.Fatalf("descriptor = %+v", desc)
	}

	read, ok := registry.Get("mcp_demo_read_resource")
	if !ok {
		t.Fatal("read_resource bridge was not registered")
	}
	if read.Schema().Name != "mcp_demo_read_resource" {
		t.Fatalf("bridge schema name = %q", read.Schema().Name)
	}
	resource, err := read.Run(ctx, json.RawMessage(`{"uri":"file:///note"}`), nil)
	if err != nil || resource.IsError || !strings.Contains(resource.Content[0].Text, "resource body") {
		t.Fatalf("resource = %+v, err = %v", resource, err)
	}
	getPrompt, ok := registry.Get("mcp_demo_get_prompt")
	if !ok {
		t.Fatal("get_prompt bridge was not registered")
	}
	prompt, err := getPrompt.Run(ctx, json.RawMessage(`{"name":"review"}`), nil)
	if err != nil || prompt.IsError || !strings.Contains(prompt.Content[len(prompt.Content)-1].Text, "review this") {
		t.Fatalf("prompt = %+v, err = %v", prompt, err)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_MCP_HELPER") != "1" {
		t.Skip("helper process")
	}
	if notice := os.Getenv("SNOW_MCP_HELPER_STDERR"); notice != "" {
		_, _ = os.Stderr.WriteString(notice)
	}
	if os.Getenv("SNOW_MCP_HELPER_OVERSIZED") == "1" {
		_, _ = os.Stdout.WriteString(strings.Repeat("x", maxCacheBytes+1) + "\n")
		return
	}
	_ = testServer().Run(context.Background(), &sdkmcp.StdioTransport{})
}

func TestManagerStdioServer(t *testing.T) {
	var serverStderr bytes.Buffer
	registry := tools.NewRegistry()
	manager := NewManager(registry, Options{CWD: t.TempDir(), HostVersion: "test", ServerStderr: &serverStderr})
	manager.ConnectAll(context.Background(), []publicmcp.ServerSpec{{
		ID: "local", Transport: publicmcp.TransportStdio, Command: os.Args[0], Args: []string{"-test.run=TestMCPHelperProcess"}, Env: map[string]string{"SNOW_MCP_HELPER": "1", "SNOW_MCP_HELPER_STDERR": "server startup notice\n"},
	}})
	statuses := manager.Statuses()
	if len(statuses) != 1 || !statuses[0].Connected || statuses[0].ProtocolVersion != "2026-07-28" {
		t.Fatalf("statuses = %+v", statuses)
	}
	tool, ok := registry.Get("mcp_local_echo_value")
	if !ok {
		t.Fatalf("schemas = %+v", registry.Descriptors())
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"value":"stdio"}`), nil)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, "stdio") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if got := serverStderr.String(); got != "server startup notice\n" {
		t.Fatalf("captured server stderr = %q", got)
	}
}

func TestManagerRejectsOversizedStdioMessageBeforeDecode(t *testing.T) {
	manager := NewManager(tools.NewRegistry(), Options{CWD: t.TempDir(), HostVersion: "test"})
	manager.ConnectAll(context.Background(), []publicmcp.ServerSpec{{
		ID: "oversized", Transport: publicmcp.TransportStdio, Command: os.Args[0],
		Args: []string{"-test.run=TestMCPHelperProcess"}, Env: map[string]string{"SNOW_MCP_HELPER": "1", "SNOW_MCP_HELPER_OVERSIZED": "1"},
	}})
	defer manager.Close()
	statuses := manager.Statuses()
	if len(statuses) != 1 || statuses[0].State != stateFailed.String() || statuses[0].Connected {
		t.Fatalf("statuses = %+v", statuses)
	}
}

func TestManagerDiscardsStdioServerStderrByDefault(t *testing.T) {
	manager := NewManager(tools.NewRegistry(), Options{})
	if manager.opts.ServerStderr != io.Discard {
		t.Fatal("default MCP server stderr writer is not io.Discard")
	}
}
