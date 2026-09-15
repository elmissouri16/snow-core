//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func queueFixtureRelease(t *testing.T, directory string, call int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("queue-release-%d", call)), []byte("release\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func queueFixtureReady(t *testing.T, manager *web.RuntimeManager, project web.Project) web.RuntimeSnapshot {
	t.Helper()
	return policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		return s.Status == "running" && s.Queue != nil && s.Queue.Token != "" && s.Queue.CanEnqueue
	})
}

func queueFixtureEnqueue(t *testing.T, manager *web.RuntimeManager, snapshot web.RuntimeSnapshot, text string) web.RuntimeSnapshot {
	t.Helper()
	if snapshot.Queue == nil {
		t.Fatal("missing queue authority")
	}
	snapshot, err := manager.EnqueueFollowUp(t.Context(), snapshot.ProjectID, snapshot.InstanceID, snapshot.SessionID, snapshot.Queue.Token, snapshot.Queue.Revision, text)
	if err != nil {
		t.Fatalf("enqueue %q: %v", text, err)
	}
	return snapshot
}

func queueFixtureContext(t *testing.T, directory string, call int, want []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, fmt.Sprintf("context-%d.json", call)))
	var got []string
	if err != nil || json.Unmarshal(data, &got) != nil || !slices.Equal(got, want) {
		t.Fatalf("provider context %d = %s; want %q; %v", call, data, want, err)
	}
}

func queueFixtureStart(t *testing.T) (*web.RuntimeManager, web.Project, web.RuntimeSnapshot, string) {
	t.Helper()
	manager, project, snapshot, directory := newMessageRegenerateFixture(t, false)
	if err := os.WriteFile(filepath.Join(directory, "queue-fixture"), []byte("queue\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, "first fictional request"); err != nil {
		t.Fatal(err)
	}
	snapshot = queueFixtureReady(t, manager, project)
	return manager, project, snapshot, directory
}

func queueFixtureWaitCall(t *testing.T, manager *web.RuntimeManager, project web.Project, directory string, call int) web.RuntimeSnapshot {
	t.Helper()
	return policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool {
		_, err := os.Stat(filepath.Join(directory, fmt.Sprintf("queue-started-%d", call)))
		if err != nil || s.Status != "running" {
			return false
		}
		// The provider marker is written after durable delivery, but it is not
		// a barrier for the independent RPC event drain. Wait for this exact
		// mid-run input projection while the provider remains gated; never
		// substitute eventual idle state for the chronology under test.
		data, err := os.ReadFile(filepath.Join(directory, fmt.Sprintf("context-%d.json", call)))
		var delivered []string
		if err != nil || json.Unmarshal(data, &delivered) != nil {
			return false
		}
		var projected []string
		lastSourceID := ""
		for _, message := range s.Messages {
			if message.Role == "user" {
				lastSourceID = message.SourceID
				projected = append(projected, message.Text)
			}
		}
		return len(delivered) != 0 && lastSourceID != "" && slices.Equal(projected, delivered)
	})
}

func TestWebQueueNextRealWorker(t *testing.T) {
	manager, project, snapshot, directory := queueFixtureStart(t)
	instance, session, cancelToken := snapshot.InstanceID, snapshot.SessionID, snapshot.CancelToken
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "second original")
	if len(snapshot.Queue.Items) != 1 {
		t.Fatalf("queue: %+v", snapshot.Queue)
	}
	second := snapshot.Queue.Items[0].ID
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "remove this request")
	removed := snapshot.Queue.Items[1].ID
	stale := snapshot.Queue.Revision
	var err error
	snapshot, err = manager.UpdateQueuedFollowUp(t.Context(), project.ID, instance, session, snapshot.Queue.Token, snapshot.Queue.Revision, second, "second revised")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpdateQueuedFollowUp(t.Context(), project.ID, instance, session, snapshot.Queue.Token, stale, second, "stale overwrite"); err == nil {
		t.Fatal("stale queued edit accepted")
	}
	snapshot, err = manager.RemoveQueuedFollowUp(t.Context(), project.ID, instance, session, snapshot.Queue.Token, snapshot.Queue.Revision, removed)
	if err != nil {
		t.Fatal(err)
	}
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "third fictional request")
	if got := webToolTimeline(t, snapshot); !slices.Equal(got, []string{"user:first fictional request"}) {
		t.Fatalf("enqueue prematurely appended chat input: %q", got)
	}
	queueFixtureRelease(t, directory, 1)
	snapshot = queueFixtureWaitCall(t, manager, project, directory, 2)
	queueFixtureContext(t, directory, 2, []string{"first fictional request", "second revised"})
	if snapshot.InstanceID != instance || snapshot.SessionID != session || snapshot.CancelToken != cancelToken {
		t.Fatal("follow-up rotated the admitted run or Stop authority")
	}
	if got := webToolTimeline(t, snapshot); !slices.Equal(got, []string{"user:first fictional request", "assistant:queue reply 1: first fictional request", "user:second revised"}) {
		t.Fatalf("delivery chronology: %q", got)
	}
	queueFixtureRelease(t, directory, 2)
	snapshot = queueFixtureWaitCall(t, manager, project, directory, 3)
	queueFixtureContext(t, directory, 3, []string{"first fictional request", "second revised", "third fictional request"})
	queueFixtureRelease(t, directory, 3)
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	want := []string{"user:first fictional request", "assistant:queue reply 1: first fictional request", "user:second revised", "assistant:queue reply 2: second revised", "user:third fictional request", "assistant:queue reply 3: third fictional request"}
	if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
		t.Fatalf("batch timeline: %q, want %q", got, want)
	}
	if snapshot.Queue != nil && len(snapshot.Queue.Items) != 0 {
		t.Fatalf("delivered work remains queued: %+v", snapshot.Queue)
	}
	if err := manager.CloseProject(t.Context(), project.ID, instance); err != nil {
		t.Fatal(err)
	}
	snapshot, err = manager.Open(t.Context(), project, session, "fake", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
		t.Fatalf("queue history did not survive reopen: %q", got)
	}
}

