//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const runtimeControlsBrowserEnv = "SNOW_WEB_RUNTIME_CONTROLS_BROWSER_DIR"

func init() {
	if directory := os.Getenv(runtimeControlsBrowserEnv); directory != "" && slices.Contains(os.Args, "--mode") {
		code := runtimeControlsBrowserWorker(directory)
		_ = os.WriteFile(filepath.Join(directory, "worker-exit"), []byte(strconv.Itoa(code)), 0600)
		os.Exit(code)
	}
}

// Retain actual manager launch args, CLI parsing, app, RPC and native operations.
// Only the provider and scheduling of one genuine steering ACK are substituted.
func runtimeControlsBrowserWorker(directory string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 90
	}
	if _, err := permissionFixtureProject(directory, cwd); err != nil {
		return 91
	}
	if os.Getenv("HOME") != filepath.Join(directory, "home") || os.Getenv("SNOW_HOME") != filepath.Join(directory, "home", ".snow") || os.Getenv("SNOW_SESSIONS_DIR") != filepath.Join(directory, "sessions") {
		return 92
	}
	opts, startup, err := managerExecutionOptions(os.Args[1:])
	if err != nil {
		return 93
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	if startup == "catalog" {
		if rpc.CatalogMain(ctx, cwd, session.DefaultSessionsRoot(), "runtime-controls-browser") != nil {
			return 94
		}
		return 0
	}
	// Native activation may omit provider/model: only this private HOME supplies
	// fake defaults. Reject explicit nonfixture provider overrides.
	if startup != "eager" || opts.Provider != "" && opts.Provider != "fake" || opts.Model != "" && opts.Model != "fake-1" || !opts.ManagedExplicitGoals || !opts.NoSession || !opts.NoPlugins || opts.NoMCP || !opts.NoSkills || opts.Subagents == nil || !*opts.Subagents {
		return 95
	}
	a, err := app.New(ctx, opts)
	if err != nil {
		return 96
	}
	defer a.Close()
	model := a.Agent.Model()
	if model.ID != "fake-1" {
		return 97
	}
	model.SupportsThinking = true
	model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh}
	model.SupportsReasoningSummary = new(true)
	model.SupportsVerbosity = true
	p := &runtimeControlsBrowserProvider{Provider: fake.NewWithModels([]protocol.Model{model}), directory: directory}
	a.Providers["fake"] = p
	if a.Agent.SetProvider(p) != nil || a.Agent.SetModel(model) != nil {
		return 98
	}
	output := &runtimeControlsBrowserOutput{out: &permissionFixtureOutput{out: os.Stdout}, directory: directory, ctx: ctx}
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			cancel()
			return
		}
		if _, err := output.Write(append(data, '\n')); err != nil {
			cancel()
			return
		}
		if event.Type == protocol.EvCompactionDone && runtimeControlsBrowserFile(directory, "hold-compaction-progress") {
			// Deliver real progress first, then hold native completion cleanup.
			_ = os.WriteFile(filepath.Join(directory, "compaction-progress-seen"), nil, 0600)
			if permissionFixtureGate(ctx, filepath.Join(directory, "release-compaction-progress")) != nil {
				cancel()
			}
		}
	})
	defer unsubscribe()
	if rpc.New(ctx, a, permissionFixtureInput{ReadCloser: os.Stdin}, output).Serve(ctx) != nil {
		return 99
	}
	return 0
}

func runtimeControlsBrowserFile(directory, name string) bool {
	_, err := os.Stat(filepath.Join(directory, name))
	return err == nil
}

type runtimeControlsBrowserOutput struct {
	out       *permissionFixtureOutput
	directory string
	ctx       context.Context
}

func (*runtimeControlsBrowserOutput) RPCWriteBounded() bool { return true }
func (*runtimeControlsBrowserOutput) Fd() uintptr           { return os.Stdout.Fd() }
func (w *runtimeControlsBrowserOutput) Write(data []byte) (int, error) {
	var frame struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return 0, err
	}
	if frame.Type == "response" && frame.Command == "managed_steer" && frame.Success && runtimeControlsBrowserFile(w.directory, "hold-steer-ack") {
		// Buffer genuine bytes only. Let real delivery and turn completion cross
		// the transport before the browser explicitly releases this native ACK.
		ack := slices.Clone(data)
		if err := os.WriteFile(filepath.Join(w.directory, "steer-ack-held"), nil, 0600); err != nil {
			return 0, err
		}
		go func() {
			if permissionFixtureGate(w.ctx, filepath.Join(w.directory, "release-steer-ack")) == nil {
				_, _ = w.out.Write(ack)
			}
		}()
		return len(data), nil
	}
	return w.out.Write(data)
}

type runtimeControlsBrowserProvider struct {
	*fake.Provider
	directory string
	mu        sync.Mutex
}

