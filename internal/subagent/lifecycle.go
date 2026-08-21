package subagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (f ChildFactoryFunc) NewChild(ctx context.Context, spec ChildSpec) (ChildRuntime, error) {
	return f(ctx, spec)
}

func New(ctx context.Context, limits Limits) *Manager {
	if ctx == nil {
		ctx = context.Background()
	}
	root, cancel := context.WithCancel(ctx)
	if limits.MaxConcurrentThreads < 1 {
		limits.MaxConcurrentThreads = 4
	}
	if limits.MaxLoadedChildren < 1 {
		limits.MaxLoadedChildren = max(1, limits.MaxConcurrentThreads)
	}
	if limits.MaxAgentsPerSession < 1 {
		limits.MaxAgentsPerSession = 32
	}
	if limits.MaxDepth < 1 {
		limits.MaxDepth = 1
	}
	if limits.MinWait <= 0 {
		limits.MinWait = 10 * time.Second
	}
	if limits.DefaultWait < limits.MinWait {
		limits.DefaultWait = 30 * time.Second
	}
	if limits.MaxWait < limits.DefaultWait {
		limits.MaxWait = time.Hour
	}
	if limits.TaskTimeout <= 0 {
		limits.TaskTimeout = 30 * time.Minute
	}
	if limits.MaxResultBytes <= 0 {
		limits.MaxResultBytes = 64 * 1024
	}
	if limits.DefaultRole == "" {
		limits.DefaultRole = "general"
	}
	if limits.Roles == nil {
		limits.Roles = map[string]Role{"general": {Name: "general"}, "explorer": {Name: "explorer"}, "implementer": {Name: "implementer"}}
	}
	return &Manager{ctx: root, cancel: cancel, byID: map[string]*runtime{}, byPath: map[protocol.AgentPath]*runtime{}, reserved: map[protocol.AgentPath]struct{}{}, waitCursor: map[string]uint64{}, limits: limits,
		slots: make(chan struct{}, max(0, limits.MaxConcurrentThreads)), activity: make(chan struct{}), closeDone: make(chan struct{})}
}

func resolveRole(roles map[string]Role, defaultRole, requested string) (string, Role, bool) {
	name := strings.ToLower(strings.TrimSpace(requested))
	if name == "" {
		name = defaultRole
	}
	role, ok := roles[name]
	return name, role, ok
}

func availableRoleError(roles map[string]Role, defaultRole, requested string) error {
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	available := strings.Join(names, ", ")
	return fmt.Errorf("subagents: unknown role %q (available: %s; omit role to use %q)", strings.TrimSpace(requested), available, defaultRole)
}

func validatePersistedRecord(rec session.SubagentRecord) error {
	if err := rec.State.Validate(); err != nil {
		return fmt.Errorf("subagents: invalid persisted state: %w", err)
	}
	if rec.ParentBranchID == "" {
		return errors.New("subagents: persisted child has no parent branch")
	}
	if len(rec.RoleFingerprint) > 128 {
		return errors.New("subagents: persisted role fingerprint is too large")
	}
	return nil
}

