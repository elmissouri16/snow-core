package agent

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func managedSteerLimitAgent(t *testing.T, reviewText string) *Agent {
	t.Helper()
	p := newBlockingProvider()
	a, _ := setup(t, p, nil, permission.ModeDeny)
	done := make(chan error, 1)
	go func() { done <- a.Prompt(t.Context(), "initial") }()
	<-p.started
	t.Cleanup(func() { a.Abort(); <-done })
	if reviewText != "" {
		// Retained review is not in the native queue but must still consume
		// capacity. Seed the same invariant as holdQueueControlLocked.
		a.mu.Lock()
		a.queueControl.review = []protocol.QueueControlItem{{ID: "retained", Text: reviewText, State: "held"}}
		a.mu.Unlock()
	}
	return a
}

func mutateManagedLimitQueue(a *Agent, command, id, text string) (protocol.QueueControl, error) {
	p := managedSteerParams(a, text)
	q, err := a.QueueControlSnapshot(protocol.RPCQueueListParams{SessionID: p.SessionID, TurnID: p.TurnID})
	if err != nil {
		return protocol.QueueControl{}, err
	}
	return a.MutateQueueControl(command, protocol.RPCQueueUpdateParams{SessionID: p.SessionID, TurnID: p.TurnID, Revision: q.Revision, ItemID: id, Text: text})
}

func assertManagedLimitRejectsBoth(t *testing.T, a *Agent) {
	t.Helper()
	before, pending := controlSnapshot(t, a), a.PendingInputs()
	if before.Accepting {
		t.Fatal("full shared queue still advertises accepting")
	}
	if _, err := a.ManagedSteer(managedSteerParams(a, "x")); !errors.Is(err, ErrManagedSteerRejected) {
		t.Fatalf("steering exceeded shared capacity: %v", err)
	}
	if _, err := mutateManagedLimitQueue(a, "queue_enqueue", "", "x"); !errors.Is(err, ErrQueueRejected) {
		t.Fatalf("follow-up exceeded shared capacity: %v", err)
	}
	if after := controlSnapshot(t, a); !reflect.DeepEqual(before, after) || !reflect.DeepEqual(pending, a.PendingInputs()) {
		t.Fatalf("rejected enqueue mutated queue: before=%+v after=%+v", before, after)
	}
}

func TestManagedSteerSharedQueueCountBothDirections(t *testing.T) {
	for _, first := range []string{"steer", "follow_up"} {
		for _, retained := range []bool{false, true} {
			name := first + "_first"
			reviewText := ""
			if retained {
				name += "_with_review"
				reviewText = "held"
			}
			t.Run(name, func(t *testing.T) {
				a := managedSteerLimitAgent(t, reviewText)
				count := protocol.RPCQueueMaxItems
				if retained {
					count--
				}
				for range count - 1 {
					if first == "steer" {
						if _, err := a.ManagedSteer(managedSteerParams(a, "steer")); err != nil {
							t.Fatal(err)
						}
					} else {
						controlEnqueue(t, a, "follow-up")
					}
				}
				if q := controlSnapshot(t, a); !q.Accepting {
					t.Fatal("queue rejected last available shared slot")
				}
				if first == "steer" {
					controlEnqueue(t, a, "last follow-up")
				} else {
					if _, err := a.ManagedSteer(managedSteerParams(a, "last steer")); err != nil {
						t.Fatal(err)
					}
				}
				assertManagedLimitRejectsBoth(t, a)
				// Editing an existing item does not add a slot; full count must
				// still permit this explicit correction.
				q := controlSnapshot(t, a)
				if _, err := mutateManagedLimitQueue(a, "queue_update", q.Items[0].ID, "edited"); err != nil {
					t.Fatal(err)
				}
				assertManagedLimitRejectsBoth(t, a)
			})
		}
	}
}

func TestManagedSteerSharedQueueBytesBothDirections(t *testing.T) {
	fullText := strings.Repeat("x", protocol.RPCQueueMaxTextBytes)
	for _, first := range []string{"steer", "follow_up"} {
		for _, retained := range []bool{false, true} {
			name := first + "_first"
			reviewText := ""
			if retained {
				name += "_with_review"
				reviewText = fullText
			}
			t.Run(name, func(t *testing.T) {
				a := managedSteerLimitAgent(t, reviewText)
				count := protocol.RPCQueueMaxTotalBytes / protocol.RPCQueueMaxTextBytes
				if retained {
					count--
				}
				for range count - 1 {
					if first == "steer" {
						if _, err := a.ManagedSteer(managedSteerParams(a, fullText)); err != nil {
							t.Fatal(err)
						}
					} else {
						controlEnqueue(t, a, fullText)
					}
				}
				if q := controlSnapshot(t, a); !q.Accepting {
					t.Fatal("queue rejected final available shared bytes")
				}
				if first == "steer" {
					controlEnqueue(t, a, fullText)
				} else {
					if _, err := a.ManagedSteer(managedSteerParams(a, fullText)); err != nil {
						t.Fatal(err)
					}
				}
				assertManagedLimitRejectsBoth(t, a)
			})
		}
	}
}

func TestManagedSteerSharedQueueUpdateSubtractsPendingBytes(t *testing.T) {
	fullText := strings.Repeat("x", protocol.RPCQueueMaxTextBytes)
	for _, retained := range []bool{false, true} {
		name, reviewText := "pending", ""
		if retained {
			name, reviewText = "pending_and_review", fullText
		}
		t.Run(name, func(t *testing.T) {
			a := managedSteerLimitAgent(t, reviewText)
			count := 3
			if retained {
				count--
			}
			for range count {
				if _, err := a.ManagedSteer(managedSteerParams(a, fullText)); err != nil {
					t.Fatal(err)
				}
			}
			q := controlEnqueue(t, a, "x")
			id := q.Items[0].ID
			controlEnqueue(t, a, "y")
			before, pending := controlSnapshot(t, a), a.PendingInputs()
			if _, err := mutateManagedLimitQueue(a, "queue_update", id, fullText); !errors.Is(err, ErrQueueRejected) {
				t.Fatalf("update ignored native/review bytes: %v", err)
			}
			if after := controlSnapshot(t, a); !reflect.DeepEqual(before, after) || !reflect.DeepEqual(pending, a.PendingInputs()) {
				t.Fatal("rejected update mutated shared queue")
			}
			// Exactly 256 KiB after replacement. Not subtracting the old byte
			// would incorrectly reject this admissible edit.
			if _, err := mutateManagedLimitQueue(a, "queue_update", id, fullText[:len(fullText)-1]); err != nil {
				t.Fatal(err)
			}
			assertManagedLimitRejectsBoth(t, a)
			if _, err := mutateManagedLimitQueue(a, "queue_update", id, fullText[:len(fullText)-2]); err != nil {
				t.Fatal(err)
			}
			if q := controlSnapshot(t, a); !q.Accepting {
				t.Fatal("shrinking edit did not release shared byte capacity")
			}
			controlEnqueue(t, a, "z")
			assertManagedLimitRejectsBoth(t, a)
		})
	}
}
