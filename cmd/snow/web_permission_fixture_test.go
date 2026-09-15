//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const permissionFixtureEnv = "SNOW_WEB_PERMISSION_FIXTURE_DIR"

// This test-only entry point accepts only the manager's exact worker arguments.
// It neither extends the CLI nor enables a production failure-injection hook.
func init() {
	if os.Getenv(permissionFixtureEnv) == "" || !slices.Contains(os.Args, "--mode") {
		return
	}
	os.Exit(runPermissionFixtureWorker())
}

func permissionFixtureWorkerArgs(args []string) (catalog, valid bool) {
	if slices.Equal(args, []string{"--mode", "rpc", "--rpc-startup", "catalog"}) {
		return true, true
	}
	base := []string{"--mode", "rpc", "--rpc-startup", "eager", "--no-session", "--managed-explicit-goals", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents", "--no-debug", "--tools", "read,glob,grep,write,edit,bash,ask_user,get_goal,create_goal,update_goal,process_start,process_status,process_logs,process_stop,process_list"}
	if len(args) < len(base) || !slices.Equal(args[:len(base)], base) {
		return false, false
	}
	suffix := args[len(base):]
	// The manager may append its explicit local selection on reactivation.
	return false, len(suffix) == 0 || slices.Equal(suffix, []string{"--provider", "fake"}) || slices.Equal(suffix, []string{"--model", "fake-1"}) || slices.Equal(suffix, []string{"--provider", "fake", "--model", "fake-1"})
}

func permissionFixtureProject(directory, cwd string) (string, error) {
	if !filepath.IsAbs(directory) {
		return "", errors.New("fixture directory must be absolute")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", errors.New("fixture directory must be a private regular directory")
	}
	for _, project := range []string{"a", "b"} {
		root := filepath.Join(directory, "project-"+project)
		info, err := os.Lstat(root)
		if err == nil && info.IsDir() && info.Mode().Perm() == 0700 && cwd == root {
			return project, nil
		}
	}
	return "", errors.New("worker CWD must be one of the two private fixture projects")
}

func runPermissionFixtureWorker() int {
	catalog, valid := permissionFixtureWorkerArgs(os.Args[1:])
	if !valid {
		return 80
	}
	directory := os.Getenv(permissionFixtureEnv)
	cwd, err := os.Getwd()
	if err != nil {
		return 81
	}
	project, err := permissionFixtureProject(directory, cwd)
	if err != nil {
		return 82
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()
	// A private marker kills this process only; no PID input or HTTP hook exists.
	// The deadline also exits a worker whose transport is blocked in stdin.
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if !catalog && permissionFixtureMarker(filepath.Join(directory, project+"-kill"), "kill") {
				_ = os.Remove(filepath.Join(directory, project+"-kill"))
				os.Exit(42)
			}
			select {
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					os.Exit(43)
				}
				return
			case <-ticker.C:
			}
		}
	}()
	if catalog {
		if err := rpc.CatalogMain(ctx, cwd, session.DefaultSessionsRoot(), "permission-fixture"); err != nil {
			return 83
		}
		return 0
	}
	// Fresh sessions default to ask in startup_config.go. Omit the launch override
	// so session_open can restore the active session's persisted permission policy.
	// RPC installs the normal interactive broker. Only the existing builtin write
	// is exposed, with app's pinned CWD guard and no additional allowed roots.
	a, err := app.New(ctx, app.Options{CWD: cwd, Provider: "fake", Model: "fake-1", NoSession: true, Tools: []string{"write"}, NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false), Debug: new(false)})
	if err != nil {
		return 84
	}
	defer a.Close()
	evidence := &permissionFixtureEvidence{directory: directory, project: project}
	if err := evidence.restore(); err != nil {
		return 85
	}
	if err := installPermissionFixture(a, evidence, cwd); err != nil {
		return 86
	}
	output := &permissionFixtureOutput{out: os.Stdout}
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
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
		return 89
	}
	return 0
}

