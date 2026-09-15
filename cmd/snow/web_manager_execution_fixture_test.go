//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
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
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"github.com/spf13/cobra"
)

const managerExecutionEnv = "SNOW_WEB_MANAGER_EXECUTION_FIXTURE_DIR"
const managerExecutionMarker = "FICTIONAL_MANAGER_PROCESS_READY"

func init() {
	if directory := os.Getenv(managerExecutionEnv); directory != "" && slices.Contains(os.Args, "--mode") {
		code := managerExecutionWorker(directory)
		_ = os.WriteFile(filepath.Join(directory, "worker-exit"), []byte(strconv.Itoa(code)), 0600)
		os.Exit(code)
	}
}

// Parse the actual source-owned profile through buildOptions. Never add tools,
// force permission allow, or set ManagedExplicitGoals behind the launcher.
func managerExecutionOptions(args []string) (app.Options, string, error) {
	cmd := &cobra.Command{}
	for _, name := range []string{"mode", "rpc-startup", "permission", "provider", "model"} {
		cmd.Flags().String(name, "", "")
	}
	for _, name := range []string{"no-session", "no-plugins", "no-mcp", "no-skills", "no-subagents", "no-debug", "managed-explicit-goals"} {
		cmd.Flags().Bool(name, false, "")
	}
	cmd.Flags().StringSlice("tools", nil, "")
	if err := cmd.ParseFlags(args); err != nil {
		return app.Options{}, "", err
	}
	mode, _ := cmd.Flags().GetString("mode")
	startup, _ := cmd.Flags().GetString("rpc-startup")
	if mode != "rpc" || cmd.Flags().NArg() != 0 || cmd.Flags().Changed("permission") {
		return app.Options{}, "", errors.New("not an exact manager worker")
	}
	opts, err := buildOptions(cmd)
	return opts, startup, err
}

func managerExecutionWorker(directory string) int {
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if startup == "catalog" {
		if err := rpc.CatalogMain(ctx, cwd, session.DefaultSessionsRoot(), "manager-execution-fixture"); err != nil {
			return 94
		}
		return 0
	}
	if startup != "eager" || opts.Provider != "fake" || opts.Model != "fake-1" || !opts.ManagedExplicitGoals || !opts.NoSession || !opts.NoPlugins || !opts.NoMCP || !opts.NoSkills || opts.Subagents == nil || *opts.Subagents || opts.Debug == nil || *opts.Debug {
		return 95
	}
	for _, name := range []string{"get_goal", "create_goal", "update_goal", "process_start", "process_status", "process_logs", "process_stop", "process_list"} {
		if !slices.Contains(opts.Tools, name) {
			return 96
		}
	}
	a, err := app.New(ctx, opts)
	if err != nil {
		return 97
	}
	defer a.Close()
	p := &managerExecutionProvider{Provider: fake.New(nil), directory: directory}
	a.Providers["fake"] = p
	if err := a.SetProvider("fake"); err != nil {
		return 98
	}
	output := &permissionFixtureOutput{out: os.Stdout}
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		// Native event identities are evidence; no fabricated RPC goal lifecycle.
		if event.Type == protocol.EvTurnDone || event.Type == protocol.EvThreadGoalUpdated {
			data, _ := json.Marshal(event)
			f, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				cancel()
				return
			}
			_, err = f.Write(append(data, '\n'))
			_ = f.Close()
			if err != nil {
				cancel()
				return
			}
		}
		data, err := json.Marshal(event)
		if err != nil {
			cancel()
			return
		}
		if _, err := output.Write(append(data, '\n')); err != nil {
			cancel()
		}
	})
	defer unsubscribe()
	if err := rpc.New(ctx, a, permissionFixtureInput{ReadCloser: os.Stdin}, output).Serve(ctx); err != nil {
		return 99
	}
	return 0
}

type managerExecutionProvider struct {
	*fake.Provider
	directory string
	mu        sync.Mutex
}

