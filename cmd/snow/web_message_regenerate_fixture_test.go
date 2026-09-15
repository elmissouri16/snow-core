//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/web"
)

// The existing worker supplies production App/RPC/session behavior. Only its
// fake provider supplies deterministic text and an optional cancelable gate.
func newMessageRegenerateFixture(t *testing.T, policy bool) (*web.RuntimeManager, web.Project, web.RuntimeSnapshot, string) {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(directory, "regeneration-fixture"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", directory)
	t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
	for _, name := range []string{messageEditFixtureEnv, policyFixtureEnv, permissionFixtureEnv, streamFixtureEnv, cancelFixtureEnv} {
		t.Setenv(name, "")
	}
	if policy {
		t.Setenv(policyFixtureEnv, directory)
	} else {
		t.Setenv(messageEditFixtureEnv, directory)
	}
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(directory, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	project, err := registry.Add(t.Context(), "regenerate", cwd)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := web.NewRuntimeManager(t.Context(), executable, filepath.Join(directory, "sessions"))
	t.Cleanup(func() { _ = manager.Close() })
	snapshot, err := manager.Open(t.Context(), project, "", "fake", "fake-1")
	if err != nil {
		exit, _ := os.ReadFile(filepath.Join(directory, "worker-exit"))
		t.Fatalf("open: %v; worker exit %s", err, exit)
	}
	return manager, project, snapshot, directory
}

func TestWebMessageRegenerateRealWorker(t *testing.T) {
	for _, name := range []string{"first", "middle", "latest", "identical", "canceled"} {
		t.Run(name, func(t *testing.T) {
			manager, project, snapshot, directory := newMessageRegenerateFixture(t, false)
			original := []string{"first fictional request", "middle fictional request", "last fictional request"}
			target := 1
			switch name {
			case "first":
				target = 0
			case "latest":
				target = 2
			case "identical":
				original = []string{"same fictional request", "same fictional request", "same fictional request"}
			}
			for _, text := range original {
				if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, text); err != nil {
					t.Fatal(err)
				}
				snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
			}
			oldInstance, sessionID, title := snapshot.InstanceID, snapshot.SessionID, snapshot.SessionName
			var replies []web.RuntimeMessage
			for _, message := range snapshot.Messages {
				if message.Role == "assistant" && message.CanRegenerate {
					replies = append(replies, message)
				}
			}
			if len(replies) != 3 {
				t.Fatalf("live replies lack authoritative regeneration identity: %+v", snapshot.Messages)
			}
			prepared, err := manager.PrepareMessageRegenerate(t.Context(), project.ID, oldInstance, replies[target].ID)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := manager.Snapshot(project.ID)
			if len(webToolTimeline(t, before)) != 6 || before.InstanceID != oldInstance {
				t.Fatal("preparation changed the live path")
			}
			if name == "canceled" {
				if err := os.WriteFile(filepath.Join(directory, "hold-regeneration"), []byte("hold\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err = manager.CommitMessageRegenerate(t.Context(), project.ID, oldInstance, prepared.EditToken)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.InstanceID == oldInstance || snapshot.SessionID != sessionID || snapshot.SessionName != title {
				t.Fatalf("regeneration must rotate authority, not chat: %+v", snapshot)
			}
			if name == "canceled" {
				if snapshot.Status != "running" || snapshot.CancelToken == "" {
					t.Fatalf("replacement lacks Stop authority: %+v", snapshot)
				}
				if err := manager.CancelTurn(t.Context(), project.ID, snapshot.InstanceID, snapshot.CancelToken); err != nil {
					t.Fatal(err)
				}
			}
			snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
			wantUsers := original[:target+1]
			var wantTimeline []string
			for i, text := range wantUsers {
				wantTimeline = append(wantTimeline, "user:"+text)
				if i < target {
					wantTimeline = append(wantTimeline, "assistant:reply: "+text)
				} else if name != "canceled" {
					wantTimeline = append(wantTimeline, "assistant:regenerated: "+text)
				}
			}
			if got := webToolTimeline(t, snapshot); !slices.Equal(got, wantTimeline) {
				t.Fatalf("replacement path: %q, want %q", got, wantTimeline)
			}
			// Cancellation may precede provider admission; ordinary regeneration must
			// send the exact original prompt, without any duplicate or removed suffix.
			if name != "canceled" {
				data, err := os.ReadFile(filepath.Join(directory, "context-4.json"))
				var got []string
				if err != nil || json.Unmarshal(data, &got) != nil || !slices.Equal(got, wantUsers) {
					t.Fatalf("provider context: %s, want %q; %v", data, wantUsers, err)
				}
			}
			if _, err := manager.CommitMessageRegenerate(t.Context(), project.ID, oldInstance, prepared.EditToken); err == nil {
				t.Fatal("retired instance replay accepted")
			}
			if _, err := manager.CommitMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, prepared.EditToken); err == nil {
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
				t.Fatalf("reopen resurrected discarded suffix: %q, want %q", got, wantTimeline)
			}
			if name != "canceled" {
				var saved []web.RuntimeMessage
				for _, message := range snapshot.Messages {
					if message.Role == "assistant" && message.CanRegenerate {
						saved = append(saved, message)
					}
				}
				if len(saved) != len(wantUsers) {
					t.Fatalf("reopened plain replies lost eligibility: %+v", snapshot.Messages)
				}
				reply := saved[len(saved)-1]
				if reply.SourceID == "" || reply.SourceTurnID != "" {
					t.Fatalf("reopened target lacks exact saved identity: %+v", reply)
				}
				prepared, err := manager.PrepareMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, reply.ID)
				if err != nil {
					t.Fatalf("prepare reopened reply: %v", err)
				}
				if err := os.Remove(filepath.Join(directory, "context-1.json")); err != nil {
					t.Fatal(err)
				}
				if _, err := manager.CommitMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, prepared.EditToken); err != nil {
					t.Fatalf("regenerate reopened reply: %v", err)
				}
				snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
				wantTimeline[len(wantTimeline)-1] = "assistant:reply: " + original[target]
				if got := webToolTimeline(t, snapshot); !slices.Equal(got, wantTimeline) {
					t.Fatalf("saved-source replacement: %q, want %q", got, wantTimeline)
				}
				data, err := os.ReadFile(filepath.Join(directory, "context-1.json"))
				var got []string
				if err != nil || json.Unmarshal(data, &got) != nil || !slices.Equal(got, wantUsers) {
					t.Fatalf("reopened provider context: %s, want %q; %v", data, wantUsers, err)
				}
			}
		})
	}
}

