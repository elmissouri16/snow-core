package subagent

import (
	"context"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// CloseAgent releases one terminal identity from the open-agent quota while
// preserving its stable path, durable transcript, result, usage, and topology.
func (m *Manager) CloseAgent(ctx context.Context, caller Caller, target string) (protocol.AgentStatus, error) {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.AgentNotFound, err
	}
	r, ref, err := m.resolveTarget(caller, target)
	if err != nil {
		return protocol.AgentNotFound, err
	}
	if ref.Path == protocol.RootAgentPath || ref.Path == caller.Path {
		return protocol.AgentNotFound, errors.New("subagents: cannot close root or self")
	}

	r.mu.Lock()
	previous := r.state.Status
	if previous == protocol.AgentClosed {
		var detached detachedRuntime
		if r.record.ChildSessionPath != "" {
			detached = detachedRuntime{
				child:       r.child,
				cancel:      r.cancel,
				unsubscribe: r.unsubscribe,
				workerStop:  r.workerStop,
				workerDone:  r.workerDone,
			}
			r.child = nil
			r.cancel = nil
			r.unsubscribe = nil
			r.workerStarted = false
			r.workerStop = nil
			r.workerDone = nil
		}
		r.mu.Unlock()
		closeDetachedRuntimes([]detachedRuntime{detached})
		return previous, nil
	}
	if !previous.Terminal() && previous != protocol.AgentNotLoaded {
		r.mu.Unlock()
		return previous, fmt.Errorf("subagents: agent %s is not terminal", ref.Path)
	}
	busy := r.finalizing || r.cancel != nil || r.followupQueued || len(r.tasks) != 0 || (r.child != nil && r.child.IsRunning())
	if busy {
		r.mu.Unlock()
		return previous, fmt.Errorf("subagents: agent %s still has active work", ref.Path)
	}
	state := r.state.Clone()
	state.Status = protocol.AgentClosed
	expected := r.state.Generation
	state.Generation = expected + 1
	record := r.record
	record.State = *state
	if err := m.persistCAS(record, expected); err != nil {
		r.mu.Unlock()
		return previous, fmt.Errorf("subagents: close %s: %w", ref.Path, err)
	}
	r.state = *state.Clone()
	r.record = record

	var detached detachedRuntime
	if record.ChildSessionPath != "" {
		detached = detachedRuntime{
			child:       r.child,
			cancel:      r.cancel,
			unsubscribe: r.unsubscribe,
			workerStop:  r.workerStop,
			workerDone:  r.workerDone,
		}
		r.child = nil
		r.cancel = nil
		r.unsubscribe = nil
		r.workerStarted = false
		r.workerStop = nil
		r.workerDone = nil
	}
	r.mu.Unlock()

	closeDetachedRuntimes([]detachedRuntime{detached})
	m.emitTerminalStatus(r, state)
	return previous, nil
}

// ResumeAgent reopens a closed identity without starting a turn. The caller
// may then send mail, or use Followup to enqueue work. Followup performs this
// admission automatically for closed agents.
func (m *Manager) ResumeAgent(ctx context.Context, caller Caller, target string) (protocol.SubagentState, error) {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.SubagentState{}, err
	}
	r, ref, err := m.resolveTarget(caller, target)
	if err != nil {
		return protocol.SubagentState{}, err
	}
	if ref.Path == protocol.RootAgentPath || ref.Path == caller.Path {
		return protocol.SubagentState{}, errors.New("subagents: cannot resume root or self")
	}
	return m.resumeRuntime(r)
}

func (m *Manager) resumeRuntime(r *runtime) (protocol.SubagentState, error) {
	current := r.snapshot()
	if current.Status != protocol.AgentClosed {
		return *current, nil
	}

	m.mu.Lock()
	if err := m.requireReadyLocked(); err != nil {
		m.mu.Unlock()
		return protocol.SubagentState{}, err
	}
	if m.openAgentCountLocked()+len(m.reserved) >= m.limits.MaxAgentsPerSession {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: open-agent limit reached; close a terminal agent or raise max_agents_per_session")
	}
	r.mu.Lock()
	if r.state.Status != protocol.AgentClosed {
		state := *r.state.Clone()
		r.mu.Unlock()
		m.mu.Unlock()
		return state, nil
	}
	state := r.state.Clone()
	state.Status = protocol.AgentNotLoaded
	expected := r.state.Generation
	state.Generation = expected + 1
	record := r.record
	record.State = *state
	if err := m.persistCAS(record, expected); err != nil {
		r.mu.Unlock()
		m.mu.Unlock()
		return protocol.SubagentState{}, fmt.Errorf("subagents: resume %s: %w", state.Agent.Path, err)
	}
	r.state = *state.Clone()
	r.record = record
	r.mu.Unlock()
	m.mu.Unlock()
	m.emit(protocol.AgentEvent{Type: protocol.EvSubagentStatus, Agent: state.Agent.Clone(), Subagent: state.Clone()})
	m.signalActivity()
	return *state, nil
}

func (m *Manager) recloseAfterFailedFollowup(r *runtime) error {
	r.mu.Lock()
	if r.state.Status == protocol.AgentClosed {
		r.mu.Unlock()
		return nil
	}
	busy := r.finalizing || r.cancel != nil || r.followupQueued || len(r.tasks) != 0 || (r.child != nil && r.child.IsRunning())
	if busy || (!r.state.Status.Terminal() && r.state.Status != protocol.AgentNotLoaded) {
		status := r.state.Status
		r.mu.Unlock()
		return fmt.Errorf("subagents: cannot restore closed state from %s", status)
	}
	state := r.state.Clone()
	state.Status = protocol.AgentClosed
	expected := r.state.Generation
	state.Generation = expected + 1
	record := r.record
	record.State = *state
	if err := m.persistCAS(record, expected); err != nil {
		r.mu.Unlock()
		return err
	}
	r.state = *state.Clone()
	r.record = record
	var detached detachedRuntime
	if record.ChildSessionPath != "" {
		detached = detachedRuntime{
			child:       r.child,
			cancel:      r.cancel,
			unsubscribe: r.unsubscribe,
			workerStop:  r.workerStop,
			workerDone:  r.workerDone,
		}
		r.child = nil
		r.cancel = nil
		r.unsubscribe = nil
		r.workerStarted = false
		r.workerStop = nil
		r.workerDone = nil
	}
	r.mu.Unlock()
	closeDetachedRuntimes([]detachedRuntime{detached})
	m.emitTerminalStatus(r, state)
	return nil
}
