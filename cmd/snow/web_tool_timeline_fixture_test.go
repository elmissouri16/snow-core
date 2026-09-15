//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/internal/web"
)

// Reuse the real app/RPC worker and safe read/write provider from the policy
// fixture, rather than teaching a synthetic worker the ordering we expect.
func TestWebToolTimelineRealWorkerMultiplePrompts(t *testing.T) {
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
	project, err := registry.Add(t.Context(), "timeline", cwd)
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
	var want []string
	firstOwner, firstCall := "", ""
	for index, prompt := range []string{"read", "write-timeline", "read"} {
		if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, prompt); err != nil {
			t.Fatal(err)
		}
		snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
		tool, result := "read", "read succeeded"
		if prompt != "read" {
			tool, result = "write", "tool succeeded"
		}
		want = append(want, "user:"+prompt, "tool:"+tool, "assistant:"+result)
		if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
			t.Fatalf("prompt %d timeline:\ngot  %q\nwant %q", index, got, want)
		}
		if len(snapshot.Activities) != index+1 {
			t.Fatalf("activity count: %d", len(snapshot.Activities))
		}
		if index == 0 {
			firstOwner, firstCall = snapshot.Activities[0].MessageID, snapshot.Activities[0].ID
		} else if snapshot.Activities[0].MessageID != firstOwner || snapshot.Activities[0].ID != firstCall {
			t.Fatal("later prompt moved or replaced the first call")
		}
	}
	if snapshot.Activities[0].ID == snapshot.Activities[2].ID || snapshot.Activities[0].MessageID == snapshot.Activities[2].MessageID {
		t.Fatal("reused provider call ID merged separate prompts")
	}
	if body, err := os.ReadFile(filepath.Join(cwd, "write-timeline.txt")); err != nil || string(body) != "policy-write-safe\n" {
		t.Fatalf("real fixture write: %q %v", body, err)
	}
	saved := snapshot.SessionID
	if err := manager.CloseProject(t.Context(), project.ID, snapshot.InstanceID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = manager.Open(t.Context(), project, saved, "fake", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Activities) != 0 {
		t.Fatal("reopened history reused live activity projection")
	}
	if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
		t.Fatalf("saved chronology differs:\ngot  %q\nwant %q", got, want)
	}
}

func webToolTimeline(t *testing.T, snapshot web.RuntimeSnapshot) []string {
	t.Helper()
	var result []string
	seen := make(map[string]bool)
	for _, message := range snapshot.Messages {
		if message.Role == "tool_activity" {
			count := 0
			for _, activity := range snapshot.Activities {
				if activity.MessageID != message.ID {
					continue
				}
				if seen[activity.ID] || activity.Status != "completed" {
					t.Fatalf("duplicate or incomplete activity: %+v", activity)
				}
				seen[activity.ID] = true
				count++
				result = append(result, "tool:"+activity.Tool)
			}
			if count == 0 {
				t.Fatal("empty chronological marker")
			}
			continue
		}
		if message.Text != "" {
			result = append(result, message.Role+":"+message.Text)
		}
		for _, tool := range message.Tools {
			if tool.OwnerID != message.SourceID || tool.Status != "completed" {
				t.Fatalf("saved association/outcome: %+v in %+v", tool, message)
			}
			result = append(result, "tool:"+tool.Tool)
		}
	}
	if len(seen) != len(snapshot.Activities) {
		t.Fatal("live call has no chronological marker")
	}
	return result
}
