package web

import (
	"context"
	"crypto/rand"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrRuntimeBusy        = errors.New("web: runtime is busy; no work was queued")
	ErrRuntimeLimit       = errors.New("web: close a live project before opening another (limit two)")
	ErrRuntimeClosed      = errors.New("web: runtime is closed")
	ErrRuntimeUnavailable = errors.New("web: runtime operation failed; close and explicitly reopen the project")
	ErrRuntimeInvalid     = errors.New("web: invalid runtime request")
	ErrRuntimeQueueReview = errors.New("web: discard pending queue review before changing chats")
)

// RuntimePermission deliberately omits raw tool arguments. These public effect
// summaries are untrusted text, not markup or a promise of process containment.
type RuntimePermission struct {
	ID           string                      `json:"id"`
	AgentPath    string                      `json:"agent_path,omitempty"`
	AgentRole    string                      `json:"agent_role,omitempty"`
	Tool         string                      `json:"tool"`
	Risk         string                      `json:"risk"`
	Reason       string                      `json:"reason"`
	ScopeLabel   string                      `json:"scope_label"`
	Paths        []string                    `json:"paths"`
	Capabilities []string                    `json:"capabilities"`
	Effects      []protocol.PermissionEffect `json:"effects"`
	Unknown      bool                        `json:"unknown"`
	Truncated    bool                        `json:"truncated"`
}

// RuntimeSnapshot owns all its slices and pointers. History is a bounded public
// text projection, not the provider transcript. Status is opening, idle, running,
// permission, input, switching, closing, or failed. Closed projects have no snapshot.
// InstanceID is a session-bound nonce, rotated on every committed switch. Every
// targeted mutation must supply it unchanged; never refresh it on behalf of a
// stale request. Only an explicit successful switch returns replacement authority.
// CancelToken is an opaque local prompt-generation nonce, never provider continuity
// data. CancelRequested stays latched until authoritative completion AND release
// of the cancellation control gate. Failed runtimes retain uncertain intent;
// neither an HTTP response nor an abort acknowledgment establishes readiness.
type RuntimeSnapshot struct {
	ReasoningEnabled      bool                       `json:"reasoning_enabled"`
	CompactionEnabled     bool                       `json:"compaction_enabled"`
	Steer                 *RuntimeSteer              `json:"steer"`
	SteerACK              *RuntimeSteerACK           `json:"steer_ack,omitempty"`
	Compaction            *RuntimeCompaction         `json:"compaction,omitempty"`
	CompactionACK         *RuntimeCompactionACK      `json:"compaction_ack,omitempty"`
	GoalRunACK            *RuntimeGoalRunACK         `json:"goal_run_ack,omitempty"`
	Goal                  *RuntimeGoal               `json:"goal"`
	Queue                 *RuntimeQueue              `json:"queue"`
	VersionsEnabled       bool                       `json:"versions_enabled"`
	HistoryControlEnabled bool                       `json:"history_control_enabled"`
	ProjectID             string                     `json:"project_id"`
	InstanceID            string                     `json:"instance_id"`
	SessionID             string                     `json:"session_id"`
	SessionName           string                     `json:"session_name"`
	Mode                  string                     `json:"mode"`
	PermissionMode        string                     `json:"permission_mode"`
	Thinking              string                     `json:"thinking"`
	Telemetry             *RuntimeTelemetry          `json:"telemetry"`
	Provider              string                     `json:"provider"`
	Model                 string                     `json:"model"`
	Status                string                     `json:"status"`
	CancelToken           string                     `json:"cancel_token"`
	CancelRequested       bool                       `json:"cancel_requested"`
	Error                 string                     `json:"error"`
	Recovery              RecoveryHint               `json:"recovery"`
	Revision              uint64                     `json:"revision"`
	Messages              []RuntimeMessage           `json:"messages"`
	HistoryTruncated      bool                       `json:"history_truncated"`
	HistoryToolsTruncated bool                       `json:"history_tools_truncated"`
	Activities            []RuntimeActivity          `json:"activities"`
	ActivitiesTruncated   bool                       `json:"activities_truncated"`
	Permission            *RuntimePermission         `json:"permission"`
	Input                 *protocol.UserInputRequest `json:"input"`
}

