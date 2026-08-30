package subagent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *Manager) Followup(ctx context.Context, caller Caller, target, message string) error {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" || len(message) > protocol.MaxAgentMessageBytes {
		return errors.New("subagents: message is empty or too large")
	}
	t, ref, err := m.resolveTarget(caller, target)
	if err != nil {
		return err
	}
	if ref.Path == protocol.RootAgentPath {
		return errors.New("subagents: root cannot receive followup_task")
	}
	reopened := false
	if t.snapshot().Status == protocol.AgentClosed {
		if _, err := m.resumeRuntime(t); err != nil {
			return err
		}
		reopened = true
	}
	rollback := func(cause error) error {
		if !reopened {
			return cause
		}
		if err := m.recloseAfterFailedFollowup(t); err != nil {
			return fmt.Errorf("%w (also failed to restore closed state: %v)", cause, err)
		}
		return cause
	}
	child := runtimeChild(t)
	if child == nil {
		if err := m.loadRuntime(t); err != nil {
			return rollback(err)
		}
		child = runtimeChild(t)
		if child == nil {
			return rollback(errors.New("subagents: child runtime unavailable"))
		}
	}
	wasRunning := child.IsRunning()
	env := protocol.AgentMessage{ID: newThreadID(), Author: caller.Path, Recipient: ref.Path, Kind: protocol.AgentMessageNewTask, Content: message, TriggerTurn: true, CreatedAt: time.Now().UnixMilli()}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return rollback(ErrClosed)
	}
	if err := ctx.Err(); err != nil {
		t.mu.Unlock()
		return rollback(err)
	}
	if !t.followupQueued && len(t.tasks) >= cap(t.tasks) {
		t.mu.Unlock()
		return rollback(errors.New("subagents: followup queue full"))
	}
	t.mu.Unlock()
	// Mailbox persistence may perform SQLite I/O. Root admission keeps this
	// runtime attached while avoiding a long hold of the runtime mutex.
	if err := child.EnqueueMailbox(env); err != nil {
		return rollback(err)
	}
	t.mu.Lock()
	if !t.followupQueued {
		t.tasks <- childTask{onlyIfPending: wasRunning, followup: true}
		t.followupQueued = true
	}
	t.mu.Unlock()
	m.setQueuedIfIdle(t)
	m.bumpActivity(env)
	return nil
}

// WaitAll blocks a host one-shot surface until every committed child turn is
// terminal. It does not consume mailbox content or start new work.
func (m *Manager) WaitAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		m.mu.RLock()
		if err := m.requireReadyLocked(); err != nil {
			m.mu.RUnlock()
			return err
		}
		active := false
		for _, r := range m.byID {
			status := r.snapshot().Status
			if status == protocol.AgentPendingInit || status == protocol.AgentQueued || status == protocol.AgentRunning || runtimeFinalizing(r) {
				active = true
				break
			}
		}
		ch := m.activity
		m.mu.RUnlock()
		if !active {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.ctx.Done():
			return ErrClosed
		case <-ch:
		}
	}
}

func (m *Manager) waitResult(caller Caller, message string, timedOut, clamped bool) protocol.WaitSubagentsResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	running, queued, terminal := 0, 0, 0
	activeBranch := m.activeBranchLocked()
	callerPrefix := string(caller.Path) + "/"
	for _, id := range m.order {
		r := m.byID[id]
		if branch := runtimeParentBranch(r); branch != "" && branch != activeBranch {
			continue
		}
		s := r.snapshot()
		if caller.Path != protocol.RootAgentPath && !strings.HasPrefix(string(s.Agent.Path), callerPrefix) {
			continue
		}
		if runtimeFinalizing(r) {
			running++
			continue
		}
		switch s.Status {
		case protocol.AgentRunning:
			running++
		case protocol.AgentPendingInit, protocol.AgentQueued:
			queued++
		default:
			terminal++
		}
	}
	return protocol.WaitSubagentsResult{
		Message: message, TimedOut: timedOut, Clamped: clamped,
		Running: running, Queued: queued, Terminal: terminal,
		AllTerminal: running == 0 && queued == 0,
	}
}

func normalizeWaitTimeout(timeout, minWait, defaultWait, maxWait time.Duration) (time.Duration, bool, error) {
	clamped := false
	if timeout <= 0 {
		timeout = defaultWait
	} else if timeout < minWait {
		timeout = minWait
		clamped = true
	}
	if timeout > maxWait {
		return 0, false, errors.New("subagents: wait timeout exceeds maximum")
	}
	return timeout, clamped, nil
}

