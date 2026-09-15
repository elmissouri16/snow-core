package web

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const runtimeHistoryControlCapability = "history_control"

func validHistoryControlName(name string, branch bool) bool {
	return name != "" && name == strings.TrimSpace(name) && len(name) <= protocol.RPCHistoryControlMaxNameBytes && utf8.ValidString(name) && !strings.ContainsFunc(name, unicode.IsControl) && ((branch && utf8.RuneCountInString(name) <= 64) || (!branch && utf8.RuneCountInString(name) <= 72))
}
func validHistoryControlRequest(p RuntimeHistoryControlRequest, action string) bool {
	return p.ExpectedRevision > 0 && runtimeIdentifier(p.SessionID) && validVersionIdentity(p.SourceBranchID, false) && validVersionIdentity(p.SourceTipID, true) && validVersionIdentity(p.TargetBranchID, false) && validVersionIdentity(p.TargetTipID, true) && validHistoryControlName(p.Name, action != "history-session-fork") && (action != "history-branch-rename" || validHistoryControlName(p.OldName, true))
}

// The existing Versions admission covers pending goals, queues/review and idle
// state. Core repeats every guard (including subagents and unfinished history)
// atomically with source/target saved-tip and name CAS immediately before write.
func (m *RuntimeManager) historyControlRuntime(ctx context.Context, projectID, instanceID string, p RuntimeHistoryControlRequest, action string) (*liveRuntime, RuntimeSnapshot, uint64, error) {
	if !validHistoryControlRequest(p, action) {
		return nil, RuntimeSnapshot{}, 0, ErrRuntimeInvalid
	}
	r, err := m.versionRuntime(ctx, projectID, instanceID, p.SessionID, true)
	if err != nil {
		return nil, RuntimeSnapshot{}, 0, err
	}
	r.mu.Lock()
	before, outgoing := r.snapshot.clone(), r.rootEpoch
	valid := r.supports(runtimeHistoryControlCapability) && before.Revision == p.ExpectedRevision
	r.mu.Unlock()
	if !valid {
		r.control.Unlock()
		return nil, RuntimeSnapshot{}, 0, ErrRuntimeInvalid
	}
	return r, before, outgoing, nil
}

// Unknown transport or mutation outcomes poison the runtime. No caller retries
// a mutation on behalf of an old inspected revision, including known rejection.
func (r *liveRuntime) historyControlCall(command string, params, result any, transition bool) error {
	if transition {
		r.mu.Lock()
		r.transitioning = true
		r.snapshot.Status = "switching"
		r.publishLocked()
		r.mu.Unlock()
	}
	data, err := json.Marshal(params)
	if err != nil {
		r.fail()
		return ErrRuntimeUnavailable
	}
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: command, Params: data})
	if err != nil {
		r.fail()
		return ErrRuntimeUnavailable
	}
	if !response.Success {
		if response.ErrorCode != protocol.RPCHistoryControlRejectedErrorCode {
			r.fail()
			return ErrRuntimeUnavailable
		}
		r.mu.Lock()
		if r.ctx.Err() == nil && r.snapshot.Status != "failed" && r.snapshot.Status != "closing" {
			if transition {
				r.transitioning = false
				r.snapshot.Status = "idle"
			}
			// Retire the inspected revision even after a known rejection.
			r.publishLocked()
		}
		r.mu.Unlock()
		return ErrRuntimeInvalid
	}
	data, err = json.Marshal(response.Data)
	if err != nil || len(data) > runtimeVersionWireBytes || json.Unmarshal(data, result) != nil {
		r.fail()
		return ErrRuntimeUnavailable
	}
	return nil
}

func (m *RuntimeManager) HistoryBranchFork(ctx context.Context, projectID, instanceID string, p RuntimeHistoryControlRequest) (RuntimeSnapshot, error) {
	r, before, outgoing, err := m.historyControlRuntime(ctx, projectID, instanceID, p, "history-branch-fork")
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	var ack protocol.RPCManagedBranchForkResult
	params := protocol.RPCManagedBranchForkParams{RPCHistoryControlBinding: p.RPCHistoryControlBinding, Name: p.Name}
	if err := r.historyControlCall("history_branch_fork", params, &ack, true); err != nil {
		return RuntimeSnapshot{}, err
	}
	r.mu.Lock()
	observedEpoch := r.rootEpoch
	r.mu.Unlock()
	if !validHistoryBranchForkACK(ack, p, outgoing) || observedEpoch > ack.RootEpoch {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		r.fail()
		return RuntimeSnapshot{}, err
	}
	// Metadata ACK is not history. Reload through the exact new cursor's bounded
	// public page; omitted tool maps never authorize raw tool-pair reconstruction.
	var page protocol.RPCBranchMessagesPage
	if err := r.call(protocol.RPCRequest{Type: "branch_messages_page"}, protocol.RPCBranchMessagesPageParams{SessionID: ack.SessionID, BranchID: ack.BranchID, TipID: ack.TipID, Limit: protocol.RPCBranchMessagesPageMaxItems}, &page); err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if !validVersionHistory(page, ack.SessionID, ack.BranchID, ack.TipID, "") || page.Total > 100000 {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	committed := runtimeVersionRestoreACK{SessionID: ack.SessionID, BranchID: ack.BranchID, TipID: ack.TipID, History: page, Mode: ack.Mode, Settings: protocol.RPCSettings{Provider: before.Provider, Model: before.Model, PermissionMode: before.PermissionMode, Thinking: ack.ReasoningEffort}}
	// outgoing was captured BEFORE dispatch. Pre-ACK events may already have
	// advanced rootEpoch; retiring that new epoch would reproduce BUG-151.
	if _, err := r.publishVersionRestore(instanceID, committed, outgoing); err != nil {
		return RuntimeSnapshot{}, err
	}
	if err := r.refreshGoal(); err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	valid := r.ctx.Err() == nil && r.snapshot.Status == "idle" && (r.snapshot.Goal == nil || r.snapshot.Goal.SessionID == ack.SessionID && r.snapshot.Goal.BranchID == ack.BranchID && !r.snapshot.Goal.Running)
	snapshot := r.snapshot.clone()
	r.mu.Unlock()
	if !valid {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return snapshot, nil
}

func validHistoryBranchForkACK(a protocol.RPCManagedBranchForkResult, p RuntimeHistoryControlRequest, outgoing uint64) bool {
	return a.SessionID == p.SessionID && validVersionIdentity(a.BranchID, false) && a.BranchID != p.SourceBranchID && a.BranchID != p.TargetBranchID && a.TipID == p.TargetTipID && a.RootEpoch > outgoing && a.Branch.ID == a.BranchID && a.Branch.TipID == a.TipID && a.Branch.Name == p.Name && a.Branch.ParentID == p.TargetBranchID && a.Branch.ForkedFromID == p.TargetTipID && a.Branch.Active && (a.Mode == protocol.ModeDefault || a.Mode == protocol.ModePlan) && slices.Contains(protocol.KnownThinkingLevels(), a.ReasoningEffort)
}

var _ RuntimeHistoryControlBackend = (*RuntimeManager)(nil)
