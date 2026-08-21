package subagent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Manager) evictIdle() {
	if m.limits.MaxLoadedChildren < 1 {
		return
	}
	for {
		unlockRoot := m.lockRootAdmission()
		m.mu.RLock()
		if m.closed {
			m.mu.RUnlock()
			unlockRoot()
			return
		}
		candidates := make([]*runtime, 0, len(m.order))
		loaded := 0
		for _, id := range m.order {
			r := m.byID[id]
			r.mu.Lock()
			if r.child != nil && r.record.ChildSessionPath != "" {
				loaded++
				candidates = append(candidates, r)
			}
			r.mu.Unlock()
		}
		m.mu.RUnlock()
		if loaded <= m.limits.MaxLoadedChildren {
			unlockRoot()
			return
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			candidates[i].mu.Lock()
			left := candidates[i].lastUsed
			candidates[i].mu.Unlock()
			candidates[j].mu.Lock()
			right := candidates[j].lastUsed
			candidates[j].mu.Unlock()
			return left.Before(right)
		})
		evicted := false
		for _, r := range candidates {
			r.mu.Lock()
			child := r.child
			status := r.state.Status
			busy := r.cancel != nil || r.followupQueued || r.finalizing || len(r.tasks) != 0
			if child == nil || busy || status == protocol.AgentPendingInit || status == protocol.AgentQueued || status == protocol.AgentRunning {
				r.mu.Unlock()
				continue
			}
			if child.IsRunning() || child.PendingMailbox() {
				r.mu.Unlock()
				continue
			}
			r.mu.Unlock()
			r.mu.Lock()
			if r.child != child || r.cancel != nil || r.followupQueued || len(r.tasks) != 0 || r.state.Status != status {
				r.mu.Unlock()
				continue
			}
			unsubscribe := r.unsubscribe
			workerStop, workerDone := r.workerStop, r.workerDone
			r.child = nil
			r.unsubscribe = nil
			r.workerStarted = false
			r.workerStop = nil
			r.workerDone = nil
			r.mu.Unlock()
			if status != protocol.AgentClosed {
				m.setStatus(r, protocol.AgentNotLoaded, "", "")
			}
			unlockRoot()
			if workerStop != nil {
				close(workerStop)
				if workerDone != nil {
					<-workerDone
				}
			}
			if unsubscribe != nil {
				unsubscribe()
			}
			child.Close()
			evicted = true
			break
		}
		if !evicted {
			unlockRoot()
			return
		}
	}
}

func (m *Manager) finalResult(r *runtime) string {
	child := runtimeChild(r)
	if child == nil {
		return ""
	}
	msgs, err := child.Messages()
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != protocol.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, c := range msgs[i].Content {
			if c.Type == protocol.BlockText || c.Type == protocol.BlockPlan {
				b.WriteString(c.Text)
			}
		}
		if strings.TrimSpace(b.String()) != "" {
			return bound(b.String(), m.limits.MaxResultBytes)
		}
	}
	return ""
}

func (m *Manager) deliverFinal(s *protocol.SubagentState, result, errText string) error {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	payload := result
	if payload == "" {
		payload = errText
	}
	if payload == "" {
		payload = "(no final response)"
	}
	env := protocol.AgentMessage{ID: newThreadID(), Author: s.Agent.Path, Recipient: s.Agent.ParentPath, Kind: protocol.AgentMessageFinal, Content: bound(payload, m.limits.MaxResultBytes), CreatedAt: time.Now().UnixMilli()}
	t, ref, err := m.resolveTarget(Caller{ThreadID: s.Agent.ThreadID, Path: s.Agent.Path}, string(s.Agent.ParentPath))
	if err != nil {
		return err
	}
	if err := m.enqueueTarget(t, ref, env); err != nil {
		return err
	}
	m.bumpActivity(env)
	return nil
}

func (m *Manager) prepareTerminal(r *runtime, status protocol.AgentStatus, result, errText string, usage *protocol.Usage) (*protocol.SubagentState, session.SubagentRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status.Terminal() {
		return r.state.Clone(), r.record
	}
	if r.closed || m.ctx.Err() != nil {
		status = protocol.AgentShutdown
	}
	if !validTransition(r.state.Status, status) {
		status = r.state.Status
	}
	r.finalizing = true
	state := r.state.Clone()
	state.Status = status
	if result != "" {
		state.Result = bound(result, m.limits.MaxResultBytes)
	}
	if errText != "" {
		state.Error = bound(errText, m.limits.MaxResultBytes)
	}
	state.Usage = usage.Clone()
	state.FinishedAt = time.Now().UnixMilli()
	state.Generation++
	rec := r.record
	rec.State = *state
	return state, rec
}

