package web

import (
	"encoding/json/v2"
	"strings"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const goalEventCount = 512
const goalEventBytes = 512 << 10

// Caller holds eventMu and mu. Retain only public allowlisted owned values;
// never raw errors, arguments, thinking, credentials or provider continuity.
func (r *liveRuntime) bufferGoalEventLocked(event clientrpc.Event) (bool, bool) {
	if !r.goal.pending {
		return false, true
	}
	public, keep, valid := publicGoalEvent(event)
	if !valid {
		return true, false
	}
	if !keep {
		return true, true
	}
	data, err := json.Marshal(public)
	if err != nil || len(r.goal.events) >= goalEventCount || len(data) > goalEventBytes-r.goal.bytes {
		return true, false
	}
	var owned clientrpc.Event
	if json.Unmarshal(data, &owned) != nil {
		return true, false
	}
	r.goal.events = append(r.goal.events, owned)
	r.goal.bytes += len(data)
	return true, true
}

func publicGoalEvent(event clientrpc.Event) (clientrpc.Event, bool, bool) {
	if c := event.GoalRunCompleted; c != nil {
		if c.RequestID == "" || !runtimeOption(c.RequestID) || c.GoalRunID == "" || !runtimeOption(c.GoalRunID) || c.GoalID == "" || !runtimeOption(c.GoalID) {
			return clientrpc.Event{}, false, false
		}
		switch c.Status {
		case "finished", "canceled", "failed":
		default:
			return clientrpc.Event{}, false, false
		}
		if c.GoalStatus != "" {
			parsed, err := protocol.ParseThreadGoalStatus(string(c.GoalStatus))
			if err != nil || parsed != c.GoalStatus {
				return clientrpc.Event{}, false, false
			}
		}
		out := &protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: strings.Clone(c.RequestID), GoalRunID: strings.Clone(c.GoalRunID), GoalID: strings.Clone(c.GoalID), Status: c.Status, GoalStatus: c.GoalStatus}
		return clientrpc.Event{GoalRunCompleted: out}, true, true
	}
	e := event.AgentEvent
	if e == nil || e.GoalRunID == "" {
		return clientrpc.Event{}, false, true
	}
	if !goalRootEvent(*e) {
		return clientrpc.Event{}, false, true
	}
	if !runtimeOption(e.GoalRunID) || !runtimeOption(e.TurnID) {
		return clientrpc.Event{}, false, false
	}
	if e.Type == protocol.EvThreadGoalUpdated {
		if e.ThreadGoal == nil || e.ThreadGoal.Goal == nil || e.ThreadGoal.Cleared {
			return clientrpc.Event{}, false, false
		}
		g := e.ThreadGoal.Goal
		projected, valid := projectRuntimeGoal(protocol.RPCGoalInspection{SessionID: g.SessionID, BranchID: g.BranchID, Goal: g})
		if !valid {
			return clientrpc.Event{}, false, false
		}
		// Reconstruct from the public projection, never copy future private fields.
		public := &protocol.ThreadGoal{SessionID: projected.SessionID, BranchID: projected.BranchID, GoalID: projected.GoalID, Objective: projected.Objective, Status: protocol.ThreadGoalStatus(projected.Status), BlockedReason: projected.BlockedReason, TokenBudget: projected.TokenBudget, TokensUsed: projected.TokensUsed, EstimatedCosts: projected.EstimatedCosts}
		return clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: e.Type, GoalRunID: strings.Clone(e.GoalRunID), RootEpoch: e.RootEpoch, TurnID: strings.Clone(e.TurnID), TurnSequence: e.TurnSequence, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: public}}}, true, true
	}
	// Queue snapshots belong to prompt roots, never goal admission.
	if e.Type == protocol.EvQueueUpdated {
		return clientrpc.Event{}, false, true
	}
	p, keep, valid := publicMessageEditEvent(event)
	if keep && p.AgentEvent != nil {
		p.AgentEvent.GoalRunID = strings.Clone(e.GoalRunID)
	}
	return p, keep, valid
}

func goalRootEvent(e protocol.AgentEvent) bool {
	ref := e.Agent
	return ref == nil || ref.Path == protocol.RootAgentPath && ref.Depth == 0 && ref.ParentPath == "" && ref.ParentThreadID == "" && ref.Validate() == nil
}