func installPermissionFixture(a *app.App, evidence *permissionFixtureEvidence, cwd string) error {
	descriptors := a.Registry.Descriptors()
	owner := ""
	for i := range descriptors {
		if descriptors[i].Schema.Name == "write" {
			owner = descriptors[i].Owner
			// Preserve the original descriptor's risk, effect, schema and owner. The
			// decorator delegates the actual write and never replaces authorization.
			descriptors[i].Tool = &permissionFixtureWrite{Tool: descriptors[i].Tool, evidence: evidence}
		}
	}
	if owner == "" {
		return errors.New("fixture builtin write is unavailable")
	}
	replacement := slices.DeleteFunc(descriptors, func(d tools.ToolDescriptor) bool { return d.Owner != owner })
	if err := a.Registry.ReplaceOwner(owner, replacement, nil); err != nil {
		return err
	}
	p := &permissionFixtureProvider{Provider: fake.New(nil), evidence: evidence, cwd: cwd}
	a.Providers["fake"] = p
	if err := a.SetProvider("fake"); err != nil {
		return err
	}
	return nil
}

type permissionFixtureInput struct{ io.ReadCloser }

func (permissionFixtureInput) InterruptsReadOnClose() bool { return true }

type permissionFixtureOutput struct {
	mu  sync.Mutex
	out io.Writer
}

func (*permissionFixtureOutput) RPCWriteBounded() bool { return true }
func (w *permissionFixtureOutput) Write(data []byte) (int, error) {
	payload := slices.Clone(data)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() { w.mu.Lock(); defer w.mu.Unlock(); n, err := w.out.Write(payload); done <- result{n, err} }()
	select {
	case result := <-done:
		return result.n, result.err
	case <-time.After(2 * time.Second):
		return 0, errors.New("fixture RPC output deadline")
	}
}

// All fixture evidence is private, durable, bounded, and separate from project
// files. Counters survive worker restarts so duplicate execution cannot hide
// behind an overwritten reached marker.
type permissionFixtureEvidence struct {
	mu                 sync.Mutex
	directory, project string
	calls, executions  int
}
type permissionFixtureRecord struct {
	Event   string `json:"event"`
	Phase   string `json:"phase"`
	Token   string `json:"token"`
	Count   int    `json:"count"`
	IsError bool   `json:"is_error"`
}

