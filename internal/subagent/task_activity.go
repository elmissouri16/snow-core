package subagent

import "github.com/elmissouri16/snow-core/pkg/protocol"

// runtimeWorkStatusLocked projects outstanding work without overwriting the
// durable outcome of the previous task. The caller holds r.mu.
func runtimeWorkStatusLocked(r *runtime) protocol.AgentStatus {
	if r.finalizing || r.state.Status == protocol.AgentRunning || (r.child != nil && r.child.IsRunning()) {
		return protocol.AgentRunning
	}
	if runtimeHasActiveWorkLocked(r) {
		return protocol.AgentQueued
	}
	return r.state.Status
}

// finishTask releases exactly one accepted task, including early exits before
// a provider turn starts. Accounting survives dequeue and slot waits, where
// channel length and the previous terminal status cannot describe activity.
func (m *Manager) finishTask(r *runtime) {
	r.mu.Lock()
	r.pendingTasks--
	r.cancel = nil
	r.interruptRequested = false
	r.mu.Unlock()
	// Never acquire the manager lock while holding r.mu. Waiters take them in
	// the opposite order; notifying after unlock also exposes settled state.
	m.signalActivity()
}