func (p *runtimeControlsBrowserProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	raw, _ := os.ReadFile(filepath.Join(p.directory, "provider-count"))
	call, _ := strconv.Atoi(string(raw))
	call++
	data, err := json.Marshal(request)
	if err == nil {
		err = os.WriteFile(filepath.Join(p.directory, fmt.Sprintf("request-%d.json", call)), data, 0600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(p.directory, "provider-count"), []byte(strconv.Itoa(call)), 0600)
	}
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := permissionFixtureGate(ctx, filepath.Join(p.directory, fmt.Sprintf("release-%d", call))); err != nil {
		return nil, err
	}
	instruction, err := os.ReadFile(filepath.Join(p.directory, fmt.Sprintf("response-%d", call)))
	if err != nil {
		return nil, err
	}
	var steps []fake.Step
	if strings.HasPrefix(request.System, "Create a factual working-state checkpoint") {
		if len(request.Tools) != 0 {
			return nil, fmt.Errorf("fixture summary unexpectedly exposes tools")
		}
		text := compact.WorkingStateTitle
		for _, section := range compact.WorkingStateSections {
			text += "\n## " + section + "\nFictional browser fixture state; no unfinished action.\n"
		}
		steps = []fake.Step{{Kind: fake.StepText, Text: text}, {Kind: fake.StepDone, Stop: protocol.StopStop}}
	} else if string(instruction) == "permission" {
		steps = []fake.Step{{Kind: fake.StepToolCall, ToolCallID: fmt.Sprintf("browser-write-%d", call), ToolName: "write", Arguments: []byte(`{"path":"fictional-browser-approved.txt","content":"fictional native permission\n"}`)}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}
	} else {
		steps = []fake.Step{{Kind: fake.StepText, Text: "Fictional runtime response: " + string(instruction)}, {Kind: fake.StepDone, Stop: protocol.StopStop}}
	}
	return fake.New(steps).Chat(ctx, request)
}

// Seed exact ordinary SQLite history before opening a runtime; never seed a
// browser snapshot or inject an agent event. Main is Plan, source is Default.
func seedRuntimeControlsBrowser(t *testing.T, directory string) string {
	t.Helper()
	cfg := config.Default()
	cfg.DefaultProvider, cfg.DefaultModel = "fake", "fake-1"
	cfg.Compaction.RetainTokens, cfg.Compaction.MinRetainedTurns = 1, 2
	cfg.Compaction.AutoThresholdPercent, cfg.Compaction.ToolHistoryBudgetPercent = 0, 0
	if err := config.Save(filepath.Join(directory, "home", ".snow", "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	if err := os.MkdirAll(filepath.Join(cwd, ".snow"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".snow", "config.json"), []byte("{\"tui\":{\"theme\":\"nord\"}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(t.Context(), app.Options{CWD: cwd, Provider: "fake", Model: "fake-1", Permission: "ask", NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false), Debug: new(false), ManagedExplicitGoals: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.SetPermissionMode("ask") != nil || a.Agent.SetMode(protocol.ModePlan) != nil {
		t.Fatal("fixture seed authority")
	}
	appendMessage := func(m protocol.Message) {
		t.Helper()
		m.ParentID = a.Session.BranchTip()
		if err := a.Session.Append(session.Entry{ID: m.ID, Type: session.EntryMessage, Message: &m}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 6 {
		appendMessage(protocol.NewUserMessage(fmt.Sprintf("browser-user-%d", i), "", fmt.Sprintf("Fictional saved request %d", i)))
		blocks := []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Fictional saved answer %d", i))}
		if i == 0 {
			blocks = append(blocks, protocol.ContentBlock{Type: protocol.BlockProviderData, Data: []byte("PRIVATE-RUNTIME-CONTINUITY")}, protocol.ContentBlock{Type: protocol.BlockThinking, Text: "PRIVATE-RUNTIME-THINKING"})
		}
		appendMessage(protocol.Message{ID: fmt.Sprintf("browser-assistant-%d", i), Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: blocks})
	}
	if _, err := a.ForkBranch(""); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModeDefault); err != nil {
		t.Fatal(err)
	}
	appendMessage(protocol.NewUserMessage("browser-source-user", "", "Fictional source continuation"))
	appendMessage(protocol.Message{ID: "browser-source-assistant", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{protocol.NewTextBlock("Fictional source answer")}})
	return a.Session.ID()
}

// Opt-in private IPC server. Parent helper starts production web.Run and pairs
// privately; no user daemon, fixed port, provider credentials, or network model.
func TestWebRuntimeControlsBrowserFixture(t *testing.T) {
	directory := os.Getenv(runtimeControlsBrowserEnv)
	if directory == "" {
		t.Skip("opt-in native runtime controls browser fixture")
	}
	f := newRuntimeControlsBrowserHTTP(t, directory)
	t.Setenv(managerExecutionEnv, "")
	t.Setenv(runtimeControlsBrowserEnv, f.directory)
	seed := seedRuntimeControlsBrowser(t, f.directory)
	ready, err := json.Marshal(map[string]any{"origin": f.origin, "cookie": f.cookie, "project": f.project, "session": seed, "provider": "fake", "model": "fake-1"})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "SNOW_RUNTIME_CONTROLS_READY "+string(ready))
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 64), 1024)
		scanner.Scan()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(165 * time.Second):
		t.Fatal("native runtime controls browser did not release its fixture")
	}
}

