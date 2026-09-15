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
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const policyFixtureEnv = "SNOW_WEB_POLICY_FIXTURE_DIR"

func init() {
	if os.Getenv(policyFixtureEnv) != "" && slices.Contains(os.Args, "--mode") {
		code := runPolicyFixtureWorker()
		_ = os.WriteFile(filepath.Join(os.Getenv(policyFixtureEnv), "worker-exit"), []byte(fmt.Sprint(code)), 0600)
		os.Exit(code)
	}
}

// Parse the actual arguments emitted by RuntimeManager and use production's
// buildOptions, including its empty permission default. Do not reconstruct a
// handpicked Options struct that could silently discard a launch override.
func policyFixtureOptions(args []string) (app.Options, error) {
	cmd := &cobra.Command{}
	for _, name := range []string{"mode", "rpc-startup", "permission", "provider", "model"} {
		cmd.Flags().String(name, "", "")
	}
	for _, name := range []string{"no-session", "managed-explicit-goals", "no-plugins", "no-mcp", "no-skills", "no-subagents", "no-debug"} {
		cmd.Flags().Bool(name, false, "")
	}
	cmd.Flags().StringSlice("tools", nil, "")
	if err := cmd.ParseFlags(args); err != nil {
		return app.Options{}, err
	}
	if cmd.Flags().NArg() != 0 {
		return app.Options{}, errors.New("unexpected fixture argument")
	}
	return buildOptions(cmd)
}