func (m *Manager) Wait(ctx context.Context, caller Caller, timeout time.Duration) (protocol.WaitSubagentsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	if err := m.requireReadyLocked(); err != nil {
		m.mu.RUnlock()
		return protocol.WaitSubagentsResult{}, err
	}
	if caller.Path.Validate() != nil {
		m.mu.RUnlock()
		return protocol.WaitSubagentsResult{}, errors.New("subagents: invalid caller")
	}
	if _, ok := m.refForCallerLocked(caller); !ok {
		m.mu.RUnlock()
		return protocol.WaitSubagentsResult{}, errors.New("subagents: invalid caller identity")
	}
	start := m.generation
	ch := m.activity
	key := caller.ThreadID
	if key == "" {
		key = string(caller.Path)
	}
	cursor := m.waitCursor[key]
	minWait, def, maxWait := m.limits.MinWait, m.limits.DefaultWait, m.limits.MaxWait
	m.mu.RUnlock()
	if m.pendingFor(caller) && start > cursor {
		m.mu.Lock()
		m.waitCursor[key] = start
		m.mu.Unlock()
		return m.waitResult(caller, "Wait completed.", false, false), nil
	}
	timeout, clamped, err := normalizeWaitTimeout(timeout, minWait, def, maxWait)
	if err != nil {
		return protocol.WaitSubagentsResult{}, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return protocol.WaitSubagentsResult{}, ctx.Err()
	case <-m.ctx.Done():
		return protocol.WaitSubagentsResult{}, ErrClosed
	case <-timer.C:
		return m.waitResult(caller, "Wait completed.", true, clamped), nil
	case <-ch:
		m.mu.Lock()
		m.waitCursor[key] = m.generation
		m.mu.Unlock()
		return m.waitResult(caller, "Wait completed.", false, clamped), nil
	}
}

// WaitUntilAll blocks until every committed descendant of caller is terminal
// or the bounded wait expires. Unlike WaitAll it is safe for recursive child
// callers because the caller's own running turn is excluded from the scope.
func (m *Manager) WaitUntilAll(ctx context.Context, caller Caller, timeout time.Duration) (protocol.WaitSubagentsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	if err := m.requireReadyLocked(); err != nil {
		m.mu.RUnlock()
		return protocol.WaitSubagentsResult{}, err
	}
	if caller.Path.Validate() != nil {
		m.mu.RUnlock()
		return protocol.WaitSubagentsResult{}, errors.New("subagents: invalid caller")
	}
	if _, ok := m.refForCallerLocked(caller); !ok {
		m.mu.RUnlock()
		return protocol.WaitSubagentsResult{}, errors.New("subagents: invalid caller identity")
	}
	minWait, def, maxWait := m.limits.MinWait, m.limits.DefaultWait, m.limits.MaxWait
	m.mu.RUnlock()

	timeout, clamped, err := normalizeWaitTimeout(timeout, minWait, def, maxWait)
	if err != nil {
		return protocol.WaitSubagentsResult{}, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		// Capture the activity generation before observing state so a completion
		// between the state check and select cannot be missed.
		m.mu.RLock()
		ch := m.activity
		m.mu.RUnlock()
		result := m.waitResult(caller, "All agents completed.", false, clamped)
		if result.AllTerminal {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return protocol.WaitSubagentsResult{}, ctx.Err()
		case <-m.ctx.Done():
			return protocol.WaitSubagentsResult{}, ErrClosed
		case <-timer.C:
			return m.waitResult(caller, "Wait timed out before all agents completed.", true, clamped), nil
		case <-ch:
		}
	}
}

func (m *Manager) Interrupt(ctx context.Context, caller Caller, target string) (protocol.AgentStatus, error) {
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
		return protocol.AgentNotFound, errors.New("subagents: cannot interrupt root or self")
	}
	prev := r.snapshot().Status
	r.mu.Lock()
	interrupting := prev == protocol.AgentQueued || prev == protocol.AgentRunning
	if prev == protocol.AgentQueued {
		r.skipQueued = true
	}
	if interrupting {
		r.interruptRequested = true
	}
	cancel := r.cancel
	child := r.child
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if child != nil && child.IsRunning() {
		if err := child.AbortContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return prev, err
		}
	}
	if r.snapshot().Status == protocol.AgentQueued {
		m.setStatus(r, protocol.AgentInterrupted, "", "")
	}
	return prev, nil
}