func (m *Manager) commitTerminal(r *runtime, state *protocol.SubagentState, rec session.SubagentRecord) (*protocol.SubagentState, session.SubagentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status == protocol.AgentShutdown {
		r.finalizing = false
		return r.state.Clone(), r.record, nil
	}
	if r.state.Status.Terminal() && !r.finalizing {
		return r.state.Clone(), r.record, nil
	}
	if r.closed || m.ctx.Err() != nil {
		state.Status = protocol.AgentShutdown
	}
	if !validTransition(r.state.Status, state.Status) {
		state.Status = r.state.Status
	}
	expected := r.state.Generation
	state.Generation = expected + 1
	rec.State = *state
	persistErr := m.persistCAS(rec, expected)
	if persistErr != nil {
		state.Status = protocol.AgentErrored
		state.Error = bound("persist terminal state: "+persistErr.Error(), m.limits.MaxResultBytes)
		rec.State = *state
	}
	r.state = *state.Clone()
	r.record = rec
	r.finalizing = false
	return state, rec, persistErr
}

func (m *Manager) setStatus(r *runtime, status protocol.AgentStatus, result, errText string) {
	r.mu.Lock()
	if r.closed && status != protocol.AgentShutdown {
		r.mu.Unlock()
		return
	}
	if r.finalizing && status != protocol.AgentShutdown {
		r.mu.Unlock()
		return
	}
	if status == protocol.AgentRunning && r.skipQueued {
		r.mu.Unlock()
		return
	}
	if !validTransition(r.state.Status, status) {
		r.mu.Unlock()
		return
	}
	r.state.Status = status
	if result != "" {
		r.state.Result = result
	}
	if errText != "" {
		r.state.Error = bound(errText, m.limits.MaxResultBytes)
	}
	if status == protocol.AgentRunning && r.state.StartedAt == 0 {
		r.state.StartedAt = time.Now().UnixMilli()
	}
	expected := r.state.Generation
	r.state.Generation++
	r.record.State = r.state
	persistErr := m.persistCAS(r.record, expected)
	state := *r.state.Clone()
	if persistErr != nil {
		r.state.Status = protocol.AgentErrored
		r.state.Error = bound("persist lifecycle state: "+persistErr.Error(), m.limits.MaxResultBytes)
		r.state.Generation++
		r.record.State = r.state
		state = *r.state.Clone()
	}
	r.mu.Unlock()
	if persistErr != nil {
		m.emit(protocol.AgentEvent{Type: protocol.EvError, Agent: state.Agent.Clone(), Message: state.Error})
	}
	if state.Status.TerminalOutcome() {
		m.emitTerminalStatus(r, &state)
	} else {
		m.emit(protocol.AgentEvent{Type: protocol.EvSubagentStatus, Agent: state.Agent.Clone(), Subagent: state.Clone()})
	}
}

func (m *Manager) emitTerminalStatus(r *runtime, state *protocol.SubagentState) {
	r.mu.Lock()
	if r.terminalEmittedGeneration == state.Generation {
		r.mu.Unlock()
		return
	}
	r.terminalEmittedGeneration = state.Generation
	r.mu.Unlock()
	m.emit(protocol.AgentEvent{Type: protocol.EvSubagentStatus, Agent: state.Agent.Clone(), Subagent: state.Clone()})
	m.signalActivity()
}

func validTransition(from, to protocol.AgentStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case protocol.AgentPendingInit:
		return to == protocol.AgentQueued || to == protocol.AgentRunning || to == protocol.AgentErrored || to == protocol.AgentShutdown
	case protocol.AgentQueued:
		return to == protocol.AgentRunning || to == protocol.AgentInterrupted || to == protocol.AgentShutdown
	case protocol.AgentRunning:
		return to == protocol.AgentCompleted || to == protocol.AgentInterrupted || to == protocol.AgentErrored || to == protocol.AgentShutdown
	case protocol.AgentCompleted, protocol.AgentInterrupted, protocol.AgentErrored, protocol.AgentNotLoaded:
		return to == protocol.AgentQueued || to == protocol.AgentRunning || to == protocol.AgentErrored || to == protocol.AgentShutdown || to == protocol.AgentNotLoaded || to == protocol.AgentClosed
	}
	return false
}

func (m *Manager) persistCAS(rec session.SubagentRecord, expected uint64) error {
	if m.store == nil || rec.ChildSessionPath == "" {
		return nil
	}
	return m.store.CompareAndSwapSubagent(rec.State.Agent.ThreadID, expected, rec)
}

func (m *Manager) setQueuedIfIdle(r *runtime) {
	s := r.snapshot().Status
	if s != protocol.AgentRunning && s != protocol.AgentQueued {
		m.setStatus(r, protocol.AgentQueued, "", "")
	}
}

func (r *runtime) snapshot() *protocol.SubagentState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.Clone()
}

