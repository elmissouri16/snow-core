package agent

import "github.com/elmissouri16/snow-core/pkg/protocol"

// A caller deadline is a host-imposed pause, not evidence that the objective
// is blocked. Preserve terminal outcomes and never pause a replacement goal.
func (a *Agent) pauseGoalAfterCallerCancellation() error {
	a.mu.RLock()
	admitted, controller := a.goalAtTurn.Clone(), a.opts.Goal
	a.mu.RUnlock()
	if admitted == nil || controller == nil {
		return nil
	}
	current, err := controller.Get()
	if err != nil {
		return err
	}
	if current == nil || current.GoalID != admitted.GoalID || current.Status != protocol.GoalActive {
		return nil
	}
	_, err = controller.SetStatus(admitted.GoalID, protocol.GoalPaused, false)
	return err
}
