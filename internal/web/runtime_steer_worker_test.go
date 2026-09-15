package web

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func openSteerRuntime(t *testing.T, mode string) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, mode)
	m.env = slices.DeleteFunc(m.env, func(s string) bool { return strings.HasPrefix(s, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_STEER_TEST_CHILD=1")
	p := projects[0]
	s, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "original"); err != nil {
		t.Fatal(err)
	}
	s = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Steer != nil && s.Steer.CanSteer })
	return m, p, s, log
}

func TestSteerWorkerNativeReceiptOrdersAndNoReadReplay(t *testing.T) {
	for _, mode := range []string{"steer-normal", "steer-event-first", "steer-ack-first"} {
		t.Run(mode, func(t *testing.T) {
			m, p, s, log := openSteerRuntime(t, mode)
			text := "/not-a-command $not-a-skill\r\n literal text"
			result, err := m.SteerCurrentRun(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Steer.Token, s.Steer.Revision, "explicit-request", text)
			if err != nil {
				t.Fatal(err)
			}
			if result.SteerACK == nil || result.SteerACK.Status != "accepted" || result.SteerACK.ItemID != "native-1" || result.SteerACK.RequestID != "explicit-request" {
				t.Fatalf("invalid receipt: %+v", result.SteerACK)
			}
			want := "accepted"
			if mode != "steer-normal" {
				want = "delivered"
			}
			result = runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
				return s.Steer != nil && len(s.Steer.Items) == 1 && s.Steer.Items[0].Status == want
			})
			if result.Steer.Items[0].Text != text || result.Queue != nil && len(result.Queue.Items) != 0 {
				t.Fatal("literal steering became follow-up or transformed text")
			}
			for range 3 {
				m.Snapshot(p.ID)
			}
			raw, _ := os.ReadFile(log)
			if strings.Count(string(raw), "managed_steer\n") != 1 || strings.Count(string(raw), "prompt\n") != 1 || strings.Contains(string(raw), "queue_enqueue") {
				t.Fatalf("replay or fallback: %s", raw)
			}
		})
	}
}

func TestSteerWorkerSharedCapacityBothDirections(t *testing.T) {
	for _, steerFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(steerFirst), func(t *testing.T) {
			m, p, s, _ := openSteerRuntime(t, "steer-normal")
			for i := range 8 {
				var err error
				if steerFirst {
					queueRevision := s.Queue.Revision
					s, err = m.SteerCurrentRun(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Steer.Token, s.Steer.Revision, fmt.Sprint("request-", i), "steering")
					if err == nil {
						// RPC ACK and native events have independent consumers. Read
						// the event-projected revision before the next explicit action;
						// never retry a mutation with stale admission authority.
						s = runtimeWait(t, m, p.ID, func(current RuntimeSnapshot) bool {
							return current.Queue != nil && current.Queue.Revision > queueRevision
						})
					}
				} else {
					s, err = m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "followup")
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if steerFirst {
				_, err = m.EnqueueFollowUp(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Queue.Token, s.Queue.Revision, "ninth followup")
			} else {
				_, err = m.SteerCurrentRun(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Steer.Token, s.Steer.Revision, "ninth", "ninth steer")
			}
			if err == nil {
				t.Fatal("shared native item limit bypassed")
			}
		})
	}
}

func TestSteerWorkerDisconnectDoesNotReplayAndNativeDiscardWins(t *testing.T) {
	m, p, s, log := openSteerRuntime(t, "steer-holdack")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := m.SteerCurrentRun(ctx, p.ID, s.InstanceID, s.SessionID, s.Steer.Token, s.Steer.Revision, "once", "kept literal")
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, _ := os.ReadFile(log)
		if strings.Contains(string(raw), "managed_steer") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not dispatched")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := m.CancelTurn(t.Context(), p.ID, s.InstanceID, s.CancelToken); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" && s.Steer.Items[0].Status == "discarded" })
	if result.Steer.CanSteer {
		t.Fatal("Stop retained steering authority")
	}
	if err := m.Prompt(t.Context(), p.ID, s.InstanceID, "new root"); err != nil {
		t.Fatal(err)
	}
	next := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Steer.CanSteer })
	if next.Steer.Token == s.Steer.Token {
		t.Fatal("root token not rotated")
	}
	if _, err := m.SteerCurrentRun(t.Context(), p.ID, s.InstanceID, s.SessionID, s.Steer.Token, s.Steer.Revision, "old-root", "do not replay"); err == nil {
		t.Fatal("old root accepted")
	}
	raw, _ := os.ReadFile(log)
	if strings.Count(string(raw), "managed_steer\n") != 1 {
		t.Fatal("steering replayed on new root")
	}
}
