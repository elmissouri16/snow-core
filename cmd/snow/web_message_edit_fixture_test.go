//go:build darwin || linux

package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const messageEditFixtureEnv = "SNOW_WEB_MESSAGE_EDIT_FIXTURE_DIR"

func init() {
	if directory := os.Getenv(messageEditFixtureEnv); directory != "" && slices.Contains(os.Args, "--mode") {
		code := runMessageEditFixtureWorker(directory)
		_ = os.WriteFile(filepath.Join(directory, "worker-exit"), []byte(fmt.Sprint(code)), 0600)
		os.Exit(code)
	}
}

// Only the provider is fake. Application admission, append-only session storage,
// normalized events, RPC and web projection are production implementations.
func runMessageEditFixtureWorker(directory string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 90
	}
	if _, err := permissionFixtureProject(directory, cwd); err != nil {
		return 91
	}
	opts, err := policyFixtureOptions(os.Args[1:])
	if err != nil || opts.Provider != "fake" || opts.Model != "fake-1" || !opts.NoSession || !opts.NoPlugins || !opts.NoMCP || !opts.NoSkills || opts.Subagents == nil || *opts.Subagents {
		return 92
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, err := app.New(ctx, opts)
	if err != nil {
		return 93
	}
	defer a.Close()
	a.Providers["fake"] = &messageEditFixtureProvider{Provider: fake.New(nil), directory: directory}
	if err := a.SetProvider("fake"); err != nil {
		return 94
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
		return 95
	}
	return 0
}

type messageEditFixtureProvider struct {
	*fake.Provider
	directory string
	calls     atomic.Uint64
}

func (p *messageEditFixtureProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	var users []string
	for _, message := range request.Messages {
		if message.Role == protocol.RoleUser {
			for _, block := range message.Content {
				if block.Type == protocol.BlockText {
					users = append(users, block.Text)
				}
			}
		}
	}
	data, err := json.Marshal(users)
	if err != nil {
		return nil, err
	}
	call := p.calls.Add(1)
	if err := os.WriteFile(filepath.Join(p.directory, fmt.Sprintf("context-%d.json", call)), data, 0600); err != nil {
		return nil, err
	}
	prefix := "reply: "
	if _, err := os.Stat(filepath.Join(p.directory, "regeneration-fixture")); err == nil && call > 3 {
		prefix = "regenerated: "
		if _, err := os.Stat(filepath.Join(p.directory, "hold-regeneration")); err == nil {
			if err := os.WriteFile(filepath.Join(p.directory, "regeneration-started"), []byte("started\n"), 0600); err != nil {
				return nil, err
			}
			if err := permissionFixtureGate(ctx, filepath.Join(p.directory, "release-regeneration")); err != nil {
				return nil, err
			}
		}
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("fixture received no user input")
	}
	reply := prefix + users[len(users)-1]
	if _, err := os.Stat(filepath.Join(p.directory, "queue-fixture")); err == nil {
		if err := os.WriteFile(filepath.Join(p.directory, fmt.Sprintf("queue-started-%d", call)), []byte("started\n"), 0600); err != nil {
			return nil, err
		}
		if err := permissionFixtureGate(ctx, filepath.Join(p.directory, fmt.Sprintf("queue-release-%d", call))); err != nil {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(p.directory, fmt.Sprintf("queue-fail-%d", call))); err == nil {
			return nil, errors.New("fictional queue fixture provider failure")
		}
		reply = fmt.Sprintf("queue reply %d: %s", call, users[len(users)-1])
	}
	if _, err := os.Stat(filepath.Join(p.directory, "regeneration-mixed-plan")); err == nil && call > 3 {
		reply = "Intro\n<proposed_plan>\n# Fictional plan\n- Review a fictional fixture.\n</proposed_plan>\nOutro"
	}
	stop := protocol.StopStop
	if _, err := os.Stat(filepath.Join(p.directory, "provider-abort")); err == nil {
		stop = protocol.StopAborted
	}
	return fake.New([]fake.Step{
		{Kind: fake.StepText, Text: reply},
		{Kind: fake.StepDone, Stop: stop},
	}).Chat(ctx, request)
}

