package web

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func openQueueRuntime(t *testing.T, mode string) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, mode)
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		commands, _ := os.ReadFile(log)
		t.Fatalf("open: %v; commands=%s", err, commands)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "original"); err != nil {
		current, _ := m.Snapshot(p.ID)
		commands, _ := os.ReadFile(log)
		t.Fatalf("prompt: %v snapshot=%+v commands=%s", err, current, commands)
	}
	// The fixture emits queue admission before its first text delta. Capture
	// both before comparing transcript lengths around later queue mutations;
	// ordinary streamed text is not optimistic delivery of a queued message.
	s = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Queue != nil && s.Queue.CanEnqueue && len(s.Messages) == 2 && s.Messages[1].Text == "first answer"
	})
	return m, p, s, log
}

func TestQueueExplicitMutationsDoNotChangePromptOrOptimisticallyDeliver(t *testing.T) {
	m, p, s, log := openQueueRuntime(t, "queue-normal")
	text := "exact\r\x1b[31m queue\n\u00a0"
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "ordinary send while busy"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("Prompt silently queued: %v", err)
	}
	if _, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.CancelToken, s.Queue.Revision, text); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("Stop token admitted queue: %v", err)
	}
	next, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Messages) != len(s.Messages) || len(next.Queue.Items) != 1 || next.Queue.Items[0].Text != text || next.Queue.Items[0].State != "pending" {
		t.Fatalf("optimistic delivery or text transformation: %+v", next)
	}
	if _, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "duplicate"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale CAS: %v", err)
	}
	s = next
	next, err = m.UpdateQueuedFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, s.Queue.Items[0].ID, "updated exact\rtext")
	if err != nil {
		t.Fatal(err)
	}
	if next.Queue.Items[0].Text != "updated exact\rtext" {
		t.Fatal("update transformed input")
	}
	s = next
	next, err = m.RemoveQueuedFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, s.Queue.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Queue.Items) != 0 || len(next.Messages) != len(s.Messages) {
		t.Fatal("removal inferred delivery")
	}
	commands, _ := os.ReadFile(log)
	if strings.Count(string(commands), "\nprompt\n") != 1 || strings.Count(string(commands), "queue_enqueue") != 1 || strings.Contains(string(commands), "queue_list") {
		t.Fatalf("implicit work/replay: %s", commands)
	}
}

func TestQueueDeliveryBeforeACKCannotResurrectPendingOrLoseHistory(t *testing.T) {
	m, p, s, _ := openQueueRuntime(t, "queue-deliver")
	after, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "queued request")
	if err != nil {
		t.Fatal(err)
	}
	after = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && len(s.Messages) == 4 })
	if after.Queue.CanEnqueue || len(after.Queue.Items) != 0 || after.Messages[0].SourceID != "user-1" || after.Messages[1].SourceID != "first-answer-id" || !after.Messages[1].CanRegenerate || after.Messages[2].SourceID != "queued-user-id" || !after.Messages[2].CanEdit || after.Messages[3].SourceID != "queued-answer-id" || !after.Messages[3].CanRegenerate {
		t.Fatalf("durable queued timeline: %+v", after)
	}
	if after.InstanceID != s.InstanceID || after.SessionID != s.SessionID || after.SessionName != s.SessionName {
		t.Fatal("queue created a different chat or root admission")
	}
	if _, err := m.UpdateQueuedFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "item-1", "late edit"); err == nil {
		t.Fatal("delivered item remained editable")
	}
}

func TestQueueStopRetainsHeldAndNewPromptNeverReplaysThem(t *testing.T) {
	m, p, s, log := openQueueRuntime(t, "queue-normal")
	queued, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "held original")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	held := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Status == "idle" && !s.CancelRequested && s.Queue != nil && len(s.Queue.Items) == 1 && s.Queue.Items[0].State == "held"
	})
	if held.Queue.CanEnqueue {
		t.Fatal("held text remains schedulable")
	}
	if _, err := m.Switch(t.Context(), p.ID, held.InstanceID, "", true); !errors.Is(err, ErrRuntimeQueueReview) {
		t.Fatalf("held review silently changed chats: %v", err)
	}
	preserved, _ := m.Snapshot(p.ID)
	if preserved.InstanceID != held.InstanceID || preserved.Queue.Token != held.Queue.Token || len(preserved.Queue.Items) != 1 {
		t.Fatal("rejected switch dropped review authority")
	}
	if _, err := m.UpdateQueuedFollowUp(t.Context(), p.ID, held.InstanceID, held.SessionID, held.Queue.Token, held.Queue.Revision, held.Queue.Items[0].ID, "retarget"); err == nil {
		t.Fatal("review-only held text became pending")
	}
	if err := m.Prompt(t.Context(), p.ID, held.InstanceID, "new explicit prompt"); !errors.Is(err, ErrRuntimeQueueReview) {
		t.Fatalf("ordinary Prompt bypassed review: %v", err)
	}
	preserved, _ = m.Snapshot(p.ID)
	if len(preserved.Queue.Items) != 1 || preserved.Queue.Token != held.Queue.Token || preserved.Queue.Items[0].State != "held" || preserved.Queue.Items[0].Text != "held original" {
		t.Fatal("ordinary Prompt lost held authority")
	}
	removed, err := m.RemoveQueuedFollowUp(t.Context(), p.ID, held.InstanceID, held.SessionID, held.Queue.Token, held.Queue.Revision, held.Queue.Items[0].ID)
	if err != nil || len(removed.Queue.Items) != 0 {
		t.Fatalf("explicit held discard: %+v %v", removed.Queue, err)
	}
	if err := m.Prompt(t.Context(), p.ID, held.InstanceID, "new explicit prompt"); err != nil {
		t.Fatal(err)
	}
	next := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Status == "running" && s.Queue != nil && s.Queue.Token != held.Queue.Token && s.Queue.CanEnqueue
	})
	if len(next.Queue.Items) != 0 {
		t.Fatal("discarded held input replayed")
	}
	if _, err := m.RemoveQueuedFollowUp(t.Context(), p.ID, next.InstanceID, next.SessionID, queued.Queue.Token, queued.Queue.Revision, queued.Queue.Items[0].ID); err == nil {
		t.Fatal("old root token controls replacement")
	}

	commands, _ := os.ReadFile(log)
	if strings.Count(string(commands), "queue_enqueue") != 1 || strings.Count(string(commands), "\nprompt\n") != 2 {
		t.Fatalf("held work replayed: %s", commands)
	}
}

