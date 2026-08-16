// Package subagent orchestrates independent agent.Agent runtimes. It owns
// identity, topology, limits, mailboxes, lifecycle and shutdown; reasoning and
// tools remain in the ordinary agent loop.
package subagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

var (
	ErrNotReady = errors.New("subagents: not ready")
	ErrClosed   = errors.New("subagents: closed")
)

const maxListAgents = 256

// rolePolicyFingerprintVersion changes whenever the built-in capability policy
// changes. Persisted children from an older policy must fail safe rather than
// silently receiving newly available tools during lazy restore.
const rolePolicyFingerprintVersion = "role-tools-v2-shell"

type Caller struct {
	ThreadID string
	Path     protocol.AgentPath
}

type Role struct {
	Name          string
	Description   string
	System        string
	Provider      string
	Model         string
	Thinking      *protocol.ThinkingLevel
	Tools         []string
	AllowMutation bool
}

type Limits struct {
	// MaxConcurrentThreads is retained for config compatibility and counts
	// concurrently executing children only; the root does not consume a slot.
	MaxConcurrentThreads  int
	MaxLoadedChildren     int
	MaxAgentsPerSession   int
	MaxDepth              int
	MinWait               time.Duration
	DefaultWait           time.Duration
	MaxWait               time.Duration
	TaskTimeout           time.Duration
	MaxResultBytes        int
	Recursive             bool
	Durable               bool
	AllowMutation         bool
	ExposeChildToolEvents bool
	DefaultProvider       string
	DefaultModel          string
	DefaultRole           string
	Roles                 map[string]Role
}

type ChildSpec struct {
	State          protocol.SubagentState
	Role           Role
	ForkTurns      string
	ParentMessages []protocol.Message
	SessionPath    string
	Restore        bool
}

type ChildRuntime interface {
	Prompt(context.Context, string) error
	RunMailbox(context.Context) error
	EnqueueMailbox(protocol.AgentMessage) error
	PendingMailbox() bool
	AbortContext(context.Context) error
	IsRunning() bool
	Messages() ([]protocol.Message, error)
	ContextMessages() ([]protocol.Message, error)
	Usage() (protocol.Usage, error)
	Subscribe(func(protocol.AgentEvent)) func()
	Close()
}

type ChildFactory interface {
	NewChild(context.Context, ChildSpec) (ChildRuntime, error)
}
type ChildFactoryFunc func(context.Context, ChildSpec) (ChildRuntime, error)

func (f ChildFactoryFunc) NewChild(ctx context.Context, spec ChildSpec) (ChildRuntime, error) {
	return f(ctx, spec)
}

type runtime struct {
	mu                        sync.Mutex
	state                     protocol.SubagentState
	record                    session.SubagentRecord
	child                     ChildRuntime
	tasks                     chan childTask
	cancel                    context.CancelFunc
	skipQueued                bool
	unsubscribe               func()
	closed                    bool
	followupQueued            bool
	interruptRequested        bool
	terminalEmittedGeneration uint64
	workerStarted             bool
	workerStop                chan struct{}
	workerDone                chan struct{}
	finalizing                bool
	lastUsed                  time.Time
}
type childTask struct {
	message       string
	initial       bool
	onlyIfPending bool
	followup      bool
}

type Manager struct {
	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.RWMutex
	byID              map[string]*runtime
	byPath            map[protocol.AgentPath]*runtime
	reserved          map[protocol.AgentPath]struct{}
	order             []string
	root              *agent.Agent
	rootRef           protocol.AgentRef
	factory           ChildFactory
	store             session.SubagentTaskStore
	publish           func(protocol.AgentEvent)
	limits            Limits
	slots             chan struct{}
	activity          chan struct{}
	generation        uint64
	waitCursor        map[string]uint64
	ready             bool
	closed            bool
	evictionScheduled bool
	evictionRequested bool
	closeDone         chan struct{}
	wg                sync.WaitGroup
	modelCatalog      func() []protocol.Model
	modelSelection    func(provider, model string) (protocol.Model, error)
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
		if len(records) > m.limits.MaxAgentsPerSession {
			return errors.New("subagents: persisted agent limit exceeded")
		}
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
	if s == protocol.AgentRunning || s == protocol.AgentQueued || s == protocol.AgentPendingInit {
		return protocol.AgentInterrupted
	}
	return protocol.AgentNotLoaded
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

type detachedRuntime struct {
	child       ChildRuntime
	cancel      context.CancelFunc
	unsubscribe func()
	workerStop  chan struct{}
	workerDone  chan struct{}
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
	if len(records) > m.limits.MaxAgentsPerSession {
		return errors.New("subagents: persisted agent limit exceeded")
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
	if len(m.byID)+len(m.reserved) >= m.limits.MaxAgentsPerSession {
		m.mu.Unlock()
		return protocol.SubagentState{}, errors.New("subagents: agent limit reached")
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
	env := protocol.AgentMessage{ID: newThreadID(), Author: caller.Path, Recipient: ref.Path, Kind: protocol.AgentMessageNormal, Content: message, CreatedAt: time.Now().UnixMilli()}
	if err := m.enqueueTarget(t, ref, env); err != nil {
		return err
	}
	m.bumpActivity(env)
	return nil
}
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
	child := runtimeChild(t)
	if child == nil {
		if err := m.loadRuntime(t); err != nil {
			return err
		}
		child = runtimeChild(t)
		if child == nil {
			return errors.New("subagents: child runtime unavailable")
		}
	}
	wasRunning := child.IsRunning()
	env := protocol.AgentMessage{ID: newThreadID(), Author: caller.Path, Recipient: ref.Path, Kind: protocol.AgentMessageNewTask, Content: message, TriggerTurn: true, CreatedAt: time.Now().UnixMilli()}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		t.mu.Unlock()
		return err
	}
	if !t.followupQueued && len(t.tasks) >= cap(t.tasks) {
		t.mu.Unlock()
		return errors.New("subagents: followup queue full")
	}
	t.mu.Unlock()
	// Mailbox persistence may perform SQLite I/O. Root admission keeps this
	// runtime attached while avoiding a long hold of the runtime mutex.
	if err := child.EnqueueMailbox(env); err != nil {
		return err
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
	result := protocol.SubagentList{ConcurrentLimit: m.limits.MaxConcurrentThreads, AgentLimit: m.limits.MaxAgentsPerSession}
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
	ids := append([]string(nil), m.order...)
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
	defer func() {
		close(done)
		m.wg.Done()
	}()
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
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
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
	}()
}

func (m *Manager) evictIdle() {
	if !m.limits.Durable || m.limits.MaxLoadedChildren < 1 {
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
			if r.child != nil {
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
			m.setStatus(r, protocol.AgentNotLoaded, "", "")
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
	if state.Status.Terminal() {
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
		return to == protocol.AgentQueued || to == protocol.AgentRunning || to == protocol.AgentErrored || to == protocol.AgentShutdown || to == protocol.AgentNotLoaded
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

// Compile-time assertion for the concrete child used by App.
var _ ChildRuntime = (*agent.Agent)(nil)