func (e *permissionFixtureEvidence) path(name string) string {
	return filepath.Join(e.directory, e.project+"-"+name)
}
func (e *permissionFixtureEvidence) restore() error {
	for _, name := range []string{"calls.jsonl", "executions.jsonl"} {
		file, err := os.Open(e.path(name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			_ = file.Close()
			return errors.New("fixture evidence limit")
		}
		scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
		for scanner.Scan() {
			var record permissionFixtureRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				_ = file.Close()
				return err
			}
			if name == "calls.jsonl" {
				e.calls = max(e.calls, record.Count)
			} else {
				e.executions = max(e.executions, record.Count)
			}
		}
		scanErr := scanner.Err()
		closeErr := file.Close()
		if err := errors.Join(scanErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}
func (e *permissionFixtureEvidence) record(name, event, token string, isError bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	count := e.executions
	if name == "calls.jsonl" {
		e.calls++
		count = e.calls
	} else if event == "started" {
		e.executions++
		count = e.executions
	}
	data, err := json.Marshal(permissionFixtureRecord{Event: event, Phase: event, Token: token, Count: count, IsError: isError})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(e.path(name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || info.Size() > 1<<20 {
		_ = file.Close()
		return errors.New("fixture evidence limit")
	}
	_, writeErr := file.Write(append(data, '\n'))
	syncErr := file.Sync()
	return errors.Join(writeErr, syncErr, file.Close())
}
func (e *permissionFixtureEvidence) marker(token, boundary string) error {
	return os.WriteFile(e.path(token+"-"+boundary), []byte("reached\n"), 0600)
}
func permissionFixtureMarker(path, want string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32 {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32))
	return err == nil && strings.TrimSpace(string(data)) == want
}
func permissionFixtureGate(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if permissionFixtureMarker(path, "release") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func permissionFixtureTokens() []string {
	return []string{"allow", "deny", "pending", "before-write", "after-write", "close-pending", "fresh"}
}
func permissionFixtureToken(token string) bool {
	return slices.Contains(permissionFixtureTokens(), token)
}

type permissionFixtureWrite struct {
	tools.Tool
	evidence *permissionFixtureEvidence
}

func (w *permissionFixtureWrite) Run(ctx context.Context, args jsonv1.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return tools.ErrorResult(err), nil
	}
	token := strings.TrimSuffix(input.Path, ".txt")
	if !permissionFixtureToken(token) || input.Path != token+".txt" || input.Content != token+"\n" {
		return tools.ErrorResult(errors.New("fixture rejected non-scripted write")), nil
	}
	e := w.evidence
	if err := e.record("executions.jsonl", "started", token, false); err != nil {
		return tools.ErrorResult(err), nil
	}
	if err := e.marker(token, "started"); err != nil {
		return tools.ErrorResult(err), nil
	}
	if token == "before-write" {
		if err := permissionFixtureGate(ctx, e.path(token+"-release-before")); err != nil {
			return tools.ErrorResult(err), nil
		}
	}
	result, err := w.Tool.Run(ctx, args, host)
	if err != nil || result.IsError {
		return result, err
	}
	if err := e.record("executions.jsonl", "executed", token, false); err != nil {
		return tools.ErrorResult(err), nil
	}
	if err := e.marker(token, "executed"); err != nil {
		return tools.ErrorResult(err), nil
	}
	if token == "after-write" {
		if err := permissionFixtureGate(ctx, e.path(token+"-release-after")); err != nil {
			return tools.ErrorResult(err), nil
		}
	}
	if err := e.record("executions.jsonl", "finish", token, false); err != nil {
		return tools.ErrorResult(err), nil
	}
	return result, nil
}

type permissionFixtureProvider struct {
	*fake.Provider
	evidence *permissionFixtureEvidence
	cwd      string
}

func (p *permissionFixtureProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	token := ""
	userIndex := -1
	for i, message := range request.Messages {
		if message.Role == protocol.RoleUser {
			userIndex = i
			token = ""
			for _, block := range message.Content {
				if block.Type == protocol.BlockText {
					token += block.Text
				}
			}
		}
	}
	if !permissionFixtureToken(token) {
		return nil, errors.New("fixture expects an exact scripted prompt token")
	}
	var result *protocol.Message
	for i := userIndex + 1; i < len(request.Messages); i++ {
		if request.Messages[i].Role == protocol.RoleTool {
			result = &request.Messages[i]
		}
	}
	event := "call"
	if result != nil {
		event = "followup"
	}
	if err := p.evidence.record("calls.jsonl", event, token, result != nil && result.IsError); err != nil {
		return nil, err
	}
	var steps []fake.Step
	if result == nil {
		args, err := json.Marshal(map[string]string{"path": token + ".txt", "content": token + "\n"})
		if err != nil {
			return nil, err
		}
		steps = []fake.Step{{Kind: fake.StepToolCall, ToolCallID: "permission-" + p.evidence.project + "-" + token, ToolName: "write", Arguments: args}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}
	} else {
		if result.ToolName != "write" || result.ToolCallID != "permission-"+p.evidence.project+"-"+token {
			return nil, errors.New("fixture received unexpected authoritative write result")
		}
		path := filepath.Join(p.cwd, token+".txt")
		text := "Permission fixture completed: " + token + "."
		if result.IsError {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("denied fixture write unexpectedly exists")
			}
			text = "Permission fixture denied: " + token + "."
		} else {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != token+"\n" {
				return nil, errors.New("fixture write result does not match actual file")
			}
		}
		steps = []fake.Step{{Kind: fake.StepText, Text: text}, {Kind: fake.StepDone, Stop: protocol.StopStop}}
	}
	return fake.New(steps).Chat(ctx, request)
}

type permissionFixtureStartup struct{ output chan string }

func (w permissionFixtureStartup) Write(data []byte) (int, error) {
	select {
	case w.output <- string(data):
		return len(data), nil
	default:
		return 0, errors.New("unexpected extra fixture startup output")
	}
}

// TestWebPermissionFixture is invoked by the private browser runner, not by an
// ordinary go test. Neither project is activated by setup; browsers must issue
// the same explicit activation command as a user. Closing stdin joins web.Run
// and its manager-owned RPC children. Never persist or print the READY cookie.
func TestWebPermissionFixture(t *testing.T) {
	directory := os.Getenv(permissionFixtureEnv)
	if directory == "" {
		t.Skip("opt-in local permission browser fixture")
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("fixture directory must be absolute")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("fixture directory must exist and be private")
	}
	// Temp roots may use a system alias (for example /var -> /private/var on
	// macOS). Match the registry's canonical CWD without expanding any roots.
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(permissionFixtureEnv, directory)
	home := filepath.Join(directory, "home")
	paths := map[string]string{"a": filepath.Join(directory, "project-a"), "b": filepath.Join(directory, "project-b")}
	for _, path := range []string{home, paths["a"], paths["b"]} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("SNOW_HOME", filepath.Join(home, ".snow"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
	t.Setenv("SNOW_WEB_LIVE_FIXTURE_DIR", "")
	manager := filepath.Join(directory, "manager")
	registry, err := web.OpenRegistry(t.Context(), manager)
	if err != nil {
		t.Fatal(err)
	}
	projects := make(map[string]string)
	for _, key := range []string{"a", "b"} {
		registered, err := registry.Add(t.Context(), "Permission fixture "+strings.ToUpper(key), paths[key])
		if err != nil {
			_ = registry.Close()
			t.Fatal(err)
		}
		projects[key] = registered.ID
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 240*time.Second)
	defer cancel()
	startup := permissionFixtureStartup{output: make(chan string, 1)}
	stopped := make(chan error, 1)
	go func() {
		stopped <- web.Run(ctx, web.Options{Listen: "127.0.0.1:0", ManagerDir: manager, Executable: executable, SessionsRoot: os.Getenv("SNOW_SESSIONS_DIR"), Version: "permission-fixture"}, startup)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-stopped:
			if err != nil {
				t.Error("fixture manager shutdown failed")
			}
		case <-time.After(8 * time.Second):
			t.Error("fixture manager shutdown deadline")
		}
	})
	var output string
	select {
	case output = <-startup.output:
	case <-stopped:
		t.Fatal("fixture manager exited during startup")
	case <-ctx.Done():
		t.Fatal("fixture startup deadline")
	}
	origin, cookie := permissionFixturePair(t, ctx, output)
	contents := make(map[string]string)
	for _, token := range permissionFixtureTokens() {
		contents[token] = token + "\n"
	}
	ready, err := json.Marshal(map[string]any{"origin": origin, "cookie": cookie, "projects": projects, "paths": paths, "contents": contents})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "SNOW_PERMISSION_READY "+string(ready))
	inputDone := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 64), 1024)
		scanner.Scan()
		close(inputDone)
	}()
	select {
	case <-inputDone:
	case <-ctx.Done():
		t.Fatal("fixture lifetime deadline")
	}
}

