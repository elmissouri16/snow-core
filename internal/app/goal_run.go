package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrGoalRunOutcomeUnknown means a goal write was attempted. Inspect current
// state before any new action; never automatically replay the request.
var ErrGoalRunOutcomeUnknown = errors.New("goal run outcome requires authoritative refresh")

// ErrGoalRunRejected identifies a failed read-only admission, before goal writes.
var ErrGoalRunRejected = errors.New("goal run rejected before mutation")

func (a *App) goalRunBindingAdmitted(sessionID, branchID string) error {
	if sessionID == "" || branchID == "" || a.Session.ID() != sessionID {
		return errors.New("goal: session binding changed")
	}
	branches, ok := a.Session.(session.ActiveBranchStore)
	if !ok || branches.ActiveBranchID() != branchID {
		return errors.New("goal: branch binding changed")
	}
	return nil
}

// StartGoalRun validates and reserves a single serial operation without starting
// a provider. Release the returned handle only after its public ACK succeeds.
func (a *App) StartGoalRun(ctx context.Context, p protocol.RPCGoalRunParams) (*agent.GoalRunHandle, error) {
	h, err := a.startGoalRun(ctx, p)
	if err != nil && !errors.Is(err, ErrGoalRunOutcomeUnknown) {
		err = errors.Join(ErrGoalRunRejected, err)
	}
	return h, err
}

func (a *App) startGoalRun(ctx context.Context, p protocol.RPCGoalRunParams) (*agent.GoalRunHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.Action != "create" && p.Action != "resume" {
		return nil, errors.New("goal: action must be create or resume")
	}
	if p.Action == "create" {
		if strings.TrimSpace(p.Objective) == "" || len(p.Objective) > 4*protocol.MaxThreadGoalObjectiveChars || (p.TokenBudget != nil && *p.TokenBudget <= 0) {
			return nil, errors.New("goal: create requires an objective and any token budget must be positive")
		}
	} else if p.Objective != "" || p.TokenBudget != nil || p.ExpectedGoalID == "" {
		return nil, errors.New("goal: resume requires exact expected_goal_id and accepts no objective or budget")
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
	if err := a.Agent.GoalRunReadyAdmitted(); err != nil {
		return nil, err
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return nil, errors.New("goal: active subagents prevent admission")
	}
	if err := a.goalRunBindingAdmitted(p.SessionID, p.BranchID); err != nil {
		return nil, err
	}
	if p.ExpectedTipID != a.Session.BranchTip() {
		return nil, errors.New("goal: branch tip changed")
	}
	goal, err := a.Goal.Get()
	if err != nil {
		return nil, err
	}
	currentID := ""
	if goal != nil {
		currentID = goal.GoalID
	}
	if currentID != p.ExpectedGoalID {
		return nil, errors.New("goal: expected goal identity changed")
	}
	if p.Action == "create" && goal != nil && !goal.Status.Terminal() {
		return nil, errors.New("goal: cannot replace unfinished goal")
	}
	if p.Action == "resume" {
		if goal == nil || goal.Status.Terminal() {
			return nil, errors.New("goal: cannot resume terminal or absent goal")
		}
		if goal.TokenBudget != nil && *goal.TokenBudget <= goal.TokensUsed {
			return nil, errors.New("goal: resume requires remaining positive token budget")
		}
		deferred, err := a.Goal.Deferred()
		if err != nil {
			return nil, err
		}
		if goal.Status == protocol.GoalActive && !deferred {
			return nil, errors.New("goal: active goal has not been deferred for review")
		}
	}
	// After this boundary write failures are ambiguous. The fixed managed policy
	// forbids legacy continuation even if persistence cannot store the deferral.
	uncertain := func(cause error) (*agent.GoalRunHandle, error) {
		return nil, errors.Join(ErrGoalRunOutcomeUnknown, cause, a.Goal.Defer(true))
	}
	if p.Action == "create" {
		goal, err = a.Goal.Create(p.Objective, p.TokenBudget, false)
		if err != nil {
			return uncertain(err)
		}
	} else if goal.Status != protocol.GoalActive {
		goal, err = a.Goal.SetStatus(goal.GoalID, protocol.GoalActive, false)
		if err != nil {
			return uncertain(err)
		}
	}
	if goal == nil {
		return uncertain(errors.New("goal: persisted goal is unavailable"))
	}
	if err := a.Goal.Defer(true); err != nil {
		return uncertain(err)
	}
	handle, err := a.Agent.StartGoalRunAdmitted(ctx, goal.GoalID)
	if err != nil {
		return uncertain(err)
	}
	return handle, nil
}

// InspectGoal is a read-only active branch projection. Absence is explicit and
// this operation never makes a deferred goal eligible to run.
func (a *App) InspectGoal(ctx context.Context, p protocol.RPCGoalInspectParams) (protocol.RPCGoalInspection, error) {
	var result protocol.RPCGoalInspection
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	if p.BranchID == "" {
		if branches, ok := a.Session.(session.ActiveBranchStore); ok {
			p.BranchID = branches.ActiveBranchID()
		}
	}
	if err := a.goalRunBindingAdmitted(p.SessionID, p.BranchID); err != nil {
		return result, err
	}
	goal, err := a.Goal.Get()
	if err != nil {
		return result, err
	}
	deferred, err := a.Goal.Deferred()
	if err != nil {
		return result, fmt.Errorf("goal: inspect deferral: %w", err)
	}
	result = protocol.RPCGoalInspection{SessionID: p.SessionID, BranchID: p.BranchID, TipID: a.Session.BranchTip(), Goal: goal.Clone(), Deferred: deferred, GoalRunID: a.Agent.GoalRunID()}
	if goal != nil && goal.TokenBudget != nil {
		result.BudgetRemaining = new(max(int64(0), *goal.TokenBudget-goal.TokensUsed))
	}
	return result, nil
}
