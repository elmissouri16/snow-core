package web

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeGoal is a public branch-bound projection. Absence has an empty GoalID;
// it still carries the reviewed branch/tip needed for explicit create consent.
// Reading this projection never starts or resumes work.
type RuntimeGoal struct {
	SessionID       string          `json:"session_id"`
	BranchID        string          `json:"branch_id"`
	TipID           string          `json:"tip_id"`
	GoalID          string          `json:"goal_id"`
	Objective       string          `json:"objective"`
	Status          string          `json:"status"`
	BlockedReason   string          `json:"blocked_reason"`
	Deferred        bool            `json:"deferred"`
	TokenBudget     *int64          `json:"token_budget"`
	TokensUsed      int64           `json:"tokens_used"`
	BudgetRemaining *int64          `json:"budget_remaining"`
	EstimatedCosts  []protocol.Cost `json:"estimated_costs"`
	GoalRunID       string          `json:"goal_run_id"`
	Running         bool            `json:"running"`
}

func (g *RuntimeGoal) clone() *RuntimeGoal {
	if g == nil {
		return nil
	}
	out := *g
	if g.TokenBudget != nil {
		out.TokenBudget = new(*g.TokenBudget)
	}
	if g.BudgetRemaining != nil {
		out.BudgetRemaining = new(*g.BudgetRemaining)
	}
	out.EstimatedCosts = slices.Clone(g.EstimatedCosts)
	return &out
}

// GoalRunID and requestID are independent authorities: both must match before
// a completion can release root admission. Turn identity changes within a run.
type runtimeGoalState struct {
	pending, active                               bool
	sessionID, branchID, goalID, runID, requestID string
	events                                        []clientrpc.Event
	bytes                                         int
	revision                                      uint64
	epoch                                         uint64
	completion                                    *protocol.RPCGoalRunCompleted
}

func projectRuntimeGoal(i protocol.RPCGoalInspection) (*RuntimeGoal, bool) {
	if i.SessionID == "" || !runtimeIdentifier(i.SessionID) || i.BranchID == "" || !runtimeOption(i.BranchID) || !runtimeOption(i.TipID) || !runtimeOption(i.GoalRunID) {
		return nil, false
	}
	out := &RuntimeGoal{SessionID: strings.Clone(i.SessionID), BranchID: strings.Clone(i.BranchID), TipID: strings.Clone(i.TipID), Deferred: i.Deferred, Status: "none", GoalRunID: i.GoalRunID, Running: i.GoalRunID != ""}
	if i.Goal == nil {
		return out, i.GoalRunID == ""
	}
	g := i.Goal
	if g.Validate() != nil || strings.TrimSpace(string(g.Status)) != string(g.Status) || g.SessionID != i.SessionID || g.BranchID != i.BranchID || !runtimeOption(g.GoalID) || len(g.Objective) > runtimeMessageBytes || len(g.BlockedReason) > 32<<10 || len(g.EstimatedCosts) > 32 {
		return nil, false
	}
	out.GoalID, out.Objective, out.Status = strings.Clone(g.GoalID), strings.Clone(runtimeText(g.Objective, runtimeMessageBytes)), string(g.Status)
	out.BlockedReason = strings.Clone(runtimeText(g.BlockedReason, 32<<10))
	out.TokensUsed = g.TokensUsed
	if g.TokenBudget != nil {
		out.TokenBudget = new(*g.TokenBudget)
	}
	out.BudgetRemaining = g.RemainingBudget()
	for _, c := range g.EstimatedCosts {
		if len(c.Currency) > 16 || runtimeText(c.Currency, 16) != c.Currency {
			return nil, false
		}
		c.Currency = strings.Clone(c.Currency)
		out.EstimatedCosts = append(out.EstimatedCosts, c)
	}
	return out, true
}

