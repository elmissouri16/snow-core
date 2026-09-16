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
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const streamFixtureEnv = "SNOW_WEB_LIVE_FIXTURE_DIR"
const streamFixturePrefix = "# Incremental answer\n\nFirst chunk is visible.\n\n```go\nfmt.Pr"
const streamFixtureSuffix = "intln(\"second chunk\")\n```\n\n**Complete** only after release."

// Only this test binary, an explicit private fixture directory, and the exact
// manager-owned RPC arguments can select the worker. Production has no hook.
func init() {
	if os.Getenv(streamFixtureEnv) == "" || !slices.Contains(os.Args, "--mode") {
		return
	}
	code := runStreamFixtureWorker()
	_ = os.WriteFile(filepath.Join(os.Getenv(streamFixtureEnv), "worker-exit"), []byte(fmt.Sprint(code)), 0600)
	os.Exit(code)
}

func runStreamFixtureWorker() int {
	flag := func(name string) string {
		i := slices.Index(os.Args, name)
		if i < 0 || i+1 >= len(os.Args) {
			return ""
		}
		return os.Args[i+1]
	}
	if flag("--mode") != "rpc" {
		return 80
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil {
		return 81
	}
	if flag("--rpc-startup") == "catalog" {
		if err := rpc.CatalogMain(ctx, cwd, session.DefaultSessionsRoot(), "live-stream-fixture"); err != nil {
			return 82
		}
		return 0
	}
	if flag("--rpc-startup") != "eager" || slices.Contains(os.Args, "--permission") {
		return 83
	}
	for _, option := range []string{"--no-session", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents", "--no-debug"} {
		if !slices.Contains(os.Args, option) {
			return 84
		}
	}
	// Keep the production interactive broker, but expose only safe local fixture
	// tools. A provider cannot invoke Bash, write, plugins, MCP, or child agents.
	a, err := app.New(ctx, app.Options{CWD: cwd, Provider: "fake", Model: "fake-1", NoSession: true, Tools: []string{"ask_user", "read"}, NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false), Debug: new(false)})
	if err != nil {
		return 85
	}
	defer a.Close()
	p := &streamFixtureProvider{Provider: fake.New(nil), directory: os.Getenv(streamFixtureEnv)}
	a.Providers["fake"] = p
	if err := a.SetProvider("fake"); err != nil {
		return 86
	}
	// rpc.Main owns its event bridge privately. Reproduce only that transport
	// wiring here; all turn execution and RPC dispatch remain production code.
	output := &streamFixtureOutput{out: os.Stdout}
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
	if err := rpc.New(ctx, a, streamFixtureInput{ReadCloser: os.Stdin}, output).Serve(ctx); err != nil {
		return 87
	}
	return 0
}

// Inherited subprocess handles may reject deadlines on macOS. Match the
// production RPC entry point's interruptible stdin and bounded serialized stdout.
type streamFixtureInput struct{ io.ReadCloser }

func (streamFixtureInput) InterruptsReadOnClose() bool { return true }

type streamFixtureOutput struct {
	mu  sync.Mutex
	out io.Writer
}

