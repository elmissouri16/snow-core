package protocol

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

const MaxThreadGoalObjectiveChars = 32 * 1024

type ThreadGoalStatus string

const (
	GoalActive        ThreadGoalStatus = "active"
	GoalPaused        ThreadGoalStatus = "paused"
	GoalBlocked       ThreadGoalStatus = "blocked"
	GoalUsageLimited  ThreadGoalStatus = "usage_limited"
	GoalBudgetLimited ThreadGoalStatus = "budget_limited"
	GoalComplete      ThreadGoalStatus = "complete"
)

func ParseThreadGoalStatus(v string) (ThreadGoalStatus, error) {
	s := ThreadGoalStatus(strings.TrimSpace(strings.ToLower(v)))
	switch s {
	case GoalActive, GoalPaused, GoalBlocked, GoalUsageLimited, GoalBudgetLimited, GoalComplete:
		return s, nil
	default:
		return "", fmt.Errorf("protocol: invalid thread goal status %q", v)
	}
}

func (s ThreadGoalStatus) Terminal() bool {
	return s == GoalComplete || s == GoalBudgetLimited
}

type ThreadGoal struct {
	SessionID      string           `json:"session_id"`
	BranchID       string           `json:"branch_id"`
	GoalID         string           `json:"goal_id"`
	Objective      string           `json:"objective"`
	Status         ThreadGoalStatus `json:"status"`
	TokenBudget    *int64           `json:"token_budget,omitempty"`
	TokensUsed     int64            `json:"tokens_used"`
	SecondsUsed    int64            `json:"seconds_used"`
	EstimatedCosts []Cost           `json:"estimated_costs,omitempty"`
	CreatedAt      int64            `json:"created_at"`
	UpdatedAt      int64            `json:"updated_at"`
}

func (g *ThreadGoal) Clone() *ThreadGoal {
	if g == nil {
		return nil
	}
	out := *g
	if g.TokenBudget != nil {
		v := *g.TokenBudget
		out.TokenBudget = &v
	}
	out.EstimatedCosts = append([]Cost(nil), g.EstimatedCosts...)
	return &out
}

func (g ThreadGoal) RemainingBudget() *int64 {
	if g.TokenBudget == nil {
		return nil
	}
	v := *g.TokenBudget - g.TokensUsed
	if v < 0 {
		v = 0
	}
	return &v
}

func (g ThreadGoal) Validate() error {
	if g.SessionID == "" || g.BranchID == "" {
		return errors.New("protocol: goal session_id and branch_id are required")
	}
	if g.GoalID == "" {
		return errors.New("protocol: goal_id is required")
	}
	objective := strings.TrimSpace(g.Objective)
	if objective == "" {
		return errors.New("protocol: goal objective is required")
	}
	if len([]rune(objective)) > MaxThreadGoalObjectiveChars {
		return fmt.Errorf("protocol: goal objective exceeds %d characters", MaxThreadGoalObjectiveChars)
	}
	if _, err := ParseThreadGoalStatus(string(g.Status)); err != nil {
		return err
	}
	if g.TokenBudget != nil && *g.TokenBudget <= 0 {
		return errors.New("protocol: goal token budget must be positive")
	}
	if g.TokensUsed < 0 || g.SecondsUsed < 0 {
		return errors.New("protocol: goal usage cannot be negative")
	}
	currencies := make(map[string]bool, len(g.EstimatedCosts))
	for _, cost := range g.EstimatedCosts {
		currency := strings.ToUpper(strings.TrimSpace(cost.Currency))
		if currency == "" {
			return errors.New("protocol: estimated goal cost currency is required")
		}
		if currencies[currency] {
			return fmt.Errorf("protocol: duplicate estimated goal cost currency %q", currency)
		}
		currencies[currency] = true
		for _, value := range []float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.Total} {
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return errors.New("protocol: estimated goal cost must be finite and non-negative")
			}
		}
	}
	return nil
}

type ThreadGoalUpdate struct {
	Goal    *ThreadGoal `json:"goal,omitempty"`
	Cleared bool        `json:"cleared,omitempty"`
}

func (u *ThreadGoalUpdate) Clone() *ThreadGoalUpdate {
	if u == nil {
		return nil
	}
	return &ThreadGoalUpdate{Goal: u.Goal.Clone(), Cleared: u.Cleared}
}

// InternalContextFragment is trusted host-generated private steering. Provider
// adapters serialize it as user-role input after conversation history.
type InternalContextFragment struct {
	Source string `json:"source"`
	Text   string `json:"text"`
}

func (f InternalContextFragment) Validate() error {
	if f.Source == "" {
		return errors.New("protocol: internal context source is required")
	}
	for _, r := range f.Source {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("protocol: invalid internal context source %q", f.Source)
		}
	}
	if strings.TrimSpace(f.Text) == "" {
		return errors.New("protocol: internal context text is required")
	}
	return nil
}