// InspectGoal refreshes only an already activated worker. Browser cancellation
// cannot turn this read into execution and no absence/terminal goal is created.
func (m *RuntimeManager) InspectGoal(ctx context.Context, projectID, instanceID, sessionID, branchID string) (RuntimeSnapshot, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	r.mu.Lock()
	if sessionID == "" || sessionID != r.snapshot.SessionID || !runtimeOption(branchID) {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	revision := r.goal.revision
	active := r.goal.active
	r.mu.Unlock()
	var inspection protocol.RPCGoalInspection
	if err := r.call(protocol.RPCRequest{Type: "goal_inspect"}, protocol.RPCGoalInspectParams{SessionID: sessionID, BranchID: branchID}, &inspection); err != nil {
		return RuntimeSnapshot{}, err
	}
	projected, valid := projectRuntimeGoal(inspection)
	if !valid || inspection.SessionID != sessionID || branchID != "" && inspection.BranchID != branchID || !active && inspection.GoalRunID != "" {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.SessionID != sessionID {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	// A slow read cannot overwrite a newer event or run completion.
	if revision == r.goal.revision {
		if r.goal.active && (inspection.GoalRunID != r.goal.runID || inspection.BranchID != r.goal.branchID || projected.GoalID != r.goal.goalID) {
			return RuntimeSnapshot{}, ErrRuntimeInvalid
		}
		r.snapshot.Goal = projected
		r.goal.revision++
		r.publishLocked()
	}
	return r.snapshot.clone(), nil
}

func (r *liveRuntime) goalBlocksHistoryLocked() bool {
	g := r.snapshot.Goal
	return r.goal.pending || r.goal.active || g != nil && g.GoalID != "" && !protocol.ThreadGoalStatus(g.Status).Terminal()
}

// RuntimeGoalRunACK is an immutable receipt returned only from the HTTP action;
// it is never retained as future execution authority in the live snapshot.
type RuntimeGoalRunACK = protocol.RPCGoalRunAccepted

// RuntimeGoalRunInput binds consent to the reviewed manager configuration and
// public state. ExpectedRevision is web-only and never enters strict core params.
type RuntimeGoalRunInput struct {
	protocol.RPCGoalRunParams
	ExpectedRevision uint64
}

// RunGoal dispatches exactly one native core goal handle, not a web tool loop.
// Admission uses the same nonqueued control and whole-run Stop token as Prompt.
func (m *RuntimeManager) RunGoal(ctx context.Context, projectID, instanceID string, input RuntimeGoalRunInput) (RuntimeSnapshot, error) {
	params := input.RPCGoalRunParams
	if params.SessionID == "" || params.BranchID == "" || !runtimeIdentifier(params.SessionID) || !runtimeOption(params.BranchID) || !runtimeOption(params.ExpectedTipID) || !runtimeOption(params.ExpectedGoalID) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	switch params.Action {
	case "create":
		if !validMessageEditText(params.Objective) || len([]rune(params.Objective)) > protocol.MaxThreadGoalObjectiveChars || (params.TokenBudget != nil && *params.TokenBudget <= 0) {
			return RuntimeSnapshot{}, ErrRuntimeInvalid
		}
	case "resume":
		if params.ExpectedGoalID == "" || params.Objective != "" || params.TokenBudget != nil {
			return RuntimeSnapshot{}, ErrRuntimeInvalid
		}
	default:
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeSnapshot{}, err
	}
	project := r.project
	project.checkIdentity()
	if !project.Available {
		return RuntimeSnapshot{}, ErrProjectInvalid
	}
	r.mu.Lock()
	// Held/review work blocks BEFORE intent persistence or any RPC/mutation.
	if r.snapshot.Queue != nil && len(r.snapshot.Queue.Items) != 0 {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeQueueReview
	}
	g := r.snapshot.Goal
	if input.ExpectedRevision == 0 || input.ExpectedRevision != r.snapshot.Revision || r.snapshot.CancelRequested || r.goal.active || r.goal.pending || g == nil || g.SessionID != params.SessionID || r.snapshot.SessionID != params.SessionID || g.BranchID != params.BranchID || g.TipID != params.ExpectedTipID || g.GoalID != params.ExpectedGoalID {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if params.Action == "create" && g.GoalID != "" && !protocol.ThreadGoalStatus(g.Status).Terminal() || params.Action == "resume" && protocol.ThreadGoalStatus(g.Status).Terminal() {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}

	if r.snapshot.Mode == "plan" || params.Action == "resume" && (g.TokenBudget != nil && *g.TokenBudget <= g.TokensUsed || g.Status == "active" && !g.Deferred) {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r.mu.Unlock()
	if !r.supports("goal_run") {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if err := r.savePromptIntent(); err != nil {
		return RuntimeSnapshot{}, err
	}
	r.mu.Lock()
	if r.ctx.Err() != nil || r.snapshot.Status != "idle" {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.goal = runtimeGoalState{pending: true, sessionID: params.SessionID, branchID: params.BranchID, revision: r.goal.revision + 1}
	r.busy = true
	r.snapshot.CancelToken = rand.Text()
	r.snapshot.CancelRequested = false
	r.snapshot.Status = "running"
	r.snapshot.Error = ""
	r.messageEdit = runtimeMessageEditState{}
	r.promptID, r.earlyCompletion = "", ""
	r.pendingUserID, r.pendingRegenerateReplyID = "", ""
	r.assistant, r.plan = -1, -1
	r.assistantHasPlan = false
	r.turnID = ""
	r.activityPrompt++
	r.activityCanceled = false
	if r.snapshot.Telemetry != nil {
		r.usageBase = *r.snapshot.Telemetry
	}
	r.publishLocked()
	r.mu.Unlock()
	data, _ := json.Marshal(params)
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: "goal_run", Params: data})
	if err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return r.goalAcknowledged(response, params)
}

func (r *liveRuntime) goalAcknowledged(response protocol.RPCResponse, params protocol.RPCGoalRunParams) (RuntimeSnapshot, error) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	if !response.Success {
		// Only a typed pre-admission rejection proves no work ran. Ambiguous ACKs
		// fail closed and cancel the worker; never automatically retry.
		if response.ErrorCode != "goal_run_rejected" {
			r.fail()
			return RuntimeSnapshot{}, ErrRuntimeUnavailable
		}
		r.mu.Lock()
		if len(r.goal.events) != 0 || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
			r.mu.Unlock()
			r.fail()
			return RuntimeSnapshot{}, ErrRuntimeUnavailable
		}
		r.goal = runtimeGoalState{revision: r.goal.revision + 1}
		r.busy = false
		r.clearTurnCancelLocked()
		r.snapshot.Status = "idle"
		r.snapshot.Recovery.State = RecoveryRejected
		r.publishLocked()
		r.mu.Unlock()
		r.persistRecovery()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	var accepted protocol.RPCGoalRunAccepted
	data, err := json.Marshal(response.Data)
	if err != nil || json.Unmarshal(data, &accepted) != nil || response.ID == "" || !runtimeOption(response.ID) || accepted.GoalRunID == "" || !runtimeOption(accepted.GoalRunID) || accepted.GoalID == "" || !runtimeOption(accepted.GoalID) || accepted.SessionID != params.SessionID || accepted.BranchID != params.BranchID || params.Action == "resume" && accepted.GoalID != params.ExpectedGoalID {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	events := r.goal.events
	r.goal.events = nil
	r.goal.bytes = 0
	r.goal.pending = false
	r.goal.active = true
	r.goal.runID, r.goal.goalID, r.goal.requestID = accepted.GoalRunID, accepted.GoalID, response.ID
	r.goal.revision++
	r.snapshot.Recovery.State = RecoveryAdmitted
	r.snapshot.Recovery.UpdatedAt = time.Now().UTC()
	if r.snapshot.Goal != nil {
		r.snapshot.Goal = r.snapshot.Goal.clone()
		g := r.snapshot.Goal
		g.GoalRunID, g.GoalID = accepted.GoalRunID, accepted.GoalID
		g.Running, g.Deferred, g.Status = true, false, "active"
		g.BlockedReason = ""
		if params.Action == "create" {
			g.Objective = runtimeText(params.Objective, runtimeMessageBytes)
			g.TokenBudget, g.BudgetRemaining = nil, nil
			if params.TokenBudget != nil {
				g.TokenBudget = new(*params.TokenBudget)
				g.BudgetRemaining = new(*params.TokenBudget)
			}
			g.TokensUsed, g.EstimatedCosts = 0, nil
		}
	}
	terminal := r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing"
	r.publishLocked()
	r.mu.Unlock()
	if !terminal {
		for _, event := range events {
			r.consumeEvent(event)
		}
	}
	r.persistRecovery()
	r.mu.Lock()
	defer r.mu.Unlock()
	if terminal || r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	result := r.snapshot.clone()
	result.GoalRunACK = new(accepted)
	return result, nil
}

// refreshGoal is a read-only activation/switch refresh, after the existing abort
// has durably deferred any saved goal. It must never replace that abort.
func (r *liveRuntime) refreshGoal() error {
	if !r.supports("goal_run") {
		return nil
	}
	r.mu.Lock()
	sessionID, revision := r.snapshot.SessionID, r.goal.revision
	r.mu.Unlock()
	var inspection protocol.RPCGoalInspection
	if err := r.call(protocol.RPCRequest{Type: "goal_inspect"}, protocol.RPCGoalInspectParams{SessionID: sessionID}, &inspection); err != nil {
		return err
	}
	projected, valid := projectRuntimeGoal(inspection)
	if !valid || inspection.SessionID != sessionID || inspection.GoalRunID != "" {
		r.fail()
		return ErrRuntimeUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.SessionID != sessionID {
		return ErrRuntimeUnavailable
	}
	if revision == r.goal.revision {
		r.snapshot.Goal = projected
		r.goal.revision++
		r.publishLocked()
	}
	return nil
}
