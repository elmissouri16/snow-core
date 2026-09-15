package web

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Keep the web parser on the final typed core ACK contract. Missing mode is
// rejected by validVersionRestoreACK rather than defaulted by presentation.
type runtimeVersionRestoreACK = protocol.RPCBranchRestoreCommitted

func (m *RuntimeManager) PrepareVersionRestore(ctx context.Context, projectID, instanceID, sessionID, currentBranch, currentTip, branchID, tipID string) (RuntimeVersionRestorePreparation, error) {
	if !validVersionIdentity(currentBranch, false) || !validVersionIdentity(currentTip, true) || !validVersionIdentity(branchID, false) || !validVersionIdentity(tipID, true) || currentBranch == branchID {
		return RuntimeVersionRestorePreparation{}, ErrRuntimeInvalid
	}
	r, err := m.versionRuntime(ctx, projectID, instanceID, sessionID, true)
	if err != nil {
		return RuntimeVersionRestorePreparation{}, err
	}
	defer r.control.Unlock()
	params := protocol.RPCBranchRestorePrepareParams{SessionID: sessionID, SourceBranchID: currentBranch, SourceTipID: currentTip, TargetBranchID: branchID, TargetTipID: tipID}
	var prepared protocol.RPCBranchRestorePrepared
	if err := r.call(protocol.RPCRequest{Type: "branch_restore_prepare"}, params, &prepared); err != nil {
		return RuntimeVersionRestorePreparation{}, err
	}
	expires := time.UnixMilli(prepared.ExpiresAt)
	now := time.Now()
	if prepared.RPCBranchRestorePrepareParams != params || prepared.RestoreToken == "" || !runtimeOption(prepared.RestoreToken) || !expires.After(now) || expires.After(now.Add(2*time.Minute)) {
		r.fail()
		return RuntimeVersionRestorePreparation{}, ErrRuntimeUnavailable
	}
	if _, err := m.versionReadScope(r, projectID, instanceID, sessionID); err != nil {
		return RuntimeVersionRestorePreparation{}, err
	}
	r.mu.Lock()
	if r.busy || r.transitioning || r.snapshot.Status != "idle" || r.snapshot.CancelRequested {
		r.mu.Unlock()
		return RuntimeVersionRestorePreparation{}, ErrRuntimeUnavailable
	}
	r.versions = runtimeVersionState{preparation: &prepared, instanceID: instanceID, activityPrompt: r.activityPrompt, expiresAt: expires, provider: r.snapshot.Provider, model: r.snapshot.Model, permission: r.snapshot.PermissionMode, thinking: r.snapshot.Thinking, mode: r.snapshot.Mode}
	r.mu.Unlock()
	return RuntimeVersionRestorePreparation{ProjectID: projectID, InstanceID: instanceID, SessionID: sessionID, CurrentBranchID: currentBranch, CurrentTipID: currentTip, BranchID: branchID, TipID: tipID, RestoreToken: prepared.RestoreToken, ExpiresAt: expires.UTC()}, nil
}

