package snowsdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SDK prompt uses the real app/agent/plugin path and a local scripted provider.
func TestJavaScriptSDKPromptEventsAndSessionPairing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs("../../examples/plugins/project-helper")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(cwd, "hello.txt"), []byte("SDK plugin result"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), Options{Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, PermissionMode: "deny", CWD: cwd, JavaScriptPlugins: map[string]plugin.JavaScriptSpec{"project-helper": {Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	provider := &oneCallJSProvider{Provider: fake.New(nil)}
	if err = s.app.Agent.SetProvider(provider); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []protocol.AgentEvent
	unsub := s.Subscribe(func(event protocol.AgentEvent) { mu.Lock(); events = append(events, event); mu.Unlock() })
	defer unsub()
	if err = s.Prompt(t.Context(), "Read hello.txt with the plugin"); err != nil {
		t.Fatal(err)
	}
	messages, err := s.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, message := range messages {
		if message.Role == protocol.RoleTool {
			count++
			if message.ToolName != "plugin_project-helper_read_file" || message.ToolCallID != "js-call" || message.IsError {
				t.Fatalf("message=%+v", message)
			}
			if len(message.Content) == 0 || !strings.Contains(message.Content[0].Text, "SDK plugin result") {
				t.Fatalf("content=%+v", message.Content)
			}
		}
	}
	if count != 1 {
		t.Fatalf("tool results=%d", count)
	}
	mu.Lock()
	defer mu.Unlock()
	progress := false
	for _, event := range events {
		if event.Type == protocol.EvToolProgress && event.ToolCallID == "js-call" {
			progress = true
		}
	}
	if !progress {
		t.Fatal("plugin progress missing from shared event stream")
	}
}

type oneCallJSProvider struct {
	*fake.Provider
	calls int
}

func (p *oneCallJSProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.calls++
	if p.calls == 1 {
		return fake.New([]fake.Step{{Kind: fake.StepToolCall, ToolCallID: "js-call", ToolName: "plugin_project-helper_read_file", Arguments: []byte(`{"path":"hello.txt"}`)}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}).Chat(ctx, req)
	}
	return fake.New([]fake.Step{{Kind: fake.StepText, Text: "Done"}, {Kind: fake.StepDone, Stop: protocol.StopStop}}).Chat(ctx, req)
}
