package session

import (
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrGoalConflict identifies an optimistic goal identity or state conflict.
var ErrGoalConflict = errors.New("session: goal conflict")

// GoalConflictError reports the current non-sensitive goal identity after a
// failed optimistic mutation. Objective text and store paths are deliberately
// excluded so callers can safely return the conflict to a model or client.
type GoalConflictError struct {
	Kind              string
	ExpectedGoalID    string
	CurrentGoalID     string
	CurrentStatus     protocol.ThreadGoalStatus
	SessionID         string
	BranchID          string
	BindingGeneration uint64
}

func (e *GoalConflictError) Error() string {
	if e == nil {
		return ErrGoalConflict.Error()
	}
	current := e.CurrentGoalID
	if current == "" {
		current = "<none>"
	}
	return fmt.Sprintf("%s: %s (expected %q, current %q, session %q, branch %q)", ErrGoalConflict, e.Kind, e.ExpectedGoalID, current, e.SessionID, e.BranchID)
}

func (e *GoalConflictError) Unwrap() error { return ErrGoalConflict }

func newGoalConflict(kind, expected, sessionID, branchID string, current *protocol.ThreadGoal) error {
	conflict := &GoalConflictError{
		Kind:           kind,
		ExpectedGoalID: expected,
		SessionID:      sessionID,
		BranchID:       branchID,
	}
	if current != nil {
		conflict.CurrentGoalID = current.GoalID
		conflict.CurrentStatus = current.Status
		if conflict.SessionID == "" {
			conflict.SessionID = current.SessionID
		}
		if conflict.BranchID == "" {
			conflict.BranchID = current.BranchID
		}
	}
	return conflict
}
