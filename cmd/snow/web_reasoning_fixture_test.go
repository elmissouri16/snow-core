//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const reasoningFixtureEnv = "SNOW_WEB_REASONING_FIXTURE_DIR"

func init() {
	if directory := os.Getenv(reasoningFixtureEnv); directory != "" && slices.Contains(os.Args, "--mode") {
		code := runReasoningFixtureWorker(directory)
		_ = os.WriteFile(filepath.Join(directory, "worker-exit"), []byte(fmt.Sprint(code)), 0600)
		os.Exit(code)
	}
}

type reasoningFixtureProvider struct {
	*fake.Provider
	directory string
}

func (p *reasoningFixtureProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
	_ = os.WriteFile(filepath.Join(p.directory, "provider-called"), []byte("unexpected chat"), 0600)
	return nil, errors.New("reasoning controls must not start provider work")
}
func runReasoningFixtureWorker(directory string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 90
	}
	if _, err := permissionFixtureProject(directory, cwd); err != nil {
		return 91
	}
	if os.Getenv("HOME") != filepath.Join(directory, "home") || os.Getenv("SNOW_HOME") != filepath.Join(directory, "home", ".snow") {
		return 92
	}
	opts, err := policyFixtureOptions(os.Args[1:])
	if err != nil || opts.Provider != "fake" || opts.Model != "fake-1" || !opts.NoSession {
		return 93
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, err := app.New(ctx, opts)
	if err != nil {
		return 94
	}
	defer a.Close()
	// Only provider metadata/test transport is substituted. Real CLI options,
	// application settings methods, sessions, RPC and manager admission execute.
	model := a.Agent.Model()
	model.SupportsThinking = true
	model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingMedium, protocol.ThinkingHigh}
	model.SupportsReasoningSummary = new(true)
	model.SupportsVerbosity = true
	p := &reasoningFixtureProvider{Provider: fake.NewWithModels([]protocol.Model{model}), directory: directory}
	a.Providers["fake"] = p
	if err := a.Agent.SetProvider(p); err != nil {
		return 95
	}
	if err := a.Agent.SetModel(model); err != nil {
		return 96
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
		return 97
	}
	return 0
}

// Real private-HOME worker composition proves session-only mutation does not
// rewrite actual host/project config bytes, send a prompt, or lose mode effort.
func TestWebReasoningRealWorkerPreservesConfigBytesAndModeOverrides(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	home := filepath.Join(directory, "home")
	for _, path := range []string{filepath.Join(cwd, ".snow"), filepath.Join(home, ".snow")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("SNOW_HOME", filepath.Join(home, ".snow"))
	t.Setenv(reasoningFixtureEnv, directory)
	t.Setenv(policyFixtureEnv, "")
	t.Setenv(permissionFixtureEnv, "")
	t.Setenv(streamFixtureEnv, "")
	t.Setenv(managerExecutionEnv, "")
	hostPath := filepath.Join(home, ".snow", "config.json")
	projectPath := filepath.Join(cwd, ".snow", "config.json")
	cfg := config.Default()
	cfg.DefaultProvider = "fake"
	cfg.DefaultModel = "fake-1"
	cfg.TUI.Theme = "nord"
	if err := config.Save(hostPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("{\"tui\":{\"theme\":\"nord\"}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hostBytes, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	projectBytes, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	project, err := registry.Add(t.Context(), "reasoning", cwd)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewRuntimeManager(t.Context(), executable, filepath.Join(directory, "sessions"))
	defer manager.Close()
	snapshot, err := manager.Open(t.Context(), project, "", "fake", "fake-1")
	if err != nil {
		exit, _ := os.ReadFile(filepath.Join(directory, "worker-exit"))
		t.Fatalf("worker: %v exit %s", err, exit)
	}
	check := func() {
		t.Helper()
		host, err := os.ReadFile(hostPath)
		if err != nil || !bytes.Equal(host, hostBytes) {
			t.Fatalf("host config changed: %v", err)
		}
		project, err := os.ReadFile(projectPath)
		if err != nil || !bytes.Equal(project, projectBytes) {
			t.Fatalf("project config changed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(directory, "provider-called")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("control invoked provider")
		}
		s, ok := manager.Snapshot(snapshot.ProjectID)
		if !ok || len(s.Messages) != 0 || s.Status != "idle" {
			t.Fatalf("control started work: %+v", s)
		}
	}
	inspect := func() web.RuntimeReasoning {
		t.Helper()
		v, err := manager.InspectReasoning(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		check()
		return v
	}
	apply := func(field, value string) web.RuntimeReasoning {
		t.Helper()
		before := inspect()
		after, err := manager.SetReasoning(t.Context(), project.ID, snapshot.InstanceID, web.RuntimeReasoningInput{Expected: before, Scope: "session", Field: field, Value: value, Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		if after.DefaultsAvailable || !after.CurrentSessionAvailable || after.BranchID != before.BranchID || after.TipID != before.TipID {
			t.Fatalf("wrong scope: %+v", after)
		}
		check()
		return after
	}
	apply("thinking", "high")
	if err := manager.SetMode(t.Context(), project.ID, snapshot.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	if v := inspect(); v.Thinking != "medium" {
		t.Fatalf("Plan effective: %+v", v)
	}
	apply("thinking", "low")
	apply("reasoning_summary", "detailed")
	apply("text_verbosity", "high")
	if err := manager.SetMode(t.Context(), project.ID, snapshot.InstanceID, "default"); err != nil {
		t.Fatal(err)
	}
	if v := inspect(); v.Thinking != "high" || v.ReasoningSummary != "detailed" || v.TextVerbosity != "high" {
		t.Fatalf("Default overwritten: %+v", v)
	}
	if err := manager.SetMode(t.Context(), project.ID, snapshot.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	if v := inspect(); v.Thinking != "low" {
		t.Fatalf("Plan override lost: %+v", v)
	}
	before := inspect()
	if _, err := manager.SetReasoning(t.Context(), project.ID, snapshot.InstanceID, web.RuntimeReasoningInput{Expected: before, Scope: "defaults", Field: "thinking", Value: "high", Confirm: true}); err == nil {
		t.Fatal("persisted fallback accepted")
	}
	check()
}
