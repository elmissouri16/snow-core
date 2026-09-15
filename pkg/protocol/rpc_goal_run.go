package protocol

// RPCGoalRunParams binds an explicit operation to the exact reviewed branch.
// Empty ExpectedGoalID asserts absence; it never means "whichever goal exists".
type RPCGoalRunParams struct {
	Action         string `json:"action"`
	SessionID      string `json:"session_id"`
	BranchID       string `json:"branch_id"`
	ExpectedTipID  string `json:"expected_tip_id"`
	ExpectedGoalID string `json:"expected_goal_id"`
	Objective      string `json:"objective,omitempty"`
	TokenBudget    *int64 `json:"token_budget,omitempty"`
}

type RPCGoalInspectParams struct {
	SessionID string `json:"session_id"`
	BranchID  string `json:"branch_id"`
}

type RPCGoalInspection struct {
	SessionID       string      `json:"session_id"`
	BranchID        string      `json:"branch_id"`
	TipID           string      `json:"tip_id"`
	Goal            *ThreadGoal `json:"goal"`
	Deferred        bool        `json:"deferred"`
	BudgetRemaining *int64      `json:"budget_remaining,omitempty"`
	GoalRunID       string      `json:"goal_run_id,omitempty"`
}

type RPCGoalRunAccepted struct {
	GoalRunID string `json:"goal_run_id"`
	GoalID    string `json:"goal_id"`
	SessionID string `json:"session_id"`
	BranchID  string `json:"branch_id"`
}

const (
	RPCTypeGoalRunCompleted     = "goal_run_completed"
	RPCGoalRunRejectedErrorCode = "goal_run_rejected"
	RPCGoalRunUnknownErrorCode  = "goal_run_unknown"
)

// RPCGoalRunCompleted describes execution, not semantic goal success. A finished
// run can leave a paused, blocked, budget-limited or complete goal.
type RPCGoalRunCompleted struct {
	Type       string           `json:"type"`
	RequestID  string           `json:"request_id"`
	GoalRunID  string           `json:"goal_run_id"`
	GoalID     string           `json:"goal_id"`
	Status     string           `json:"status"` // finished, canceled, failed
	GoalStatus ThreadGoalStatus `json:"goal_status,omitempty"`
	Error      string           `json:"error,omitempty"`
}
