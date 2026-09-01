//go:build darwin || linux

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
)

func newRuntimeRPCApp(t *testing.T) *app.App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(home, "snow-home"))
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(t.Context(), app.Options{
		Provider: "fake", Permission: "ask", NoSession: true,
		CWD: cwd, NoMCP: true, NoPlugins: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func decodeRuntimeResponses(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	responses := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func TestRPCPermissionModeAndSessionInfoStayCorrelated(t *testing.T) {
	a := newRuntimeRPCApp(t)
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	for _, req := range []Request{
		{ID: "get", Type: "permission_mode_get"},
		{ID: "set", Type: "permission_mode_set", Params: json.RawMessage(`{"mode":"deny"}`)},
		{ID: "info", Type: "session_info"},
	} {
		if err := s.handle(t.Context(), req); err != nil {
			t.Fatalf("%s: %v", req.Type, err)
		}
	}
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) != 3 {
		t.Fatalf("responses = %#v", responses)
	}
	if responses[0]["id"] != "get" || responses[0]["command"] != "permission_mode_get" || responses[0]["data"].(map[string]any)["mode"] != "ask" {
		t.Fatalf("get response = %#v", responses[0])
	}
	if responses[1]["id"] != "set" || responses[1]["data"].(map[string]any)["mode"] != "deny" {
		t.Fatalf("set response = %#v", responses[1])
	}
	if responses[2]["id"] != "info" || responses[2]["data"].(map[string]any)["permission_mode"] != "deny" {
		t.Fatalf("session_info response = %#v", responses[2])
	}
	if err := s.handle(t.Context(), Request{Type: "permission_mode_set", Params: json.RawMessage(`{"mode":"invalid"}`)}); err == nil {
		t.Fatal("invalid permission mode accepted")
	}
}

func TestRPCTrustSetPersistsForRestartAndUsesCanonicalPath(t *testing.T) {
	a := newRuntimeRPCApp(t)
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := s.handle(t.Context(), Request{ID: "trust", Type: "trust_set", Params: json.RawMessage(`{"level":"allow"}`)}); err != nil {
		t.Fatal(err)
	}
	response := decodeRuntimeResponses(t, &out)[0]
	data := response["data"].(map[string]any)
	if response["id"] != "trust" || response["command"] != "trust_set" || data["level"] != "allow" {
		t.Fatalf("response = %#v", response)
	}
	if data["loaded"] != false || data["restart_required"] != true {
		t.Fatalf("trust restart state = %#v", data)
	}
	path, ok := data["path"].(string)
	if !ok || !filepath.IsAbs(path) || path != a.ProjectInputRoot {
		t.Fatalf("canonical path = %q, project input root = %q", path, a.ProjectInputRoot)
	}
	if a.ProjectAllowed {
		t.Fatal("trust_set changed running project input policy")
	}
}

func TestRPCManagedProcessInventoryAndLogsAreBounded(t *testing.T) {
	a := newRuntimeRPCApp(t)
	state, err := a.ProcessManager.Start(context.Background(), managedprocess.StartRequest{Command: "printf 'ready\\n'; sleep 10", Name: "server", Readiness: &managedprocess.ReadinessRequest{Type: "log", Pattern: "ready", TimeoutMS: 1000}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := s.handle(t.Context(), Request{ID: "list", Type: "processes_list"}); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{"process_id": state.ProcessID, "max_bytes": 64})
	if err := s.handle(t.Context(), Request{ID: "logs", Type: "process_logs", Params: params}); err != nil {
		t.Fatal(err)
	}
	responses := decodeRuntimeResponses(t, &out)
	processes := responses[0]["data"].(map[string]any)["processes"].([]any)
	if len(processes) != 1 || processes[0].(map[string]any)["process_id"] != state.ProcessID {
		t.Fatalf("processes = %#v", processes)
	}
	logs := responses[1]["data"].(map[string]any)
	if logs["process_id"] != state.ProcessID || !strings.Contains(logs["output"].(string), "ready") || len(logs["output"].(string)) > 64 {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRPCProjectInitUsesPromptLifecycleAndCorePrompt(t *testing.T) {
	a := newRuntimeRPCApp(t)
	var out bytes.Buffer
	s := New(t.Context(), a, &bytes.Buffer{}, &out)
	if err := s.handle(t.Context(), Request{ID: "init", Type: "project_init"}); err != nil {
		t.Fatal(err)
	}
	s.promptWG.Wait()
	responses := decodeRuntimeResponses(t, &out)
	if len(responses) < 2 {
		t.Fatalf("responses = %#v", responses)
	}
	if responses[0]["id"] != "init" || responses[0]["command"] != "project_init" || responses[0]["success"] != true {
		t.Fatalf("admission = %#v", responses[0])
	}
	last := responses[len(responses)-1]
	if last["type"] != "prompt_completed" || last["request_id"] != "init" || last["status"] != "completed" {
		t.Fatalf("completion = %#v", last)
	}
	messages, err := a.Agent.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || len(messages[0].Content) == 0 || !strings.Contains(messages[0].Content[0].Text, "AGENTS.md") {
		t.Fatalf("init prompt was not persisted through agent lifecycle: %#v", messages)
	}
}