func runPolicyFixtureWorker() int {
	directory := os.Getenv(policyFixtureEnv)
	cwd, err := os.Getwd()
	if err != nil {
		return 90
	}
	if _, err := permissionFixtureProject(directory, cwd); err != nil {
		return 91
	}
	opts, err := policyFixtureOptions(os.Args[1:])
	if err != nil || opts.Provider != "fake" || opts.Model != "fake-1" || !opts.NoSession || !opts.NoPlugins || !opts.NoMCP || !opts.NoSkills || opts.Subagents == nil || *opts.Subagents || opts.Debug == nil || *opts.Debug {
		return 92
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, err := app.New(ctx, opts)
	if err != nil {
		return 93
	}
	defer a.Close()
	a.Providers["fake"] = &policyFixtureProvider{Provider: fake.New(nil)}
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

type policyFixtureProvider struct{ *fake.Provider }

func (p *policyFixtureProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	prompt, user := "", -1
	for i, message := range request.Messages {
		if message.Role == protocol.RoleUser {
			user, prompt = i, ""
			for _, block := range message.Content {
				if block.Type == protocol.BlockText {
					prompt += block.Text
				}
			}
		}
	}
	if prompt != "read" && !strings.HasPrefix(prompt, "write-") {
		return nil, errors.New("unknown policy fixture prompt")
	}
	for _, ch := range prompt {
		if ch != '-' && (ch < 'a' || ch > 'z') {
			return nil, errors.New("invalid policy fixture token")
		}
	}
	for _, message := range request.Messages[user+1:] {
		if message.Role == protocol.RoleTool {
			text := "tool succeeded"
			if message.IsError {
				text = "tool denied"
			}
			if prompt == "read" {
				text = "read failed"
				for _, block := range message.Content {
					if strings.Contains(block.Text, "policy-read-safe") {
						text = "read succeeded"
					}
				}
			}
			return fake.New([]fake.Step{{Kind: fake.StepText, Text: text}}).Chat(ctx, request)
		}
	}
	tool, args := "read", map[string]string{"path": "seed.txt"}
	if prompt != "read" {
		tool, args = "write", map[string]string{"path": prompt + ".txt", "content": "policy-write-safe\n"}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return fake.New([]fake.Step{{Kind: fake.StepToolCall, ToolCallID: "policy-" + prompt, ToolName: tool, Arguments: encoded}, {Kind: fake.StepDone, Stop: protocol.StopToolUse}}).Chat(ctx, request)
}

// End to end: actual manager launch args -> CLI options -> app session binding ->
// real RPC mode setter/session switch -> real builtin permission gates. No worker
// policy map or synthetic session_info response can conceal restoration failure.
func TestWebPolicyRealWorkerRestoresPersistedModes(t *testing.T) {
	for _, mode := range []string{"deny", "allow"} {
		t.Run(mode, func(t *testing.T) {
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
			t.Setenv(permissionFixtureEnv, "")
			t.Setenv(streamFixtureEnv, "")
			registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
			if err != nil {
				t.Fatal(err)
			}
			defer registry.Close()
			project, err := registry.Add(t.Context(), "policy", cwd)
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
			if err != nil || snapshot.PermissionMode != "ask" {
				exit, _ := os.ReadFile(filepath.Join(directory, "worker-exit"))
				t.Fatalf("fresh policy: %+v %v, worker exit %s", snapshot, err, exit)
			}
			if err := manager.SetPermissionMode(t.Context(), project.ID, snapshot.InstanceID, mode, mode == "allow"); err != nil {
				t.Fatal(err)
			}
			savedID := snapshot.SessionID
			policyFixtureCheckTools(t, manager, project, snapshot.InstanceID, "initial", mode)
			snapshot, err = manager.Switch(t.Context(), project.ID, snapshot.InstanceID, "", false)
			if err != nil || snapshot.PermissionMode != "ask" {
				t.Fatalf("new-session baseline: %+v %v", snapshot, err)
			}
			policyFixtureCheckTools(t, manager, project, snapshot.InstanceID, "fresh", "ask")
			snapshot, err = manager.Switch(t.Context(), project.ID, snapshot.InstanceID, savedID, false)
			if err != nil || snapshot.PermissionMode != mode {
				t.Fatalf("restored policy after switch: got %q, want %q; %v", snapshot.PermissionMode, mode, err)
			}
			policyFixtureCheckTools(t, manager, project, snapshot.InstanceID, "switched", mode)
			if err := manager.CloseProject(t.Context(), project.ID, snapshot.InstanceID); err != nil {
				t.Fatal(err)
			}
			snapshot, err = manager.Open(t.Context(), project, savedID, "fake", "fake-1")
			if err != nil || snapshot.PermissionMode != mode {
				t.Fatalf("restored policy after restart: got %q, want %q; %v", snapshot.PermissionMode, mode, err)
			}
			policyFixtureCheckTools(t, manager, project, snapshot.InstanceID, "reopened", mode)
		})
	}
}

func policyFixtureWait(t *testing.T, manager *web.RuntimeManager, projectID string, ready func(web.RuntimeSnapshot) bool) web.RuntimeSnapshot {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, ok := manager.Snapshot(projectID)
		if !ok || snapshot.Status == "failed" {
			t.Fatalf("worker unavailable: %+v", snapshot)
		}
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	snapshot, _ := manager.Snapshot(projectID)
	t.Fatalf("worker did not reach expected state: %+v", snapshot)
	return web.RuntimeSnapshot{}
}

func policyFixtureCheckTools(t *testing.T, manager *web.RuntimeManager, project web.Project, instanceID, phase, mode string) {
	t.Helper()
	if err := manager.Prompt(t.Context(), project.ID, instanceID, "read"); err != nil {
		t.Fatal(err)
	}
	read := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		if s.Permission != nil {
			t.Fatal("read-risk operation unexpectedly asked for approval")
		}
		return s.Status == "idle"
	})
	if len(read.Messages) == 0 || read.Messages[len(read.Messages)-1].Text != "read succeeded" {
		t.Fatalf("actual read failed under %s: %+v", mode, read.Messages)
	}
	prompt := "write-" + phase
	if err := manager.Prompt(t.Context(), project.ID, instanceID, prompt); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project.Path, prompt+".txt")
	if mode == "ask" {
		pending := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Permission != nil })
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("Ask wrote before explicit approval")
		}
		if err := manager.ReplyPermission(t.Context(), project.ID, instanceID, pending.Permission.ID, protocol.PermissionAllow); err != nil {
			t.Fatal(err)
		}
	}
	done := policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		if mode != "ask" && s.Permission != nil {
			t.Fatalf("%s unexpectedly asked for write approval", mode)
		}
		return s.Status == "idle"
	})
	want := "tool succeeded"
	if mode == "deny" {
		want = "tool denied"
	}
	if len(done.Messages) == 0 || done.Messages[len(done.Messages)-1].Text != want {
		t.Fatalf("actual write result under %s: %+v", mode, done.Messages)
	}
	data, err := os.ReadFile(path)
	if mode == "deny" {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("Deny allowed a real write")
		}
	} else if err != nil || string(data) != "policy-write-safe\n" {
		t.Fatalf("%s did not execute approved write: %q %v", mode, data, err)
	}
}
