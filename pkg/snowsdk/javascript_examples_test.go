package snowsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type exampleProvider struct {
	*fake.Provider
	steps  []fake.Step
	called bool
}

func (p *exampleProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if !p.called {
		p.called = true
		steps := append([]fake.Step(nil), p.steps...)
		steps = append(steps, fake.Step{Kind: fake.StepDone, Stop: protocol.StopToolUse})
		return fake.New(steps).Chat(ctx, req)
	}
	return fake.New([]fake.Step{{Kind: fake.StepText, Text: "Examples complete"}, {Kind: fake.StepDone, Stop: protocol.StopStop}}).Chat(ctx, req)
}

func exampleWorkspace(t *testing.T) string {
	t.Helper()
	t.Setenv("SNOW_HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		"README.md":           "# Fixture project\nA small example application.\n",
		"go.mod":              "module example.test/fixture\n\ngo 1.27rc3\n",
		"src/main.go":         "package main\n// TODO: add retries\n// FIXME: handle timeout\n// HACK: remove fallback\n",
		"src/skip.go":         "package main\n// TODO: config-excluded-marker\n",
		"vendor/generated.go": "// TODO: vendor-marker\n",
		"ignored.go":          "// TODO: ignored-marker\n",
		".gitignore":          "ignored.go\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runExampleTools(t *testing.T, cwd, id string, opts Options, calls ...fake.Step) []protocol.Message {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("../../examples/plugins", id))
	if err != nil {
		t.Fatal(err)
	}
	spec := plugin.JavaScriptSpec{Path: path}
	if prior, ok := opts.JavaScriptPlugins[id]; ok {
		spec.Config = prior.Config
	}
	opts.JavaScriptPlugins = map[string]plugin.JavaScriptSpec{id: spec}
	opts.CWD, opts.Provider, opts.Thinking = cwd, "fake", "off"
	opts.NoSession, opts.NoMCP, opts.NoSkills, opts.DisableSubagents = true, true, true, true
	if opts.PermissionMode == "" {
		opts.PermissionMode = "deny"
	}
	s, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	for i := range calls {
		calls[i].Kind = fake.StepToolCall
		calls[i].ToolCallID = fmt.Sprintf("example-%d", i)
	}
	if err := s.app.Agent.SetProvider(&exampleProvider{Provider: fake.New(nil), steps: calls}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(t.Context(), "Exercise the example tools"); err != nil {
		t.Fatal(err)
	}
	messages, err := s.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	var results []protocol.Message
	for _, message := range messages {
		if message.Role == protocol.RoleTool {
			results = append(results, message)
		}
	}
	if len(results) != len(calls) {
		t.Fatalf("got %d results for %d outer calls", len(results), len(calls))
	}
	for i, result := range results {
		if result.ToolName != calls[i].ToolName || result.ToolCallID != calls[i].ToolCallID {
			t.Fatalf("nested transcript pair: %+v", result)
		}
	}
	return results
}

func exampleText(message protocol.Message) string {
	var text strings.Builder
	for _, block := range message.Content {
		text.WriteString(block.Text)
	}
	return text.String()
}

func TestJavaScriptProjectContextExample(t *testing.T) {
	root := exampleWorkspace(t)
	results := runExampleTools(t, root, "project-context", Options{CollaborationMode: "plan"},
		fake.Step{ToolName: "plugin_project-context_brief", Arguments: json.RawMessage(`{}`)},
		fake.Step{ToolName: "plugin_project-context_brief", Arguments: json.RawMessage(`{"path":"../outside"}`)},
		fake.Step{ToolName: "plugin_project-context_brief", Arguments: json.RawMessage(`{"path":"missing"}`)},
	)
	text := exampleText(results[0])
	for _, want := range []string{"Fixture project", "module example.test/fixture", "src/main.go", "package.json (unavailable)", "not a complete"} {
		if results[0].IsError || !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if !results[1].IsError || !results[2].IsError {
		t.Fatal("invalid/missing directories succeeded")
	}
}

func TestJavaScriptTodoRadarExample(t *testing.T) {
	root := exampleWorkspace(t)
	opts := Options{CollaborationMode: "plan", JavaScriptPlugins: map[string]plugin.JavaScriptSpec{
		"todo-radar": {Config: json.RawMessage(`{"default_limit":1,"exclude":["src/skip.go"]}`)},
	}}
	results := runExampleTools(t, root, "todo-radar", opts,
		fake.Step{ToolName: "plugin_todo-radar_scan", Arguments: json.RawMessage(`{"glob":"**/*.go","limit":20}`)},
		fake.Step{ToolName: "plugin_todo-radar_scan", Arguments: json.RawMessage(`{"kind":"fixme"}`)},
		fake.Step{ToolName: "plugin_todo-radar_scan", Arguments: json.RawMessage(`{}`)},
		fake.Step{ToolName: "plugin_todo-radar_scan", Arguments: json.RawMessage(`{"limit":201}`)},
	)
	text := exampleText(results[0])
	if results[0].IsError || !strings.Contains(text, "src/main.go:2: // TODO: add retries") || !strings.Contains(text, "FIXME: handle timeout") {
		t.Fatal(text)
	}
	for _, excluded := range []string{"vendor-marker", "ignored-marker", "config-excluded-marker"} {
		if strings.Contains(text, excluded) {
			t.Fatalf("excluded match %s: %s", excluded, text)
		}
	}
	if text := exampleText(results[1]); results[1].IsError || !strings.Contains(text, "FIXME") || strings.Contains(text, "add retries") {
		t.Fatal(text)
	}
	if text := exampleText(results[2]); results[2].IsError || !strings.Contains(text, "limit 1") || !strings.Contains(text, "max matches reached: 1") {
		t.Fatal(text)
	}
	if !results[3].IsError {
		t.Fatal("out-of-range match cap accepted")
	}
}

func gitExampleWorkspace(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := exampleWorkspace(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	git := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("add", ".")
	git("-c", "user.name=Plugin Test", "-c", "user.email=plugin@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "-m", "fixture")
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("staged-marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "staged.txt")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Fixture project\nunstaged-marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestJavaScriptGitReviewExample(t *testing.T) {
	root := gitExampleWorkspace(t)
	var requests []protocol.PermissionRequest
	results := runExampleTools(t, root, "git-review", Options{PermissionMode: "ask", PermissionHandler: func(_ context.Context, req protocol.PermissionRequest) (protocol.PermissionResponse, error) {
		requests = append(requests, req)
		return protocol.PermissionResponse{RequestID: req.ID, Decision: protocol.PermissionAllow}, nil
	}},
		fake.Step{ToolName: "plugin_git-review_status", Arguments: json.RawMessage(`{}`)},
		fake.Step{ToolName: "plugin_git-review_diff", Arguments: json.RawMessage(`{"scope":"staged"}`)},
		fake.Step{ToolName: "plugin_git-review_diff", Arguments: json.RawMessage(`{"scope":"unstaged"}`)},
		fake.Step{ToolName: "plugin_git-review_diff", Arguments: json.RawMessage(`{"scope":"staged","stat":true}`)},
		fake.Step{ToolName: "plugin_git-review_diff", Arguments: json.RawMessage(`{"scope":"staged; touch INJECTED"}`)},
	)
	for i, want := range []string{"staged.txt", "+staged-marker", "+unstaged-marker", "1 file changed"} {
		if text := exampleText(results[i]); results[i].IsError || !strings.Contains(text, want) {
			t.Fatalf("result %d: %s", i, text)
		}
	}
	if strings.Contains(exampleText(results[1]), "unstaged-marker") || strings.Contains(exampleText(results[2]), "staged.txt") {
		t.Fatal("diff scopes mixed")
	}
	if !results[4].IsError {
		t.Fatal("command injection scope accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "INJECTED")); !os.IsNotExist(err) {
		t.Fatalf("injected file: %v", err)
	}
	nested := 0
	for _, req := range requests {
		if req.Plugin != nil && req.Plugin.HostTool == "bash" {
			nested++
		}
	}
	if nested != 4 {
		t.Fatalf("nested command approvals=%d", nested)
	}
}

func TestJavaScriptGitReviewRespectsDenialAndPlan(t *testing.T) {
	for _, mode := range []string{"deny", "plan", "deny-host"} {
		t.Run(mode, func(t *testing.T) {
			root := exampleWorkspace(t)
			opts := Options{PermissionMode: "deny"}
			if mode == "plan" {
				opts.PermissionMode, opts.CollaborationMode = "allow", "plan"
			}
			if mode == "deny-host" {
				opts.PermissionMode = "ask"
				opts.PermissionHandler = func(_ context.Context, req protocol.PermissionRequest) (protocol.PermissionResponse, error) {
					decision := protocol.PermissionAllow
					if req.Plugin != nil && req.Plugin.HostTool != "" {
						decision = protocol.PermissionDeny
					}
					return protocol.PermissionResponse{RequestID: req.ID, Decision: decision}, nil
				}
			}
			results := runExampleTools(t, root, "git-review", opts, fake.Step{ToolName: "plugin_git-review_status", Arguments: json.RawMessage(`{}`)})
			text := strings.ToLower(exampleText(results[0]))
			if !results[0].IsError || strings.Contains(text, "not a git repository") {
				t.Fatalf("git executed through denial: %s", text)
			}
			if !strings.Contains(text, "denied") && !strings.Contains(text, "plan mode") {
				t.Fatalf("unexpected error: %s", text)
			}
		})
	}
}
