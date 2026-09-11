package agent

import (
	"errors"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// LockPluginReloadAdmission is nonblocking and never stops automatic work.
// Reload is a catalog change, not a request to cancel a user's turn or goal.
func (a *Agent) LockPluginReloadAdmission() (func(), error) {
	m := &a.admissionMu
	m.once.Do(func() { m.token = make(chan struct{}, 1) })
	select {
	case m.token <- struct{}{}:
	default:
		return nil, errors.New("plugin reload: root operation active")
	}
	if err := a.PluginReloadBusyAdmitted(); err != nil {
		m.Unlock()
		return nil, err
	}
	return m.Unlock, nil
}

// PluginReloadBusyAdmitted checks idle catalog-mutation eligibility without
// acquiring admission or changing goal state. The caller holds admission.
func (a *Agent) PluginReloadBusyAdmitted() error {
	a.mu.RLock()
	closed, busy := a.closed, a.running || a.autoRunning || a.autoPending
	a.mu.RUnlock()
	if closed {
		return errors.New("plugin reload: agent closed")
	}
	if busy {
		return errors.New("plugin reload: root turn or automatic worker active")
	}
	if a.opts.Goal != nil {
		goal, err := a.opts.Goal.Get()
		if err != nil {
			return err
		}
		if goal != nil && goal.Status == protocol.GoalActive {
			return errors.New("plugin reload: pause the active goal first")
		}
	}
	return nil
}
