//go:build darwin || linux

package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestWebPermissionFixtureWorkerGuard(t *testing.T) {
	catalog := []string{"--mode", "rpc", "--rpc-startup", "catalog"}
	eager := []string{"--mode", "rpc", "--rpc-startup", "eager", "--no-session", "--managed-explicit-goals", "--no-plugins", "--subagents", "--no-skills", "--no-debug", "--tools", "read,glob,grep,write,edit,bash,ask_user,get_goal,create_goal,update_goal,process_start,process_status,process_logs,process_stop,process_list"}
	for _, args := range [][]string{catalog, eager, append(slices.Clone(eager), "--provider", "fake", "--model", "fake-1")} {
		if _, valid := permissionFixtureWorkerArgs(args); !valid {
			t.Fatalf("rejected exact manager args: %v", args)
		}
	}
	for _, args := range [][]string{nil, {"--mode", "rpc"}, append(slices.Clone(catalog), "--permission", "allow"), append(slices.Clone(eager), "--provider", "openai"), append(slices.Clone(eager), "--unknown"), eager[:len(eager)-1]} {
		if _, valid := permissionFixtureWorkerArgs(args); valid {
			t.Fatalf("accepted non-fixture args: %v", args)
		}
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(directory, "project-a")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if project, err := permissionFixtureProject(directory, cwd); err != nil || project != "a" {
		t.Fatalf("project guard: %q %v", project, err)
	}
	if _, err := permissionFixtureProject(directory, directory); err == nil {
		t.Fatal("accepted unrelated worker CWD")
	}
	if _, err := permissionFixtureProject("relative", cwd); err == nil {
		t.Fatal("accepted relative fixture root")
	}
}

func TestWebPermissionFixtureGateCancellation(t *testing.T) {
	kill := filepath.Join(t.TempDir(), "kill")
	if err := os.WriteFile(kill, []byte("kill\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !permissionFixtureConsumeMarker(kill, "kill") {
		t.Fatal("valid kill marker was not consumed")
	}
	if _, err := os.Lstat(kill); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("consumed kill marker remains: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := permissionFixtureGate(ctx, filepath.Join(t.TempDir(), "absent")); !errors.Is(err, context.Canceled) {
		t.Fatalf("gate cancellation: %v", err)
	}
	path := filepath.Join(t.TempDir(), "release")
	if err := os.WriteFile(path, []byte("release\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := permissionFixtureGate(t.Context(), path); err != nil {
		t.Fatal(err)
	}
}

// This network-free test runs the fixture provider, actual app.Agent loop,
// existing builtin write and real permission broker. It proves an approval is
// necessary, denial still has exactly one provider followup, and failure gates
// surround the real filesystem operation rather than fabricated tool events.
func TestWebPermissionFixtureRealWriteBroker(t *testing.T) {
	for _, test := range []struct {
		token  string
		denied bool
	}{{"allow", false}, {"deny", true}, {"allow", true}, {"before-write", false}, {"after-write", false}} {
		t.Run(test.token, func(t *testing.T) {
			token := test.token
			directory := t.TempDir()
			cwd := filepath.Join(directory, "project-a")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", directory)
			t.Setenv("SNOW_HOME", filepath.Join(directory, "home"))
			t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(directory, "sessions"))
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			// Omit Permission deliberately: a fresh session must default to ask.
			a, err := app.New(ctx, app.Options{CWD: cwd, Provider: "fake", Model: "fake-1", NoSession: true, Tools: []string{"write"}, NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false), Debug: new(false)})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if a.Perm.Mode() != permission.ModeAsk {
				t.Fatal("fresh fixture session did not default to ask")
			}
			evidence := &permissionFixtureEvidence{directory: directory, project: "a"}
			if err := installPermissionFixture(a, evidence, cwd); err != nil {
				t.Fatal(err)
			}
			a.EnablePermissionReplies()
			requests := make(chan protocol.PermissionRequest, 2)
			unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
				if event.Type == protocol.EvPermissionRequest && event.Permission != nil {
					requests <- event.Permission.Request
				}
			})
			defer unsubscribe()
			done := make(chan error, 1)
			go func() { done <- a.Agent.Prompt(ctx, token) }()
			var request protocol.PermissionRequest
			select {
			case request = <-requests:
			case err := <-done:
				t.Fatalf("turn ended without real approval: %v", err)
			case <-ctx.Done():
				t.Fatal("approval deadline")
			}
			if request.Tool != "write" {
				t.Fatalf("permission tool = %q", request.Tool)
			}
			path := filepath.Join(cwd, token+".txt")
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("write occurred before approval")
			}
			if _, err := os.Stat(evidence.path("executions.jsonl")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("decorator ran before approval")
			}
			decision := protocol.PermissionAllow
			if test.denied {
				decision = protocol.PermissionDeny
			}
			if err := a.ReplyPermission(protocol.PermissionResponse{RequestID: request.ID, Decision: decision}); err != nil {
				t.Fatal(err)
			}
			if token == "before-write" || token == "after-write" {
				boundary, release := "started", "release-before"
				if token == "after-write" {
					boundary, release = "executed", "release-after"
				}
				for {
					if _, err := os.Stat(evidence.path(token + "-" + boundary)); err == nil {
						break
					}
					select {
					case err := <-done:
						t.Fatalf("turn ended before gate: %v", err)
					case <-ctx.Done():
						t.Fatal("gate deadline")
					case <-time.After(10 * time.Millisecond):
					}
				}
				_, err := os.Stat(path)
				if token == "before-write" && !errors.Is(err, os.ErrNotExist) {
					t.Fatal("before gate allowed write")
				}
				if token == "after-write" && err != nil {
					t.Fatal("after gate reached without write")
				}
				records := permissionFixtureReadRecords(t, evidence.path("calls.jsonl"))
				if len(records) != 1 {
					t.Fatal("provider continued while tool result was gated")
				}
				if err := os.WriteFile(evidence.path(token+"-"+release), []byte("release"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("turn completion deadline")
			}
			select {
			case <-requests:
				t.Fatal("second write permission requested")
			default:
			}
			calls := permissionFixtureReadRecords(t, evidence.path("calls.jsonl"))
			if len(calls) != 2 || calls[0].Event != "call" || calls[1].Event != "followup" || calls[1].Count != 2 || calls[1].IsError != (test.denied) {
				t.Fatalf("provider steps: %+v", calls)
			}
			if test.denied {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("denied write exists")
				}
				if _, err := os.Stat(evidence.path("executions.jsonl")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("denied write executed")
				}
			} else {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != token+"\n" {
					t.Fatalf("write content: %q %v", data, err)
				}
				records := permissionFixtureReadRecords(t, evidence.path("executions.jsonl"))
				if len(records) != 3 || records[0].Event != "started" || records[1].Event != "executed" || records[2].Event != "finish" {
					t.Fatalf("execution records: %+v", records)
				}
				for _, record := range records {
					if record.Count != 1 || record.Token != token {
						t.Fatalf("duplicate execution: %+v", record)
					}
				}
			}
			restored := &permissionFixtureEvidence{directory: directory, project: "a"}
			if err := restored.restore(); err != nil {
				t.Fatal(err)
			}
			if restored.calls != 2 || restored.executions != evidence.executions {
				t.Fatal("restart lost durable counters")
			}
		})
	}
}

func permissionFixtureReadRecords(t *testing.T, path string) []permissionFixtureRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []permissionFixtureRecord
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var record permissionFixtureRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}
