package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/snow-core/snow/internal/app"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

var errCLIClose = errors.New("cli close failed")

type closeFailingCLIPlugin struct{}

func (closeFailingCLIPlugin) Manifest() publicplugin.Manifest {
	return publicplugin.Manifest{ID: "close-failure", Name: "Close failure", Version: "1", ProtocolVersion: publicplugin.ProtocolVersion}
}
func (closeFailingCLIPlugin) Register(context.Context, publicplugin.Registrar) error { return nil }
func (closeFailingCLIPlugin) Close(context.Context) error                            { return errCLIClose }

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_CLI_HELPER") != "1" {
		return
	}
	os.Args = []string{"snow", "--definitely-invalid"}
	main()
}

func TestCLIPrintsCommandErrorsOnce(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestCLIHelperProcess")
	command.Env = append(os.Environ(), "SNOW_CLI_HELPER=1", "SNOW_HOME="+t.TempDir())
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("invalid command unexpectedly succeeded")
	}
	text := string(output)
	if strings.Count(text, "snow: unknown flag") != 1 || strings.Contains(text, "Error: unknown flag") {
		t.Fatalf("stderr contained duplicate command diagnostics: %q", text)
	}
}

// TestCLIPrintAndJSONEndToEnd drives the actual Cobra command through both
// non-interactive output modes. The provider is a local OpenAI-compatible SSE
// server, so the test covers CLI flag parsing, app wiring, provider streaming,
// the agent loop, and output serialization without requiring credentials or
// network access.
func TestRunPrintPropagatesCloseFailure(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	_, err := captureStdout(t, func() error {
		return runPrint(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), GoPlugins: []publicplugin.Plugin{closeFailingCLIPlugin{}}, NoMCP: true}, "hello", false, false)
	})
	if !errors.Is(err, errCLIClose) {
		t.Fatalf("close error = %v", err)
	}
}

func TestRunPrintRejectsBlankPromptBeforeRuntimeConstruction(t *testing.T) {
	err := runPrint(context.Background(), app.Options{Provider: "definitely-invalid", CWD: t.TempDir()}, " \n\t", true, false)
	if err == nil || !strings.Contains(err.Error(), "requires -p prompt") {
		t.Fatalf("blank prompt error = %v", err)
	}
	if strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("runtime was constructed before prompt validation: %v", err)
	}
}

func TestCLIPrintAndJSONEndToEnd(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
	}{
		{name: "print", mode: "print"},
		{name: "json", mode: "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SNOW_HOME", t.TempDir())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprint(w, `{"data":[{"id":"cli-model"}]}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"cli answer\"}}]}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()

			oldArgs := os.Args
			os.Args = []string{
				"snow", "--provider", "opencode-go", "--model", "cli-model",
				"--api-key", "test-key", "--base-url", server.URL,
				"--permission", "allow", "--no-session", "--mode", tc.mode,
				"-p", "say hello",
			}
			t.Cleanup(func() { os.Args = oldArgs })

			output, err := captureStdout(t, run)
			if err != nil {
				t.Fatalf("CLI returned error: %v; output=%q", err, output)
			}
			if tc.mode == "print" {
				if output != "cli answer\n" {
					t.Fatalf("print output = %q, want %q", output, "cli answer\n")
				}
				return
			}

			var events []protocol.AgentEvent
			scanner := bufio.NewScanner(strings.NewReader(output))
			for scanner.Scan() {
				var ev protocol.AgentEvent
				if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
					t.Fatalf("invalid JSON event %q: %v", scanner.Text(), err)
				}
				events = append(events, ev)
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if len(events) < 3 {
				t.Fatalf("events = %+v, want session/text/done", events)
			}
			var gotText, gotDone bool
			for _, ev := range events {
				if ev.Type == protocol.EvTextDelta && ev.Text == "cli answer" {
					gotText = true
				}
				if ev.Type == protocol.EvTurnDone {
					gotDone = true
				}
			}
			if !gotText || !gotDone {
				t.Fatalf("events = %+v, want text_delta and turn_done", events)
			}
		})
	}
}

func TestParseMCPSpecsCommonManifestAndURL(t *testing.T) {
	specs, err := parseMCPSpecs(`{"mcpServers":{"files":{"command":"mcp-files","args":["--root","."]},"remote":{"type":"http","url":"https://example.test/mcp"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].ID != "files" || specs[0].Command != "mcp-files" || specs[1].ID != "remote" || specs[1].EffectiveTransport() != publicmcp.TransportStreamableHTTP {
		t.Fatalf("specs = %+v", specs)
	}
	urlSpecs, err := parseMCPSpecs("https://tools.example.test/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(urlSpecs) != 1 || urlSpecs[0].ID != "tools-example-test" || urlSpecs[0].Transport != publicmcp.TransportStreamableHTTP {
		t.Fatalf("url specs = %+v", urlSpecs)
	}
}

func TestInspectionCommandsIgnoreConfiguredThinkingLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"thinking":"high"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"snow", "--no-mcp", "--mode", "json", "mcp"},
		{"snow", "--no-skills", "--mode", "json", "skills"},
	} {
		oldArgs := os.Args
		os.Args = args
		_, err := captureStdout(t, run)
		os.Args = oldArgs
		if err != nil {
			t.Fatalf("%v returned error: %v", args, err)
		}
	}
}

