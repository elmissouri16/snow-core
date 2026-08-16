package permission

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestManualAskerFIFOAttributionAndClose(t *testing.T) {
	events := make(chan protocol.AgentEvent, 4)
	asker := NewManualAsker(func(event protocol.AgentEvent) { events <- event })
	var wg sync.WaitGroup
	results := make(chan Decision, 2)
	for _, tool := range []string{"write", "bash"} {
		wg.Add(1)
		go func(tool string) {
			defer wg.Done()
			decision, _ := asker.Ask(context.Background(), Request{Tool: tool, Risk: RiskExec})
			results <- decision
		}(tool)
		// Preserve deterministic enqueue order while still exercising concurrent
		// outstanding requests.
		time.Sleep(10 * time.Millisecond)
	}
	first := <-events
	if first.Permission == nil || first.Permission.Request.Tool != "write" || first.Permission.Request.ID == "" {
		t.Fatalf("first event = %#v", first.Permission)
	}
	if err := asker.Reply("stale", DecisionAllow); err == nil {
		t.Fatal("stale reply unexpectedly succeeded")
	}
	if len(events) != 0 {
		t.Fatal("second request published before FIFO head resolved")
	}
	if err := asker.Reply(first.Permission.Request.ID, DecisionAllow); err != nil {
		t.Fatal(err)
	}
	second := <-events
	if second.Permission == nil || second.Permission.Request.Tool != "bash" || second.Permission.Request.ID == first.Permission.Request.ID {
		t.Fatalf("second event = %#v", second.Permission)
	}
	asker.Close()
	wg.Wait()
	close(results)
	seenAllow, seenDeny := false, false
	for decision := range results {
		seenAllow = seenAllow || decision == DecisionAllow
		seenDeny = seenDeny || decision == DecisionDeny
	}
	if !seenAllow || !seenDeny {
		t.Fatalf("decisions did not include allow and close-deny")
	}
}
