package agent

import (
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestActiveTurnSnapshotMatchesCompactionEventsAndBranchFence(t *testing.T) {
	a, _ := setup(t, fake.New(nil), nil, permission.ModeDeny)
	events := make(chan protocol.AgentEvent, 32)
	unsubscribe := a.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
	defer unsubscribe()
	before := a.ActiveTurnSnapshot()
	for range 2 {
		_, admitted, err := a.CompactWithTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		after := a.ActiveTurnSnapshot()
		if admitted != after {
			t.Fatalf("returned identity differs from admitted operation: returned=%+v current=%+v", admitted, after)
		}
		if after.ID == "" || after.ID == before.ID || after.Origin != "compact" || after.Running || after.Sequence <= before.Sequence || after.Epoch == 0 {
			t.Fatalf("invalid completed operation snapshot: before=%+v after=%+v", before, after)
		}
		if err := a.bus.Drain(t.Context()); err != nil {
			t.Fatal(err)
		}
		for len(events) > 0 {
			ev := <-events
			if ev.TurnID != after.ID || ev.TurnSequence != after.Sequence || ev.RootEpoch != after.Epoch || ev.TurnOrigin != after.Origin {
				t.Fatalf("snapshot/event identity mismatch: snapshot=%+v event=%+v", after, ev)
			}
		}
		before = after
	}
	unlock := a.LockAdmission()
	a.ResetTurnIdentityAdmitted()
	unlock()
	fenced := a.ActiveTurnSnapshot()
	if fenced.ID != "" || fenced.Sequence != 0 || fenced.Running || fenced.Epoch <= before.Epoch {
		t.Fatalf("branch fence retained old admission: before=%+v after=%+v", before, fenced)
	}
}

func TestManualCompactionRejectsClosedAgentBeforeAdmission(t *testing.T) {
	a, _ := setup(t, fake.New(nil), nil, permission.ModeDeny)
	if _, err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.Close()
	before := a.ActiveTurnSnapshot()
	if _, rejected, err := a.CompactWithTurn(t.Context()); err == nil || rejected != (TurnSnapshot{}) {
		t.Fatalf("rejected compaction inherited an identity: turn=%+v err=%v", rejected, err)
	}
	if _, err := a.Compact(t.Context()); err == nil {
		t.Fatal("ordinary Compact accepted a closed agent")
	}
	if after := a.ActiveTurnSnapshot(); after != before {
		t.Fatalf("closed agent admitted work: before=%+v after=%+v", before, after)
	}
}