func (m *Manager) List(_ context.Context, caller Caller, prefix string) (protocol.SubagentList, error) {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.requireReadyLocked(); err != nil {
		return protocol.SubagentList{}, err
	}
	if caller.Path.Validate() != nil {
		return protocol.SubagentList{}, errors.New("subagents: invalid caller")
	}
	if _, ok := m.refForCallerLocked(caller); !ok {
		return protocol.SubagentList{}, errors.New("subagents: invalid caller identity")
	}
	p := protocol.RootAgentPath
	if strings.TrimSpace(prefix) != "" {
		var err error
		p, err = protocol.ResolveAgentPath(caller.Path, prefix)
		if err != nil {
			return protocol.SubagentList{}, err
		}
	} else if caller.Path != protocol.RootAgentPath {
		p = caller.Path
	}
	includeRoot := p == protocol.RootAgentPath
	out := make([]protocol.SubagentState, 0, len(m.order)+1)
	if includeRoot {
		rootStatus := protocol.AgentCompleted
		if m.root.IsRunning() {
			rootStatus = protocol.AgentRunning
		}
		out = append(out, protocol.SubagentState{Agent: m.rootRef, Status: rootStatus, Model: m.root.Model().ID, Provider: m.root.Model().Provider, Thinking: m.root.Thinking()})
	}
	result := protocol.SubagentList{Open: m.openAgentCountLocked(), ConcurrentLimit: m.limits.MaxConcurrentThreads, AgentLimit: m.limits.MaxAgentsPerSession}
	activeBranch := m.activeBranchLocked()
	for _, id := range m.order {
		r := m.byID[id]
		if branch := runtimeParentBranch(r); branch != "" && branch != activeBranch {
			continue
		}
		s := r.snapshot()
		path := string(s.Agent.Path)
		prefix := string(p)
		if path != prefix && !strings.HasPrefix(path, prefix+"/") {
			continue
		}
		switch s.Status {
		case protocol.AgentRunning:
			result.Running++
		case protocol.AgentPendingInit, protocol.AgentQueued:
			result.Queued++
		case protocol.AgentClosed:
			result.Closed++
			result.Terminal++
		default:
			result.Terminal++
		}
		if len(out) < maxListAgents {
			out = append(out, *s)
		} else {
			result.Truncated = true
		}
	}
	result.Agents = out
	return result, nil
}

func (m *Manager) Messages(_ context.Context, target string) ([]protocol.Message, error) {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	m.mu.RLock()
	if err := m.requireReadyLocked(); err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	r := m.lookupLocked(target, protocol.RootAgentPath)
	m.mu.RUnlock()
	if r == nil {
		return nil, session.ErrNotFound
	}
	child := runtimeChild(r)
	if child == nil {
		if err := m.loadRuntime(r); err != nil {
			return nil, err
		}
		child = runtimeChild(r)
	}
	if child == nil {
		return nil, session.ErrNotFound
	}
	return child.Messages()
}

func (m *Manager) Get(_ context.Context, target string) (protocol.SubagentState, error) {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := m.requireReadyLocked(); err != nil {
		return protocol.SubagentState{}, err
	}
	if target == string(protocol.RootAgentPath) || target == m.rootRef.ThreadID {
		status := protocol.AgentCompleted
		if m.root != nil && m.root.IsRunning() {
			status = protocol.AgentRunning
		}
		return protocol.SubagentState{Agent: m.rootRef, Status: status, Model: m.root.Model().ID, Provider: m.root.Model().Provider, Thinking: m.root.Thinking()}, nil
	}
	if r := m.lookupLocked(target, protocol.RootAgentPath); r != nil {
		return *r.snapshot(), nil
	}
	return protocol.SubagentState{Status: protocol.AgentNotFound}, session.ErrNotFound
}

func (m *Manager) HasAgents() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byID) != 0 || len(m.reserved) != 0
}

func (m *Manager) HasActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.reserved) != 0 {
		return true
	}
	for _, r := range m.byID {
		r.mu.Lock()
		active := runtimeHasActiveWorkLocked(r)
		r.mu.Unlock()
		if active {
			return true
		}
	}
	return false
}

func (m *Manager) Usage() (protocol.Usage, error) {
	m.mu.RLock()
	ids := slices.Clone(m.order)
	m.mu.RUnlock()
	var total protocol.Usage
	for _, id := range ids {
		m.mu.RLock()
		r := m.byID[id]
		m.mu.RUnlock()
		if r != nil {
			if u := r.snapshot().Usage; u != nil {
				total = total.Add(*u)
			}
		}
	}
	return total, nil
}

