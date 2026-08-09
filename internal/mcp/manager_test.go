package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/snow-core/snow/internal/tools"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

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

func TestManagerStreamableHTTPNegotiatesLatestAndBridgesCapabilities(t *testing.T) {
	ctx := context.Background()
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
		t.Fatalf("registered tools = %+v", registry.Schemas())
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
		t.Fatalf("schemas = %+v", registry.Schemas())
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

func TestManagerDiscardsStdioServerStderrByDefault(t *testing.T) {
	manager := NewManager(tools.NewRegistry(), Options{})
	if manager.opts.ServerStderr != io.Discard {
		t.Fatal("default MCP server stderr writer is not io.Discard")
	}
}