func (p *managerExecutionProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	countPath := filepath.Join(p.directory, "provider-count")
	previous, _ := os.ReadFile(countPath)
	call, _ := strconv.Atoi(string(previous))
	call++
	if err := os.WriteFile(countPath, []byte(strconv.Itoa(call)), 0600); err != nil {
		return nil, err
	}
	// Only fictional context is written, into the private isolated fixture root.
	data, err := json.Marshal(request.Messages)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(p.directory, fmt.Sprintf("context-%d.json", call)), data, 0600); err != nil {
		return nil, err
	}
	// Every provider entry is gated, including followups, making stop races and
	// browser evidence deterministic without altering the agent scheduler.
	if err := permissionFixtureGate(ctx, filepath.Join(p.directory, fmt.Sprintf("release-%d", call))); err != nil {
		return nil, err
	}
	instruction, err := os.ReadFile(filepath.Join(p.directory, fmt.Sprintf("response-%d", call)))
	if err != nil {
		return nil, err
	}
	var steps []fake.Step
	switch string(instruction) {
	case "process":
		args, _ := json.Marshal(map[string]any{"command": "trap 'printf stopped > fictional-process-stopped; exit 0' TERM; printf '" + managerExecutionMarker + "\\n'; i=0; while [ \"$i\" -lt 120 ]; do sleep 1; i=$((i+1)); done", "name": "fictional-manager-worker", "readiness": map[string]any{"type": "log", "pattern": managerExecutionMarker, "timeout_ms": 2000}})
		steps = []fake.Step{{Kind: fake.StepToolCall, ToolCallID: fmt.Sprintf("process-%d", call), ToolName: "process_start", Arguments: args}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}
	case "create":
		steps = []fake.Step{{Kind: fake.StepToolCall, ToolCallID: "unsolicited-goal", ToolName: "create_goal", Arguments: []byte(`{"objective":"Fictional unsolicited invisible work"}`)}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}
	case "complete":
		selected, err := os.ReadFile(filepath.Join(p.directory, "selected-goal"))
		if err != nil {
			return nil, err
		}
		args, _ := json.Marshal(map[string]string{"goal_id": string(selected), "status": "complete"})
		steps = []fake.Step{{Kind: fake.StepToolCall, ToolCallID: fmt.Sprintf("complete-%d", call), ToolName: "update_goal", Arguments: args}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}
	default:
		steps = []fake.Step{{Kind: fake.StepText, Text: fmt.Sprintf("Fictional progress %d: %s", call, instruction)}}
	}
	return fake.New(steps).Chat(ctx, request)
}

type managerExecutionHTTP struct {
	t                                        *testing.T
	directory, origin, cookie, csrf, project string
	client                                   *http.Client
}

