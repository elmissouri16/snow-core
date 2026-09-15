package web

import (
	"context"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *RuntimeManager) HistorySessionFork(ctx context.Context, projectID, instanceID string, p RuntimeHistoryControlRequest) (RuntimeHistoryControlResult, error) {
	r, before, outgoing, err := m.historyControlRuntime(ctx, projectID, instanceID, p, "history-session-fork")
	if err != nil {
		return RuntimeHistoryControlResult{}, err
	}
	defer r.control.Unlock()
	var ack protocol.RPCManagedSessionForkResult
	params := protocol.RPCManagedSessionForkParams{RPCHistoryControlBinding: p.RPCHistoryControlBinding, Name: p.Name}
	if err := r.historyControlCall("history_session_fork", params, &ack, false); err != nil {
		return RuntimeHistoryControlResult{}, err
	}
	if !validHistorySessionForkACK(ack, p, outgoing) {
		r.fail()
		return RuntimeHistoryControlResult{}, ErrRuntimeUnavailable
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		r.fail()
		return RuntimeHistoryControlResult{}, err
	}
	// Do not publish or reload the parent. Detached children have no current
	// manager worker. The browser refreshes inventory and offers explicit Open.
	r.mu.Lock()
	valid := r.ctx.Err() == nil && r.instanceID == instanceID && r.snapshot.SessionID == p.SessionID && r.rootEpoch == outgoing && r.snapshot.Revision == before.Revision && r.snapshot.Status == "idle"
	r.mu.Unlock()
	if !valid {
		r.fail()
		return RuntimeHistoryControlResult{}, ErrRuntimeUnavailable
	}
	return RuntimeHistoryControlResult{ProjectID: projectID, InstanceID: instanceID, SessionID: p.SessionID, Revision: before.Revision, BranchID: p.TargetBranchID, TipID: p.TargetTipID, Name: ack.Name, ChildSessionID: ack.SessionID}, nil
}

func validHistorySessionForkACK(a protocol.RPCManagedSessionForkResult, p RuntimeHistoryControlRequest, outgoing uint64) bool {
	return runtimeIdentifier(a.SessionID) && a.SessionID != p.SessionID && a.Name == p.Name && a.SourceSessionID == p.SessionID && a.SourceBranchID == p.TargetBranchID && a.SourceTipID == p.TargetTipID && a.RootEpoch == outgoing && validVersionIdentity(a.Branch.ID, false) && validVersionIdentity(a.Branch.TipID, true) && a.Branch.Active && (a.Mode == protocol.ModeDefault || a.Mode == protocol.ModePlan)
}

func (m *RuntimeManager) HistoryBranchRename(ctx context.Context, projectID, instanceID string, p RuntimeHistoryControlRequest) (RuntimeHistoryControlResult, error) {
	r, before, outgoing, err := m.historyControlRuntime(ctx, projectID, instanceID, p, "history-branch-rename")
	if err != nil {
		return RuntimeHistoryControlResult{}, err
	}
	defer r.control.Unlock()
	var ack protocol.RPCManagedBranchRenameResult
	params := protocol.RPCManagedBranchRenameParams{RPCHistoryControlBinding: p.RPCHistoryControlBinding, OldName: p.OldName, Name: p.Name}
	if err := r.historyControlCall("history_branch_rename", params, &ack, false); err != nil {
		return RuntimeHistoryControlResult{}, err
	}
	if !validHistoryBranchRenameACK(ack, p, outgoing) {
		r.fail()
		return RuntimeHistoryControlResult{}, ErrRuntimeUnavailable
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		r.fail()
		return RuntimeHistoryControlResult{}, err
	}
	r.mu.Lock()
	valid := r.ctx.Err() == nil && r.instanceID == instanceID && r.snapshot.SessionID == p.SessionID && r.rootEpoch == outgoing && r.snapshot.Revision == before.Revision && r.snapshot.Status == "idle"
	// Names live in bounded Versions pages, not the live transcript. Publishing
	// a fresh revision invalidates stale inspected labels without rotating any
	// epoch, edit/goal control or current instance.
	if valid {
		r.publishLocked()
	}
	revision := r.snapshot.Revision
	r.mu.Unlock()
	if !valid {
		r.fail()
		return RuntimeHistoryControlResult{}, ErrRuntimeUnavailable
	}
	return RuntimeHistoryControlResult{ProjectID: projectID, InstanceID: instanceID, SessionID: p.SessionID, Revision: revision, BranchID: ack.Branch.ID, TipID: ack.Branch.TipID, Name: ack.Branch.Name}, nil
}

func validHistoryBranchRenameACK(a protocol.RPCManagedBranchRenameResult, p RuntimeHistoryControlRequest, outgoing uint64) bool {
	return a.SessionID == p.SessionID && a.BranchID == p.SourceBranchID && a.TipID == p.SourceTipID && a.RootEpoch == outgoing && a.Branch.ID == p.TargetBranchID && a.Branch.TipID == p.TargetTipID && a.Branch.Name == p.Name && a.Branch.Active == (p.TargetBranchID == p.SourceBranchID)
}