// Reuse the real permission/tool worker to verify that editing drops old tool
// rows but never reverses an already executed filesystem operation.
func TestWebMessageEditKeepsToolEffects(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "seed.txt"), []byte("policy-read-safe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", directory)
	t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
	t.Setenv(policyFixtureEnv, directory)
	for _, name := range []string{messageEditFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv} {
		t.Setenv(name, "")
	}
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	project, err := registry.Add(t.Context(), "tool-edit", cwd)
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
		t.Fatal(err)
	}
	if err := manager.SetPermissionMode(t.Context(), project.ID, snapshot.InstanceID, "allow", true); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"read", "write-edited-tool", "read"} {
		if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, text); err != nil {
			t.Fatal(err)
		}
		snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	}
	source := ""
	for _, message := range snapshot.Messages {
		if message.Role == "user" && message.Text == "write-edited-tool" {
			source = message.ID
		}
	}
	prepared, err := manager.PrepareMessageEdit(t.Context(), project.ID, snapshot.InstanceID, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CommitMessageEdit(t.Context(), project.ID, snapshot.InstanceID, prepared.EditToken, "read"); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	want := []string{"user:read", "tool:read", "assistant:read succeeded", "user:read", "tool:read", "assistant:read succeeded"}
	if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
		t.Fatalf("edited tool timeline: %q, want %q", got, want)
	}
	if snapshot.PermissionMode != "allow" {
		t.Fatalf("edit changed current authority to %q", snapshot.PermissionMode)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "write-edited-tool.txt"))
	if err != nil || string(data) != "policy-write-safe\n" {
		t.Fatalf("editing reversed a real tool effect: %q %v", data, err)
	}
}

func TestWebMessageEditRealWorker(t *testing.T) {
	for target := range 3 {
		t.Run([]string{"first", "middle", "latest"}[target], func(t *testing.T) {
			directory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			cwd := filepath.Join(directory, "project-a")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", directory)
			t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
			t.Setenv(messageEditFixtureEnv, directory)
			for _, name := range []string{policyFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv} {
				t.Setenv(name, "")
			}
			registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
			if err != nil {
				t.Fatal(err)
			}
			defer registry.Close()
			project, err := registry.Add(t.Context(), "editable", cwd)
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
				t.Fatalf("open: %v; worker exit %s", err, exit)
			}
			original := []string{"first fictional request", "middle fictional request", "last fictional request"}
			for _, text := range original {
				if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, text); err != nil {
					t.Fatal(err)
				}
				snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
			}
			sessionID, title, instanceID := snapshot.SessionID, snapshot.SessionName, snapshot.InstanceID
			var users []web.RuntimeMessage
			for _, message := range snapshot.Messages {
				if message.Role == "user" {
					users = append(users, message)
				}
			}
			if len(users) != 3 || !users[target].CanEdit {
				t.Fatalf("live users lack authoritative editing identity: %+v", users)
			}
			prepared, err := manager.PrepareMessageEdit(t.Context(), project.ID, instanceID, users[target].ID)
			if err != nil || prepared.Text != original[target] {
				t.Fatalf("prepare exact live source: %+v %v", prepared, err)
			}
			before, _ := manager.Snapshot(project.ID)
			if got := webToolTimeline(t, before); len(got) != 6 {
				t.Fatalf("prepare mutated visible history: %q", got)
			}
			replacement := original[target] + " revised"
			snapshot, err = manager.CommitMessageEdit(t.Context(), project.ID, instanceID, prepared.EditToken, replacement)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.InstanceID == instanceID || snapshot.SessionID != sessionID || snapshot.SessionName != title {
				t.Fatalf("edit must rotate authority, not chat: %+v", snapshot)
			}
			snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
			wantUsers := append(slices.Clone(original[:target]), replacement)
			var wantTimeline []string
			for _, text := range wantUsers {
				wantTimeline = append(wantTimeline, "user:"+text, "assistant:reply: "+text)
			}
			if got := webToolTimeline(t, snapshot); !slices.Equal(got, wantTimeline) {
				t.Fatalf("active edit path:\ngot  %q\nwant %q", got, wantTimeline)
			}
			data, err := os.ReadFile(filepath.Join(directory, "context-4.json"))
			var providerUsers []string
			if err != nil || json.Unmarshal(data, &providerUsers) != nil || !slices.Equal(providerUsers, wantUsers) {
				t.Fatalf("provider retained removed continuation: %s, want %q; %v", data, wantUsers, err)
			}
			if _, err := manager.CommitMessageEdit(t.Context(), project.ID, instanceID, prepared.EditToken, replacement); err == nil {
				t.Fatal("old edit instance replay accepted")
			}
			if _, err := manager.CommitMessageEdit(t.Context(), project.ID, snapshot.InstanceID, prepared.EditToken, replacement); err == nil {
				t.Fatal("consumed token replay accepted")
			}
			if err := manager.CloseProject(t.Context(), project.ID, snapshot.InstanceID); err != nil {
				t.Fatal(err)
			}
			snapshot, err = manager.Open(t.Context(), project, sessionID, "fake", "fake-1")
			if err != nil {
				t.Fatal(err)
			}
			if got := webToolTimeline(t, snapshot); !slices.Equal(got, wantTimeline) {
				t.Fatalf("reopen resurrected removed continuation: %q, want %q", got, wantTimeline)
			}
		})
	}
}