func (m *RuntimeManager) CommitVersionRestore(ctx context.Context, projectID, instanceID, sessionID, token string) (RuntimeSnapshot, error) {
	if token == "" || !runtimeOption(token) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r, err := m.versionRuntime(ctx, projectID, instanceID, sessionID, true)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	r.mu.Lock()
	authorization := r.versions
	prepared := authorization.preparation
	if prepared == nil || prepared.RestoreToken != token || prepared.SessionID != sessionID || authorization.instanceID != instanceID {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	// No automatic retry or reuse, even on a known core rejection.
	r.versions.preparation = nil
	if !time.Now().Before(authorization.expiresAt) || authorization.activityPrompt != r.activityPrompt || authorization.provider != r.snapshot.Provider || authorization.model != r.snapshot.Model || authorization.permission != r.snapshot.PermissionMode || authorization.thinking != r.snapshot.Thinking || authorization.mode != r.snapshot.Mode {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	// Metadata events can advance rootEpoch while the restore is in flight.
	// Capture the outgoing authority before dispatch, never at ACK publication.
	outgoingEpoch := r.rootEpoch
	r.transitioning = true
	r.snapshot.Status = "switching"
	r.publishLocked()
	r.mu.Unlock()
	data, _ := json.Marshal(protocol.RPCBranchRestoreCommitParams{SessionID: sessionID, RestoreToken: token})
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: "branch_restore_commit", Params: data})
	if err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if !response.Success {
		if response.ErrorCode != protocol.RPCBranchRestoreRejectedErrorCode {
			r.fail()
			return RuntimeSnapshot{}, ErrRuntimeUnavailable
		}
		r.mu.Lock()
		if r.ctx.Err() == nil && r.snapshot.Status == "switching" {
			r.transitioning = false
			r.snapshot.Status = "idle"
			r.publishLocked()
		}
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	var committed runtimeVersionRestoreACK
	data, err = json.Marshal(response.Data)
	if err != nil || len(data) > runtimeVersionWireBytes || json.Unmarshal(data, &committed) != nil || !validVersionRestoreACK(committed, *prepared, authorization) {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		r.fail()
		return RuntimeSnapshot{}, err
	}
	if _, err := r.publishVersionRestore(instanceID, committed, outgoingEpoch); err != nil {
		return RuntimeSnapshot{}, err
	}
	// Restore never runs a goal. Refresh only its newly selected branch's public
	// status after retiring every old run/preparation capability.
	if err := r.refreshGoal(); err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	valid := r.snapshot.Goal == nil || r.snapshot.Goal.SessionID == committed.SessionID && r.snapshot.Goal.BranchID == committed.BranchID && !r.snapshot.Goal.Running
	snapshot := r.snapshot.clone()
	terminal := r.ctx.Err() != nil || snapshot.Status == "failed" || snapshot.Status == "closing"
	r.mu.Unlock()
	if !valid {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if terminal {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return snapshot, nil
}

func validVersionRestoreACK(c runtimeVersionRestoreACK, p protocol.RPCBranchRestorePrepared, state runtimeVersionState) bool {
	return c.SessionID == p.SessionID && c.BranchID == p.TargetBranchID && c.TipID == p.TargetTipID && validVersionHistory(c.History, p.SessionID, p.TargetBranchID, p.TargetTipID, "") &&
		c.Settings.Provider == state.provider && c.Settings.Model == state.model && c.Settings.PermissionMode == state.permission && slices.Contains(protocol.KnownThinkingLevels(), c.Settings.Thinking) &&
		(c.Mode == protocol.ModeDefault || c.Mode == protocol.ModePlan)
}

// A fully verified ACK retires the old instance, even if lifetime failure won
// the final waiter race. It can replace history but never revive failed/closing
// status, execute a prompt, or resurrect any old control token.
func (r *liveRuntime) publishVersionRestore(instanceID string, c runtimeVersionRestoreACK, outgoingEpoch uint64) (RuntimeSnapshot, error) {
	projected := projectVersionHistory(c.History)
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.instanceID != instanceID || r.snapshot.SessionID != c.SessionID {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	terminal := r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing"
	status, notice := r.snapshot.Status, r.snapshot.Error
	if !terminal {
		status = "idle"
		notice = ""
	}
	r.instanceID = rand.Text()
	r.resetRunControlsForReplacementLocked()
	old := r.snapshot
	r.snapshot = RuntimeSnapshot{ProjectID: old.ProjectID, InstanceID: r.instanceID, SessionID: old.SessionID, SessionName: old.SessionName, Provider: old.Provider, Model: old.Model, PermissionMode: old.PermissionMode, Thinking: string(c.Settings.Thinking), Mode: string(c.Mode), Status: status, Error: notice, Revision: old.Revision, Messages: projected.Messages, HistoryTruncated: projected.HistoryTruncated, HistoryToolsTruncated: projected.HistoryToolsTruncated, Telemetry: &RuntimeTelemetry{}, Recovery: old.Recovery}
	// Keep any new epoch already observed from pre-ACK metadata eligible. The
	// same capture also works when the ACK wins and metadata arrives afterward.
	r.retiredEpoch = max(r.retiredEpoch, outgoingEpoch)
	r.turnID = ""
	r.turnSequence = 0
	r.promptID = ""
	r.earlyCompletion = ""
	r.busy = false
	r.transitioning = false
	r.assistant = -1
	r.plan = -1
	r.assistantHasPlan = false
	r.pendingUserID = ""
	r.pendingRegenerateReplyID = ""
	r.cancelTaskToken = ""
	r.activityKeys = nil
	r.activityPrompt++
	r.activityCanceled = false
	r.usageBase = RuntimeTelemetry{}
	r.messageEdit = runtimeMessageEditState{}
	r.queue = runtimeQueueState{}
	r.versions = runtimeVersionState{}
	r.goal = runtimeGoalState{} // Any prior goal capability belongs to the retired branch/instance.
	r.publishLocked()
	if terminal {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return r.snapshot.clone(), nil
}

var _ RuntimeVersionsBackend = (*RuntimeManager)(nil)