func runtimeChild(r *runtime) ChildRuntime {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.child != nil {
		r.lastUsed = time.Now()
	}
	return r.child
}

func runtimeFinalizing(r *runtime) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finalizing
}

func runtimeParentBranch(r *runtime) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.record.ParentBranchID
}

func (m *Manager) forward(r *runtime, ev protocol.AgentEvent) {
	if ev.Type == protocol.EvSessionUpdated {
		return
	}
	if !m.limits.ExposeChildToolEvents && (ev.Type == protocol.EvTextDelta || ev.Type == protocol.EvThinkingDelta || ev.Type == protocol.EvToolStart || ev.Type == protocol.EvToolProgress || ev.Type == protocol.EvToolEnd) {
		return
	}
	s := r.snapshot()
	ev.Agent = s.Agent.Clone()
	m.emit(ev)
}

func (m *Manager) emit(ev protocol.AgentEvent) {
	m.mu.RLock()
	fn := m.publish
	m.mu.RUnlock()
	if fn != nil {
		fn(ev.Clone())
	}
}

func (m *Manager) signalActivity() { m.mu.Lock(); m.signalActivityLocked(); m.mu.Unlock() }

func (m *Manager) signalActivityLocked() {
	m.generation++
	close(m.activity)
	m.activity = make(chan struct{})
}

func (m *Manager) bumpActivity(env protocol.AgentMessage) {
	m.emit(protocol.AgentEvent{Type: protocol.EvSubagentMessage, AgentMessage: env.Clone()})
	m.signalActivity()
}

func (m *Manager) resolveTarget(caller Caller, target string) (*runtime, protocol.AgentRef, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.requireReadyLocked(); err != nil {
		return nil, protocol.AgentRef{}, err
	}
	if caller.Path.Validate() != nil {
		return nil, protocol.AgentRef{}, errors.New("subagents: invalid caller")
	}
	if _, ok := m.refForCallerLocked(caller); !ok {
		return nil, protocol.AgentRef{}, errors.New("subagents: invalid caller identity")
	}
	if target == m.rootRef.ThreadID || target == string(protocol.RootAgentPath) {
		return nil, m.rootRef, nil
	}
	p, err := protocol.ResolveAgentPath(caller.Path, target)
	if err == nil {
		if r := m.byPath[p]; r != nil {
			if branch := runtimeParentBranch(r); branch != "" && branch != m.activeBranchLocked() {
				return nil, protocol.AgentRef{}, session.ErrNotFound
			}
			return r, r.snapshot().Agent, nil
		}
	}
	if r := m.byID[target]; r != nil {
		if branch := runtimeParentBranch(r); branch != "" && branch != m.activeBranchLocked() {
			return nil, protocol.AgentRef{}, session.ErrNotFound
		}
		return r, r.snapshot().Agent, nil
	}
	return nil, protocol.AgentRef{}, session.ErrNotFound
}

func (m *Manager) lookupLocked(target string, base protocol.AgentPath) *runtime {
	var r *runtime
	if byID := m.byID[target]; byID != nil {
		r = byID
	} else if p, err := protocol.ResolveAgentPath(base, target); err == nil {
		r = m.byPath[p]
	}
	if r != nil {
		branch := runtimeParentBranch(r)
		if branch != "" && branch != m.activeBranchLocked() {
			return nil
		}
	}
	return r
}

func (m *Manager) refForCallerLocked(c Caller) (protocol.AgentRef, bool) {
	if c.Path == protocol.RootAgentPath && (c.ThreadID == "" || c.ThreadID == m.rootRef.ThreadID) {
		return m.rootRef, true
	}
	if r := m.byPath[c.Path]; r != nil {
		if branch := runtimeParentBranch(r); branch != "" && branch != m.activeBranchLocked() {
			return protocol.AgentRef{}, false
		}
		state := r.snapshot()
		if c.ThreadID != "" && c.ThreadID == state.Agent.ThreadID {
			return state.Agent, true
		}
	}
	return protocol.AgentRef{}, false
}

func (m *Manager) enqueueTarget(r *runtime, ref protocol.AgentRef, env protocol.AgentMessage) error {
	if ref.Path == protocol.RootAgentPath {
		return m.root.EnqueueMailboxAdmitted(env)
	}
	if r == nil {
		return session.ErrNotFound
	}
	child := runtimeChild(r)
	if child == nil {
		if err := m.loadRuntime(r); err != nil {
			return err
		}
		child = runtimeChild(r)
	}
	if child == nil {
		return session.ErrNotFound
	}
	return child.EnqueueMailbox(env)
}

func (m *Manager) pendingFor(c Caller) bool {
	if c.Path == protocol.RootAgentPath {
		return m.root.PendingMailbox()
	}
	m.mu.RLock()
	r := m.byPath[c.Path]
	m.mu.RUnlock()
	child := runtimeChild(r)
	return child != nil && child.PendingMailbox()
}