// Bind is single-use. Restored topology remains unloaded until Ready.
func (m *Manager) Bind(root *agent.Agent, factory ChildFactory, publish func(protocol.AgentEvent), store session.SubagentTaskStore) error {
	if root == nil || factory == nil {
		return errors.New("subagents: root and factory are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.root != nil {
		return errors.New("subagents: already bound")
	}
	rootID := "root"
	if store != nil {
		rootID = "root-" + storeID(store)
	}
	rootRef := protocol.AgentRef{ThreadID: rootID, Path: protocol.RootAgentPath, Depth: 0, Role: "root"}
	byID := make(map[string]*runtime)
	byPath := make(map[protocol.AgentPath]*runtime)
	var order []string
	var deleteIDs []string
	if store != nil {
		records, err := store.ListSubagents()
		if err != nil {
			return err
		}
		if len(records) > maxStoredAgentIdentities {
			return errors.New("subagents: persisted identity safety limit exceeded")
		}
		openRecords := 0
		for _, rec := range records {
			if err := validatePersistedRecord(rec); err != nil {
				return err
			}
			if rec.State.Agent.Path == protocol.RootAgentPath {
				return errors.New("subagents: persisted child cannot be root")
			}
			if _, exists := byID[rec.State.Agent.ThreadID]; exists {
				return errors.New("subagents: duplicate persisted thread id")
			}
			if _, exists := byPath[rec.State.Agent.Path]; exists {
				return errors.New("subagents: duplicate persisted agent path")
			}
			if rec.ChildSessionPath == "" {
				deleteIDs = append(deleteIDs, rec.State.Agent.ThreadID)
				continue
			}
			copy := rec
			if !validChildLocator(store, copy.ChildSessionPath, copy.State.Agent.ThreadID) {
				copy.State.Status = protocol.AgentErrored
				copy.State.Error = "invalid child session locator"
			} else {
				copy.State.Status = normalizeRestoredStatus(copy.State.Status)
			}
			if copy.State.Status != rec.State.Status || copy.State.Error != rec.State.Error {
				expectedGeneration := rec.State.Generation
				copy.State.Generation++
				if err := store.CompareAndSwapSubagent(rec.State.Agent.ThreadID, expectedGeneration, copy); err != nil {
					return fmt.Errorf("subagents: reconcile %s: %w", rec.State.Agent.Path, err)
				}
			}
			r := &runtime{state: *copy.State.Clone(), record: copy, tasks: make(chan childTask, 64), lastUsed: time.Now()}
			byID[copy.State.Agent.ThreadID] = r
			byPath[copy.State.Agent.Path] = r
			order = append(order, copy.State.Agent.ThreadID)
			if copy.State.Status != protocol.AgentClosed {
				openRecords++
			}
		}
		if openRecords > m.limits.MaxAgentsPerSession {
			return errors.New("subagents: persisted open-agent limit exceeded")
		}
		for _, id := range deleteIDs {
			_ = store.DeleteSubagent(id)
		}
	}
	m.root, m.factory, m.publish, m.store = root, factory, publish, store
	m.rootRef = rootRef
	m.byID, m.byPath, m.order = byID, byPath, order
	return nil
}

func discardChildLocator(store session.SubagentTaskStore, child, threadID string) {
	if !validChildLocator(store, child, threadID) {
		return
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(child + suffix)
	}
}

func validChildLocator(store session.SubagentTaskStore, child string, threadID ...string) bool {
	if child == "" {
		return true
	}
	root, ok := store.(interface{ Path() string })
	if !ok || root.Path() == "" {
		return false
	}
	base, err := filepath.Abs(root.Path() + ".agents")
	if err != nil {
		return false
	}
	candidate, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	baseInfo, err := os.Lstat(base)
	if err != nil || baseInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	childInfo, err := os.Lstat(candidate)
	if err != nil || childInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Ext(candidate) != ".db" || filepath.Dir(rel) != "." {
		return false
	}
	return len(threadID) == 0 || filepath.Base(candidate) == threadID[0]+".db"
}

func storeID(store session.SubagentTaskStore) string {
	if s, ok := store.(interface{ ID() string }); ok {
		return s.ID()
	}
	return "session"
}

func normalizeRestoredStatus(s protocol.AgentStatus) protocol.AgentStatus {
	if s == protocol.AgentClosed {
		return protocol.AgentClosed
	}
	if s == protocol.AgentRunning || s == protocol.AgentQueued || s == protocol.AgentPendingInit {
		return protocol.AgentInterrupted
	}
	return protocol.AgentNotLoaded
}

func (m *Manager) storedIdentityLimitLocked() int {
	if m.limits.Durable || m.limits.MaxAgentsPerSession >= maxStoredAgentIdentities/2 {
		return maxStoredAgentIdentities
	}
	return max(1, m.limits.MaxAgentsPerSession*2)
}

func (m *Manager) openAgentCountLocked() int {
	open := 0
	for _, r := range m.byID {
		if r.snapshot().Status != protocol.AgentClosed {
			open++
		}
	}
	return open
}

// SetStore rebinds the manager during an App session switch and restores
// topology metadata from the new root store. Completed children belong to the
// old root session, so their in-memory runtimes are detached before rebinding;
// active work is still rejected by the caller and this method.
func (m *Manager) SetStore(store session.SubagentTaskStore) error {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	return m.setStore(store)
}

// SetStoreAdmitted is used by App session transactions that already hold the
// root admission mutex.
func (m *Manager) SetStoreAdmitted(store session.SubagentTaskStore) error {
	return m.setStore(store)
}

func runtimeHasActiveWorkLocked(r *runtime) bool {
	if r == nil {
		return false
	}
	switch r.state.Status {
	case protocol.AgentPendingInit, protocol.AgentQueued, protocol.AgentRunning:
		return true
	}
	return r.finalizing || r.cancel != nil || r.followupQueued || len(r.tasks) != 0 || (r.child != nil && r.child.IsRunning())
}

func (m *Manager) ensureIdleTreeLocked() error {
	if len(m.reserved) != 0 {
		return errors.New("subagents: cannot switch session while subagents are being created")
	}
	for _, r := range m.byID {
		r.mu.Lock()
		active := runtimeHasActiveWorkLocked(r)
		r.mu.Unlock()
		if active {
			return errors.New("subagents: cannot switch session while subagents are active")
		}
	}
	return nil
}

// detachIdleTreeLocked marks and removes terminal runtimes while m.mu is held.
// The actual worker joins and child closes happen after releasing m.mu so a
// child shutdown cannot block manager observers. The root admission lock held
// by SetStore prevents new spawns/followups while this transition completes.
func (m *Manager) detachIdleTreeLocked() ([]detachedRuntime, error) {
	if err := m.ensureIdleTreeLocked(); err != nil {
		return nil, err
	}
	detached := make([]detachedRuntime, 0, len(m.byID))
	for _, r := range m.byID {
		r.mu.Lock()
		r.closed = true
		detached = append(detached, detachedRuntime{
			child:       r.child,
			cancel:      r.cancel,
			unsubscribe: r.unsubscribe,
			workerStop:  r.workerStop,
			workerDone:  r.workerDone,
		})
		r.mu.Unlock()
	}
	m.byID = make(map[string]*runtime)
	m.byPath = make(map[protocol.AgentPath]*runtime)
	m.order = nil
	return detached, nil
}

func closeDetachedRuntimes(runtimes []detachedRuntime) {
	for _, detached := range runtimes {
		if detached.cancel != nil {
			detached.cancel()
		}
		if detached.workerStop != nil {
			close(detached.workerStop)
		}
		if detached.workerDone != nil {
			<-detached.workerDone
		}
		if detached.unsubscribe != nil {
			detached.unsubscribe()
		}
		if detached.child != nil {
			detached.child.Close()
		}
	}
}

func (m *Manager) setStore(store session.SubagentTaskStore) error {
	if store == nil {
		return errors.New("subagents: topology store required")
	}
	records, err := store.ListSubagents()
	if err != nil {
		return err
	}
	if len(records) > maxStoredAgentIdentities {
		return errors.New("subagents: persisted identity safety limit exceeded")
	}
	openRecords := 0
	for _, rec := range records {
		if rec.State.Status != protocol.AgentClosed {
			openRecords++
		}
	}
	if openRecords > m.limits.MaxAgentsPerSession {
		return errors.New("subagents: persisted open-agent limit exceeded")
	}
	// Check before reconciling the target store so an attempted switch cannot
	// mutate target topology while an old child is still running.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if err := m.ensureIdleTreeLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()

	byID := make(map[string]*runtime)
	byPath := make(map[protocol.AgentPath]*runtime)
	var order []string
	var states []protocol.SubagentState
	var deleteIDs []string
	normalizedOpenRecords := 0
	for _, rec := range records {
		if err := validatePersistedRecord(rec); err != nil {
			return err
		}
		if rec.State.Agent.Path == protocol.RootAgentPath {
			return errors.New("subagents: persisted child cannot be root")
		}
		if _, exists := byID[rec.State.Agent.ThreadID]; exists {
			return errors.New("subagents: duplicate persisted thread id")
		}
		if _, exists := byPath[rec.State.Agent.Path]; exists {
			return errors.New("subagents: duplicate persisted agent path")
		}
		if rec.ChildSessionPath == "" {
			deleteIDs = append(deleteIDs, rec.State.Agent.ThreadID)
			continue
		}
		copy := rec
		if !validChildLocator(store, copy.ChildSessionPath, copy.State.Agent.ThreadID) {
			copy.State.Status = protocol.AgentErrored
			copy.State.Error = "invalid child session locator"
		} else {
			copy.State.Status = normalizeRestoredStatus(copy.State.Status)
		}
		if copy.State.Status != rec.State.Status || copy.State.Error != rec.State.Error {
			expectedGeneration := rec.State.Generation
			copy.State.Generation++
			if err := store.CompareAndSwapSubagent(rec.State.Agent.ThreadID, expectedGeneration, copy); err != nil {
				return fmt.Errorf("subagents: reconcile %s: %w", rec.State.Agent.Path, err)
			}
		}
		r := &runtime{state: *copy.State.Clone(), record: copy, tasks: make(chan childTask, 64), lastUsed: time.Now()}
		byID[copy.State.Agent.ThreadID] = r
		byPath[copy.State.Agent.Path] = r
		order = append(order, copy.State.Agent.ThreadID)
		states = append(states, copy.State)
		if copy.State.Status != protocol.AgentClosed {
			normalizedOpenRecords++
		}
	}
	if normalizedOpenRecords > m.limits.MaxAgentsPerSession {
		return errors.New("subagents: persisted open-agent limit exceeded")
	}
	for _, id := range deleteIDs {
		_ = store.DeleteSubagent(id)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	detached, err := m.detachIdleTreeLocked()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	ready := m.ready
	m.mu.Unlock()
	closeDetachedRuntimes(detached)

	m.mu.Lock()
	m.store = store
	m.rootRef.ThreadID = "root-" + storeID(store)
	m.byID, m.byPath, m.order = byID, byPath, order
	m.mu.Unlock()
	if ready {
		for i := range states {
			m.emit(protocol.AgentEvent{Type: protocol.EvSubagentStatus, Agent: states[i].Agent.Clone(), Subagent: states[i].Clone()})
		}
	}
	return nil
}

// Ready publishes restored snapshots but never restarts stale work.
func (m *Manager) Ready(context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.root == nil {
		m.mu.Unlock()
		return ErrNotReady
	}
	if m.ready {
		m.mu.Unlock()
		return nil
	}
	m.ready = true
	states := make([]protocol.SubagentState, 0, len(m.order))
	for _, id := range m.order {
		states = append(states, *m.byID[id].state.Clone())
	}
	m.mu.Unlock()
	for i := range states {
		m.emit(protocol.AgentEvent{Type: protocol.EvSubagentStatus, Agent: states[i].Agent.Clone(), Subagent: states[i].Clone()})
	}
	return nil
}

func (m *Manager) requireReadyLocked() error {
	if m.closed {
		return ErrClosed
	}
	if !m.ready || m.root == nil || m.factory == nil {
		return ErrNotReady
	}
	return nil
}

// SetModelCatalog provides a secret-free live provider/model catalog to the
// manager-bound discovery tool.
func (m *Manager) SetModelCatalog(catalog func() []protocol.Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelCatalog = catalog
}

// SetModelSelection validates and resolves provider/model pairs before a child
// identity is committed. The callback must not call back into Manager.
func (m *Manager) SetModelSelection(resolve func(provider, model string) (protocol.Model, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelSelection = resolve
}

func (m *Manager) Models() []protocol.Model {
	m.mu.RLock()
	catalog := m.modelCatalog
	m.mu.RUnlock()
	if catalog == nil {
		return nil
	}
	return catalog()
}

func (m *Manager) RootCaller() Caller {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Caller{ThreadID: m.rootRef.ThreadID, Path: protocol.RootAgentPath}
}

// lockRootAdmission serializes manager operations that touch the active root
// branch with Agent.SelectBranch/SetSession. It is deliberately held only for
// admission/targeting; child turns continue independently under m.ctx.
func (m *Manager) lockRootAdmission() func() {
	m.mu.RLock()
	root := m.root
	m.mu.RUnlock()
	if root == nil {
		return func() {}
	}
	return root.LockAdmission()
}

func (m *Manager) Spawn(ctx context.Context, caller Caller, req protocol.SpawnSubagentRequest) (protocol.SubagentState, error) {
	unlockRoot := m.lockRootAdmission()
	defer unlockRoot()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return protocol.SubagentState{}, err
	}
	if strings.TrimSpace(req.Task) == "" || len(req.Task) > protocol.MaxAgentMessageBytes {
		return protocol.SubagentState{}, errors.New("subagents: initial task is empty or too large")
	}
	path, err := protocol.ResolveAgentPath(caller.Path, req.Name)
	if err != nil {
		return protocol.SubagentState{}, err
	}
	fork, err := ParseForkTurns(req.ForkTurns)
	if err != nil {
		return protocol.SubagentState{}, err
	}
	m.mu.Lock()
	if err = m.requireReadyLocked(); err != nil {
		m.mu.Unlock()
		return protocol.SubagentState{}, err
	}
	if _, ok := m.byPath[path]; ok {
		m.mu.Unlock()
		return protocol.SubagentState{}, fmt.Errorf("subagents: path %s already exists", path)
	}
	if _, ok := m.reserved[path]; ok {
		m.mu.Unlock()
		return protocol.SubagentState{}, fmt.Errorf("subagents: path %s already exists", path)
	}
	if m.limits.MaxConcurrentThreads < 1 {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: no child execution capacity configured")
	}
	if len(m.byID)+len(m.reserved) >= m.storedIdentityLimitLocked() {
		m.mu.Unlock()
		if m.limits.Durable {
			return protocol.SubagentState{}, errors.New("subagents: stored identity safety limit reached")
		}
		return protocol.SubagentState{}, errors.New("subagents: non-durable stored identity limit reached; reuse a closed agent or enable durable subagents")
	}
	if m.openAgentCountLocked()+len(m.reserved) >= m.limits.MaxAgentsPerSession {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: open-agent limit reached; close a terminal agent or raise max_agents_per_session")
	}
	if path.Depth() > m.limits.MaxDepth {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: max depth reached")
	}
	if caller.Path != protocol.RootAgentPath && (!m.limits.Recursive || caller.Path.Depth() >= m.limits.MaxDepth) {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: recursive spawning is disabled")
	}
	parentRef, ok := m.refForCallerLocked(caller)
	if !ok {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: invalid caller")
	}
	roleName, role, ok := resolveRole(m.limits.Roles, m.limits.DefaultRole, req.Role)
	if !ok {
		m.mu.Unlock()
		return protocol.SubagentState{}, availableRoleError(m.limits.Roles, m.limits.DefaultRole, req.Role)
	}
	if caller.Path == protocol.RootAgentPath && m.root.Mode() == protocol.ModePlan && (roleName != "explorer" || role.AllowMutation) {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: Plan mode permits only read-only explorer children")
	}
	thinkingExplicit := req.ReasoningEffort != ""
	thinking := protocol.NormalizeThinkingLevel(req.ReasoningEffort)
	if req.ReasoningEffort == "" {
		thinkingExplicit = role.Thinking != nil
		if role.Thinking != nil {
			thinking = protocol.NormalizeThinkingLevel(*role.Thinking)
		} else if parentRef.Path == protocol.RootAgentPath {
			thinking = m.root.Thinking()
		}
	}
	model, provider := req.Model, req.Provider
	if provider == "" && role.Provider != "" {
		provider = role.Provider
	}
	if provider == "" && m.limits.DefaultProvider != "" {
		provider = m.limits.DefaultProvider
	}
	if model == "" && role.Model != "" {
		model = role.Model
	}
	if model == "" && m.limits.DefaultModel != "" {
		model = m.limits.DefaultModel
	}
	if parentRef.Path == protocol.RootAgentPath {
		pm := m.root.Model()
		if provider == "" {
			provider = pm.Provider
		}
		if model == "" && provider == pm.Provider {
			model = pm.ID
		}
		if req.ReasoningEffort == "" && role.Thinking == nil {
			thinking = m.root.Thinking()
		}
	} else if parent := m.byPath[caller.Path]; parent != nil {
		parentState := parent.snapshot()
		if provider == "" {
			provider = parentState.Provider
		}
		if model == "" && provider == parentState.Provider {
			model = parentState.Model
		}
		if req.ReasoningEffort == "" && role.Thinking == nil {
			thinking = parentState.Thinking
		}
	}
	if resolve := m.modelSelection; resolve != nil {
		resolved, resolveErr := resolve(provider, model)
		if resolveErr != nil {
			m.mu.Unlock()
			return protocol.SubagentState{}, resolveErr
		}
		provider, model = resolved.Provider, resolved.ID
		if !resolved.SupportsThinkingLevel(thinking) {
			if thinkingExplicit {
				m.mu.Unlock()
				return protocol.SubagentState{}, fmt.Errorf("subagents: model %s/%s does not support explicitly requested reasoning effort %q (supported: %v)", provider, model, thinking, resolved.SupportedThinkingLevels())
			}
			thinking = protocol.NormalizeThinkingLevel(resolved.DefaultThinking)
			if !resolved.SupportsThinkingLevel(thinking) {
				thinking = protocol.ThinkingOff
			}
		}
	}
	id := newThreadID()
	now := time.Now().UnixMilli()
	state := protocol.SubagentState{Agent: protocol.AgentRef{ThreadID: id, ParentThreadID: parentRef.ThreadID, Path: path, ParentPath: caller.Path, Role: roleName, Depth: path.Depth()}, Status: protocol.AgentPendingInit, Model: model, Provider: provider, Thinking: thinking, CreatedAt: now, Generation: 1}
	record := session.SubagentRecord{State: state, ParentBranchID: m.activeBranchLocked(), ChildSessionPath: m.childPathLocked(id), RoleFingerprint: roleFingerprint(role)}
	topologyStore := m.store
	r := &runtime{state: state, record: record, tasks: make(chan childTask, 64), lastUsed: time.Now()}
	m.reserved[path] = struct{}{}
	if record.ChildSessionPath != "" {
		if info, statErr := os.Lstat(record.ChildSessionPath); statErr == nil || (info != nil && info.Mode()&os.ModeSymlink != 0) {
			delete(m.reserved, path)
			m.mu.Unlock()
			return protocol.SubagentState{}, errors.New("subagents: child session path already exists")
		}
	}
	m.mu.Unlock()
	rollback := func(e error) (protocol.SubagentState, error) {
		m.mu.Lock()
		delete(m.reserved, path)
		m.mu.Unlock()
		discardChildLocator(topologyStore, record.ChildSessionPath, id)
		return protocol.SubagentState{}, e
	}
	var parentMessages []protocol.Message
	if fork != 0 {
		parentMessages, err = m.messagesFor(caller)
		if err != nil {
			return rollback(err)
		}
	}
	child, err := m.factory.NewChild(ctx, ChildSpec{State: state, Role: role, ForkTurns: req.ForkTurns, ParentMessages: parentMessages, SessionPath: record.ChildSessionPath})
	if err != nil {
		return rollback(err)
	}
	if err := ctx.Err(); err != nil {
		child.Close()
		return rollback(err)
	}
	m.mu.Lock()
	if m.closed {
		delete(m.reserved, path)
		m.mu.Unlock()
		child.Close()
		discardChildLocator(topologyStore, record.ChildSessionPath, id)
		return protocol.SubagentState{}, ErrClosed
	}
	if m.store != nil && record.ChildSessionPath != "" {
		if err := m.store.PutSubagent(record); err != nil {
			delete(m.reserved, path)
			m.mu.Unlock()
			child.Close()
			discardChildLocator(topologyStore, record.ChildSessionPath, id)
			return protocol.SubagentState{}, err
		}
	}
	workerStop := make(chan struct{})
	workerDone := make(chan struct{})
	r.mu.Lock()
	r.child = child
	r.unsubscribe = child.Subscribe(func(ev protocol.AgentEvent) { m.forward(r, ev) })
	r.workerStarted = true
	r.workerStop = workerStop
	r.workerDone = workerDone
	r.mu.Unlock()
	delete(m.reserved, path)
	m.byID[id] = r
	m.byPath[path] = r
	m.order = append(m.order, id)
	m.wg.Add(1)
	m.mu.Unlock()
	go m.worker(r, workerStop, workerDone)
	m.emit(protocol.AgentEvent{Type: protocol.EvSubagentStarted, Agent: state.Agent.Clone(), Subagent: r.snapshot()})
	m.setStatus(r, protocol.AgentQueued, "", "")
	r.tasks <- childTask{message: req.Task, initial: true}
	return *r.snapshot(), nil
}

func (m *Manager) SendMessage(ctx context.Context, caller Caller, target, message string) error {
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
	if t != nil && t.snapshot().Status == protocol.AgentClosed {
		return fmt.Errorf("subagents: agent %s is closed; resume it before sending a message", ref.Path)
	}
	env := protocol.AgentMessage{ID: newThreadID(), Author: caller.Path, Recipient: ref.Path, Kind: protocol.AgentMessageNormal, Content: message, CreatedAt: time.Now().UnixMilli()}
	if err := m.enqueueTarget(t, ref, env); err != nil {
		return err
	}
	m.bumpActivity(env)
	return nil
}