func TestMCPManagementLifecycleAndRedaction(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"future":{"preserved":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runCLI(t, "snow", "mcp", "add", "demo", "--env", "TOKEN=secret", "--json", "--", "npx", "--api-key=secret", "demo-mcp"); err != nil {
		t.Fatal(err)
	}
	listed, err := runCLI(t, "snow", "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, "secret") || !strings.Contains(listed, "[redacted]") || !strings.Contains(listed, `"name":"demo"`) {
		t.Fatalf("redacted list = %s", listed)
	}
	if _, err := runCLI(t, "snow", "mcp", "disable", "demo"); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `"future"`) || !strings.Contains(string(cfg), `"disabled": true`) {
		t.Fatalf("updated config = %s", cfg)
	}
	if _, err := runCLI(t, "snow", "mcp", "enable", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "mcp", "remove", "demo"); err != nil {
		t.Fatal(err)
	}
	listed, err = runCLI(t, "snow", "mcp", "list", "--json")
	if err != nil || strings.Contains(listed, `"name":"demo"`) {
		t.Fatalf("post-remove list = %s, err = %v", listed, err)
	}
}

func TestMCPListDoesNotStartConfiguredProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	sentinel := filepath.Join(home, "started")
	configJSON := fmt.Sprintf(`{"mcp_servers":{"danger":{"command":"/bin/sh","args":["-c","touch %s"]}}}`, sentinel)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "mcp", "list", "--json"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("mcp list started configured process: %v", err)
	}
}

func TestMCPCheckReportsLiveNegotiation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "cli-mcp", Version: "1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil
	})
	httpServer := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{Stateless: true}))
	defer httpServer.Close()
	configJSON := fmt.Sprintf(`{"mcp_servers":{"remote":{"url":%q}}}`, httpServer.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "snow", "mcp", "check", "remote", "--json")
	if err != nil || !strings.Contains(out, `"connected":true`) || !strings.Contains(out, `"tool_count":1`) {
		t.Fatalf("check = %s, err = %v", out, err)
	}
}

func TestSkillPolicyCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	if _, err := runCLI(t, "snow", "skills", "disable", "review", "--json"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "skills", "disable", "--all"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"review": false`) || !strings.Contains(string(data), `"disabled": true`) {
		t.Fatalf("skills config = %s", data)
	}
	if _, err := runCLI(t, "snow", "skills", "enable", "review"); err != nil {
		t.Fatal(err)
	}
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	oldArgs := os.Args
	os.Args = args
	defer func() { os.Args = oldArgs }()
	return captureStdout(t, run)
}

// captureStdout keeps command-level tests independent from the process's
// terminal and ensures the CLI's output remains parseable.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = writePipe
	defer func() {
		os.Stdout = old
		_ = readPipe.Close()
	}()

	result := fn()
	if err := writePipe.Close(); err != nil && result == nil {
		result = err
	}
	data, readErr := io.ReadAll(readPipe)
	if result == nil {
		result = readErr
	}
	return string(data), result
}
