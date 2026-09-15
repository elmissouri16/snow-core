package app

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrProcessControlRejected = errors.New("app: managed process request rejected; review current session, handle and limits")
	ErrProcessControlDenied   = errors.New("app: stopping managed processes requires Default mode and current process_stop permission")
)

// lockProcessControl binds all manager access to the same app session transaction
// as SetSession/RebindSession. Admission also stabilizes collaboration mode.
func (a *App) lockProcessControl(ctx context.Context, sessionID string) (func(), error) {
	if ctx == nil || ctx.Err() != nil || !protocol.ValidProcessControlSession(sessionID) || a == nil {
		return nil, ErrProcessControlRejected
	}
	if !a.stateMu.TryLock() {
		return nil, ErrProcessControlRejected
	}
	if a.Agent == nil || a.ProcessManager == nil || a.Session == nil || a.Session.ID() != sessionID {
		a.stateMu.Unlock()
		return nil, ErrProcessControlRejected
	}
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		a.stateMu.Unlock()
		return nil, ErrProcessControlRejected
	}
	return func() { unlock(); a.stateMu.Unlock() }, nil
}
func (a *App) ProcessControlList(ctx context.Context, p protocol.RPCProcessControlListParams) (protocol.RPCProcessControlList, error) {
	unlock, err := a.lockProcessControl(ctx, p.SessionID)
	if err != nil {
		return protocol.RPCProcessControlList{}, err
	}
	defer unlock()
	states := a.ProcessManager.List()
	out := protocol.RPCProcessControlList{SessionID: p.SessionID, Processes: make([]protocol.RPCManagedProcess, 0, min(len(states), protocol.ProcessControlMaxRecords)), Truncated: len(states) > protocol.ProcessControlMaxRecords}
	for _, state := range states[:min(len(states), protocol.ProcessControlMaxRecords)] {
		out.Processes = append(out.Processes, processControlState(state))
	}
	return out, nil
}
func (a *App) ProcessControlLogs(ctx context.Context, p protocol.RPCProcessControlLogsParams) (protocol.RPCProcessControlLogs, error) {
	if !p.Valid() {
		return protocol.RPCProcessControlLogs{}, ErrProcessControlRejected
	}
	unlock, err := a.lockProcessControl(ctx, p.SessionID)
	if err != nil {
		return protocol.RPCProcessControlLogs{}, err
	}
	defer unlock()
	logs, err := a.ProcessManager.Logs(ctx, managedprocess.LogsRequest{ProcessID: p.ProcessID, Cursor: p.Cursor, MaxBytes: p.MaxBytes})
	if err != nil {
		return protocol.RPCProcessControlLogs{}, ErrProcessControlRejected
	}
	return protocol.RPCProcessControlLogs{SessionID: p.SessionID, ProcessID: logs.ProcessID, Status: logs.Status, Output: logs.Output, NextCursor: logs.NextCursor, Omitted: logs.Omitted, EOF: logs.EOF}, nil
}

// ProcessControlStop is deliberately NOT an exception to Plan Mode: the builtin
// process_stop is RiskExec / EffectMutating. Operator intent neither exits Plan
// Mode nor overrides deny. This bounded, noninteractive control uses the current
// permission state and remembered rules, but never creates a broker question
// while occupying the RPC/control lane. Undecided ask therefore fails closed.
// Tool exposure is not authority: this control also works when process_stop is
// absent from the model's tool profile, subject to the same exec policy.
func (a *App) ProcessControlStop(ctx context.Context, p protocol.RPCProcessControlStopParams) (protocol.RPCProcessControlStop, error) {
	if !p.Valid() {
		return protocol.RPCProcessControlStop{}, ErrProcessControlRejected
	}
	unlock, err := a.lockProcessControl(ctx, p.SessionID)
	if err != nil {
		return protocol.RPCProcessControlStop{}, err
	}
	defer unlock()
	if _, err := a.ProcessManager.Status(p.ProcessID); err != nil {
		return protocol.RPCProcessControlStop{}, ErrProcessControlRejected
	}
	if a.Agent.Mode() != protocol.ModeDefault || a.Perm == nil {
		return protocol.RPCProcessControlStop{}, ErrProcessControlDenied
	}
	req := processControlStopRequest(p)
	if err := authorizeProcessControlPolicy(ctx, a.Agent, req); err != nil {
		return protocol.RPCProcessControlStop{}, err
	}
	policy := permission.NewService(permission.ModeDeny, permission.DenyAll{})
	policy.RestoreState(a.Perm.State())
	decision, err := policy.Authorize(ctx, req)
	if err != nil || decision != permission.DecisionAllow {
		return protocol.RPCProcessControlStop{}, ErrProcessControlDenied
	}
	state, err := a.ProcessManager.Stop(ctx, p.ProcessID, time.Duration(p.GraceMS)*time.Millisecond)
	if err != nil {
		return protocol.RPCProcessControlStop{}, ErrProcessControlRejected
	}
	return protocol.RPCProcessControlStop{SessionID: p.SessionID, Process: processControlState(state)}, nil
}
func processControlState(s managedprocess.State) protocol.RPCManagedProcess {
	return protocol.RPCManagedProcess{ProcessID: s.ProcessID, Name: s.Name, Status: s.Status, StartedAt: s.StartedAt, FinishedAt: s.FinishedAt, ExitCode: s.ExitCode, Signal: s.Signal, Reason: s.Reason, Ready: s.Ready}
}
