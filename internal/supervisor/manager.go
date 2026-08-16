package supervisor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxObservedPermissionArgs  = 16 * 1024
	maxObservedPermissionPaths = 64
)

// Manager owns attached worker processes for one controlling Snow runtime.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   Options

	mu        sync.RWMutex
	workers   map[WorkerID]*managedWorker
	closing   bool
	eventMu   sync.Mutex
	events    chan Event
	closeOnce sync.Once
}

type managedWorker struct {
	state  WorkerState
	client *rpcClient
}

// New creates an idle supervisor. It never starts a process until Start is
// called by the human-controlled surface.
func New(ctx context.Context, opts Options) (*Manager, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.MaxConcurrent == 0 {
		opts.MaxConcurrent = 4
	}
	if opts.MaxConcurrent < 1 || opts.MaxConcurrent > 8 {
		return nil, errors.New("supervisor: max concurrent workers must be 1..8")
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 5 * time.Second
	}
	if opts.StartupTimeout <= 0 {
		opts.StartupTimeout = 15 * time.Second
	}
	if opts.EventBuffer <= 0 {
		opts.EventBuffer = 512
	}
	if opts.CommandFactory == nil {
		if opts.Executable == "" {
			executable, err := os.Executable()
			if err != nil {
				return nil, fmt.Errorf("supervisor: resolve Snow executable: %w", err)
			}
			opts.Executable = executable
		}
		absolute, err := filepath.Abs(opts.Executable)
		if err != nil {
			return nil, fmt.Errorf("supervisor: resolve Snow executable: %w", err)
		}
		opts.Executable = absolute
	}
	managerCtx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: managerCtx, cancel: cancel, opts: opts, workers: make(map[WorkerID]*managedWorker), events: make(chan Event, opts.EventBuffer)}, nil
}

// Events is a bounded multiplexed stream. Agent deltas may be dropped under a
// stalled consumer; state snapshots and durable transcript hydration remain.
func (m *Manager) Events() <-chan Event { return m.events }

// Start launches and verifies one exact worktree/session. It rejects duplicate
// live ownership before process creation.
func (m *Manager) Start(ctx context.Context, req StartRequest) (WorkerState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.SessionPath == "" || req.WorktreePath == "" || req.SessionID == "" {
		return WorkerState{}, errors.New("supervisor: session ID, session path, and worktree path are required")
	}
	if err := session.ValidateSQLiteSession(req.SessionPath); err != nil {
		return WorkerState{}, fmt.Errorf("supervisor: validate session: %w", err)
	}
	if req.ID == "" {
		req.ID = stableWorkerID(req.WorkspaceID, req.SessionID)
	}
	canonicalWorktree, err := canonicalPath(req.WorktreePath)
	if err != nil {
		return WorkerState{}, fmt.Errorf("supervisor: canonical worktree: %w", err)
	}
	canonicalSession, err := filepath.Abs(req.SessionPath)
	if err != nil {
		return WorkerState{}, fmt.Errorf("supervisor: canonical session: %w", err)
	}
	req.WorktreePath = canonicalWorktree
	req.SessionPath = filepath.Clean(canonicalSession)

	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return WorkerState{}, errors.New("supervisor: closing")
	}
	active := 0
	for id, worker := range m.workers {
		if worker.client != nil && (worker.state.ProcessStatus == ProcessStarting || worker.state.ProcessStatus == ProcessReady || worker.state.ProcessStatus == ProcessStopping) {
			active++
			if id == req.ID {
				m.mu.Unlock()
				return WorkerState{}, errors.New("supervisor: worker is already managed")
			}
			if sameCanonicalPath(worker.state.SessionPath, req.SessionPath) {
				m.mu.Unlock()
				return WorkerState{}, errors.New("supervisor: session already has a managed worker")
			}
			if sameCanonicalPath(worker.state.WorktreePath, req.WorktreePath) {
				m.mu.Unlock()
				return WorkerState{}, errors.New("supervisor: worktree already has a managed worker")
			}
		}
	}
	if active >= m.opts.MaxConcurrent {
		m.mu.Unlock()
		return WorkerState{}, fmt.Errorf("supervisor: concurrent worker limit %d reached", m.opts.MaxConcurrent)
	}
	generation := uint64(1)
	if previous := m.workers[req.ID]; previous != nil {
		generation = previous.state.ProcessGeneration + 1
	}
	state := WorkerState{
		ID: req.ID, WorkspaceID: req.WorkspaceID, SessionID: req.SessionID,
		SessionPath: req.SessionPath, WorktreePath: req.WorktreePath, Branch: req.Branch,
		ProcessGeneration: generation, ProcessStatus: ProcessStarting, TurnStatus: TurnIdle,
		Provider: req.Provider, Model: req.Model, Thinking: req.Thinking, StartedAt: time.Now(),
	}
	worker := &managedWorker{state: state}
	m.workers[req.ID] = worker
	m.mu.Unlock()
	m.publishState(state)

	client, messages, err := startRPCClient(m.ctx, m.opts, req)
	if err != nil {
		m.mu.Lock()
		if current := m.workers[req.ID]; current == worker && current.state.ProcessGeneration == generation {
			current.state.ProcessStatus = ProcessStopped
			current.state.LastError = err.Error()
			state = current.state.clone()
		}
		m.mu.Unlock()
		m.publishState(state)
		return state, err
	}

	m.mu.Lock()
	if m.closing || m.workers[req.ID] != worker || worker.state.ProcessGeneration != generation {
		m.mu.Unlock()
		_ = client.close(m.opts.ShutdownTimeout)
		return WorkerState{}, errors.New("supervisor: stale worker start")
	}
	worker.client = client
	worker.state.ProcessStatus = ProcessReady
	worker.state.Provider = client.info.Provider
	worker.state.Model = client.info.Model
	worker.state.Thinking = client.info.Thinking
	worker.state.Messages = cloneMessages(messages)
	state = worker.state.clone()
	m.mu.Unlock()
	m.publishState(state)
	go m.observe(req.ID, generation, client)
	return state, nil
}