func (m *Manager) messagesFor(c Caller) ([]protocol.Message, error) {
	if c.Path == protocol.RootAgentPath {
		return m.root.ContextMessagesAdmitted()
	}
	m.mu.RLock()
	r := m.byPath[c.Path]
	m.mu.RUnlock()
	child := runtimeChild(r)
	if child == nil {
		return nil, session.ErrNotFound
	}
	return child.ContextMessages()
}

func (m *Manager) activeBranchLocked() string {
	if m.store != nil {
		return m.store.ActiveBranchID()
	}
	return "main"
}

func (m *Manager) childPathLocked(id string) string {
	if !m.limits.Durable || m.store == nil {
		return ""
	}
	if root, ok := m.store.(interface{ Path() string }); ok && root.Path() != "" {
		return root.Path() + ".agents/" + id + ".db"
	}
	return ""
}

func (m *Manager) loadRuntime(r *runtime) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if r == nil {
		m.mu.Unlock()
		return session.ErrNotFound
	}
	r.mu.Lock()
	if r.child != nil {
		r.mu.Unlock()
		m.mu.Unlock()
		return nil
	}
	role, ok := m.limits.Roles[r.state.Agent.Role]
	legacyRole := r.record.RoleFingerprint == ""
	if !ok && !legacyRole {
		r.mu.Unlock()
		m.mu.Unlock()
		err := fmt.Errorf("subagents: persisted role %q is unavailable", r.state.Agent.Role)
		m.setStatus(r, protocol.AgentErrored, "", err.Error())
		return err
	}
	if legacyRole {
		// v5 records predate role fingerprints. Restore them with a conservative
		// read-only policy rather than adopting a possibly changed mutation role.
		role.Name = r.state.Agent.Role
		role = conservativeRestoreRole(role)
	} else if r.record.RoleFingerprint != roleFingerprint(role) {
		r.mu.Unlock()
		m.mu.Unlock()
		err := fmt.Errorf("subagents: persisted role %q changed and cannot be restored safely", r.state.Agent.Role)
		m.setStatus(r, protocol.AgentErrored, "", err.Error())
		return err
	}
	spec := ChildSpec{State: r.state, Role: role, SessionPath: r.record.ChildSessionPath, Restore: true}
	factory := m.factory
	m.wg.Add(1) // Close waits for a load already admitted under m.mu.
	r.mu.Unlock()
	m.mu.Unlock()
	defer m.wg.Done()
	child, err := factory.NewChild(m.ctx, spec)
	if err != nil {
		m.setStatus(r, protocol.AgentErrored, "", bound(err.Error(), m.limits.MaxResultBytes))
		return err
	}
	m.mu.Lock()
	r.mu.Lock()
	if m.closed || r.closed {
		r.mu.Unlock()
		m.mu.Unlock()
		child.Close()
		return ErrClosed
	}
	if r.child != nil {
		r.mu.Unlock()
		m.mu.Unlock()
		child.Close()
		return nil
	}
	startWorker := !r.workerStarted
	workerStop := r.workerStop
	workerDone := r.workerDone
	if startWorker {
		workerStop = make(chan struct{})
		workerDone = make(chan struct{})
	}
	r.child = child
	r.lastUsed = time.Now()
	r.unsubscribe = child.Subscribe(func(ev protocol.AgentEvent) { m.forward(r, ev) })
	if startWorker {
		r.workerStarted = true
		r.workerStop = workerStop
		r.workerDone = workerDone
		m.wg.Add(1)
	}
	r.mu.Unlock()
	m.mu.Unlock()
	if startWorker {
		go m.worker(r, workerStop, workerDone)
	}
	return nil
}

func conservativeRestoreRole(role Role) Role {
	role.AllowMutation = false
	if len(role.Tools) != 0 {
		allowed := map[string]struct{}{
			"read": {}, "grep": {}, "glob": {},
			"activate_skill": {}, "read_skill_resource": {},
		}
		filtered := make([]string, 0, len(role.Tools))
		for _, name := range role.Tools {
			if _, ok := allowed[name]; ok {
				filtered = append(filtered, name)
			}
		}
		role.Tools = filtered
	}
	return role
}

func roleFingerprint(role Role) string {
	tools := append([]string(nil), role.Tools...)
	sort.Strings(tools)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00", rolePolicyFingerprintVersion, role.Name, role.Description, role.System, role.Provider, role.Model, role.AllowMutation)
	if role.Thinking != nil {
		fmt.Fprintf(h, "%s", *role.Thinking)
	}
	for _, tool := range tools {
		fmt.Fprintf(h, "\x00%s", tool)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func newThreadID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func bound(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.ValidString(s[:end]) {
		end--
	}
	return s[:end]
}