func TestWebMessageRegenerateMixedPlanEligibility(t *testing.T) {
	manager, project, snapshot, directory := newMessageRegenerateFixture(t, false)
	if err := manager.SetMode(t.Context(), project.ID, snapshot.InstanceID, "plan"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first plain request", "middle plain request", "last plain request"} {
		if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, text); err != nil {
			t.Fatal(err)
		}
		snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	}
	var source string
	for _, message := range snapshot.Messages {
		if message.Role == "assistant" && message.CanRegenerate {
			source = message.ID
		}
	}
	if source == "" {
		t.Fatalf("plain Plan Mode reply must remain eligible: %+v", snapshot.Messages)
	}
	prepared, err := manager.PrepareMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "regeneration-mixed-plan"), []byte("mixed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CommitMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, prepared.EditToken); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	assertMixed := func(s web.RuntimeSnapshot) {
		t.Helper()
		intro, outro, plan := false, false, false
		for _, message := range s.Messages {
			if message.Role == "plan" {
				plan = true
			}
			if message.Role == "assistant" && strings.TrimSpace(message.Text) == "Intro" {
				intro = true
				if message.CanRegenerate {
					t.Fatal("mixed-response intro became regeneratable")
				}
			}
			if message.Role == "assistant" && strings.TrimSpace(message.Text) == "Outro" {
				outro = true
				if message.CanRegenerate {
					t.Fatal("mixed-response outro became regeneratable")
				}
			}
		}
		if !intro || !outro || !plan {
			t.Fatalf("fixture did not exercise mixed text/plan/text: %+v", s.Messages)
		}
	}
	assertMixed(snapshot)
	sessionID := snapshot.SessionID
	if err := manager.CloseProject(t.Context(), project.ID, snapshot.InstanceID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = manager.Open(t.Context(), project, sessionID, "fake", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	assertMixed(snapshot)
}

func TestWebMessageRegenerateRespectsCurrentToolPolicy(t *testing.T) {
	manager, project, snapshot, _ := newMessageRegenerateFixture(t, true)
	if err := manager.SetPermissionMode(t.Context(), project.ID, snapshot.InstanceID, "allow", true); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"read", "write-regenerated-tool", "read"} {
		if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, text); err != nil {
			t.Fatal(err)
		}
		snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	}
	var replies []web.RuntimeMessage
	for _, message := range snapshot.Messages {
		if message.Role == "assistant" && message.CanRegenerate {
			replies = append(replies, message)
		}
	}
	if len(replies) != 3 {
		t.Fatalf("tool turn final replies: %+v", snapshot.Messages)
	}
	path := filepath.Join(project.Path, "write-regenerated-tool.txt")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "policy-write-safe\n" {
		t.Fatalf("initial real write: %q %v", data, err)
	}
	if err := manager.SetPermissionMode(t.Context(), project.ID, snapshot.InstanceID, "deny", false); err != nil {
		t.Fatal(err)
	}
	prepared, err := manager.PrepareMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, replies[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CommitMessageRegenerate(t.Context(), project.ID, snapshot.InstanceID, prepared.EditToken); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	if snapshot.PermissionMode != "deny" || snapshot.Permission != nil {
		t.Fatalf("regeneration revived prior approval authority: %+v", snapshot)
	}
	last := snapshot.Messages[len(snapshot.Messages)-1]
	if last.Role != "assistant" || last.Text != "tool denied" {
		t.Fatalf("regenerated write bypassed Deny: %+v", snapshot.Messages)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "policy-write-safe\n" {
		t.Fatalf("regeneration removed an earlier effect: %q %v", data, err)
	}
	users := 0
	for _, message := range snapshot.Messages {
		if message.Role == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("regeneration duplicated the prompt or retained the suffix: %+v", snapshot.Messages)
	}
}