// RuntimeManager owns at most two explicitly activated project workers. It never
// starts work from a snapshot/read and never queues prompts or activations.
// The context belongs to the manager, not an HTTP request or browser connection.
type RuntimeManager struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	executable string
	env        []string
	workers    map[string]*liveRuntime
	closed     bool
	closeOnce  func()
	recovery   RecoveryStore
}

type liveRuntime struct {
	skillsEnabled                         bool // Explicit per-worker opt-in; never remembered project trust.
	mu                                    sync.Mutex
	eventMu                               sync.Mutex // Serializes wire projection with message-edit ACK replay.
	recoveryMu                            sync.Mutex // Serializes bounded hint writes; never held by event projection.
	recoveryStore                         RecoveryStore
	control                               sync.Mutex     // TryLock rejects overlapping controls; latched turn cancellation waits.
	cancelTaskToken                       string         // Private ownership until dispatcher releases control.
	cancelTasks                           sync.WaitGroup // Registered under mu before closing; joined after lifetime cancellation.
	ctx                                   context.Context
	cancel                                context.CancelFunc
	project                               Project
	instanceID                            string
	worker                                *process.Worker
	started, drained                      chan struct{}
	snapshot                              RuntimeSnapshot
	busy                                  bool
	promptID, earlyCompletion             string
	assistant                             int
	activityPrompt                        uint64
	activityCanceled                      bool
	activityKeys                          []runtimeActivityKey
	choices                               RuntimeChoices
	transitioning                         bool
	rootEpoch, retiredEpoch, turnSequence uint64
	turnID                                string
	plan                                  int
	completedRequest                      uint64
	usageBase                             RuntimeTelemetry
	messageSequence                       uint64
	imageReads                            int // Independent bounded reads never hold the mutation/Stop gate.
	pendingUserID                         string
	permissionAgent                       *protocol.AgentRef // Non-nil only for a child request projected into the shared attention card.
	activeChildren                        map[string]protocol.AgentStatus
	pendingRegenerateReplyID              string
	assistantHasPlan                      bool
	messageEdit                           runtimeMessageEditState
	goal                                  runtimeGoalState
	compaction                            runtimeCompactionState
	queue                                 runtimeQueueState
	queueSupported                        bool
	steer                                 runtimeSteerState
	steerSupported                        bool
	versions                              runtimeVersionState
	subscribers                           map[*runtimeSubscription]struct{}
}

// NewRuntimeManager is inert: construction does not start a process, load project
// configuration, discover extensions, open sessions, or contact a provider.
func NewRuntimeManager(ctx context.Context, executable, sessionsRoot string, stores ...RecoveryStore) *RuntimeManager {
	ctx, cancel := context.WithCancel(ctx)
	env, err := freezeWorkerEnvironment(sessionsRoot)
	if err != nil {
		executable = ""
	} // Open fails closed before spawning.
	m := &RuntimeManager{ctx: ctx, cancel: cancel, executable: executable, env: env, workers: make(map[string]*liveRuntime)}
	if len(stores) != 0 {
		m.recovery = stores[0]
	}
	m.closeOnce = sync.OnceFunc(m.shutdown)
	context.AfterFunc(ctx, func() { _ = m.Close() })
	return m
}

// Open is explicit authorization to load host/project configuration and project
// instructions in an OS-privileged RPC worker. Empty sessionID creates a durable
// session. Provider/model are optional overrides of normal host defaults.
// An existing worker is returned only for the same explicitly identified session;
// opening another session requires closing the project first. No implicit switch.
func (m *RuntimeManager) Open(ctx context.Context, project Project, sessionID, provider, model string) (RuntimeSnapshot, error) {
	return m.OpenWithSkills(ctx, project, sessionID, provider, model, false)
}