// Capture only this fixture environment before RuntimeManager freezes it.
func newRuntimeControlsBrowserHTTP(t *testing.T, directory string) *managerExecutionHTTP {
	t.Helper()
	if directory == "" {
		directory = t.TempDir()
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil || !filepath.IsAbs(directory) || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("fixture root must be absolute and private")
	}
	for _, child := range []string{"home", "project-a"} {
		if err := os.Mkdir(filepath.Join(directory, child), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{messageEditFixtureEnv, policyFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv} {
		t.Setenv(name, "")
	}
	t.Setenv(managerExecutionEnv, "")
	t.Setenv(runtimeControlsBrowserEnv, directory)
	t.Setenv("HOME", filepath.Join(directory, "home"))
	t.Setenv("SNOW_HOME", filepath.Join(directory, "home", ".snow"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.Add(t.Context(), "Fictional execution fixture", filepath.Join(directory, "project-a"))
	if err != nil {
		_ = registry.Close()
		t.Fatal(err)
	}
	if err := registry.SetProjectSkills(t.Context(), project, false); err != nil {
		_ = registry.Close()
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	startup := permissionFixtureStartup{output: make(chan string, 1)}
	done := make(chan error, 1)
	go func() {
		done <- web.Run(ctx, web.Options{Listen: "127.0.0.1:0", ManagerDir: filepath.Join(directory, "manager"), Executable: executable, SessionsRoot: filepath.Join(directory, "sessions"), Version: "manager-execution-fixture"}, startup)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("manager shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("owned fixture manager/children shutdown timed out")
		}
	})
	var output string
	select {
	case output = <-startup.output:
	case err := <-done:
		t.Fatalf("manager startup: %v", err)
	case <-ctx.Done():
		t.Fatal("manager startup deadline")
	}
	origin, cookie := permissionFixturePair(t, ctx, output)
	f := &managerExecutionHTTP{t: t, directory: directory, origin: origin, cookie: cookie, project: project.ID, client: &http.Client{Timeout: 15 * time.Second}}
	status, body := f.request("GET", "/", nil)
	if status != http.StatusOK {
		t.Fatalf("paired page: %d", status)
	}
	csrf, err := fixturePageCSRF(body)
	if err != nil {
		t.Fatal(err)
	}
	f.csrf = csrf
	return f
}

func TestWebRuntimeControlsBrowserWorkerLaunch(t *testing.T) {
	f := newRuntimeControlsBrowserHTTP(t, "")
	seed := seedRuntimeControlsBrowser(t, f.directory)
	var snapshot web.RuntimeSnapshot
	f.runtime("open", url.Values{"confirm": {"activate"}, "session_id": {seed}}, &snapshot)
	if snapshot.Provider != "fake" || snapshot.Model != "fake-1" || snapshot.SessionID != seed || !snapshot.ReasoningEnabled || !snapshot.CompactionEnabled || !snapshot.HistoryControlEnabled || snapshot.Status != "idle" {
		t.Fatalf("native fixture launch failed capability/profile contract: %+v", snapshot)
	}
}

// Native browser reproduction: compact immediately after an ordinary prompt,
// without a hidden goal inspection that could silently refresh stale consent.
func TestWebRuntimeControlsBrowserPromptRefreshesCompactionScope(t *testing.T) {
	f := newRuntimeControlsBrowserHTTP(t, "")
	seed := seedRuntimeControlsBrowser(t, f.directory)
	snapshot := f.open(seed)
	f.runtime("prompt", url.Values{"instance_id": {snapshot.InstanceID}, "text": {"Fictional compaction scope request"}}, nil)
	f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 1 && s.Status == "running" })
	f.release(1, "scope completed")
	snapshot = f.wait(func(s web.RuntimeSnapshot) bool {
		return s.Status == "idle" && len(s.Messages) > 0 && strings.Contains(s.Messages[len(s.Messages)-1].Text, "scope completed")
	})
	if snapshot.Goal == nil || snapshot.Goal.TipID != snapshot.Messages[len(snapshot.Messages)-1].SourceID {
		t.Fatal("ordinary completion left manual compaction scope at the previous durable tip")
	}
}
