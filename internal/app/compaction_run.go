package app

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrCompactionRejected is a read-only admission failure: no provider request,
// compaction marker or goal deferral was attempted. Once accepted, transport
// uncertainty requires refresh and must never cause automatic replay.
var ErrCompactionRejected = errors.New("compaction rejected before execution")
var ErrCompactionOutcomeUnknown = errors.New("compaction outcome requires authoritative refresh")

// StartCompaction captures exact session/branch/tip admission before ACK. The
// returned handle owns the serial runtime immediately but cannot use a provider
// until the caller successfully acknowledges and calls Release. On ACK failure
// cancel it and wait for its captured Done; do not sample a replacement turn.
func (a *App) StartCompaction(ctx context.Context, p protocol.RPCCompactionStartParams) (handle *agent.CompactionRunHandle, retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(ErrCompactionRejected, retErr)
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	if !session.ValidVersionIdentity(p.SessionID, false) || !session.ValidVersionIdentity(p.BranchID, false) || !session.ValidVersionIdentity(p.ExpectedTipID, true) {
		return nil, errors.New("compaction: invalid session, branch or tip identity")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.Agent.CompactionReadyAdmitted(); err != nil {
		return nil, err
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return nil, errors.New("compaction: active subagents prevent admission")
	}
	if err := a.goalRunBindingAdmitted(p.SessionID, p.BranchID); err != nil {
		return nil, err
	}
	if a.Session.BranchTip() != p.ExpectedTipID {
		return nil, errors.New("compaction: branch tip changed")
	}
	return a.Agent.StartCompactionAdmitted(ctx)
}
