package app

import (
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestManagedSteerAppForwardsLiteralExactRootWithoutPrompt(t *testing.T) {
	a := messageEditTestApp(t, false)
	p := &reviewQueueProvider{Provider: a.Provider, started: make(chan struct{})}
	if err := a.Agent.SetProvider(p); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(t.Context(), "initial") }()
	<-p.started
	defer func() { a.Agent.Abort(); <-done }()
	turn := a.Agent.ActiveTurnSnapshot()
	params := protocol.RPCManagedSteerParams{SessionID: a.Session.ID(), TurnID: turn.ID, RootEpoch: turn.Epoch, RequestID: "explicit-click", Text: "  /plan $review\nLiteral manager text.  "}
	before := a.Session.BranchTip()
	result, err := a.ManagedSteer(params)
	if err != nil || result.Status != "accepted" || result.ItemID == "" || result.RequestID != params.RequestID {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	pending := a.Agent.PendingInputs()
	if len(pending.Items) != 1 || pending.Items[0].ID != result.ItemID || pending.Items[0].Text != params.Text || pending.Items[0].Kind != protocol.QueuedInputSteer {
		t.Fatalf("native literal submission=%+v", pending)
	}
	if a.Session.BranchTip() != before || a.Agent.ActiveTurnSnapshot() != turn {
		t.Fatal("steering admission created a prompt or changed root identity")
	}
	params.RootEpoch++
	if _, err := a.ManagedSteer(params); !errors.Is(err, agent.ErrManagedSteerStale) {
		t.Fatalf("stale app request admitted: %v", err)
	}
	if len(a.Agent.PendingInputs().Items) != 1 {
		t.Fatal("rejected app request changed native queue")
	}
}