func TestWebQueueNextStopRetainsForReview(t *testing.T) {
	manager, project, snapshot, directory := queueFixtureStart(t)
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "keep second request")
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "keep third request")
	if err := manager.CancelTurn(t.Context(), project.ID, snapshot.InstanceID, snapshot.CancelToken); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if snapshot.Queue == nil || snapshot.Queue.CanEnqueue || len(snapshot.Queue.Items) != 2 {
		t.Fatalf("Stop lost review work: %+v", snapshot.Queue)
	}
	for _, item := range snapshot.Queue.Items {
		if item.State != "held" {
			t.Fatalf("known undelivered input not held: %+v", item)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "context-2.json")); !os.IsNotExist(err) {
		t.Fatalf("Stop executed a follow-up: %v", err)
	}
	if _, err := manager.EnqueueFollowUp(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID, snapshot.Queue.Token, snapshot.Queue.Revision, "must not automatically run"); err == nil {
		t.Fatal("stopped queue accepted work")
	}
	beforeSwitch := snapshot
	if _, err := manager.Switch(t.Context(), project.ID, snapshot.InstanceID, "", true); err == nil {
		t.Fatal("session creation stranded retained queue work")
	}
	snapshot, _ = manager.Snapshot(project.ID)
	if snapshot.InstanceID != beforeSwitch.InstanceID || snapshot.SessionID != beforeSwitch.SessionID || snapshot.Queue == nil || snapshot.Queue.Token != beforeSwitch.Queue.Token || len(snapshot.Queue.Items) != 2 {
		t.Fatalf("rejected switch lost review authority: %+v", snapshot)
	}
	for snapshot.Queue != nil && len(snapshot.Queue.Items) > 0 {
		var err error
		snapshot, err = manager.RemoveQueuedFollowUp(t.Context(), project.ID, snapshot.InstanceID, snapshot.SessionID, snapshot.Queue.Token, snapshot.Queue.Revision, snapshot.Queue.Items[0].ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	queueFixtureRelease(t, directory, 2)
	if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, "explicit fresh request"); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	queueFixtureContext(t, directory, 2, []string{"first fictional request", "explicit fresh request"})
}

func TestWebQueueNextFailureRetainsWithoutReplay(t *testing.T) {
	manager, project, snapshot, directory := queueFixtureStart(t)
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "review after failure")
	if err := os.WriteFile(filepath.Join(directory, "queue-fail-1"), []byte("fail\n"), 0600); err != nil {
		t.Fatal(err)
	}
	queueFixtureRelease(t, directory, 1)
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && s.Error != "" })
	if snapshot.Queue == nil || snapshot.Queue.CanEnqueue || len(snapshot.Queue.Items) != 1 || snapshot.Queue.Items[0].State != "held" || snapshot.Queue.Items[0].Text != "review after failure" {
		t.Fatalf("failure lost unsent review work: %+v", snapshot.Queue)
	}
	if _, err := os.Stat(filepath.Join(directory, "context-2.json")); !os.IsNotExist(err) {
		t.Fatalf("failure retried a pending input: %v", err)
	}
	if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, "do not preempt review"); err == nil {
		t.Fatal("fresh prompt silently discarded review work")
	}
	if _, err := os.Stat(filepath.Join(directory, "context-2.json")); !os.IsNotExist(err) {
		t.Fatal("rejected fresh prompt started provider work")
	}
}