func newManagerExecutionHTTP(t *testing.T, directory string) *managerExecutionHTTP {
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
	t.Setenv(managerExecutionEnv, directory)
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

func (f *managerExecutionHTTP) request(method, path string, values url.Values) (int, []byte) {
	f.t.Helper()
	if values != nil {
		values = values.Clone()
		values.Set("csrf", f.csrf)
	}
	req, err := http.NewRequestWithContext(f.t.Context(), method, f.origin+path, strings.NewReader(values.Encode()))
	if err != nil {
		f.t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "snow_manager_local_session", Value: f.cookie})
	if method == "POST" {
		req.Header.Set("Origin", f.origin)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := f.client.Do(req)
	if err != nil {
		f.t.Fatal("fixture HTTP unavailable:", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		f.t.Fatal(err)
	}
	return res.StatusCode, body
}
func (f *managerExecutionHTTP) runtime(action string, values url.Values, out any) {
	f.t.Helper()
	code, body := f.request("POST", "/projects/"+f.project+"/runtime/"+action, values)
	if code != http.StatusOK {
		exit, _ := os.ReadFile(filepath.Join(f.directory, "worker-exit"))
		f.t.Fatalf("%s: HTTP %d: %s (worker exit %s)", action, code, body, exit)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			f.t.Fatal(err)
		}
	}
}
func (f *managerExecutionHTTP) open(sessionID string) web.RuntimeSnapshot {
	f.t.Helper()
	var s web.RuntimeSnapshot
	f.runtime("open", url.Values{"confirm": {"activate"}, "provider": {"fake"}, "model": {"fake-1"}, "session_id": {sessionID}}, &s)
	return s
}
func (f *managerExecutionHTTP) snapshot() web.RuntimeSnapshot {
	f.t.Helper()
	code, body := f.request("GET", "/projects/"+f.project+"/runtime", nil)
	var s web.RuntimeSnapshot
	if code != 200 || json.Unmarshal(body, &s) != nil {
		f.t.Fatalf("snapshot: %d %s", code, body)
	}
	return s
}
func (f *managerExecutionHTTP) wait(check func(web.RuntimeSnapshot) bool) web.RuntimeSnapshot {
	f.t.Helper()
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	for {
		s := f.snapshot()
		if check(s) {
			return s
		}
		select {
		case <-deadline.C:
			f.t.Fatalf("snapshot deadline: status=%s goal=%+v permission=%+v calls=%d", s.Status, s.Goal, s.Permission, f.count())
		case <-f.t.Context().Done():
			f.t.Fatal("fixture canceled")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func (f *managerExecutionHTTP) count() int {
	data, _ := os.ReadFile(filepath.Join(f.directory, "provider-count"))
	n, _ := strconv.Atoi(string(data))
	return n
}
func (f *managerExecutionHTTP) write(name, value string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.directory, name), []byte(value), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *managerExecutionHTTP) release(call int, response string) {
	f.write(fmt.Sprintf("response-%d", call), response)
	f.write(fmt.Sprintf("release-%d", call), "release")
}
func (f *managerExecutionHTTP) inspect(s web.RuntimeSnapshot) web.RuntimeSnapshot {
	f.t.Helper()
	branch := ""
	if s.Goal != nil {
		branch = s.Goal.BranchID
	}
	f.runtime("goal-inspect", url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "branch_id": {branch}}, &s)
	return s
}
func managerExecutionGoalForm(s web.RuntimeSnapshot) url.Values {
	return url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "branch_id": {s.Goal.BranchID}, "expected_revision": {strconv.FormatUint(s.Revision, 10)}, "expected_tip_id": {s.Goal.TipID}, "expected_goal_id": {s.Goal.GoalID}}
}
func (f *managerExecutionHTTP) mode(s web.RuntimeSnapshot, mode string) web.RuntimeSnapshot {
	f.runtime("mode", url.Values{"instance_id": {s.InstanceID}, "mode": {mode}}, &s)
	return s
}
func (f *managerExecutionHTTP) permissionMode(s web.RuntimeSnapshot, mode string) web.RuntimeSnapshot {
	v := url.Values{"instance_id": {s.InstanceID}, "mode": {mode}}
	if mode == "allow" {
		v.Set("confirm_allow", "allow")
	}
	f.runtime("permission-mode", v, &s)
	return s
}

// Optional production browser server. IPC contains a pairing cookie and must be
// consumed privately, never printed by the runner or retained in screenshots.
func TestWebManagerExecutionBrowserFixture(t *testing.T) {
	directory := os.Getenv(managerExecutionEnv)
	if directory == "" {
		t.Skip("opt-in isolated manager execution browser fixture")
	}
	f := newManagerExecutionHTTP(t, directory)
	ready, err := json.Marshal(map[string]any{"origin": f.origin, "cookie": f.cookie, "project": f.project, "directory": f.directory, "provider": "fake", "model": "fake-1", "marker": managerExecutionMarker, "provider_count": "provider-count", "release_pattern": "release-%d", "response_pattern": "response-%d", "selected_goal": "selected-goal"})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "SNOW_MANAGER_EXECUTION_READY "+string(ready))
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 64), 1024)
		scanner.Scan()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(150 * time.Second):
		t.Fatal("browser fixture deadline")
	}
}

func TestWebManagerExecutionProfileOptions(t *testing.T) {
	args := []string{"--mode", "rpc", "--rpc-startup", "eager", "--managed-explicit-goals", "--no-session", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents", "--no-debug", "--provider", "fake", "--model", "fake-1", "--tools", "get_goal,create_goal,update_goal,process_start,process_status,process_logs,process_stop,process_list"}
	opts, startup, err := managerExecutionOptions(args)
	if err != nil || startup != "eager" || !opts.ManagedExplicitGoals || opts.Permission != "" || len(opts.Tools) != 8 {
		t.Fatalf("source profile not honored: %+v %v", opts, err)
	}
	without := slices.Delete(slices.Clone(args), 4, 5)
	opts, _, err = managerExecutionOptions(without)
	if err != nil || opts.ManagedExplicitGoals {
		t.Fatal("helper silently enabled missing source policy")
	}
	for _, bad := range [][]string{append(slices.Clone(args), "--permission", "allow"), append(slices.Clone(args), "--invented"), {"--mode", "print"}} {
		if _, _, err := managerExecutionOptions(bad); err == nil {
			t.Fatal("fixture accepted untrusted launch override")
		}
	}
}