func TestQueueUnknownDoesNotRetryAndKnownRejectionDoesNotFailWorker(t *testing.T) {
	for _, mode := range []string{"queue-reject", "queue-unknown", "queue-exit", "queue-unknown-after-event"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, log := openQueueRuntime(t, mode)
			_, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "unsent browser draft")
			after, _ := m.Snapshot(p.ID)
			if mode == "queue-reject" {
				if !errors.Is(err, ErrRuntimeInvalid) || after.Status != "running" || len(after.Messages) != len(s.Messages) {
					t.Fatalf("known rejection: %+v %v", after, err)
				}
			} else {
				if !errors.Is(err, ErrRuntimeUnavailable) || after.Status != "failed" || after.Queue.CanEnqueue {
					t.Fatalf("unknown operation revived queue: %+v %v", after, err)
				}
				if mode == "queue-unknown-after-event" {
					after = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return len(s.Queue.Items) == 1 })
					if after.Queue.Items[0].State != "uncertain" {
						t.Fatal("unknown pending claimed unsent")
					}
					cleared, err := m.RemoveQueuedFollowUp(t.Context(), p.ID, after.InstanceID, after.SessionID, after.Queue.Token, after.Queue.Revision, after.Queue.Items[0].ID)
					if err != nil || len(cleared.Queue.Items) != 0 {
						t.Fatalf("local review discard failed: %+v %v", cleared.Queue, err)
					}
				}
			}
			commands, _ := os.ReadFile(log)
			if strings.Count(string(commands), "queue_enqueue") != 1 || strings.Contains(string(commands), "queue_remove") {
				t.Fatalf("unknown mutation replay or failed-worker command: %s", commands)
			}
		})
	}
}

func TestQueueAdmittedMutationSurvivesBrowserDisconnectAndStopLatches(t *testing.T) {
	m, p, s, log := openQueueRuntime(t, "queue-holdack")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := m.EnqueueFollowUp(ctx, p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "queued before disconnect")
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(log)
		if strings.Contains(string(data), "queue_enqueue") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue call not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	stopping, _ := m.Snapshot(p.ID)
	if !stopping.CancelRequested || stopping.Queue.CanEnqueue {
		t.Fatal("Stop cannot latch behind queue ACK")
	}
	if _, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "overlap"); err == nil {
		t.Fatal("overlapping queue queued itself")
	}
	if err := os.WriteFile(log+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	held := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Status == "idle" && !s.CancelRequested && len(s.Queue.Items) == 1
	})
	if held.Queue.Items[0].State != "held" {
		t.Fatal("stop did not preserve known unexecuted followup")
	}
}

func TestQueueMutationCountAndByteBounds(t *testing.T) {
	m, p, s, _ := openQueueRuntime(t, "queue-normal")
	for range 8 {
		next, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "x")
		if err != nil {
			t.Fatal(err)
		}
		s = next
	}
	if s.Queue.CanEnqueue || len(s.Queue.Items) != 8 {
		t.Fatal("full queue still advertises room")
	}
	if _, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "ninth"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("count overflow: %v", err)
	}
	for i := range 3 {
		next, err := m.UpdateQueuedFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, s.Queue.Items[i].ID, strings.Repeat("x", 64<<10))
		if err != nil {
			t.Fatal(err)
		}
		s = next
	}
	// Four complete 64-KiB items plus the other four nonempty items exceed 256 KiB.
	if _, err := m.UpdateQueuedFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, s.Queue.Items[3].ID, strings.Repeat("x", 64<<10)); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("byte overflow: %v", err)
	}
}

func TestQueueHeldPromptRejectionPreservesDiscardAuthority(t *testing.T) {
	m, p, s, _ := openQueueRuntime(t, "queue-reject-held")
	_, err := m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "held text")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	held := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Status == "idle" && !s.CancelRequested && len(s.Queue.Items) == 1 && s.Queue.Items[0].State == "held"
	})
	if err := m.Prompt(t.Context(), p.ID, held.InstanceID, "new text"); err == nil {
		t.Fatal("new admission bypassed held review")
	}
	current, _ := m.Snapshot(p.ID)
	if current.InstanceID != held.InstanceID || current.Queue.Token != held.Queue.Token || len(current.Queue.Items) != 1 {
		t.Fatal("rejection lost held review")
	}
	if _, err := m.RemoveQueuedFollowUp(t.Context(), p.ID, current.InstanceID, current.SessionID, current.Queue.Token, current.Queue.Revision, current.Queue.Items[0].ID); err != nil {
		t.Fatalf("held Prompt rejection stranded discard authority: %v", err)
	}
}
