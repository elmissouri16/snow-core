package web

import (
	"strconv"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// A read-only completion refresh owns the original busy generation, not the
// control mutex: the admission caller may still be waiting for its early ACK.
// New prompts/session controls remain blocked by busy. Revalidate every root
// coordinate before projecting a slow read or releasing readiness.
type runtimePromptCompletionBinding struct {
	instance, session, branch, turn, request, cancelToken string
	epoch                                                 uint64
	activity                                              uint64
}

func (r *liveRuntime) promptCompletionMatchesLocked(b runtimePromptCompletionBinding) bool {
	return r.ctx.Err() == nil && r.busy && !r.transitioning && r.snapshot.Status != "failed" && r.snapshot.Status != "closing" && r.instanceID == b.instance && r.snapshot.SessionID == b.session && r.turnID == b.turn && r.rootEpoch == b.epoch && r.activityPrompt == b.activity && r.snapshot.CancelToken == b.cancelToken && (r.promptID == b.request || r.promptID == "" && r.earlyCompletion == b.request) && (b.branch == "" || r.snapshot.Goal != nil && r.snapshot.Goal.BranchID == b.branch) && !r.goal.pending && !r.goal.active && !r.compaction.pending && !r.compaction.active
}

func (r *liveRuntime) consumePromptCompletion(completed protocol.RPCPromptCompleted) {
	r.mu.Lock()
	if r.messageEdit.committedTurn != "" && completed.RequestID != r.promptID {
		r.mu.Unlock()
		return
	}
	number, _ := strconv.ParseUint(completed.RequestID, 10, 64)
	if number != 0 && number <= r.completedRequest {
		r.mu.Unlock()
		return
	}
	mismatch := r.busy && r.promptID != "" && r.promptID != completed.RequestID
	if !r.busy || mismatch || r.snapshot.Status == "closing" || r.snapshot.Status == "failed" {
		r.mu.Unlock()
		if mismatch {
			r.fail()
		}
		return
	}
	if r.promptID == "" {
		r.earlyCompletion = completed.RequestID
	}
	binding := runtimePromptCompletionBinding{instance: r.instanceID, session: r.snapshot.SessionID, turn: r.turnID, request: completed.RequestID, cancelToken: r.snapshot.CancelToken, epoch: r.rootEpoch, activity: r.activityPrompt}
	if r.snapshot.Goal != nil {
		binding.branch = r.snapshot.Goal.BranchID
	}
	refresh := binding.session != "" && r.worker != nil && r.worker.Client != nil && r.supports("goal_run")
	if refresh {
		// Root attention belongs to the finished prompt. A child permission request
		// is independent and must remain resolvable after the root turn completes.
		if r.permissionAgent == nil {
			r.clearPermissionLocked()
		}
		r.snapshot.Input = nil
		if r.snapshot.Permission != nil {
			r.snapshot.Status = "permission"
		} else {
			r.snapshot.Status = "running"
		}
		r.publishLocked()
	}
	r.mu.Unlock()
	var projected *RuntimeGoal
	if refresh {
		var inspection protocol.RPCGoalInspection
		err := r.call(protocol.RPCRequest{Type: "goal_inspect"}, protocol.RPCGoalInspectParams{SessionID: binding.session, BranchID: binding.branch}, &inspection)
		var valid bool
		if err == nil {
			projected, valid = projectRuntimeGoal(inspection)
		}
		if err != nil || !valid || inspection.SessionID != binding.session || binding.branch != "" && inspection.BranchID != binding.branch || inspection.GoalRunID != "" {
			// A failed read cannot advertise a stale CAS scope as ready. Do not let a
			// delayed failure poison an unrelated replacement session/root either.
			r.mu.Lock()
			current := r.promptCompletionMatchesLocked(binding)
			r.mu.Unlock()
			if current {
				r.fail()
			}
			return
		}
	}
	r.mu.Lock()
	if !r.promptCompletionMatchesLocked(binding) {
		r.mu.Unlock()
		return
	}
	if projected != nil {
		r.snapshot.Goal = projected
		r.goal.revision++
	}
	r.busy = false
	r.clearTurnCancelLocked()
	r.completedRequest = max(r.completedRequest, number)
	r.completeRecoveryLocked(completed.Status)
	r.completeRegenerateReplyLocked(completed.Status)
	r.cancelActivities()
	r.assistant = -1
	r.pendingUserID = ""
	if r.permissionAgent == nil {
		r.clearPermissionLocked()
	}
	r.snapshot.Input = nil
	if r.snapshot.Permission != nil {
		r.snapshot.Status = "permission"
	} else {
		r.snapshot.Status = "idle"
	}
	if completed.Status == protocol.RPCPromptFailedStatus {
		r.snapshot.Error = runtimePromptFailureText(r.snapshot.Error)
	}
	r.publishLocked()
	r.mu.Unlock()
	r.persistRecovery()
}