func (m *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	alreadyClosed := m.closed
	closeDone := m.closeDone
	m.mu.RUnlock()
	if alreadyClosed {
		select {
		case <-closeDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	unlockRoot := m.lockRootAdmission()
	m.mu.Lock()
	if m.closed {
		done := m.closeDone
		m.mu.Unlock()
		unlockRoot()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.closed = true
	m.cancel()
	rs := make([]*runtime, 0, len(m.byID))
	for _, r := range m.byID {
		rs = append(rs, r)
	}
	m.signalActivityLocked()
	m.mu.Unlock()
	unlockRoot()
	for _, r := range rs {
		r.mu.Lock()
		r.closed = true
		if r.cancel != nil {
			r.cancel()
		}
		child := r.child
		r.mu.Unlock()
		if status := r.snapshot().Status; !status.Terminal() {
			m.setStatus(r, protocol.AgentShutdown, "", "")
		}
		if child != nil {
			_ = child.AbortContext(ctx)
		}
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	cleanup := func() {
		for _, r := range rs {
			r.mu.Lock()
			unsubscribe, child := r.unsubscribe, r.child
			r.unsubscribe = nil
			r.child = nil
			r.mu.Unlock()
			if unsubscribe != nil {
				unsubscribe()
			}
			if child != nil {
				child.Close()
			}
		}
		close(m.closeDone)
	}
	select {
	case <-done:
		cleanup()
		return nil
	case <-ctx.Done():
		go func() {
			<-done
			cleanup()
		}()
		return ctx.Err()
	}
}

func (m *Manager) worker(r *runtime, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-stop:
			return
		case <-m.ctx.Done():
			m.setStatus(r, protocol.AgentShutdown, "", "")
			return
		case task := <-r.tasks:
			r.mu.Lock()
			if task.followup {
				r.followupQueued = false
			}
			child := r.child
			skip := r.skipQueued
			if skip {
				r.skipQueued = false
			}
			r.mu.Unlock()
			if skip {
				r.mu.Lock()
				r.interruptRequested = false
				r.mu.Unlock()
				continue
			}
			if child == nil {
				continue
			}
			if task.onlyIfPending && !child.PendingMailbox() {
				continue
			}
			select {
			case m.slots <- struct{}{}:
			case <-m.ctx.Done():
				return
			}
			turnCtx, cancel := context.WithTimeout(m.ctx, m.limits.TaskTimeout)
			r.mu.Lock()
			r.cancel = cancel
			child = r.child
			skip = r.skipQueued
			if skip {
				r.skipQueued = false
			}
			r.mu.Unlock()
			if skip {
				cancel()
				<-m.slots
				r.mu.Lock()
				r.interruptRequested = false
				r.mu.Unlock()
				continue
			}
			m.setStatus(r, protocol.AgentRunning, "", "")
			r.mu.Lock()
			skip = r.skipQueued
			if skip {
				r.skipQueued = false
			}
			r.mu.Unlock()
			if skip {
				cancel()
				<-m.slots
				r.mu.Lock()
				r.cancel = nil
				r.interruptRequested = false
				r.mu.Unlock()
				continue
			}
			var err error
			if task.initial {
				err = child.Prompt(turnCtx, task.message)
			} else {
				err = child.RunMailbox(turnCtx)
			}
			cancel()
			<-m.slots
			r.mu.Lock()
			interrupted := r.interruptRequested
			r.interruptRequested = false
			r.cancel = nil
			r.mu.Unlock()
			result := m.finalResult(r)
			usage, _ := child.Usage()
			status := protocol.AgentCompleted
			errText := ""
			if interrupted {
				status = protocol.AgentInterrupted
			} else if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					status = protocol.AgentInterrupted
				} else {
					status = protocol.AgentErrored
					errText = bound(err.Error(), m.limits.MaxResultBytes)
				}
			}
			terminal, record := m.prepareTerminal(r, status, result, errText, &usage)
			if terminal.Status != protocol.AgentShutdown {
				if deliveryErr := m.deliverFinal(terminal, result, errText); deliveryErr != nil {
					terminal.Status = protocol.AgentErrored
					terminal.Error = bound("deliver final result: "+deliveryErr.Error(), m.limits.MaxResultBytes)
					record.State = *terminal
				}
			}
			terminal, _, persistErr := m.commitTerminal(r, terminal, record)
			if persistErr != nil {
				m.emit(protocol.AgentEvent{Type: protocol.EvError, Agent: terminal.Agent.Clone(), Message: terminal.Error})
			}
			m.emitTerminalStatus(r, terminal)
			m.scheduleEviction()
		}
	}
}

func (m *Manager) scheduleEviction() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.evictionScheduled {
		m.evictionRequested = true
		m.mu.Unlock()
		return
	}
	m.evictionScheduled = true
	m.wg.Go(func() {
		for {
			m.evictIdle()
			m.mu.Lock()
			if !m.evictionRequested || m.closed {
				m.evictionScheduled = false
				m.evictionRequested = false
				m.mu.Unlock()
				return
			}
			m.evictionRequested = false
			m.mu.Unlock()
		}
	})
	m.mu.Unlock()
}
