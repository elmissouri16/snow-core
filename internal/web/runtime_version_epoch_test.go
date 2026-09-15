package web

import (
	"os"
	"strings"
	"testing"
	"time"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// BUG-151: metadata is observed even during transitions. The event consumer may
// therefore already know epoch 2 by the time the restore ACK retires epoch 1.
func TestVersionRestoreEpochOrderingKeepsNextPromptAndGoalAttention(t *testing.T) {
	for _, ordering := range []string{"epoch-before-ack", "epoch-after-ack"} {
		for _, next := range []string{"prompt", "goal"} {
			t.Run(ordering+"/"+next, func(t *testing.T) {
				m, p, before, log := openVersionRuntime(t, ordering)
				r, err := m.runtime(p.ID, before.InstanceID)
				if err != nil {
					t.Fatal(err)
				}
				r.mu.Lock()
				r.rootEpoch = 1
				r.mu.Unlock()
				preparation := prepareTestVersion(t, m, p, before)
				type result struct {
					snapshot RuntimeSnapshot
					err      error
				}
				done := make(chan result, 1)
				go func() {
					s, err := m.CommitVersionRestore(t.Context(), p.ID, before.InstanceID, before.SessionID, preparation.RestoreToken)
					done <- result{s, err}
				}()
				if ordering == "epoch-before-ack" {
					deadline := time.Now().Add(3 * time.Second)
					for {
						r.mu.Lock()
						epoch, transitioning, text := r.rootEpoch, r.transitioning, r.snapshot.Messages[0].Text
						r.mu.Unlock()
						if epoch == 2 {
							if !transitioning || text != before.Messages[0].Text {
								t.Fatal("pre-ACK metadata mutated transcript or ended transition")
							}
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("new epoch metadata not consumed before ACK")
						}
						time.Sleep(time.Millisecond)
					}
					if err := os.WriteFile(log+".release", nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				restored := <-done
				if restored.err != nil {
					t.Fatal(restored.err)
				}
				if restored.snapshot.Status != "idle" || restored.snapshot.InstanceID == before.InstanceID {
					t.Fatal("restore was not explicit idle authority rotation")
				}
				// Both independent event and ACK paths have now run, without depending on
				// channel wakeup scheduling or a guessed relationship to the next prompt.
				deadline := time.Now().Add(3 * time.Second)
				for {
					r.mu.Lock()
					epoch := r.rootEpoch
					r.mu.Unlock()
					if epoch == 2 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("post-ACK mode metadata missing")
					}
					time.Sleep(time.Millisecond)
				}
				if next == "prompt" {
					err = m.Prompt(t.Context(), p.ID, restored.snapshot.InstanceID, "explicit after restore")
				} else {
					current, _ := m.Snapshot(p.ID)
					_, err = m.RunGoal(t.Context(), p.ID, current.InstanceID, RuntimeGoalRunInput{ExpectedRevision: current.Revision, RPCGoalRunParams: protocol.RPCGoalRunParams{Action: "create", SessionID: current.SessionID, BranchID: "target", ExpectedTipID: "target-tip", Objective: "explicit goal after restore"}})
				}
				if err != nil {
					t.Fatal(err)
				}
				deadline = time.Now().Add(2 * time.Second)
				for {
					current, _ := m.Snapshot(p.ID)
					textVisible := false
					for _, message := range current.Messages {
						if strings.Contains(message.Text, "New explicit answer") {
							textVisible = true
						}
					}
					if current.Status == "permission" && current.Permission != nil && current.Permission.ID == "restored-permission" && textVisible {
						if next == "goal" && (current.Goal == nil || !current.Goal.Running || current.Goal.GoalRunID != "restored-goal-run") {
							t.Fatal("next goal lost exact active run")
						}
						break
					}
					if time.Now().After(deadline) {
						r.mu.Lock()
						root, retired := r.rootEpoch, r.retiredEpoch
						r.mu.Unlock()
						t.Fatalf("new epoch text/attention invisible: next=%s root=%d retired=%d status=%s text=%v permission=%+v", next, root, retired, current.Status, textVisible, current.Permission)
					}
					time.Sleep(time.Millisecond)
				}
				r.mu.Lock()
				retired := r.retiredEpoch
				r.mu.Unlock()
				if retired != 1 {
					t.Fatalf("retired %d, want only captured outgoing epoch 1", retired)
				}
				// Preserving the new epoch must not revive the captured old one.
				current, _ := m.Snapshot(p.ID)
				runID := ""
				if next == "goal" {
					runID = current.Goal.GoalRunID
				}
				r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, GoalRunID: runID, RootEpoch: 1, TurnSequence: 99, TurnID: "old-root", Text: "OLD EPOCH MUST STAY RETIRED"}})
				r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvPermissionRequest, GoalRunID: runID, RootEpoch: 1, TurnSequence: 99, TurnID: "old-root", Permission: &protocol.Permission{Request: protocol.PermissionRequest{ID: "old-permission", Tool: "bash"}}}})
				after, _ := m.Snapshot(p.ID)
				if after.Revision != current.Revision || after.Permission == nil || after.Permission.ID != "restored-permission" {
					t.Fatal("retired epoch replaced the new run's text or attention")
				}
			})
		}
	}
}
