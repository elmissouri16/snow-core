package agent

import (
	"context"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// TurnSnapshot is the process-local identity and admission state of a root
// operation. Its ordering fields are read together so command completions can
// fence delayed events without mixing identities from different admissions.
type TurnSnapshot struct {
	Origin   string
	ID       string
	Epoch    uint64
	Sequence uint64
	Running  bool
}

// ActiveTurnSnapshot returns one atomic view of root admission state. The last
// identity can remain available after an operation finishes; Running indicates
// whether it still owns admission.
func (a *Agent) ActiveTurnSnapshot() TurnSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeTurnSnapshotLocked()
}

// CompactWithTurn executes the same manual compaction path as Compact, returning
// the identity captured at admission rather than sampling a possibly newer
// operation afterward. A pre-admission rejection returns a zero identity.
func (a *Agent) CompactWithTurn(ctx context.Context) (result protocol.CompactionResult, turn TurnSnapshot, err error) {
	result, err = a.compactManualCaptured(ctx, &turn)
	turn.Running = false
	return
}

func (a *Agent) activeTurnSnapshotLocked() TurnSnapshot {
	return TurnSnapshot{
		Origin: a.turnOrigin, ID: a.turnID, Epoch: a.rootEpoch,
		Sequence: a.activeTurnSequence, Running: a.running,
	}
}