func (*streamFixtureOutput) RPCWriteBounded() bool { return true }
func (w *streamFixtureOutput) Write(data []byte) (int, error) {
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

type streamFixtureProvider struct {
	*fake.Provider
	directory string
	mu        sync.Mutex
	calls     int
}

func (p *streamFixtureProvider) reserveCall(prompt string) (int, error) {
	for range 4096 {
		p.calls++
		file, err := os.OpenFile(filepath.Join(p.directory, fmt.Sprintf("call-%d", p.calls)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		_, err = file.WriteString(prompt)
		if err = errors.Join(err, file.Close()); err != nil {
			return 0, err
		}
		return p.calls, nil
	}
	return 0, errors.New("fixture provider call inventory exhausted")
}

func (p *streamFixtureProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	prompt := ""
	for _, message := range request.Messages {
		if message.Role == protocol.RoleUser {
			for _, block := range message.Content {
				if block.Type == protocol.BlockText {
					prompt = block.Text
				}
			}
		}
	}
	// Reserve a never-reused call number across worker replacements. Existing
	// release gates must never authorize a new worker's provider request.
	p.mu.Lock()
	call, err := p.reserveCall(prompt)
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	steps := []streamFixtureStep{{event: protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: streamFixturePrefix}}, {gate: fmt.Sprintf("release-%d-second", call), event: protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: streamFixtureSuffix}}, {gate: fmt.Sprintf("release-%d-done", call), event: protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}
	if prompt == "cancel before text" {
		steps[0].gate = fmt.Sprintf("release-%d-first", call)
	}
	if prompt == "ask a question" {
		last := request.Messages[len(request.Messages)-1]
		if last.Role == protocol.RoleTool {
			text := ""
			for _, block := range last.Content {
				text += block.Text
			}
			if !strings.Contains(text, "blue") {
				return nil, errors.New("fixture expected an authoritative blue answer")
			}
			steps = []streamFixtureStep{{event: protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: "Received authoritative answer: blue."}}, {event: protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}
		} else {
			steps = []streamFixtureStep{{event: protocol.StreamEvent{Type: protocol.EvStreamToolCallDone, ToolCallID: "fixture-question", ToolName: "ask_user", Arguments: []byte(`{"questions":[{"id":"color","header":"Color","question":"Choose a color for the fixture.","choices_only":true,"options":[{"label":"blue","description":"Use blue"},{"label":"green","description":"Use green"}]}]}`)}}, {event: protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}}}
		}
	}
	return &streamFixtureStream{ctx: ctx, directory: p.directory, steps: steps}, nil
}

type streamFixtureStep struct {
	gate  string
	event protocol.StreamEvent
}
type streamFixtureStream struct {
	ctx       context.Context
	directory string
	steps     []streamFixtureStep
	position  int
}

func (s *streamFixtureStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.position >= len(s.steps) {
		return protocol.StreamEvent{}, io.EOF
	}
	step := s.steps[s.position]
	if step.gate != "" {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(filepath.Join(s.directory, step.gate)); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return protocol.StreamEvent{}, err
			}
			select {
			case <-ctx.Done():
				return protocol.StreamEvent{}, ctx.Err()
			case <-s.ctx.Done():
				return protocol.StreamEvent{}, s.ctx.Err()
			case <-ticker.C:
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return protocol.StreamEvent{}, err
	}
	s.position++
	return step.event, nil
}
func (s *streamFixtureStream) Close() error { return nil }

type streamFixtureStartup struct{ output chan string }

func (w streamFixtureStartup) Write(data []byte) (int, error) {
	w.output <- string(data)
	return len(data), nil
}

// Invoked only by scripts/tests/browser/live-stream/run.mjs. Pair through the
// real HTTP login, then send its browser cookie over the private parent pipe.
// Pairing credentials are never printed, written to artifacts, or put in URLs.
func TestWebLiveStreamFixture(t *testing.T) {
	directory := os.Getenv(streamFixtureEnv)
	if directory == "" {
		t.Skip("opt-in local browser fixture")
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("fixture directory must be absolute")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("fixture directory must exist and be private")
	}
	home, project := filepath.Join(directory, "home"), filepath.Join(directory, "project")
	for _, path := range []string{home, project} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("SNOW_HOME", filepath.Join(home, ".snow"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
	manager := filepath.Join(directory, "manager")
	registry, err := web.OpenRegistry(t.Context(), manager)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := registry.Add(t.Context(), "Live stream fixture", project)
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
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	startup := streamFixtureStartup{output: make(chan string, 1)}
	stopped := make(chan error, 1)
	go func() {
		stopped <- web.Run(ctx, web.Options{Listen: "127.0.0.1:0", ManagerDir: manager, Executable: executable, SessionsRoot: os.Getenv("SNOW_SESSIONS_DIR"), Version: "live-stream-fixture"}, startup)
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
	case <-ctx.Done():
		t.Fatal("fixture startup deadline")
	}
	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		t.Fatal("invalid fixture startup")
	}
	origin := lines[1]
	code, ok := fixturePairingCode(output)
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
		t.Fatal(err)
	}
	response, err := client.Do(login)
	if err != nil {
		t.Fatal("fixture login unavailable")
	}
	_ = response.Body.Close()
	parsed, _ := url.Parse(origin)
	csrf := ""
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "snow_manager_local_pair_csrf" {
			csrf = cookie.Value
		}
	}
	request, err := http.NewRequestWithContext(ctx, "POST", origin+"/login", strings.NewReader(url.Values{"csrf": {csrf}, "code": {code}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal("fixture pairing failed")
	}
	_ = response.Body.Close()
	cookieValue := ""
	for _, cookie := range jar.Cookies(parsed) {
		if cookie.Name == "snow_manager_local_session" {
			cookieValue = cookie.Value
		}
	}
	if cookieValue == "" {
		t.Fatal("fixture pairing did not issue a browser cookie")
	}
	ready, err := json.Marshal(map[string]string{"origin": origin, "project": registered.ID, "cookie": cookieValue, "prefix": streamFixturePrefix, "complete": streamFixturePrefix + streamFixtureSuffix})
	if err != nil {
		t.Fatal(err)
	}
	// The runner consumes and suppresses this private IPC line; never log it.
	fmt.Fprintln(os.Stdout, "SNOW_LIVE_READY "+string(ready))
	inputDone := make(chan struct{})
	go func() { scanner := bufio.NewScanner(os.Stdin); scanner.Scan(); close(inputDone) }()
	select {
	case <-inputDone:
	case <-ctx.Done():
		t.Fatal("fixture lifetime deadline")
	}
}

func TestWebStreamFixtureGateCancellation(t *testing.T) {
	directory := t.TempDir()
	p := &streamFixtureProvider{Provider: fake.New(nil), directory: directory}
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := p.Chat(ctx, protocol.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Next(ctx)
	if err != nil || first.Text != streamFixturePrefix {
		t.Fatalf("prefix: %v", err)
	}
	cancel()
	if _, err := stream.Next(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("gated stream did not cancel: %v", err)
	}
}

func TestWebStreamFixtureChunksRequireSeparateGates(t *testing.T) {
	directory := t.TempDir()
	p := &streamFixtureProvider{Provider: fake.New(nil), directory: directory}
	stream, err := p.Chat(t.Context(), protocol.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Next(t.Context())
	if err != nil || first.Type != protocol.EvStreamTextDelta || first.Text != streamFixturePrefix {
		t.Fatalf("first step = %#v, %v", first, err)
	}
	for _, phase := range []string{"second", "done"} {
		// The next provider event cannot arrive merely because a browser waited.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := stream.Next(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s gate bypassed: %v", phase, err)
		}
		if err := os.WriteFile(filepath.Join(directory, "release-1-"+phase), nil, 0600); err != nil {
			t.Fatal(err)
		}
		event, err := stream.Next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if phase == "second" && (event.Type != protocol.EvStreamTextDelta || event.Text != streamFixtureSuffix) {
			t.Fatalf("second step = %#v", event)
		}
		if phase == "done" && (event.Type != protocol.EvStreamDone || event.StopReason != protocol.StopStop) {
			t.Fatalf("completion = %#v", event)
		}
	}
	if _, err := stream.Next(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatalf("missing final EOF: %v", err)
	}
}