func permissionFixturePair(t *testing.T, ctx context.Context, output string) (string, string) {
	t.Helper()
	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		t.Fatal("invalid fixture startup")
	}
	origin := lines[1]
	_, code, ok := strings.Cut(lines[2], "): ")
	if !ok {
		t.Fatal("missing local pairing credential")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	login, err := http.NewRequestWithContext(ctx, "GET", origin+"/login", nil)
	if err != nil {
		t.Fatal("invalid fixture login URL")
	}
	response, err := client.Do(login)
	if err != nil {
		t.Fatal("fixture login unavailable")
	}
	_ = response.Body.Close()
	parsed, err := url.Parse(origin)
	if err != nil {
		t.Fatal("invalid fixture origin")
	}
	csrf := ""
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "snow_manager_local_pair_csrf" {
			csrf = cookie.Value
		}
	}
	request, err := http.NewRequestWithContext(ctx, "POST", origin+"/login", strings.NewReader(url.Values{"csrf": {csrf}, "code": {code}}.Encode()))
	if err != nil {
		t.Fatal("invalid fixture pairing URL")
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal("fixture pairing failed")
	}
	_ = response.Body.Close()
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "snow_manager_local_session" && cookie.Value != "" {
			return origin, cookie.Value
		}
	}
	t.Fatal("fixture pairing did not issue a browser cookie")
	return "", ""
}