// consumeGoalEvent is called before legacy prompt projection with eventMu held.
// False means an exact run-owned event may enter the existing public projector.
func (r *liveRuntime) consumeGoalEvent(event clientrpc.Event) bool {
	r.mu.Lock()
	if c := event.GoalRunCompleted; c != nil {
		if !r.goal.active || c.RequestID != r.goal.requestID || c.GoalRunID != r.goal.runID || c.GoalID != r.goal.goalID || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
			r.mu.Unlock()
			return true
		}
		_, _, valid := publicGoalEvent(event)
		if !valid {
			r.mu.Unlock()
			r.fail()
			return true
		}
		r.goal.completion = &protocol.RPCGoalRunCompleted{RequestID: c.RequestID, GoalRunID: c.GoalRunID, GoalID: c.GoalID, Status: c.Status, GoalStatus: c.GoalStatus}
		r.goal.revision++
		if r.permissionAgent == nil {
			r.clearPermissionLocked()
		}
		r.snapshot.Input = nil
		r.cancelActivities()
		r.finishGoalRunLocked()
		r.publishLocked()
		r.mu.Unlock()
		r.persistRecovery()
		return true
	}
	if event.PromptCompleted != nil && (r.goal.pending || r.goal.active) {
		r.mu.Unlock()
		return true
	}
	e := event.AgentEvent
	if e == nil {
		r.mu.Unlock()
		return false
	}
	if !r.goal.active {
		// A stale run cannot repaint a subsequent prompt/read-only session.
		if e.GoalRunID != "" {
			r.mu.Unlock()
			return true
		}
		if e.Type == protocol.EvThreadGoalUpdated {
			valid := r.applyIdleGoalUpdateLocked(*e)
			r.mu.Unlock()
			if !valid {
				r.fail()
			}
			return true
		}
		r.mu.Unlock()
		return false
	}
	if e.GoalRunID != r.goal.runID || !goalRootEvent(*e) || e.RootEpoch == 0 || e.RootEpoch <= r.retiredEpoch || e.RootEpoch < r.rootEpoch || r.goal.epoch != 0 && e.RootEpoch != r.goal.epoch || r.goal.completion != nil {
		r.mu.Unlock()
		return true
	}
	r.goal.epoch = e.RootEpoch
	if e.Type == protocol.EvThreadGoalUpdated {
		valid := r.applyRunGoalUpdateLocked(*e)
		r.mu.Unlock()
		if !valid {
			r.fail()
		}
		return true
	}
	if e.Type == protocol.EvQueueUpdated || e.TurnID == "" || e.TurnSequence == 0 || e.TurnSequence < r.turnSequence || e.TurnSequence == r.turnSequence && r.turnID != "" && e.TurnID != r.turnID {
		r.mu.Unlock()
		return true
	}
	if e.TurnID != r.turnID {
		if e.TurnSequence <= r.turnSequence {
			r.mu.Unlock()
			return true
		}
		r.assistant, r.plan = -1, -1
		if r.snapshot.Telemetry != nil {
			r.usageBase = *r.snapshot.Telemetry
		}
		r.assistantHasPlan = false
		r.pendingUserID, r.pendingRegenerateReplyID = "", ""
		r.activityPrompt++
		r.activityCanceled = false
		if r.permissionAgent == nil {
			r.clearPermissionLocked()
		}
		r.snapshot.Input = nil
		if r.snapshot.Permission != nil {
			r.snapshot.Status = "permission"
		} else {
			r.snapshot.Status = "running"
		}
	}
	r.mu.Unlock()
	return false
}

func (r *liveRuntime) applyRunGoalUpdateLocked(e protocol.AgentEvent) bool {
	if e.ThreadGoal == nil || e.ThreadGoal.Goal == nil || e.ThreadGoal.Cleared {
		return false
	}
	g := e.ThreadGoal.Goal
	if g.SessionID != r.goal.sessionID || g.BranchID != r.goal.branchID || g.GoalID != r.goal.goalID {
		return true
	}
	tip := ""
	if r.snapshot.Goal != nil {
		tip = r.snapshot.Goal.TipID
	}
	projected, valid := projectRuntimeGoal(protocol.RPCGoalInspection{SessionID: g.SessionID, BranchID: g.BranchID, TipID: tip, Goal: g, GoalRunID: r.goal.runID})
	if !valid {
		return false
	}
	r.rootEpoch = e.RootEpoch
	r.snapshot.Goal = projected
	r.goal.revision++
	r.publishLocked()
	return true
}

func (r *liveRuntime) applyIdleGoalUpdateLocked(e protocol.AgentEvent) bool {
	g := r.snapshot.Goal
	if g == nil || r.transitioning || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" || !goalRootEvent(e) || e.RootEpoch == 0 || e.RootEpoch <= r.retiredEpoch || e.RootEpoch != r.rootEpoch || e.ThreadGoal == nil || e.ThreadGoal.Goal == nil {
		return true
	}
	update := e.ThreadGoal.Goal
	if update.SessionID != r.snapshot.SessionID || update.BranchID != g.BranchID || update.GoalID != g.GoalID {
		return true
	}
	projected, valid := projectRuntimeGoal(protocol.RPCGoalInspection{SessionID: update.SessionID, BranchID: update.BranchID, TipID: g.TipID, Goal: update, Deferred: g.Deferred})
	if !valid {
		return false
	}
	r.snapshot.Goal = projected
	r.goal.revision++
	r.publishLocked()
	return true
}

// Called with mu held after cancellation task retirement as well as completion.
// Individual turn_done and an abort ACK never retire whole-run Stop authority.
func (r *liveRuntime) finishGoalRunLocked() {
	c := r.goal.completion
	if c == nil || !r.goal.active || r.cancelTaskToken != "" || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return
	}
	r.goal.active = false
	r.busy = false
	r.clearTurnCancelLocked()
	if r.permissionAgent == nil {
		r.clearPermissionLocked()
	}
	r.snapshot.Input = nil
	if r.snapshot.Permission != nil {
		r.snapshot.Status = "permission"
	} else {
		r.snapshot.Status = "idle"
	}
	r.assistant, r.plan = -1, -1
	r.assistantHasPlan = false
	status := protocol.RPCPromptCompletedStatus
	if c.Status == "canceled" {
		status = protocol.RPCPromptCanceledStatus
	}
	if c.Status == "failed" {
		status = protocol.RPCPromptFailedStatus
		r.snapshot.Error = "Goal run failed. Review the saved goal before explicitly resuming."
	}
	r.completeRecoveryLocked(status)
	if r.snapshot.Goal != nil {
		r.snapshot.Goal = r.snapshot.Goal.clone()
		g := r.snapshot.Goal
		g.Running = false
		g.GoalRunID = ""
		if c.GoalStatus != "" {
			if _, err := protocol.ParseThreadGoalStatus(string(c.GoalStatus)); err == nil {
				g.Status = string(c.GoalStatus)
			}
		}
		// Exact durable deferred state is obtained by the next read-only inspection.
		if c.Status != "finished" {
			g.Deferred = true
		}
	}
	r.goal.revision++
}