func (m *Manager) observe(id WorkerID, generation uint64, client *rpcClient) {
	for {
		select {
		case event := <-client.events:
			m.applyAgentEvent(id, generation, event)
		case <-client.done:
			m.workerExited(id, generation, client.failureSnapshot())
			return
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) applyAgentEvent(id WorkerID, generation uint64, event protocol.AgentEvent) {
	m.mu.Lock()
	worker := m.workers[id]
	if worker == nil || worker.state.ProcessGeneration != generation {
		m.mu.Unlock()
		return
	}
	switch event.Type {
	case protocol.EvPermissionRequest:
		worker.state.TurnStatus = TurnPermission
		if event.Permission != nil {
			permissionRequest := event.Permission.Request
			permissionRequest.Args = append([]byte(nil), permissionRequest.Args...)
			if len(permissionRequest.Args) > maxObservedPermissionArgs {
				permissionRequest.Args = []byte(`{"_snow_observer_truncated":true}`)
			}
			permissionRequest.Paths = append([]string(nil), permissionRequest.Paths...)
			if len(permissionRequest.Paths) > maxObservedPermissionPaths {
				permissionRequest.Paths = permissionRequest.Paths[:maxObservedPermissionPaths:maxObservedPermissionPaths]
			}
			worker.state.Permission = &permissionRequest
		}
	case protocol.EvUserInputRequest:
		worker.state.TurnStatus = TurnInputNeeded
		if event.UserInput != nil {
			input := event.Clone().UserInput
			worker.state.UserInput = input
		}
	case protocol.EvUsage:
		if event.Usage != nil {
			if worker.state.Usage == nil {
				worker.state.Usage = event.Usage.Clone()
			} else {
				total := worker.state.Usage.Add(*event.Usage)
				worker.state.Usage = &total
			}
		}
	case protocol.EvModelChanged:
		if event.Model != nil {
			worker.state.Provider = event.Model.Provider
			worker.state.Model = event.Model.ID
		}
	case protocol.EvError:
		worker.state.LastError = event.Message
	}
	state := worker.state.clone()
	m.mu.Unlock()
	m.publish(Event{WorkerID: id, Generation: generation, State: &state, Agent: cloneAgentEvent(event)})
}

func (m *Manager) workerExited(id WorkerID, generation uint64, exitErr error) {
	m.mu.Lock()
	worker := m.workers[id]
	if worker == nil || worker.state.ProcessGeneration != generation {
		m.mu.Unlock()
		return
	}
	wasStopping := worker.state.ProcessStatus == ProcessStopping || m.closing
	worker.client = nil
	if wasStopping {
		worker.state.ProcessStatus = ProcessStopped
		worker.state.TurnStatus = TurnIdle
	} else if worker.state.TurnStatus != TurnIdle {
		worker.state.ProcessStatus = ProcessCrashed
		worker.state.TurnStatus = TurnOutcomeUnknown
		worker.state.OutcomeUnknown = true
	} else {
		worker.state.ProcessStatus = ProcessCrashed
	}
	if exitErr != nil && !wasStopping {
		worker.state.LastError = exitErr.Error()
	}
	state := worker.state.clone()
	m.mu.Unlock()
	m.publishState(state)
}

// Prompt starts one worker turn and waits for definitive prompt_completed.
func (m *Manager) Prompt(ctx context.Context, id WorkerID, message string) error {
	if message == "" {
		return errors.New("supervisor: prompt message is required")
	}
	worker, generation, client, err := m.beginTurn(id)
	if err != nil {
		return err
	}
	err = client.prompt(ctx, message)
	refreshCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	messages, refreshErr := client.sessionMessages(refreshCtx)
	cancel()
	m.mu.Lock()
	if current := m.workers[id]; current == worker && current.state.ProcessGeneration == generation {
		current.state.Permission = nil
		current.state.UserInput = nil
		if refreshErr == nil {
			current.state.Messages = cloneMessages(messages)
		}
		if current.state.ProcessStatus == ProcessReady {
			current.state.TurnStatus = TurnIdle
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			current.state.LastError = err.Error()
		}
		state := current.state.clone()
		m.mu.Unlock()
		m.publishState(state)
	} else {
		m.mu.Unlock()
	}
	return err
}

func (m *Manager) beginTurn(id WorkerID) (*managedWorker, uint64, *rpcClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil || worker.state.ProcessStatus != ProcessReady {
		return nil, 0, nil, errors.New("supervisor: worker is not ready")
	}
	if worker.state.TurnStatus != TurnIdle {
		return nil, 0, nil, errors.New("supervisor: worker already has active work")
	}
	worker.state.TurnStatus = TurnWorking
	worker.state.TurnStartedAt = time.Now()
	worker.state.LastError = ""
	state := worker.state.clone()
	go m.publishState(state)
	return worker, worker.state.ProcessGeneration, worker.client, nil
}

func (m *Manager) Steer(ctx context.Context, id WorkerID, message string) error {
	client, err := m.activeClient(id)
	if err != nil {
		return err
	}
	return client.command(ctx, "steer", message, nil)
}

func (m *Manager) FollowUp(ctx context.Context, id WorkerID, message string) error {
	client, err := m.activeClient(id)
	if err != nil {
		return err
	}
	return client.command(ctx, "follow_up", message, nil)
}

func (m *Manager) Abort(ctx context.Context, id WorkerID) error {
	m.mu.Lock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil {
		m.mu.Unlock()
		return errors.New("supervisor: worker is not managed")
	}
	worker.state.TurnStatus = TurnAborting
	state := worker.state.clone()
	client := worker.client
	m.mu.Unlock()
	m.publishState(state)
	return client.command(ctx, "abort", "", nil)
}

func (m *Manager) ReplyPermission(ctx context.Context, id WorkerID, requestID string, decision permission.Decision) error {
	m.mu.RLock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil || worker.state.Permission == nil || worker.state.Permission.ID != requestID {
		m.mu.RUnlock()
		return errors.New("supervisor: permission request is not pending")
	}
	client := worker.client
	m.mu.RUnlock()
	payload := protocol.RPCPermissionReply{RequestID: requestID, Decision: string(decision)}
	if err := client.command(ctx, "permission_reply", "", payload); err != nil {
		return err
	}
	m.clearInteraction(id, requestID, true)
	return nil
}

func (m *Manager) RejectPermission(ctx context.Context, id WorkerID, requestID string) error {
	m.mu.RLock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil || worker.state.Permission == nil || worker.state.Permission.ID != requestID {
		m.mu.RUnlock()
		return errors.New("supervisor: permission request is not pending")
	}
	client := worker.client
	m.mu.RUnlock()
	if err := client.command(ctx, "permission_reject", "", protocol.RPCPermissionReject{RequestID: requestID}); err != nil {
		return err
	}
	m.clearInteraction(id, requestID, true)
	return nil
}

func (m *Manager) ReplyUserInput(ctx context.Context, id WorkerID, response protocol.UserInputResponse) error {
	m.mu.RLock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil || worker.state.UserInput == nil || worker.state.UserInput.ID != response.RequestID {
		m.mu.RUnlock()
		return errors.New("supervisor: user input request is not pending")
	}
	client := worker.client
	m.mu.RUnlock()
	if err := client.command(ctx, "user_input_reply", "", response); err != nil {
		return err
	}
	m.clearInteraction(id, response.RequestID, false)
	return nil
}

func (m *Manager) RejectUserInput(ctx context.Context, id WorkerID, requestID string) error {
	client, err := m.activeClient(id)
	if err != nil {
		return err
	}
	if err := client.command(ctx, "user_input_reject", "", map[string]string{"request_id": requestID}); err != nil {
		return err
	}
	m.clearInteraction(id, requestID, false)
	return nil
}

func (m *Manager) clearInteraction(id WorkerID, requestID string, permissionRequest bool) {
	m.mu.Lock()
	worker := m.workers[id]
	if worker == nil {
		m.mu.Unlock()
		return
	}
	matched := false
	if permissionRequest && worker.state.Permission != nil && worker.state.Permission.ID == requestID {
		worker.state.Permission = nil
		matched = true
		if worker.state.TurnStatus == TurnPermission {
			worker.state.TurnStatus = TurnWorking
		}
	}
	if !permissionRequest && worker.state.UserInput != nil && worker.state.UserInput.ID == requestID {
		worker.state.UserInput = nil
		matched = true
		if worker.state.TurnStatus == TurnInputNeeded {
			worker.state.TurnStatus = TurnWorking
		}
	}
	if !matched {
		m.mu.Unlock()
		return
	}
	state := worker.state.clone()
	m.mu.Unlock()
	m.publishState(state)
}

// Stop gracefully aborts and closes one worker while preserving its worktree,
// branch, and durable session.
func (m *Manager) Stop(ctx context.Context, id WorkerID) error {
	m.mu.Lock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil {
		m.mu.Unlock()
		return errors.New("supervisor: worker is not managed")
	}
	worker.state.ProcessStatus = ProcessStopping
	state := worker.state.clone()
	client := worker.client
	m.mu.Unlock()
	m.publishState(state)
	result := make(chan error, 1)
	go func() { result <- client.close(m.opts.ShutdownTimeout) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) activeClient(id WorkerID) (*rpcClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	worker := m.workers[id]
	if worker == nil || worker.client == nil || worker.state.ProcessStatus != ProcessReady {
		return nil, errors.New("supervisor: worker is not ready")
	}
	if worker.state.TurnStatus == TurnIdle {
		return nil, errors.New("supervisor: worker has no active work")
	}
	return worker.client, nil
}

// State returns an independent worker snapshot.
func (m *Manager) State(id WorkerID) (WorkerState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	worker := m.workers[id]
	if worker == nil {
		return WorkerState{}, false
	}
	return worker.state.clone(), true
}

// List returns stable worker snapshots sorted by workspace and worker ID.
func (m *Manager) List() []WorkerState {
	m.mu.RLock()
	states := make([]WorkerState, 0, len(m.workers))
	for _, worker := range m.workers {
		states = append(states, worker.state.clone())
	}
	m.mu.RUnlock()
	sort.Slice(states, func(i, j int) bool {
		if states[i].WorkspaceID != states[j].WorkspaceID {
			return states[i].WorkspaceID < states[j].WorkspaceID
		}
		return states[i].ID < states[j].ID
	})
	return states
}

// Close stops all managed workers with bounded escalation. It is idempotent.
func (m *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var result error
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closing = true
		workers := make([]*rpcClient, 0, len(m.workers))
		for _, worker := range m.workers {
			if worker.client != nil {
				worker.state.ProcessStatus = ProcessStopping
				workers = append(workers, worker.client)
			}
		}
		m.mu.Unlock()
		var wg sync.WaitGroup
		var closeErrMu sync.Mutex
		var closeErr error
		for _, client := range workers {
			wg.Add(1)
			go func(client *rpcClient) {
				defer wg.Done()
				if err := client.close(m.opts.ShutdownTimeout); err != nil {
					closeErrMu.Lock()
					closeErr = errors.Join(closeErr, err)
					closeErrMu.Unlock()
				}
			}(client)
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
			closeErrMu.Lock()
			result = errors.Join(result, closeErr)
			closeErrMu.Unlock()
		case <-ctx.Done():
			result = ctx.Err()
		}
		m.cancel()
	})
	return result
}

func (m *Manager) publishState(state WorkerState) {
	m.publish(Event{WorkerID: state.ID, Generation: state.ProcessGeneration, State: ptrState(state.clone())})
}

func (m *Manager) publish(event Event) {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	if droppableWorkerEvent(event) {
		// Reserve half the bounded mailbox for interaction and lifecycle state.
		if len(m.events) >= cap(m.events)/2 {
			return
		}
		select {
		case m.events <- event:
		default:
		}
		return
	}
	// Permission/input and lifecycle/control state are not lossy. Backpressure
	// here is preferable to leaving a managed process blocked on an invisible
	// request. Manager cancellation always releases a stalled publisher.
	select {
	case m.events <- event:
	case <-m.ctx.Done():
	}
}

func droppableWorkerEvent(event Event) bool {
	if event.Agent == nil {
		return false
	}
	switch event.Agent.Type {
	case protocol.EvPermissionRequest, protocol.EvUserInputRequest, protocol.EvError:
		return false
	default:
		return true
	}
}

func ptrState(state WorkerState) *WorkerState { return &state }

func cloneAgentEvent(event protocol.AgentEvent) *protocol.AgentEvent {
	cloned := event.Clone()
	return &cloned
}

func cloneMessages(messages []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, len(messages))
	for i, message := range messages {
		out[i] = message.Clone()
	}
	return out
}

func stableWorkerID(workspaceID, sessionID string) WorkerID {
	sum := sha256.Sum256([]byte(workspaceID + "\x00" + sessionID))
	return WorkerID(fmt.Sprintf("worker-%x", sum[:10]))
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(resolved), nil
}
