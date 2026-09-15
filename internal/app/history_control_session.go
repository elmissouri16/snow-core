package app

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ManagedSessionFork creates an atomic, detached durable child in the current
// workspace. It never switches or rebinds the parent, stops goal continuation,
// consumes queue input, activates skills, or accepts caller-selected paths.
func (a *App) ManagedSessionFork(ctx context.Context, p protocol.RPCManagedSessionForkParams) (result protocol.RPCManagedSessionForkResult, retErr error) {
	defer func() { retErr = historyControlError(retErr) }()
	if err := validateHistoryControlName(p.Name); err != nil {
		return result, err
	}
	if utf8.RuneCountInString(p.Name) > 72 {
		return result, errors.New("history control session name exceeds 72 runes")
	}
	unlock, err := a.lockHistoryControl(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	snapshot, err := a.historyControlSnapshotAdmitted(ctx, p.RPCHistoryControlBinding)
	if err != nil {
		return result, err
	}
	if err := session.ValidateForkBoundary(snapshot.Entries); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	manager, ok := a.Session.(session.HistoryControlStore)
	if !ok {
		return result, errors.New("app: managed session fork unsupported")
	}
	capture, err := manager.CaptureHistoryFork(ctx, p, snapshot.Mode)
	if err != nil {
		return result, err
	}
	index := session.NewFileIndex(session.DefaultSessionsRoot())
	child, fork, err := index.CreateHistoryFork(ctx, a.cwd, capture)
	if err != nil {
		return result, err
	}
	if err := copyForkArtifacts(ctx, a.artifacts, child, fork); err != nil {
		_ = child.Close()
		removeSessionFiles(fork.SessionPath)
		return result, errors.Join(ErrHistoryControlUnknown, err)
	}
	if err := child.Close(); err != nil {
		removeSessionFiles(fork.SessionPath)
		return result, errors.Join(ErrHistoryControlUnknown, err)
	}
	return protocol.RPCManagedSessionForkResult{SessionID: fork.SessionID, Name: fork.Name, Branch: historyControlBranch(fork.Branch), SourceSessionID: p.SessionID, SourceBranchID: p.TargetBranchID, SourceTipID: p.TargetTipID, Mode: snapshot.Mode, RootEpoch: a.Agent.RootEpoch()}, nil
}