func (m *RuntimeManager) open(ctx context.Context, project Project, sessionID, provider, model string, skills bool) (RuntimeSnapshot, error) {
	if ctx.Err() != nil {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if !filepath.IsAbs(m.executable) || !project.Available || !runtimeIdentifier(project.ID) || (sessionID != "" && !runtimeIdentifier(sessionID)) || !runtimeOption(provider) || !runtimeOption(model) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	project.checkIdentity()
	if !project.Available {
		return RuntimeSnapshot{}, ErrProjectInvalid
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeClosed
	}
	if old := m.workers[project.ID]; old != nil {
		old.mu.Lock()
		same := old.skillsEnabled == skills && sessionID != "" && old.snapshot.SessionID == sessionID && old.snapshot.Status != "opening" && old.snapshot.Status != "closing" && old.snapshot.Status != "failed" && provider == "" && model == ""
		snapshot := old.snapshot.clone()
		old.mu.Unlock()
		m.mu.Unlock()
		if same {
			return snapshot, nil
		}
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	if len(m.workers) >= 2 {
		m.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeLimit
	}
	lifetime, cancel := context.WithCancel(m.ctx)
	instanceID := rand.Text()
	r := &liveRuntime{skillsEnabled: skills, recoveryStore: m.recovery, instanceID: instanceID, ctx: lifetime, cancel: cancel, project: project, started: make(chan struct{}), drained: make(chan struct{}), assistant: -1, plan: -1, transitioning: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: instanceID, Status: "opening", Mode: "default", Thinking: "off", Telemetry: &RuntimeTelemetry{}, Revision: 1}}
	m.workers[project.ID] = r
	m.mu.Unlock()
	err := m.start(r, sessionID, provider, model)
	close(r.started)
	if err != nil {
		r.stop()
		m.mu.Lock()
		if m.workers[project.ID] == r {
			delete(m.workers, project.ID)
		}
		m.mu.Unlock()
		return RuntimeSnapshot{}, err
	}
	r.mu.Lock()
	snapshot := r.snapshot.clone()
	r.mu.Unlock()
	return snapshot, nil
}

func (m *RuntimeManager) start(r *liveRuntime, sessionID, provider, model string) error {
	// RPC itself installs interactive permission/input brokers; there are no
	// --interactive-* flags. No-session is bootstrap only, never the live store.
	// Omit --permission: app defaults fresh sessions to Ask independently of
	// configuration. An explicit override would suppress saved session policies
	// on every session_open, including switches and worker restarts. Configured
	// MCP servers and bounded subagents intentionally match their TUI runtimes;
	// plugins and debug remain outside the managed Web profile.
	args := []string{"--mode", "rpc", "--rpc-startup", "eager", "--no-session", "--managed-explicit-goals", "--no-plugins", "--subagents"}
	if !r.skillsEnabled {
		args = append(args, "--no-skills")
	}
	tools := "read,glob,grep,write,edit,bash,ask_user,get_goal,create_goal,update_goal,process_start,process_status,process_logs,process_stop,process_list"
	if r.skillsEnabled {
		tools += ",activate_skill,deactivate_skill,read_skill_resource"
	}
	args = append(args, "--no-debug", "--tools", tools)
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	worker, err := process.Start(r.ctx, process.Options{Executable: m.executable, Args: args, Dir: r.project.Path, Env: m.env, RPC: clientrpc.Options{MaxFrameBytes: 4 << 20, EventBuffer: 128, MaxPending: 4, HandshakeTimeout: 8 * time.Second, WriteTimeout: 2 * time.Second}, ShutdownTimeout: 500 * time.Millisecond})
	if err != nil {
		close(r.drained)
		return ErrRuntimeUnavailable
	}
	r.mu.Lock()
	r.worker = worker
	r.queueSupported = slices.Contains(worker.Client.Ready().Capabilities, "queue_next")
	r.steerSupported = slices.Contains(worker.Client.Ready().Capabilities, "managed_steer")
	r.mu.Unlock()
	go r.drain()
	for _, capability := range []string{"session_management", "session_info", "prompt_completion", "permission_interaction", "user_input", "messages_page"} {
		if !slices.Contains(worker.Client.Ready().Capabilities, capability) {
			return ErrRuntimeUnavailable
		}
	}
	project := r.project
	project.checkIdentity()
	if !project.Available {
		return ErrProjectInvalid
	}
	command := "session_create"
	var params any
	if sessionID != "" {
		command = "session_open"
		params = struct {
			SessionID string `json:"session_id"`
		}{sessionID}
	}
	var session protocol.RPCSessionSummary
	if err := r.call(protocol.RPCRequest{Type: command}, params, &session); err != nil || !runtimeIdentifier(session.SessionID) || (sessionID != "" && session.SessionID != sessionID) {
		return ErrRuntimeUnavailable
	}
	// SetSession never auto-resumes goals. Abort immediately persists deferral;
	// the managed explicit policy prevents later prompts from starting unowned work.
	if err := r.call(protocol.RPCRequest{Type: "abort"}, nil, nil); err != nil {
		return err
	}
	info, err := r.verifiedInfo(session.SessionID)
	if err != nil {
		return err
	}
	if err := r.loadHistory(); err != nil {
		return err
	}
	if err := r.refreshTelemetry(); err != nil {
		return err
	}
	if err := r.bindRecovery(session.SessionID); err != nil {
		r.fail()
		return err
	}
	r.mu.Lock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		r.mu.Unlock()
		return ErrRuntimeClosed
	}
	r.applyInfo(info)
	r.snapshot.SessionID = session.SessionID
	r.mu.Unlock()
	if err := r.refreshGoal(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return ErrRuntimeClosed
	}
	r.transitioning = false
	r.snapshot.Status = "idle"
	r.publishLocked()
	return nil
}

func runtimeIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, ch := range s {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
func runtimeOption(s string) bool {
	return len(s) <= 256 && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n") && (s == "" || strings.TrimSpace(s) == s)
}

func (m *RuntimeManager) Snapshot(projectID string) (RuntimeSnapshot, bool) {
	m.mu.Lock()
	r := m.workers[projectID]
	m.mu.Unlock()
	if r == nil {
		return RuntimeSnapshot{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot.clone(), true
}
func (m *RuntimeManager) runtime(projectID, instanceID string) (*liveRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrRuntimeClosed
	}
	r := m.workers[projectID]
	if r == nil {
		return nil, ErrRuntimeClosed
	}
	// Compare while holding the map lock, before returning a worker pointer. A
	// stale browser request must never be rebound to a replacement process whose
	// worker-local permission/input counters may have restarted.
	r.mu.Lock()
	defer r.mu.Unlock()
	if instanceID == "" || r.instanceID != instanceID {
		return nil, ErrRuntimeInvalid
	}
	return r, nil
}

// CloseProject cancels the worker's entire lifetime and joins/reaps it. The slot
// stays occupied until shutdown finishes. Request cancellation cannot orphan it.
func (m *RuntimeManager) CloseProject(ctx context.Context, projectID, instanceID string) error {
	if ctx.Err() != nil {
		return ErrRuntimeInvalid
	}
	r, err := m.runtime(projectID, instanceID)
	if err != nil {
		return err
	}
	if !r.control.TryLock() {
		return ErrRuntimeBusy
	}
	defer r.control.Unlock()
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return err
	}
	r.stop()
	m.mu.Lock()
	if m.workers[projectID] == r {
		delete(m.workers, projectID)
	}
	m.mu.Unlock()
	return nil
}
func (r *liveRuntime) stop() {
	r.mu.Lock()
	r.snapshot.Status = "closing"
	r.uncertainCompactionLocked()
	r.unknownActivitiesLocked()
	r.clearPermissionLocked()
	r.snapshot.Input = nil
	r.publishLocked()
	r.mu.Unlock()
	r.cancel()
	<-r.started
	r.mu.Lock()
	worker := r.worker
	r.mu.Unlock()
	if worker != nil {
		_ = worker.Close()
	}
	<-r.drained
	r.cancelTasks.Wait()
}
func (m *RuntimeManager) shutdown() {
	m.mu.Lock()
	m.closed = true
	workers := slices.Collect(maps.Values(m.workers))
	m.mu.Unlock()
	m.cancel()
	var wg sync.WaitGroup
	for _, r := range workers {
		wg.Go(r.stop)
	}
	wg.Wait()
	m.mu.Lock()
	clear(m.workers)
	m.mu.Unlock()
}
func (m *RuntimeManager) Close() error { m.closeOnce(); return nil }
