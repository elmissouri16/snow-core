package main

import (
	"bufio"
	"context"
	"database/sql"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var errCLIClose = errors.New("cli close failed")

func TestPrintableGoalBlockedReason(t *testing.T) {
	if got := printableGoalBlockedReason("  CI\x1b[31m\n unavailable  "); got != "CI[31m unavailable" {
		t.Fatalf("printable reason = %q", got)
	}
	if got := printableGoalBlockedReason("\x00\x1b"); got != "No blocker reason was recorded." {
		t.Fatalf("empty printable reason = %q", got)
	}
}

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

func TestBuildOptionsReadsSubagentModel(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("provider", "", "")
	cmd.Flags().String("model", "", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("permission", "", "")
	cmd.Flags().String("session", "", "")
	cmd.Flags().Bool("no-session", false, "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("auth", "", "")
	cmd.Flags().String("thinking", "", "")
	cmd.Flags().StringSlice("tools", nil, "")
	cmd.Flags().String("collaboration-mode", "", "")
	cmd.Flags().Bool("no-plugins", false, "")
	cmd.Flags().Bool("no-mcp", false, "")
	cmd.Flags().StringArray("skill-dir", nil, "")
	cmd.Flags().Bool("no-skills", false, "")
	cmd.Flags().Bool("subagents", false, "")
	cmd.Flags().Bool("no-subagents", false, "")
	cmd.Flags().String("subagent-provider", "", "")
	cmd.Flags().String("subagent-model", "", "")
	cmd.Flags().Int("subagent-max-concurrency", 0, "")
	cmd.Flags().Int("subagent-max-agents", 0, "")
	cmd.Flags().Int("subagent-max-depth", 0, "")
	cmd.Flags().StringArray("plugin", nil, "")
	cmd.Flags().StringArray("mcp", nil, "")
	if err := cmd.Flags().Set("subagent-provider", "opencode-go"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("subagent-model", "model-x"); err != nil {
		t.Fatal(err)
	}
	opts, err := buildOptions(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if opts.SubagentProvider != "opencode-go" || opts.SubagentModel != "model-x" {
		t.Fatalf("subagent selection = %s/%s", opts.SubagentProvider, opts.SubagentModel)
	}
	if opts.BuildVersion != version {
		t.Fatalf("build version = %q, want %q", opts.BuildVersion, version)
	}
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

func TestCLIResumeLatestSession(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	t.Setenv("SNOW_SESSIONS_DIR", t.TempDir())
	cwd := mustCWD()
	idx := session.NewFileIndex(session.DefaultSessionsRoot())
	create := func(sessionCWD, id, text string) string {
		t.Helper()
		st, err := idx.Create(sessionCWD)
		if err != nil {
			t.Fatal(err)
		}
		message := protocol.NewUserMessage(id, "root", text)
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
		path := st.Path()
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldPath := create(cwd, "resume-old", "older current session")
	latestPath := create(cwd, "resume-latest", "latest current session")
	foreignCWD := t.TempDir()
	foreignPath := create(foreignCWD, "resume-foreign", "newer foreign session")
	unrelatedPath := filepath.Join(session.DefaultSessionsRoot(), session.EncodeCWD(cwd), "unrelated.db")
	unrelatedDB, err := sql.Open("sqlite", unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unrelatedDB.Exec(`CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated(value) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if err := unrelatedDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unrelatedPath, 0o644); err != nil {
		t.Fatal(err)
	}
	unrelatedBefore, err := os.ReadFile(unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	for path, timestamp := range map[string]time.Time{
		oldPath:       time.Unix(1000, 0),
		latestPath:    time.Unix(2000, 0),
		foreignPath:   time.Unix(3000, 0),
		unrelatedPath: time.Unix(4000, 0),
	} {
		if err := os.Chtimes(path, timestamp, timestamp); err != nil {
			t.Fatal(err)
		}
	}

	output, err := runCLI(t, "snow", "resume", "--provider", "fake", "--permission", "allow", "--no-plugins", "--no-mcp", "--no-skills", "-p", "continue here")
	if err != nil {
		t.Fatalf("snow resume failed: %v; output=%q", err, output)
	}

	transcript := func(path, sessionCWD string) string {
		t.Helper()
		st, err := session.NewSQLiteStore(path, sessionCWD, session.Options{})
		if err != nil {
			t.Fatal(err)
		}
		messages, err := st.Messages()
		if closeErr := st.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, message := range messages {
			for _, block := range message.Content {
				text.WriteString(block.Text)
				text.WriteByte('\n')
			}
		}
		return text.String()
	}
	if text := transcript(latestPath, cwd); !strings.Contains(text, "latest current session") || !strings.Contains(text, "continue here") {
		t.Fatalf("latest transcript = %q", text)
	}
	if text := transcript(oldPath, cwd); strings.Contains(text, "continue here") {
		t.Fatalf("older session was resumed: %q", text)
	}
	if text := transcript(foreignPath, foreignCWD); strings.Contains(text, "continue here") {
		t.Fatalf("foreign session was resumed: %q", text)
	}
	unrelatedAfter, err := os.ReadFile(unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(unrelatedAfter) != string(unrelatedBefore) {
		t.Fatal("no-argument resume changed an unrelated indexed database")
	}
	info, err := os.Stat(unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("no-argument resume changed unrelated database mode to %o", info.Mode().Perm())
	}
}

func TestCLIResumeRejectsMissingOrConflictingSession(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	t.Setenv("SNOW_SESSIONS_DIR", t.TempDir())

	if _, err := runCLI(t, "snow", "resume", "--provider", "fake", "-p", "hello"); err == nil || !strings.Contains(err.Error(), "no saved sessions") {
		t.Fatalf("empty resume error = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, err := runCLI(t, "snow", "resume", missing, "--provider", "fake", "-p", "hello"); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing resume error = %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing resume created a database: %v", err)
	}
	if _, err := runCLI(t, "snow", "resume", missing, "--session", missing, "--provider", "fake", "-p", "hello"); err == nil || !strings.Contains(err.Error(), "both as an argument") {
		t.Fatalf("conflicting resume error = %v", err)
	}

	unrelated := filepath.Join(t.TempDir(), "unrelated.db")
	db, err := sql.Open("sqlite", unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated(value) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unrelated, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "snow", "resume", unrelated, "--provider", "fake", "-p", "hello"); err == nil || !strings.Contains(err.Error(), "invalid sqlite session metadata") {
		t.Fatalf("unrelated database error = %v", err)
	}
	after, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("resume changed an unrelated SQLite database")
	}
	info, err := os.Stat(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("resume changed unrelated database mode to %o", info.Mode().Perm())
	}

	if _, err := runCLI(t, "snow", "resume", "--no-session", "--provider", "fake", "-p", "hello"); err == nil || !strings.Contains(err.Error(), "--no-session") {
		t.Fatalf("ephemeral resume error = %v", err)
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

func TestCLIOpenAICompatiblePrint(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"responses-model"}]}`)
		case "/opencode/models":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/v1/responses":
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("authorization=%q", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"responses answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.json")
	configBody := fmt.Sprintf(`{"providers":{"opencode-go":{"base_url":%q}}}`, server.URL+"/opencode")
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	os.Args = []string{"snow", "--provider", "openai-compatible", "--model", "responses-model", "--base-url", server.URL + "/v1", "--api-key", "test-key", "--config", configPath, "--permission", "allow", "--no-session", "--mode", "print", "-p", "hello"}
	t.Cleanup(func() { os.Args = oldArgs })
	output, err := captureStdout(t, run)
	if err != nil || output != "responses answer\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}

	os.Args = []string{"snow", "--provider", "openai-compatible", "--model", "responses-model", "--base-url", server.URL + "/v1", "--api-key", "test-key", "--config", configPath, "--permission", "allow", "--no-session", "--mode", "json", "-p", "hello"}
	output, err = captureStdout(t, run)
	if err != nil || !strings.Contains(output, `"type":"text_delta"`) || !strings.Contains(output, `"text":"responses answer"`) {
		t.Fatalf("json output=%q err=%v", output, err)
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

	if _, err := runCLI(t, "snow", "mcp", "add", "demo", "--env", "TOKEN=secret", "--lifecycle", "lazy", "--cache-bootstrap", "explicit", "--json", "--", "npx", "--api-key=secret", "demo-mcp"); err != nil {
		t.Fatal(err)
	}
	listed, err := runCLI(t, "snow", "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, "secret") || !strings.Contains(listed, "[redacted]") || !strings.Contains(listed, `"name":"demo"`) || !strings.Contains(listed, `"cache_bootstrap":"explicit"`) {
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
	configJSON := fmt.Sprintf(`{"mcp_servers":{"remote":{"url":%q,"lifecycle":"lazy"}}}`, httpServer.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "snow", "mcp", "check", "remote", "--json")
	if err != nil || !strings.Contains(out, `"connected":true`) || !strings.Contains(out, `"tool_count":1`) {
		t.Fatalf("check = %s, err = %v", out, err)
	}
}

func TestMCPCacheStatusStrictMissDoesNotStartServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	sentinel := filepath.Join(home, "started")
	configJSON := fmt.Sprintf(`{"mcp_servers":{"strict":{"command":"/bin/sh","args":["-c","touch %s"],"lifecycle":"lazy","cache_bootstrap":"explicit"}}}`, sentinel)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "snow", "mcp", "cache", "status", "strict", "--json")
	if err != nil || !strings.Contains(out, `"state":"missing"`) {
		t.Fatalf("cache status = %s, err = %v", out, err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("cache status started server: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
		t.Fatalf("cache status mutated an absent cache directory: %v", err)
	}
}

func TestMCPCacheRefreshRejectsInvalidOriginalConfigBeforeTransport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	var requests atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer httpServer.Close()
	for _, declaration := range []string{
		fmt.Sprintf(`{"url":%q,"lifecycle":"invalid"}`, httpServer.URL),
		fmt.Sprintf(`{"url":%q,"lifecycle":"eager","idle_timeout_ms":1000}`, httpServer.URL),
		fmt.Sprintf(`{"url":%q,"lifecycle":"eager","cache_bootstrap":"explicit"}`, httpServer.URL),
	} {
		configJSON := fmt.Sprintf(`{"mcp_servers":{"invalid":%s}}`, declaration)
		if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := runCLI(t, "snow", "mcp", "cache", "refresh", "invalid", "--json"); err == nil {
			t.Fatalf("invalid refresh succeeded: %s", out)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid refresh made %d transport requests", requests.Load())
	}
}

func TestMCPCacheRefreshStatusAndClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	var requests atomic.Int32
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "cache-cli", Version: "1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{}, nil
	})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	configJSON := fmt.Sprintf(`{"mcp_servers":{"remote":{"url":%q,"lifecycle":"lazy","cache_bootstrap":"explicit"}}}`, httpServer.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "snow", "mcp", "cache", "refresh", "remote", "--json")
	if err != nil || !strings.Contains(out, `"state":"valid"`) || !strings.Contains(out, `"tool_count":1`) || requests.Load() == 0 {
		t.Fatalf("cache refresh = %s, err = %v, requests=%d", out, err, requests.Load())
	}
	out, err = runCLI(t, "snow", "mcp", "cache", "status", "remote", "--json")
	if err != nil || !strings.Contains(out, `"state":"valid"`) {
		t.Fatalf("cache status = %s, err = %v", out, err)
	}
	out, err = runCLI(t, "snow", "mcp", "cache", "clear", "remote", "--json")
	if err != nil || !strings.Contains(out, `"removed":1`) {
		t.Fatalf("cache clear = %s, err = %v", out, err)
	}
	out, err = runCLI(t, "snow", "mcp", "cache", "status", "remote", "--json")
	if err != nil || !strings.Contains(out, `"state":"missing"`) {
		t.Fatalf("post-clear status = %s, err = %v", out, err)
	}
}

func TestForkCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "source.db")
	store, err := session.NewSQLiteStore(path, cwd, session.Options{Name: "source"})
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewUserMessage("u1", "root", "hello")
	if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	forkJSON, err := runCLI(t, "snow", "fork", path, "--source-branch", "main", "--from-entry", "u1", "--name", "independent")
	if err != nil {
		t.Fatal(err)
	}
	var result protocol.SessionForkResult
	if err := json.Unmarshal([]byte(forkJSON), &result); err != nil {
		t.Fatal(err)
	}
	if result.SourceSessionID == result.SessionID || result.Name != "independent" {
		t.Fatalf("result=%+v", result)
	}
	if err := session.ValidateSQLiteSession(result.SessionPath); err != nil {
		t.Fatal(err)
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