func TestWebQueueNextKeepsPerToolApprovals(t *testing.T) {
	manager, project, snapshot, _ := newMessageRegenerateFixture(t, true)
	if err := manager.SetPermissionMode(t.Context(), project.ID, snapshot.InstanceID, "ask", false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Prompt(t.Context(), project.ID, snapshot.InstanceID, "write-queue-first"); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Permission != nil && s.Queue != nil && s.Queue.CanEnqueue })
	firstPermission := snapshot.Permission.ID
	snapshot = queueFixtureEnqueue(t, manager, snapshot, "write-queued-permission")
	if snapshot.Permission == nil || snapshot.Permission.ID != firstPermission {
		t.Fatal("enqueue replaced the active approval")
	}
	if err := manager.ReplyPermission(t.Context(), project.ID, snapshot.InstanceID, firstPermission, protocol.PermissionAllow); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Permission != nil && s.Permission.ID != firstPermission })
	if snapshot.PermissionMode != "ask" {
		t.Fatal("queued delivery changed permission policy")
	}
	path := filepath.Join(project.Path, "write-queued-permission.txt")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("queued write executed without its own approval: %v", err)
	}
	if err := manager.ReplyPermission(t.Context(), project.ID, snapshot.InstanceID, snapshot.Permission.ID, protocol.PermissionDeny); err != nil {
		t.Fatal(err)
	}
	snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("denied queued write took effect: %v", err)
	}
	if last := snapshot.Messages[len(snapshot.Messages)-1]; last.Role != "assistant" || last.Text != "tool denied" {
		t.Fatalf("unexpected queued denial response: %+v", last)
	}
}

func TestWebQueuedHistoryEditAndRegenerate(t *testing.T) {
	for _, action := range []string{"edit", "regenerate"} {
		for target := range 3 {
			t.Run(fmt.Sprintf("%s-%d", action, target), func(t *testing.T) {
				manager, project, snapshot, directory := queueFixtureStart(t)
				original := []string{"first fictional request", "second fictional request", "third fictional request"}
				for _, text := range original[1:] {
					snapshot = queueFixtureEnqueue(t, manager, snapshot, text)
				}
				for call := 1; call <= 3; call++ {
					queueFixtureRelease(t, directory, call)
				}
				snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
				var users, replies []web.RuntimeMessage
				for _, message := range snapshot.Messages {
					if message.Role == "user" {
						if !message.CanEdit {
							t.Fatalf("queued root input lost editing: %+v", message)
						}
						users = append(users, message)
					}
					if message.Role == "assistant" {
						if !message.CanRegenerate {
							t.Fatalf("queued root reply lost regeneration: %+v", message)
						}
						replies = append(replies, message)
					}
				}
				if len(users) != 3 || len(replies) != 3 {
					t.Fatalf("queued fixture incomplete: %+v", snapshot.Messages)
				}
				queueFixtureRelease(t, directory, 4)
				wantUsers := slices.Clone(original[:target+1])
				oldInstance, session := snapshot.InstanceID, snapshot.SessionID
				if action == "edit" {
					prepared, err := manager.PrepareMessageEdit(t.Context(), project.ID, oldInstance, users[target].ID)
					if err != nil {
						t.Fatal(err)
					}
					if prepared.Text != original[target] {
						t.Fatalf("wrong input span: %q", prepared.Text)
					}
					wantUsers[target] = "edited " + original[target]
					if _, err := manager.CommitMessageEdit(t.Context(), project.ID, oldInstance, prepared.EditToken, wantUsers[target]); err != nil {
						t.Fatal(err)
					}
				} else {
					prepared, err := manager.PrepareMessageRegenerate(t.Context(), project.ID, oldInstance, replies[target].ID)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := manager.CommitMessageRegenerate(t.Context(), project.ID, oldInstance, prepared.EditToken); err != nil {
						t.Fatal(err)
					}
				}
				snapshot = policyFixtureWait(t, manager, project.ID, func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
				if snapshot.InstanceID == oldInstance || snapshot.SessionID != session {
					t.Fatal("revision failed to preserve chat and retire old authority")
				}
				queueFixtureContext(t, directory, 4, wantUsers)
				var want []string
				for i, text := range wantUsers {
					call := i + 1
					if i == target {
						call = 4
					}
					want = append(want, "user:"+text, fmt.Sprintf("assistant:queue reply %d: %s", call, text))
				}
				if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
					t.Fatalf("revised span history: %q, want %q", got, want)
				}
				if err := manager.CloseProject(t.Context(), project.ID, snapshot.InstanceID); err != nil {
					t.Fatal(err)
				}
				var err error
				snapshot, err = manager.Open(t.Context(), project, session, "fake", "fake-1")
				if err != nil {
					t.Fatal(err)
				}
				if got := webToolTimeline(t, snapshot); !slices.Equal(got, want) {
					t.Fatalf("reopened span history: %q", got)
				}
			})
		}
	}
}
