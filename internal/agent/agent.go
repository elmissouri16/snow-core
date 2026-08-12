// Package agent implements the streaming turn loop: prompt → provider →
// permission gate → tools → loop until the model stops.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/compact"
	goalpkg "github.com/snow-core/snow/internal/goal"
	"github.com/snow-core/snow/internal/permission"
	planpkg "github.com/snow-core/snow/internal/plan"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

// MaxToolRetries bounds retries for malformed tool arguments.
const MaxToolRetries = 1

const (
	defaultDeferredTopK  = 5
	deferredCandidateK   = 20
	maxRoutingEventTools = 20
	maxPendingRootInputs = 64
	maxQueuedInputBytes  = 64 * 1024
	automaticTurnDelay   = 25 * time.Millisecond
	skillActivationMeta  = "agent_skill_activation"
)

// ErrNotRunning is returned when an operation requires an active,
// queue-accepting agent run.
var (
	ErrNotRunning     = errors.New("agent: no running turn accepting queued input")
	ErrPromptRejected = errors.New("agent: prompt rejected before admission")
)

// Options configures an Agent.
type Options struct {
	Provider      provider.Provider
	Registry      tools.Registry
	Session       session.Store
	Permission    permission.Service
	ToolHost      tools.ToolHost
	Router        tools.Router
	SystemPrompt  string
	Model         protocol.Model
	MaxTurns      int // 0 = unlimited
	CallLimit     int // max tool calls per turn (0 = unlimited)
	MaxToolOutput int
	// Thinking level forwarded to providers that support reasoning effort.
	Thinking protocol.ThinkingLevel
	// ReasoningSummary and TextVerbosity are forwarded to adapters that support
	// the Responses API controls.
	ReasoningSummary protocol.ReasoningSummary
	TextVerbosity    protocol.TextVerbosity
	// CollaborationMode selects Default or Plan behavior. Empty restores the
	// branch state, then falls back to Default.
	CollaborationMode protocol.CollaborationMode
	// PlanThinking overrides reasoning effort while in Plan mode. Nil uses
	// Medium when advertised, otherwise the configured effort/Off.
	PlanThinking *protocol.ThinkingLevel
	Goal         *goalpkg.Controller
	// Auth resolves credentials (auth.json). Optional: env fallback is
	// implemented by providers for known env vars.
	Auth auth.Store
	// APIKey is an explicit credential override (CLI --api-key / SDK option).
	// APIKeyProvider binds that secret to one provider so runtime model/provider
	// switching cannot forward it to a different origin. Empty is initialized
	// from the initial Model.Provider for backward-compatible embedders.
	APIKey         string
	APIKeyProvider string
	// Identity attributes permission and host-interaction requests for child
	// agents. Root leaves it nil for backward-compatible events.
	Identity *protocol.AgentRef
	// SkillNames filters both restored and direct $name activations. Nil keeps
	// the legacy unrestricted behavior for standalone Agent embedders; an empty
	// non-nil map disables every persisted skill activation.
	SkillNames map[string]bool
	// Compaction configures manual compaction and goal-only automatic compaction.
	Compaction CompactionOptions
}

// CompactionOptions is kept in agent to avoid coupling core runtime behavior to
// persisted configuration packages.
type CompactionOptions struct {
	RetainTokens             int
	MinRetainedTurns         int
	SummaryMaxTokens         int
	Fallback                 string
	Guidance                 string
	GoalAutoThresholdPercent int
}

// Agent drives turns against a provider and tool registry.
type Agent struct {
	mu          sync.RWMutex
	admissionMu sync.Mutex
	// queuePublishMu serializes queue mutation with snapshot publication. Queue
	// callbacks never run under mu, while observers still see snapshots in the
	// exact order the underlying queue changed.
	queuePublishMu sync.Mutex
	// mailboxMu protects attributed collaboration messages. Producers only
	// enqueue; the admitted agent loop is the sole writer that drains them into
	// the mutable session cursor at safe boundaries.
	mailboxMu        sync.Mutex
	mailboxPersistMu sync.Mutex
	mailbox          []protocol.AgentMessage
	mailboxUnread    bool
	mailboxActivity  chan struct{}
	mailboxClosed    bool
	opts             Options
	model            protocol.Model
	bus              *eventBus
	running          bool
	// tool results retained between the tool_use assistant message and the
	// continuation provider call
	pending      map[string]protocol.ContentBlock
	pendingOrder []string
	// toolStarts is used to add useful duration metadata to tool_end events.
	toolStarts          map[string]time.Time
	turnUsage           protocol.Usage
	usageSet            bool
	turnProgress        bool
	latestContextTokens int
	// Deferred schemas selected for the current user turn. The base selection
	// is sticky; the latest search_tools result may add at most five more.
	baseDeferred     []string
	searchedDeferred []string
	// queuedInputs are in-memory until a safe provider boundary persists them as
	// ordinary user messages. queueAccepting closes atomically with the final
	// empty check so an accepted input can never become stranded.
	queuedInputs   []protocol.QueuedInput
	queueSequence  uint64
	queueAccepting bool
	// activeSkills are re-appended to the system instructions on every provider
	// request so manual compaction cannot silently discard activated guidance.
	activeSkills  map[string]string
	mode          protocol.CollaborationMode
	turnMode      protocol.CollaborationMode
	turnPlanSeen  bool
	activeCancel  context.CancelFunc
	activeDone    chan struct{}
	autoRunning   bool
	autoStop      bool
	autoPending   bool
	autoEmpty     int
	autoEmptyGoal string
	autoDone      chan struct{}
	autoWG        sync.WaitGroup
	turnWG        sync.WaitGroup
	closed        bool
	turnOrigin    string
	turnID        string
	goalAtTurn    *protocol.ThreadGoal
	turnStarted   time.Time
	goalTurn      int
	goalTurnID    string
	budgetWrap    bool
}

// New creates an agent.
func New(opts Options) (*Agent, error) {
	if opts.Provider == nil {
		return nil, errors.New("agent: provider required")
	}
	if opts.Registry == nil {
		return nil, errors.New("agent: tool registry required")
	}
	if opts.Session == nil {
		return nil, errors.New("agent: session required")
	}
	if opts.Permission == nil {
		opts.Permission = permission.NewService(permission.ModeDeny, nil)
	}
	if opts.Model.Provider == "" && opts.Provider != nil {
		opts.Model.Provider = opts.Provider.ID()
	}
	if opts.APIKey != "" && opts.APIKeyProvider == "" {
		opts.APIKeyProvider = opts.Model.Provider
	}
	thinking, err := protocol.ParseThinkingLevel(string(opts.Thinking))
	if err != nil {
		return nil, err
	}
	opts.Thinking = thinking
	summary, err := protocol.ParseReasoningSummary(string(opts.ReasoningSummary))
	if err != nil {
		return nil, err
	}
	opts.ReasoningSummary = summary
	verbosity, err := protocol.ParseTextVerbosity(string(opts.TextVerbosity))
	if err != nil {
		return nil, err
	}
	opts.TextVerbosity = verbosity
	if !opts.Model.SupportsThinkingLevel(thinking) {
		return nil, unsupportedThinkingError(opts.Model, thinking)
	}
	mode := opts.CollaborationMode
	if mode == "" {
		mode, err = loadCollaborationMode(opts.Session)
		if err != nil {
			return nil, fmt.Errorf("agent: restore collaboration mode: %w", err)
		}
	}
	mode, err = protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return nil, err
	}
	if opts.PlanThinking != nil {
		parsed, err := protocol.ParseThinkingLevel(string(*opts.PlanThinking))
		if err != nil {
			return nil, err
		}
		opts.PlanThinking = &parsed
	}
	opts.Identity = opts.Identity.Clone()
	opts.Model = opts.Model.Clone()
	if opts.SkillNames != nil {
		names := make(map[string]bool, len(opts.SkillNames))
		for name, allowed := range opts.SkillNames {
			names[name] = allowed
		}
		opts.SkillNames = names
	}
	a := &Agent{opts: opts, model: opts.Model, bus: newEventBus(), mode: mode, turnMode: mode, mailboxActivity: make(chan struct{}, 1)}
	a.pending = make(map[string]protocol.ContentBlock)
	a.toolStarts = make(map[string]time.Time)
	a.activeSkills = restoreActiveSkills(opts.Session, opts.Registry, opts.ToolHost, opts.SkillNames)
	if state, ok := opts.Session.(session.ThreadStateStore); ok {
		if err := state.SetCollaborationMode(mode); err != nil {
			return nil, fmt.Errorf("agent: persist collaboration mode: %w", err)
		}
	}
	return a, nil
}

// Model returns the current model.
func (a *Agent) Model() protocol.Model {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model.Clone()
}

// Thinking returns the effective effort used for subsequent provider requests.
func (a *Agent) Thinking() protocol.ThinkingLevel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.effectiveThinkingLocked(a.mode)
}

func (a *Agent) effectiveThinkingLocked(mode protocol.CollaborationMode) protocol.ThinkingLevel {
	base := protocol.NormalizeThinkingLevel(a.opts.Thinking)
	if mode != protocol.ModePlan {
		return base
	}
	if a.opts.PlanThinking != nil {
		level := protocol.NormalizeThinkingLevel(*a.opts.PlanThinking)
		if a.model.SupportsThinkingLevel(level) {
			return level
		}
		return protocol.ThinkingOff
	}
	if a.model.SupportsThinkingLevel(protocol.ThinkingMedium) {
		return protocol.ThinkingMedium
	}
	if a.model.SupportsThinkingLevel(base) {
		return base
	}
	return protocol.ThinkingOff
}

func (a *Agent) requestThinking() protocol.ThinkingLevel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.effectiveThinkingLocked(a.turnMode)
}

func (a *Agent) capturedTurnMode() protocol.CollaborationMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.turnMode
}

// Mode returns the active branch collaboration mode.
func (a *Agent) Mode() protocol.CollaborationMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mode
}

// SetMode changes the active branch mode while idle.
func (a *Agent) SetMode(mode protocol.CollaborationMode) error {
	unlockAdmission := a.LockAdmission()
	admissionHeld := true
	defer func() {
		if admissionHeld {
			unlockAdmission()
		}
	}()
	reentrantEventCallback := a.bus.InCallback()
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	// A mode switch is never an implicit abort of an admitted turn.
	a.mu.RLock()
	running := a.running
	wasAutomatic := a.autoRunning
	automaticGoalTurn := wasAutomatic && a.turnOrigin == "goal"
	a.mu.RUnlock()
	if running && !automaticGoalTurn {
		return errors.New("agent: cannot switch collaboration mode while running")
	}
	stoppedAutomatic := parsed == protocol.ModePlan && wasAutomatic
	resumeAfterFailure := func(err error) error {
		if stoppedAutomatic {
			a.ContinueGoal()
		}
		return err
	}
	if parsed == protocol.ModePlan {
		if err := a.StopGoal(context.Background(), false); err != nil {
			return resumeAfterFailure(err)
		}
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return resumeAfterFailure(errors.New("agent: cannot switch collaboration mode while running"))
	}
	state, ok := a.opts.Session.(session.ThreadStateStore)
	if ok {
		if err := state.SetCollaborationMode(parsed); err != nil {
			a.mu.Unlock()
			return resumeAfterFailure(err)
		}
	}
	a.mode = parsed
	effort := a.effectiveThinkingLocked(parsed)
	a.mu.Unlock()
	unlockAdmission()
	admissionHeld = false
	a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: parsed, ReasoningEffort: effort}})
	if parsed == protocol.ModeDefault {
		deferred := false
		if a.opts.Goal != nil {
			deferred, _ = a.opts.Goal.Deferred()
		}
		if !deferred {
			a.ContinueGoal()
		}
	}
	if !reentrantEventCallback {
		_ = a.bus.Drain(context.Background())
	}
	return nil
}

// SetThinking updates the effort for subsequent provider requests. The
// selected model must advertise the requested non-off level; off is always
// accepted.
func (a *Agent) SetThinking(level protocol.ThinkingLevel) error {
	parsed, err := protocol.ParseThinkingLevel(string(level))
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.model.SupportsThinkingLevel(parsed) {
		return unsupportedThinkingError(a.model, parsed)
	}
	a.opts.Thinking = parsed
	return nil
}

// ReasoningSummary returns the summary preference used for subsequent calls.
func (a *Agent) ReasoningSummary() protocol.ReasoningSummary {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return protocol.NormalizeReasoningSummary(a.opts.ReasoningSummary)
}

// SetReasoningSummary updates the summary preference for subsequent calls.
func (a *Agent) SetReasoningSummary(summary protocol.ReasoningSummary) error {
	parsed, err := protocol.ParseReasoningSummary(string(summary))
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.opts.ReasoningSummary = parsed
	a.mu.Unlock()
	return nil
}

// TextVerbosity returns the text verbosity used for subsequent calls.
func (a *Agent) TextVerbosity() protocol.TextVerbosity {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return protocol.NormalizeTextVerbosity(a.opts.TextVerbosity)
}

// SetTextVerbosity updates the text verbosity for subsequent calls.
func (a *Agent) SetTextVerbosity(verbosity protocol.TextVerbosity) error {
	parsed, err := protocol.ParseTextVerbosity(string(verbosity))
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.opts.TextVerbosity = parsed
	a.mu.Unlock()
	return nil
}

func unsupportedThinkingError(model protocol.Model, level protocol.ThinkingLevel) error {
	allowed := model.SupportedThinkingLevels()
	parts := make([]string, 0, len(allowed))
	for _, supported := range allowed {
		parts = append(parts, string(supported))
	}
	return fmt.Errorf("agent: model %q does not advertise thinking level %q (supported: %s)", model.ID, level, strings.Join(parts, "|"))
}

// SystemPrompt returns the assembled system prompt.
func (a *Agent) SystemPrompt() string { return a.opts.SystemPrompt }

// LockAdmission serializes a compound App session transaction against prompt
// and control admission. The returned function must be deferred by the caller.
func (a *Agent) LockAdmission() func() {
	a.admissionMu.Lock()
	return a.admissionMu.Unlock
}

// SetSession switches the durable conversation store. Callers must only
// switch sessions while the agent is idle.
func (a *Agent) SetSession(st session.Store) error {
	unlock := a.LockAdmission()
	defer unlock()
	return a.setSessionAdmitted(st, true)
}
func (a *Agent) SetSessionQuiet(st session.Store) error {
	unlock := a.LockAdmission()
	defer unlock()
	return a.setSessionAdmitted(st, false)
}

// SetSessionQuietAdmitted participates in an App transaction that already
// holds LockAdmission.
func (a *Agent) SetSessionQuietAdmitted(st session.Store) error {
	return a.setSessionAdmitted(st, false)
}

func (a *Agent) setSessionAdmitted(st session.Store, publish bool) error {
	if st == nil {
		return errors.New("agent: session is nil")
	}
	a.mu.RLock()
	closed := a.closed
	a.mu.RUnlock()
	if closed {
		return errors.New("agent: closed")
	}
	if err := a.stopAutomaticForControl(context.Background(), "switch session"); err != nil {
		return err
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("agent: cannot switch session while running")
	}
	mode, err := loadCollaborationMode(st)
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("agent: restore collaboration mode: %w", err)
	}
	a.opts.Session = st
	a.activeSkills = restoreActiveSkills(st, a.opts.Registry, a.opts.ToolHost, a.opts.SkillNames)
	a.mode = mode
	a.turnMode = mode
	a.latestContextTokens = 0
	effort := a.effectiveThinkingLocked(mode)
	a.mu.Unlock()
	if publish {
		a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: effort}})
	}
	return nil
}

// SetProvider updates the active provider used for subsequent turns.
func (a *Agent) SetProvider(p provider.Provider) error {
	if p == nil {
		return errors.New("agent: provider is nil")
	}
	unlock := a.LockAdmission()
	defer unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return errors.New("agent: cannot change provider while running")
	}
	a.opts.Provider = p
	return nil
}

// SetProviderAndModel changes provider and model as one admitted idle
// transaction, preventing a prompt from observing a mixed pair.
func (a *Agent) SetProviderAndModel(p provider.Provider, m protocol.Model) error {
	if p == nil {
		return errors.New("agent: provider is nil")
	}
	if m.Provider == "" {
		return errors.New("agent: model has no provider")
	}
	m = m.Clone()
	unlock := a.LockAdmission()
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		unlock()
		return errors.New("agent: cannot change provider and model while running")
	}
	a.opts.Provider = p
	a.model = m
	a.mu.Unlock()
	unlock()
	reentrant := a.bus.InCallback()
	a.publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	if !reentrant {
		_ = a.bus.Drain(context.Background())
	}
	return nil
}

// SetProviderModelThinking changes provider, model, and reasoning effort as one
// admitted idle transaction, so prompts cannot observe an intermediate choice.
func (a *Agent) SetProviderModelThinking(p provider.Provider, m protocol.Model, level protocol.ThinkingLevel) error {
	if p == nil {
		return errors.New("agent: provider is nil")
	}
	if strings.TrimSpace(m.Provider) == "" {
		return errors.New("agent: model has no provider")
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("agent: model has no id")
	}
	parsed, err := protocol.ParseThinkingLevel(string(level))
	if err != nil {
		return err
	}
	m = m.Clone()
	if !m.SupportsThinkingLevel(parsed) {
		return unsupportedThinkingError(m, parsed)
	}
	unlock := a.LockAdmission()
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		unlock()
		return errors.New("agent: cannot change provider, model, and thinking while running")
	}
	a.opts.Provider = p
	a.model = m
	a.opts.Thinking = parsed
	a.mu.Unlock()
	unlock()
	reentrant := a.bus.InCallback()
	a.publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	if !reentrant {
		_ = a.bus.Drain(context.Background())
	}
	return nil
}

// currentProvider returns the provider selected for the next turn.
func (a *Agent) currentProvider() provider.Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.opts.Provider
}

// SetModel updates the active model.
func (a *Agent) SetModel(m protocol.Model) error {
	if strings.TrimSpace(m.Provider) == "" {
		return errors.New("agent: model has no provider")
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("agent: model has no id")
	}
	m = m.Clone()
	unlock := a.LockAdmission()
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		unlock()
		return errors.New("agent: cannot change model while running")
	}
	a.model = m
	a.mu.Unlock()
	unlock()
	reentrantEventCallback := a.bus.InCallback()
	a.publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	if !reentrantEventCallback {
		_ = a.bus.Drain(context.Background())
	}
	return nil
}

// WaitIdle waits for the currently admitted operation to release the agent.
// It returns immediately when already idle.
func (a *Agent) WaitIdle(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.RLock()
	running := a.running
	done := a.activeDone
	a.mu.RUnlock()
	if !running || done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning reports whether a turn is in flight.
func (a *Agent) IsRunning() bool {
	_, _, running := a.ActiveTurn()
	return running
}

// ActiveTurn returns the identity of the currently admitted root operation.
// The identity is intended for UI reconciliation when delayed lifecycle events
// are delivered after a newer operation has already started.
func (a *Agent) ActiveTurn() (origin, id string, running bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.turnOrigin, a.turnID, a.running
}

// Steer queues text for the next safe boundary of an active run. Steering is
// delivered after the current assistant response and its complete tool batch.
func (a *Agent) Steer(text string) error {
	_, err := a.QueueInput(protocol.QueuedInputSteer, text)
	return err
}

// FollowUp queues text for delivery after the active run has naturally
// stopped and no steering input remains.
func (a *Agent) FollowUp(text string) error {
	_, err := a.QueueInput(protocol.QueuedInputFollowUp, text)
	return err
}

// QueueInput is the correlated queue-admission seam used by the native TUI.
// SDK and RPC callers should use Steer or FollowUp; the returned item lets the
// TUI retain the user's compact composer text separately from expanded model
// input without guessing which queue event belongs to which submission.
func (a *Agent) QueueInput(kind protocol.QueuedInputKind, text string) (protocol.QueuedInput, error) {
	return a.enqueueRootInput(kind, text)
}

// PendingInputs returns an independent submission-ordered queue snapshot.
func (a *Agent) PendingInputs() protocol.InputQueue {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.inputQueueLocked()
}

// ClearPendingInputs atomically stops queue admission and returns every input
// that was accepted but not yet delivered. It is used by interactive abort so
// an enqueue racing the key press is either returned here or rejected, never
// silently cleared after the TUI took its recovery snapshot.
func (a *Agent) ClearPendingInputs() protocol.InputQueue {
	return a.closeInputQueue(true)
}

func (a *Agent) enqueueRootInput(kind protocol.QueuedInputKind, text string) (protocol.QueuedInput, error) {
	if kind != protocol.QueuedInputSteer && kind != protocol.QueuedInputFollowUp {
		return protocol.QueuedInput{}, fmt.Errorf("agent: invalid queued input kind %q", kind)
	}
	if strings.TrimSpace(text) == "" {
		return protocol.QueuedInput{}, errors.New("agent: queued input is empty")
	}
	if len(text) > maxQueuedInputBytes {
		return protocol.QueuedInput{}, fmt.Errorf("agent: queued input exceeds %d bytes", maxQueuedInputBytes)
	}
	a.queuePublishMu.Lock()
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return protocol.QueuedInput{}, errors.New("agent: closed")
	}
	if !a.running || !a.queueAccepting {
		a.mu.Unlock()
		return protocol.QueuedInput{}, ErrNotRunning
	}
	if len(a.queuedInputs) >= maxPendingRootInputs {
		a.mu.Unlock()
		return protocol.QueuedInput{}, fmt.Errorf("agent: queued input limit %d reached", maxPendingRootInputs)
	}
	a.queueSequence++
	item := protocol.QueuedInput{ID: newID(), Kind: kind, Text: text, Order: a.queueSequence}
	a.queuedInputs = append(a.queuedInputs, item)
	snapshot := a.inputQueueLocked()
	a.mu.Unlock()
	a.publishInputQueue(snapshot)
	return item, nil
}

func (a *Agent) inputQueueLocked() protocol.InputQueue {
	items := make([]protocol.QueuedInput, len(a.queuedInputs))
	copy(items, a.queuedInputs)
	return protocol.InputQueue{Items: items}
}

func (a *Agent) publishInputQueue(queue protocol.InputQueue) {
	a.publish(protocol.AgentEvent{Type: protocol.EvQueueUpdated, Queue: queue.Clone()})
}

// closeInputQueue stops admissions and optionally drops pending input. It
// publishes only when the visible snapshot changes.
func (a *Agent) closeInputQueue(clear bool) protocol.InputQueue {
	a.queuePublishMu.Lock()
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	a.queueAccepting = false
	cleared := protocol.InputQueue{}
	changed := clear && len(a.queuedInputs) > 0
	if changed {
		cleared = a.inputQueueLocked()
		a.queuedInputs = nil
	}
	snapshot := a.inputQueueLocked()
	a.mu.Unlock()
	if changed {
		a.publishInputQueue(snapshot)
	}
	return cleared
}

// Subscribe registers an event listener; returns an unsubscribe func.
func (a *Agent) Subscribe(fn func(protocol.AgentEvent)) func() { return a.bus.Subscribe(fn) }
func (a *Agent) DrainEvents(ctx context.Context) error         { return a.bus.Drain(ctx) }
func (a *Agent) InEventCallback() bool                         { return a.bus.InCallback() }

// StateEvent returns an explicit point-in-time state snapshot for surfaces
// that subscribe after construction. Callers decide when to emit it, avoiding
// constructor-time event loss or ordering races.
func (a *Agent) StateEvent() protocol.AgentEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{
		Mode: a.mode, ReasoningEffort: a.effectiveThinkingLocked(a.mode),
	}}
}

// EmitUserInputRequest publishes a host interaction request through the same
// normalized stream observed by the TUI, SDK, JSON, RPC, and plugins.
// Publish emits a trusted host lifecycle event.
func (a *Agent) Publish(ev protocol.AgentEvent) { a.publish(ev) }

// publish attaches the active root turn identity to events emitted while a
// turn is admitted. Callers may provide an explicit identity for lifecycle
// snapshots; those values are preserved. This keeps every TUI lifecycle event
// correlatable without forcing individual provider/tool paths to copy IDs.
func (a *Agent) publish(ev protocol.AgentEvent) {
	if ev.Agent == nil && ev.TurnID == "" {
		a.mu.RLock()
		if a.running {
			ev.TurnID = a.turnID
			ev.TurnOrigin = a.turnOrigin
		}
		a.mu.RUnlock()
	}
	a.bus.Publish(ev)
}

func (a *Agent) EmitUserInputRequest(req protocol.UserInputRequest) {
	copy := req
	copy.Questions = make([]protocol.UserInputQuestion, len(req.Questions))
	for i, question := range req.Questions {
		copy.Questions[i] = question
		copy.Questions[i].Options = append([]protocol.UserInputOption(nil), question.Options...)
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvUserInputRequest, UserInput: &copy})
}

// Messages returns the linearized session messages.
func (a *Agent) Messages() (messages []protocol.Message, err error) {
	err = a.withSessionRead(func(store session.Store) error {
		messages, err = store.Messages()
		return err
	})
	return messages, err
}

// SessionIdentity returns a synchronized snapshot of the active store identity.
func (a *Agent) SessionIdentity() (id, path string, err error) {
	err = a.withSessionRead(func(store session.Store) error {
		id, path = store.ID(), store.Path()
		return nil
	})
	return id, path, err
}

func (a *Agent) withSessionRead(read func(session.Store) error) error {
	unlock := a.LockAdmission()
	defer unlock()
	a.mu.RLock()
	if a.closed {
		a.mu.RUnlock()
		return errors.New("agent: closed")
	}
	store := a.opts.Session
	a.mu.RUnlock()
	if store == nil {
		return errors.New("agent: session is nil")
	}
	return read(store)
}

// EnqueueMailbox queues an attributed message without allowing an external
// goroutine to mutate the session cursor. When idle, admission is acquired and
// the envelope is persisted immediately; while running, run drains it before
// the next provider request or at turn finalization.
func (a *Agent) EnqueueMailbox(message protocol.AgentMessage) error {
	unlock := a.LockAdmission()
	defer unlock()
	return a.enqueueMailboxAdmitted(message)
}

// EnqueueMailboxAdmitted is the branch-transaction variant for hosts that
// already hold LockAdmission. It keeps mailbox admission and root branch
// selection atomic without attempting to lock the non-reentrant mutex twice.
func (a *Agent) EnqueueMailboxAdmitted(message protocol.AgentMessage) error {
	return a.enqueueMailboxAdmitted(message)
}

func (a *Agent) enqueueMailboxAdmitted(message protocol.AgentMessage) error {
	if err := message.Validate(); err != nil {
		return err
	}
	a.mailboxPersistMu.Lock()
	defer a.mailboxPersistMu.Unlock()
	a.mailboxMu.Lock()
	if a.mailboxClosed {
		a.mailboxMu.Unlock()
		return errors.New("agent: mailbox closed")
	}
	a.mu.RLock()
	closed, running := a.closed, a.running
	a.mu.RUnlock()
	if closed {
		a.mailboxMu.Unlock()
		return errors.New("agent: closed")
	}
	a.mailbox = append(a.mailbox, message)
	a.mailboxUnread = true
	select {
	case a.mailboxActivity <- struct{}{}:
	default:
	}
	if running {
		a.mailboxMu.Unlock()
		return nil
	}
	batch := append([]protocol.AgentMessage(nil), a.mailbox...)
	a.mailbox = nil
	a.mailboxMu.Unlock()
	return a.persistMailboxBatchLocked(batch)
}

// MailboxActivity is an edge-triggered notification channel. Consumers must
// check PendingMailbox after waking; notifications do not consume messages.
func (a *Agent) MailboxActivity() <-chan struct{} { return a.mailboxActivity }

// PendingMailbox reports whether attributed input is waiting for a safe point.
func (a *Agent) PendingMailbox() bool {
	a.mailboxMu.Lock()
	defer a.mailboxMu.Unlock()
	return len(a.mailbox) != 0 || a.mailboxUnread
}

// drainMailboxForProvider acknowledges only the envelopes included in the
// immediately following provider context. A producer arriving after the take
// sets unread again and will wake wait_agent for the next safe boundary.
func (a *Agent) drainMailboxForProvider() error {
	a.mailboxPersistMu.Lock()
	defer a.mailboxPersistMu.Unlock()
	a.mailboxMu.Lock()
	batch := append([]protocol.AgentMessage(nil), a.mailbox...)
	a.mailbox = nil
	a.mailboxUnread = false
	a.mailboxMu.Unlock()
	return a.persistMailboxBatchLocked(batch)
}

func (a *Agent) drainMailbox() error {
	a.mailboxPersistMu.Lock()
	defer a.mailboxPersistMu.Unlock()
	a.mailboxMu.Lock()
	batch := append([]protocol.AgentMessage(nil), a.mailbox...)
	a.mailbox = nil
	a.mailboxMu.Unlock()
	return a.persistMailboxBatchLocked(batch)
}

func (a *Agent) persistMailboxBatchLocked(batch []protocol.AgentMessage) error {
	if len(batch) == 0 {
		return nil
	}
	parent := a.opts.Session.BranchTip()
	entries := make([]session.Entry, 0, len(batch))
	for _, envelope := range batch {
		msg := protocol.NewAgentMessage(envelope.ID, parent, envelope)
		entries = append(entries, session.Entry{Type: session.EntryMessage, ID: msg.ID, ParentID: parent, Message: &msg})
		parent = msg.ID
	}
	if batched, ok := a.opts.Session.(session.BatchStore); ok {
		if err := batched.AppendBatch(entries); err != nil {
			a.requeueMailbox(batch)
			return fmt.Errorf("agent: persist mailbox batch: %w", err)
		}
	} else {
		for i, entry := range entries {
			if err := a.opts.Session.Append(entry); err != nil {
				a.requeueMailbox(batch[i:])
				return fmt.Errorf("agent: persist mailbox: %w", err)
			}
		}
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
}
func (a *Agent) requeueMailbox(batch []protocol.AgentMessage) {
	a.mailboxMu.Lock()
	a.mailbox = append(append([]protocol.AgentMessage(nil), batch...), a.mailbox...)
	a.mailboxUnread = true
	a.mailboxMu.Unlock()
}

// finishTurnMailbox atomically marks a turn idle with respect to producers and
// takes any final envelopes. A producer can therefore never observe a stale
// running=true after the final drain and leave mail stranded in memory.
func (a *Agent) finishTurnMailbox(mark func()) error {
	// Serialize the running=false transition and final append with idle
	// producers. Admission cannot be used here: automatic goal control may
	// already hold it while waiting for this turn to finish.
	a.mailboxPersistMu.Lock()
	defer a.mailboxPersistMu.Unlock()
	a.mailboxMu.Lock()
	a.mu.Lock()
	mark()
	a.mu.Unlock()
	batch := append([]protocol.AgentMessage(nil), a.mailbox...)
	a.mailbox = nil
	a.mailboxMu.Unlock()
	return a.persistMailboxBatchLocked(batch)
}

// Usage returns the aggregate usage for the active session branch.
func (a *Agent) Usage() (total protocol.Usage, err error) {
	err = a.withSessionRead(func(store session.Store) error {
		msgs, readErr := store.Messages()
		if readErr != nil {
			return readErr
		}
		for _, msg := range msgs {
			if msg.Usage != nil {
				total = total.Add(*msg.Usage)
			}
		}
		return nil
	})
	return total, err
}

// ContextMessages returns the provider-facing post-compaction projection. It
// is used to build independent subagent fork contexts without copying stale
// pre-compaction history.
func (a *Agent) ContextMessages() ([]protocol.Message, error) { return a.contextMessages() }

func (a *Agent) contextMessages() (messages []protocol.Message, err error) {
	err = a.withSessionRead(func(store session.Store) error {
		messages, err = contextMessagesFromStore(store)
		return err
	})
	return messages, err
}

// ContextMessagesAdmitted is the non-reentrant variant for callers that
// already hold LockAdmission while creating a child from the active branch.
func (a *Agent) ContextMessagesAdmitted() ([]protocol.Message, error) {
	return a.contextMessagesCurrent()
}

// contextMessagesCurrent is used by an admitted caller or an active turn.
// Session switching rejects active turns, so the captured store remains live.
func (a *Agent) contextMessagesCurrent() ([]protocol.Message, error) {
	a.mu.RLock()
	if a.closed {
		a.mu.RUnlock()
		return nil, errors.New("agent: closed")
	}
	store := a.opts.Session
	a.mu.RUnlock()
	return contextMessagesFromStore(store)
}

func contextMessagesFromStore(store session.Store) ([]protocol.Message, error) {
	if store == nil {
		return nil, errors.New("agent: session is nil")
	}
	if projected, ok := store.(session.ContextStore); ok {
		return projected.ContextMessages()
	}
	return store.Messages()
}

// Branches lists durable branch references for the active session.
func (a *Agent) Branches() (result []protocol.SessionBranch, err error) {
	err = a.withSessionRead(func(store session.Store) error {
		branches, ok := store.(session.BranchStore)
		if !ok {
			return errors.New("agent: session does not support durable branches")
		}
		result, err = branches.Branches()
		return err
	})
	return result, err
}

func (a *Agent) stopAutomaticForControl(ctx context.Context, operation string) error {
	a.mu.RLock()
	running := a.running
	automatic := a.autoRunning
	a.mu.RUnlock()
	if running && !automatic {
		return fmt.Errorf("agent: cannot %s while running", operation)
	}
	return a.StopGoal(ctx, false)
}

// SelectBranch switches the active branch while the agent is idle.
func (a *Agent) SelectBranch(branchID string) error {
	unlockAdmission := a.LockAdmission()
	defer unlockAdmission()
	return a.SelectBranchAdmitted(branchID)
}

// SelectBranchAdmitted switches branches while the caller holds the admission lock.
func (a *Agent) SelectBranchAdmitted(branchID string) error {
	if err := a.stopAutomaticForControl(context.Background(), "switch branch"); err != nil {
		return err
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("agent: cannot switch branch while running")
	}
	branches, ok := a.opts.Session.(session.BranchStore)
	if !ok {
		a.mu.Unlock()
		return errors.New("agent: session does not support durable branches")
	}
	listed, err := branches.Branches()
	oldBranchID := ""
	if err == nil {
		for _, existing := range listed {
			if existing.Active {
				oldBranchID = existing.ID
				break
			}
		}
		err = branches.SelectBranch(branchID)
	}
	if err == nil {
		var restored protocol.CollaborationMode
		restored, err = loadCollaborationMode(a.opts.Session)
		if err == nil {
			a.activeSkills = restoreActiveSkills(a.opts.Session, a.opts.Registry, a.opts.ToolHost, a.opts.SkillNames)
			a.mode = restored
			a.turnMode = restored
			a.latestContextTokens = 0
		} else if oldBranchID != "" {
			err = errors.Join(err, branches.SelectBranch(oldBranchID))
		}
	}
	mode := a.mode
	effort := a.effectiveThinkingLocked(mode)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: effort}})
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	a.publishGoalSnapshot()
	return nil
}

// Fork creates and activates a durable branch at an existing entry.
func (a *Agent) Fork(fromEntryID string) (protocol.SessionBranch, error) {
	return a.ForkWithOptions(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}

func (a *Agent) ForkWithOptions(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	unlockAdmission := a.LockAdmission()
	defer unlockAdmission()
	return a.ForkWithOptionsAdmitted(opts)
}

// ForkAdmitted preserves the legacy admitted fork entry point.
func (a *Agent) ForkAdmitted(fromEntryID string) (protocol.SessionBranch, error) {
	return a.ForkWithOptionsAdmitted(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}

// ForkWithOptionsAdmitted creates and activates a branch while admission is held.
func (a *Agent) ForkWithOptionsAdmitted(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	if err := a.stopAutomaticForControl(context.Background(), "fork"); err != nil {
		return protocol.SessionBranch{}, err
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return protocol.SessionBranch{}, errors.New("agent: cannot fork while running")
	}
	branches, ok := a.opts.Session.(session.BranchStore)
	if !ok {
		a.mu.Unlock()
		return protocol.SessionBranch{}, errors.New("agent: session does not support durable branches")
	}
	listed, err := branches.Branches()
	if err != nil {
		a.mu.Unlock()
		return protocol.SessionBranch{}, err
	}
	oldBranchID := ""
	for _, existing := range listed {
		if existing.Active {
			oldBranchID = existing.ID
			break
		}
	}
	managedText, managed := "", false
	sourceBranchID := opts.SourceBranchID
	if sourceBranchID == "" {
		sourceBranchID = oldBranchID
	}
	if a.opts.Goal != nil {
		if sourceBranchID == oldBranchID {
			managedText, managed, err = a.opts.Goal.ManagedTextForFork()
		} else {
			managedText, managed, err = a.opts.Goal.ManagedTextForBranch(sourceBranchID)
		}
		if err != nil {
			a.mu.Unlock()
			return protocol.SessionBranch{}, err
		}
	}
	var branch protocol.SessionBranch
	if manager, ok := a.opts.Session.(session.BranchManagementStore); ok {
		branch, err = manager.ForkBranchWithOptions(opts)
	} else {
		branch, err = branches.ForkBranch(opts.FromEntryID)
	}
	if err == nil && managed {
		err = a.opts.Goal.CopyManagedForFork(managedText)
	}
	if err != nil {
		rollbackErr := rollbackFork(branches, branch.ID, oldBranchID)
		a.mu.Unlock()
		return protocol.SessionBranch{}, errors.Join(err, rollbackErr)
	}
	mode, err := loadCollaborationMode(a.opts.Session)
	if err != nil {
		if managed && a.opts.Goal != nil {
			a.opts.Goal.DiscardManagedCurrent()
		}
		rollbackErr := rollbackFork(branches, branch.ID, oldBranchID)
		a.mu.Unlock()
		return protocol.SessionBranch{}, errors.Join(err, rollbackErr)
	}
	a.activeSkills = restoreActiveSkills(a.opts.Session, a.opts.Registry, a.opts.ToolHost, a.opts.SkillNames)
	a.mode = mode
	a.turnMode = mode
	a.latestContextTokens = 0
	effort := a.effectiveThinkingLocked(mode)
	a.mu.Unlock()
	a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: effort}})
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	a.publishGoalSnapshot()
	return branch, nil
}

func (a *Agent) RenameBranch(branchID, name string) (protocol.SessionBranch, error) {
	unlock := a.LockAdmission()
	defer unlock()
	return a.RenameBranchAdmitted(branchID, name)
}
func (a *Agent) RenameBranchAdmitted(branchID, name string) (protocol.SessionBranch, error) {
	a.mu.RLock()
	running := a.running
	a.mu.RUnlock()
	if running {
		return protocol.SessionBranch{}, errors.New("agent: cannot rename branch while running")
	}
	manager, ok := a.opts.Session.(session.BranchManagementStore)
	if !ok {
		return protocol.SessionBranch{}, errors.New("agent: session does not support branch management")
	}
	branch, err := manager.RenameBranch(branchID, name)
	if err == nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	}
	return branch, err
}

func (a *Agent) DeleteBranch(branchID string) error {
	unlock := a.LockAdmission()
	defer unlock()
	return a.DeleteBranchAdmitted(branchID)
}
func (a *Agent) DeleteBranchAdmitted(branchID string) error {
	a.mu.RLock()
	running := a.running
	a.mu.RUnlock()
	if running {
		return errors.New("agent: cannot delete branch while running")
	}
	manager, ok := a.opts.Session.(session.BranchManagementStore)
	if !ok {
		return errors.New("agent: session does not support branch management")
	}
	if err := manager.DeleteBranch(branchID); err != nil {
		return err
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
}

func rollbackFork(branches session.BranchStore, createdBranchID, oldBranchID string) error {
	if createdBranchID == "" || oldBranchID == "" {
		return errors.New("agent: cannot roll back incomplete fork")
	}
	if err := branches.SelectBranch(oldBranchID); err != nil {
		return fmt.Errorf("agent: restore branch after failed fork: %w", err)
	}
	deleter, ok := branches.(session.BranchRollbackStore)
	if !ok {
		return errors.New("agent: session cannot roll back failed fork")
	}
	if err := deleter.DeleteBranchForRollback(createdBranchID); err != nil {
		return fmt.Errorf("agent: delete failed fork: %w", err)
	}
	return nil
}

// Compact manually compacts the active branch. Goal continuation may also run
// the same compaction operation automatically at a configured context threshold.
// The active provider is asked for a concise summary; the local summarizer is
// used when that request fails, provided the context is still live.
func (a *Agent) Compact(ctx context.Context) (result protocol.CompactionResult, retErr error) {
	unlockAdmission := a.LockAdmission()
	admissionHeld := true
	defer func() {
		if admissionHeld {
			unlockAdmission()
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.RLock()
	pauseAutomaticGoal := a.autoRunning
	a.mu.RUnlock()
	if err := a.stopAutomaticForControl(ctx, "compact"); err != nil {
		if pauseAutomaticGoal && a.opts.Goal != nil {
			_ = a.opts.Goal.Defer(true)
		}
		return protocol.CompactionResult{}, err
	}
	// Manual compaction is an explicit control boundary. If it interrupted an
	// automatic goal worker, leave that goal durably paused instead of silently
	// starting a fresh provider request as soon as the summary completes.
	if pauseAutomaticGoal && a.opts.Goal != nil {
		if err := a.opts.Goal.Defer(true); err != nil {
			return protocol.CompactionResult{}, fmt.Errorf("agent: pause goal after compact: %w", err)
		}
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return protocol.CompactionResult{}, errors.New("agent: cannot compact while running")
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = false
	a.autoStop = false
	a.turnOrigin, a.turnID = "compact", newID()
	a.turnWG.Add(1)
	runCtx, cancel := context.WithCancel(ctx)
	a.activeCancel = cancel
	a.activeDone = make(chan struct{})
	a.mu.Unlock()
	unlockAdmission()
	admissionHeld = false
	defer func() {
		wasCanceled := runCtx.Err() != nil
		cancel()
		retErr = errors.Join(retErr, a.finishTurnMailbox(func() {
			a.running = false
			a.queueAccepting = false
			a.activeCancel = nil
			a.goalAtTurn = nil
			if a.activeDone != nil {
				close(a.activeDone)
				a.activeDone = nil
			}
		}))
		a.turnWG.Done()
		if pauseAutomaticGoal && wasCanceled && a.opts.Goal != nil {
			_ = a.opts.Goal.Defer(true)
		}
	}()
	ctx = runCtx

	return a.compactActiveContext(ctx, false)
}

func latestPersistedContextTokens(messages []protocol.Message) int {
	// Usage before a projected compaction summary describes the old full
	// request. Usage after that marker is valid for the newly grown context.
	start := 0
	if len(messages) > 0 && messages[0].Role == protocol.RoleCustom && strings.HasPrefix(messageTextBlocks(messages[0]), "Conversation summary:") {
		// The projection places retained pre-marker tail messages after the
		// summary too. Only messages whose parent chain starts at the compaction
		// marker were requested after compaction and have valid occupancy usage.
		markerID := strings.TrimPrefix(messages[0].ID, "compaction-")
		start = len(messages)
		for i := 1; i < len(messages); i++ {
			if messages[i].ParentID == markerID {
				start = i
				break
			}
		}
	}
	for i := len(messages) - 1; i >= start; i-- {
		if messages[i].Usage == nil {
			continue
		}
		usage := *messages[i].Usage
		if usage.Input > 0 {
			return usage.Input
		}
		if tokens := contextTokensForCompaction(usage); tokens > 0 {
			return tokens
		}
	}
	return 0
}

func (a *Agent) goalAutoCompactionDue(messages []protocol.Message) bool {
	threshold := a.opts.Compaction.GoalAutoThresholdPercent
	model := a.Model()
	if threshold == 0 || model.ContextWindow <= 0 {
		return false
	}
	a.mu.Lock()
	if a.latestContextTokens <= 0 {
		a.latestContextTokens = latestPersistedContextTokens(messages)
	}
	current := a.latestContextTokens
	stopped := a.autoStop
	mode := a.mode
	a.mu.Unlock()
	return current > 0 && !stopped && mode == protocol.ModeDefault && int64(current)*100 >= int64(model.ContextWindow)*int64(threshold)
}

// autoCompactAdmittedGoalBoundary compacts at a safe boundary inside an
// already-admitted goal turn. Goal turns can contain many provider/tool cycles,
// so checking only between autonomous turns allows context to overrun first.
func (a *Agent) autoCompactAdmittedGoalBoundary(ctx context.Context, messages []protocol.Message) (bool, error) {
	a.mu.RLock()
	admittedGoal := a.goalAtTurn.Clone()
	a.mu.RUnlock()
	if admittedGoal == nil || !a.goalAutoCompactionDue(messages) || a.opts.Goal == nil {
		return false, nil
	}
	goal, err := a.opts.Goal.Get()
	if err != nil || goal == nil || goal.GoalID != admittedGoal.GoalID || goal.Status != protocol.GoalActive {
		return false, err
	}
	result, err := a.compactActiveContext(ctx, true)
	if err != nil {
		return true, err
	}
	if result.SummarizedMessages > 0 {
		a.mu.Lock()
		a.latestContextTokens = 0
		a.mu.Unlock()
	}
	return true, nil
}

func (a *Agent) autoCompactGoalBoundary(ctx context.Context) (bool, error) {
	if !a.goalAutoCompactionDue(nil) {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.Lock()
	if a.closed || a.running || a.autoStop || a.mode != protocol.ModeDefault {
		a.mu.Unlock()
		return false, nil
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = false
	a.turnOrigin, a.turnID = "compact", newID()
	a.turnWG.Add(1)
	runCtx, cancel := context.WithCancel(ctx)
	a.activeCancel = cancel
	a.activeDone = make(chan struct{})
	a.mu.Unlock()
	defer func() {
		cancel()
		_ = a.finishTurnMailbox(func() {
			a.running = false
			a.queueAccepting = false
			a.activeCancel = nil
			a.goalAtTurn = nil
			if a.activeDone != nil {
				close(a.activeDone)
				a.activeDone = nil
			}
		})
		a.turnWG.Done()
	}()

	result, err := a.compactActiveContext(runCtx, true)
	if err != nil {
		return true, err
	}
	if result.SummarizedMessages > 0 {
		a.mu.Lock()
		a.latestContextTokens = 0
		a.mu.Unlock()
	}
	return true, nil
}

func (a *Agent) compactActiveContext(ctx context.Context, automatic bool) (protocol.CompactionResult, error) {
	msgs, err := a.contextMessagesCurrent()
	if err != nil {
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact load context: %w", err)
	}
	model := a.Model()
	budget := a.opts.Compaction.RetainTokens
	if budget <= 0 {
		budget = model.ContextWindow / 4
		if budget < 8*1024 {
			budget = 8 * 1024
		}
		if budget > 32*1024 {
			budget = 32 * 1024
		}
	}
	if model.ContextWindow > 0 && budget > model.ContextWindow/2 {
		budget = model.ContextWindow / 2
	}
	minTurns := a.opts.Compaction.MinRetainedTurns
	if minTurns <= 0 {
		minTurns = 2
	}
	plan := compact.PlannerWithOptions(msgs, compact.PlannerOptions{RetainTokens: budget, MinRetainedTurns: minTurns})
	result := protocol.CompactionResult{SummarizedMessages: len(plan.CompactionCandidates), RetainedMessages: len(msgs) - len(plan.CompactionCandidates), Automatic: automatic}
	message := fmt.Sprintf("compacting %d messages", result.SummarizedMessages)
	if automatic {
		message = fmt.Sprintf("goal context reached %d%%; %s", a.opts.Compaction.GoalAutoThresholdPercent, message)
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvCompactionStarted, Message: message})
	if len(plan.CompactionCandidates) == 0 {
		if automatic {
			err := errors.New("context threshold reached but no complete older turns are available to compact")
			a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: err.Error(), IsError: true, Compaction: &result})
			return result, err
		}
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Compaction: &result})
		return result, nil
	}

	summary, summaryErr := a.summarizeForCompaction(ctx, plan.CompactionCandidates)
	usedFallback := false
	if summaryErr == nil && strings.TrimSpace(summary) == "" {
		summaryErr = errors.New("provider returned a blank compaction summary")
	}
	if summaryErr != nil && a.opts.Compaction.Fallback != "error" {
		if ctx.Err() != nil {
			a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: ctx.Err().Error(), IsError: true, Compaction: &result})
			return protocol.CompactionResult{}, ctx.Err()
		}
		summary, summaryErr = compact.DefaultSummarizer(ctx, plan.CompactionCandidates)
		usedFallback = summaryErr == nil
	}
	if summaryErr != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: summaryErr.Error(), IsError: true, Compaction: &result})
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact summary: %w", summaryErr)
	}
	result.Summary = summary
	result.UsedFallback = usedFallback
	if _, err = compact.Apply(ctx, a.opts.Session, func(context.Context, []protocol.Message) (string, error) { return summary, nil }, plan); err != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: err.Error(), IsError: true, Compaction: &result})
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact apply: %w", err)
	}
	message = ""
	if usedFallback {
		message = "provider summary failed; used local fallback"
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: message, Compaction: &result})
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return result, nil
}

func (a *Agent) summarizeForCompaction(ctx context.Context, msgs []protocol.Message) (string, error) {
	p := a.currentProvider()
	creds, err := p.Resolve(ctx, a.resolveCreds(ctx))
	if err != nil {
		return "", err
	}
	contract := `Create factual continuation context for a coding agent, not a conversational recap. Use compact sections for: user objective and constraints; decisions with rationale; exact files and symbols changed; commands/tests and outcomes; important tool results; errors and failed approaches; current repository state; and unresolved next steps. Preserve exact identifiers and paths when known. Do not invent facts or call tools.`
	if guidance := strings.TrimSpace(a.opts.Compaction.Guidance); guidance != "" {
		contract += "\n\nAdditional operator guidance (additive; the contract above remains mandatory):\n" + guidance
	}
	maxTokens := a.opts.Compaction.SummaryMaxTokens
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	model := a.Model()
	if model.ContextWindow > 0 && maxTokens > model.ContextWindow/4 {
		maxTokens = model.ContextWindow / 4
	}
	if maxTokens < 128 {
		maxTokens = 128
	}
	req := protocol.ChatRequest{
		Model:              model,
		Messages:           msgs,
		System:             contract,
		MaxTokens:          maxTokens,
		Thinking:           protocol.ThinkingOff,
		SessionAffinityKey: a.requestAffinityKey("compaction"),
	}
	stream, err := p.Chat(ctx, creds, req)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var out strings.Builder
	for {
		ev, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		switch ev.Type {
		case protocol.EvStreamTextDelta:
			out.WriteString(ev.Text)
		case protocol.EvStreamError:
			if ev.Err != nil {
				return "", ev.Err
			}
			return "", errors.New("provider summary failed")
		}
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("provider summary returned no text")
	}
	return strings.TrimSpace(out.String()), nil
}

func (a *Agent) turnCompletionLocked() (string, string, *protocol.Usage) {
	var usage *protocol.Usage
	if a.usageSet {
		usage = a.turnUsage.Clone()
	}
	return a.turnOrigin, a.turnID, usage
}

func (a *Agent) publishTurnDone(continuing bool, origin, id string, usage *protocol.Usage) {
	a.publish(protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: usage, TurnOrigin: origin, TurnID: id, GoalContinuing: continuing})
}

func (a *Agent) clearCompletedTurnIdentity(id string) {
	a.mu.Lock()
	if !a.running && a.turnID == id {
		a.turnID = ""
		a.turnOrigin = ""
	}
	a.mu.Unlock()
}

// Prompt runs a full user turn in the active collaboration mode.
func (a *Agent) Prompt(ctx context.Context, text string) error {
	return a.prompt(ctx, text, nil, nil)
}

// PromptContent runs a full user turn with text and image content blocks.
func (a *Agent) PromptContent(ctx context.Context, text string, attachments []protocol.ContentBlock) error {
	return a.prompt(ctx, text, cloneContentBlocks(attachments), nil)
}

// RunMailbox starts one turn from already-persisted attributed mailbox input.
// It is used by subagent follow-up scheduling and never creates an anonymous
// user message.
func (a *Agent) RunMailbox(ctx context.Context) (retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlock := a.LockAdmission()
	admissionHeld := true
	defer func() {
		if admissionHeld {
			unlock()
		}
	}()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("agent: closed")
	}
	if a.running {
		a.mu.Unlock()
		return errors.New("agent: already running")
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = true
	a.turnWG.Add(1)
	a.turnMode = a.mode
	a.turnOrigin, a.turnID = "subagent", newID()
	a.turnStarted = time.Now()
	a.turnPlanSeen = false
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.toolStarts = make(map[string]time.Time)
	a.turnUsage = protocol.Usage{}
	a.usageSet = false
	a.turnProgress = false
	runCtx, cancel := context.WithCancel(ctx)
	a.activeCancel = cancel
	a.activeDone = make(chan struct{})
	a.mu.Unlock()
	if err := a.drainMailboxForProvider(); err != nil {
		a.mu.Lock()
		a.running = false
		a.queueAccepting = false
		a.activeCancel = nil
		close(a.activeDone)
		a.activeDone = nil
		a.mu.Unlock()
		a.turnWG.Done()
		cancel()
		return err
	}
	unlock()
	admissionHeld = false
	defer func() {
		a.closeInputQueue(true)
		cancel()
		retErr = errors.Join(retErr, a.drainMailbox())
		var origin, turnID string
		var usage *protocol.Usage
		retErr = errors.Join(retErr, a.finishTurnMailbox(func() {
			origin, turnID, usage = a.turnCompletionLocked()
			a.running = false
			a.activeCancel = nil
			if a.activeDone != nil {
				close(a.activeDone)
				a.activeDone = nil
			}
		}))
		a.publishTurnDone(false, origin, turnID, usage)
		a.clearCompletedTurnIdentity(turnID)
		a.turnWG.Done()
	}()
	return a.run(runCtx)
}

// PromptWithMode atomically applies a mode and starts the user turn, avoiding
// a SetMode/Prompt race for SDK, RPC, and TUI transitions.
func (a *Agent) PromptWithMode(ctx context.Context, text string, mode protocol.CollaborationMode) error {
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	return a.prompt(ctx, text, nil, &parsed)
}

// PromptContentWithMode atomically applies a mode and starts a user turn with
// mixed text/image content.
func (a *Agent) PromptContentWithMode(ctx context.Context, text string, attachments []protocol.ContentBlock, mode protocol.CollaborationMode) error {
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	return a.prompt(ctx, text, cloneContentBlocks(attachments), &parsed)
}

// TryInternalTurn atomically starts one private goal continuation without a
// visible or persisted user message.
func (a *Agent) TryInternalTurn(ctx context.Context) error { return a.internalTurn(ctx, false) }

func cloneContentBlocks(blocks []protocol.ContentBlock) []protocol.ContentBlock {
	cloned := make([]protocol.ContentBlock, len(blocks))
	for i, block := range blocks {
		cloned[i] = block
		cloned[i].Data = append([]byte(nil), block.Data...)
		cloned[i].Arguments = append(json.RawMessage(nil), block.Arguments...)
	}
	return cloned
}

const (
	maxUserImageBytes      = 20 << 20
	maxUserImageTotalBytes = 40 << 20
	maxUserImages          = 8
)

func validateUserAttachments(model protocol.Model, attachments []protocol.ContentBlock) error {
	if len(attachments) > maxUserImages {
		return fmt.Errorf("agent: at most %d image attachments are allowed", maxUserImages)
	}
	total := 0
	for _, block := range attachments {
		if block.Type != protocol.BlockImage {
			return fmt.Errorf("agent: unsupported user attachment type %q", block.Type)
		}
		if !model.SupportsVision {
			return fmt.Errorf("agent: model %q does not support image input", model.ID)
		}
		switch block.MIMEType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return fmt.Errorf("agent: unsupported image MIME type %q", block.MIMEType)
		}
		if len(block.Data) == 0 {
			return errors.New("agent: image attachment is empty")
		}
		if len(block.Data) > maxUserImageBytes {
			return fmt.Errorf("agent: image attachment exceeds %d MiB limit", maxUserImageBytes>>20)
		}
		total += len(block.Data)
		if total > maxUserImageTotalBytes {
			return fmt.Errorf("agent: image attachments exceed %d MiB aggregate limit", maxUserImageTotalBytes>>20)
		}
	}
	return nil
}

func (a *Agent) prompt(ctx context.Context, text string, attachments []protocol.ContentBlock, requestedMode *protocol.CollaborationMode) (retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlockAdmission := a.LockAdmission()
	admissionHeld := true
	defer func() {
		if admissionHeld {
			unlockAdmission()
		}
	}()
	reentrantEventCallback := a.bus.InCallback()
	if strings_trim(text) == "" && len(attachments) == 0 {
		return errors.New("agent: empty prompt")
	}

	// Validate without disturbing admitted user work or a running goal. An
	// unsupported prompt must not stop an automatic goal and leave it idle.
	a.mu.RLock()
	closed, running, wasAutomatic := a.closed, a.running, a.autoRunning
	prospectiveMode := a.mode
	if requestedMode != nil {
		prospectiveMode = *requestedMode
	}
	level := a.effectiveThinkingLocked(prospectiveMode)
	model := a.model
	a.mu.RUnlock()
	if closed {
		return fmt.Errorf("%w: agent closed", ErrPromptRejected)
	}
	if running && !wasAutomatic {
		return fmt.Errorf("%w: agent already running", ErrPromptRejected)
	}
	if !model.SupportsThinkingLevel(level) {
		return errors.Join(ErrPromptRejected, unsupportedThinkingError(model, level))
	}
	if err := validateUserAttachments(model, attachments); err != nil {
		return errors.Join(ErrPromptRejected, err)
	}
	a.stopAutomatic(false)

	// Claim the running flag BEFORE applying the attached mode or appending so
	// concurrent callers cannot observe a half-applied transition.
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("agent: closed")
	}
	if a.running {
		a.mu.Unlock()
		return errors.New("agent: already running")
	}
	previousMode := a.mode
	modeApplied := false
	rollbackAdmission := func(cause error) error {
		var rollbackErr error
		if modeApplied {
			if state, ok := a.opts.Session.(session.ThreadStateStore); ok {
				if err := state.SetCollaborationMode(previousMode); err != nil {
					rollbackErr = fmt.Errorf("agent: restore collaboration mode: %w", err)
				}
			}
			if rollbackErr == nil {
				a.mode = previousMode
				a.turnMode = previousMode
			}
		}
		resumeAutomatic := wasAutomatic && a.mode == protocol.ModeDefault
		modeAfter, effortAfter := a.mode, a.effectiveThinkingLocked(a.mode)
		publishMode := modeApplied && rollbackErr != nil
		a.mu.Unlock()
		if publishMode {
			a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: modeAfter, ReasoningEffort: effortAfter}})
		}
		if resumeAutomatic {
			a.ContinueGoal()
		}
		return errors.Join(cause, rollbackErr)
	}
	if requestedMode != nil {
		if state, ok := a.opts.Session.(session.ThreadStateStore); ok {
			if err := state.SetCollaborationMode(*requestedMode); err != nil {
				a.mu.Unlock()
				if wasAutomatic {
					a.ContinueGoal()
				}
				return errors.Join(ErrPromptRejected, err)
			}
		}
		a.mode = *requestedMode
		modeApplied = *requestedMode != previousMode
	}
	a.turnMode = a.mode
	level = a.effectiveThinkingLocked(a.turnMode)
	if !a.model.SupportsThinkingLevel(level) {
		return rollbackAdmission(unsupportedThinkingError(a.model, level))
	}
	// A normal prompt may temporarily take admission from automatic goal work,
	// but it must never clear a durable abort/manual-compaction deferral. Only an
	// explicit goal continue/resume operation may make deferred work eligible.
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = true
	// Stopping the previous automatic worker is an admission barrier for this
	// user turn, not a permanent abort of subsequent goal continuation.
	a.autoStop = false
	a.turnWG.Add(1)
	runCtx, cancel := context.WithCancel(ctx)
	a.activeCancel = cancel
	a.activeDone = make(chan struct{})
	a.turnOrigin, a.turnID = "user", newID()
	a.goalAtTurn = nil
	if a.turnMode != protocol.ModePlan && a.opts.Goal != nil {
		if g, _ := a.opts.Goal.Get(); g != nil && g.Status == protocol.GoalActive {
			a.goalAtTurn = g
			if a.goalTurnID != g.GoalID {
				a.goalTurnID = g.GoalID
				a.goalTurn = 0
			}
			a.goalTurn++
			a.opts.Goal.RecordGoalTurn(g.GoalID)
		}
	}
	a.turnStarted = time.Now()
	a.turnPlanSeen = false
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.toolStarts = make(map[string]time.Time)
	a.turnUsage = protocol.Usage{}
	a.usageSet = false
	a.turnProgress = false
	a.baseDeferred = nil
	a.searchedDeferred = nil
	modeChanged := requestedMode != nil
	mode := a.mode
	a.mu.Unlock()
	unlockAdmission()
	admissionHeld = false
	if modeChanged {
		a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: level}})
	}
	a.prepareToolRouting(ctx, text)

	// Ensure we stop running on any exit.
	defer func() {
		a.closeInputQueue(true)
		cancel()
		// Persist any mail that arrived after the final provider request before
		// releasing turn admission. This keeps delivery ordered and durable for
		// the next user/follow-up turn without racing tool-result chaining.
		retErr = errors.Join(retErr, a.drainMailbox())
		continuing, accountingErr := a.finalizeGoalTurn(retErr, true)
		retErr = errors.Join(retErr, accountingErr)
		var origin, turnID string
		var usage *protocol.Usage
		retErr = errors.Join(retErr, a.finishTurnMailbox(func() {
			origin, turnID, usage = a.turnCompletionLocked()
			a.running = false
			a.activeCancel = nil
			a.goalAtTurn = nil
			if a.activeDone != nil {
				close(a.activeDone)
				a.activeDone = nil
			}
		}))
		// Queue completion before a continuation can overwrite turn metadata.
		a.publishTurnDone(continuing, origin, turnID, usage)
		a.clearCompletedTurnIdentity(turnID)
		a.turnWG.Done()
		if continuing {
			a.ContinueGoal()
		}
		if !reentrantEventCallback {
			_ = a.bus.Drain(context.Background())
		}
	}()

	content := make([]protocol.ContentBlock, 0, 1+len(attachments))
	if text != "" {
		content = append(content, protocol.NewTextBlock(text))
	}
	content = append(content, attachments...)
	userMsg := protocol.NewUserContentMessage(newID(), "", content)
	// A previous turn may have marked itself idle while it is still flushing
	// final mailbox mail. Serialize this first user append with that flush so
	// the next provider context cannot outrun attributed completion mail.
	a.mailboxPersistMu.Lock()
	appendErr := a.opts.Session.Append(session.Entry{
		Type:     session.EntryMessage,
		ID:       userMsg.ID,
		ParentID: "",
		Message:  &userMsg,
	})
	a.mailboxPersistMu.Unlock()
	if appendErr != nil {
		return fmt.Errorf("agent: append user message: %w", appendErr)
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if err := a.activateExplicitSkillMentions(runCtx, text); err != nil {
		return fmt.Errorf("agent: activate explicit skill: %w", err)
	}

	retErr = a.run(runCtx)
	return retErr
}

func (a *Agent) internalTurn(ctx context.Context, budgetWrap bool) (retErr error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errors.New("agent: closed")
	}
	if a.running {
		a.mu.Unlock()
		return errors.New("agent: already running")
	}
	if a.mode == protocol.ModePlan {
		a.mu.Unlock()
		return errors.New("agent: automatic turns are not allowed in Plan mode")
	}
	if a.opts.Goal == nil {
		a.mu.Unlock()
		return errors.New("agent: goal controller unavailable")
	}
	g, err := a.opts.Goal.Get()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if g == nil || (!budgetWrap && g.Status != protocol.GoalActive) || (budgetWrap && g.Status != protocol.GoalBudgetLimited) {
		a.mu.Unlock()
		return errors.New("agent: no continuable active goal")
	}
	if !a.goalToolsAvailableLocked() {
		a.mu.Unlock()
		return errors.New("agent: goal continuation requires get_goal, create_goal, and update_goal tools")
	}
	deferred, _ := a.opts.Goal.Deferred()
	if deferred && !budgetWrap {
		a.mu.Unlock()
		return errors.New("agent: goal continuation deferred")
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = true
	a.turnWG.Add(1)
	a.turnMode = a.mode
	a.turnOrigin, a.turnID = "goal", newID()
	a.goalAtTurn, a.turnStarted = g, time.Now()
	if a.goalTurnID != g.GoalID {
		a.goalTurnID = g.GoalID
		a.goalTurn = 0
	}
	a.goalTurn++
	a.opts.Goal.RecordGoalTurn(g.GoalID)
	a.budgetWrap = budgetWrap
	runCtx, cancel := context.WithCancel(ctx)
	a.activeCancel = cancel
	a.activeDone = make(chan struct{})
	a.turnPlanSeen = false
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.toolStarts = make(map[string]time.Time)
	a.turnUsage = protocol.Usage{}
	a.usageSet = false
	a.turnProgress = false
	a.mu.Unlock()
	a.publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g.Clone()}, TurnOrigin: "goal", TurnID: a.turnID, GoalContinuing: true})
	defer func() {
		a.closeInputQueue(true)
		cancel()
		retErr = errors.Join(retErr, a.drainMailbox())
		continuing, accountingErr := a.finalizeGoalTurn(retErr, false)
		retErr = errors.Join(retErr, accountingErr)
		var origin, turnID string
		var usage *protocol.Usage
		retErr = errors.Join(retErr, a.finishTurnMailbox(func() {
			origin, turnID, usage = a.turnCompletionLocked()
			a.running = false
			a.activeCancel = nil
			a.goalAtTurn = nil
			if a.activeDone != nil {
				close(a.activeDone)
				a.activeDone = nil
			}
		}))
		a.publishTurnDone(continuing, origin, turnID, usage)
		a.clearCompletedTurnIdentity(turnID)
		a.turnWG.Done()
	}()
	retErr = a.run(runCtx)
	return retErr
}

func (a *Agent) goalToolsAvailableLocked() bool {
	for _, name := range []string{"get_goal", "create_goal", "update_goal"} {
		if _, ok := a.opts.Registry.Get(name); !ok {
			return false
		}
	}
	return true
}

func (a *Agent) stopGoalOnError(turnErr error) {
	a.mu.RLock()
	g := a.goalAtTurn.Clone()
	controller := a.opts.Goal
	a.mu.RUnlock()
	if g == nil || controller == nil || g.Status != protocol.GoalActive || errors.Is(turnErr, context.Canceled) {
		return
	}
	status := protocol.GoalBlocked
	var limited provider.UsageLimitedError
	if errors.As(turnErr, &limited) && limited.UsageLimited() {
		status = protocol.GoalUsageLimited
	}
	_, _ = controller.SetStatus(g.GoalID, status, false)
}

func (a *Agent) finalizeGoalTurn(turnErr error, userOrigin bool) (bool, error) {
	crossed, accountingErr := a.finishGoalAccounting()
	if accountingErr != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal accounting: " + accountingErr.Error()})
		a.stopGoalOnError(accountingErr)
	}
	if turnErr != nil || accountingErr != nil {
		// Accounting always precedes terminal classification, but a failed
		// request must never launch a budget wrap-up or another autonomous turn.
		a.mu.Lock()
		a.budgetWrap = false
		a.mu.Unlock()
		if accountingErr == nil && !errors.Is(turnErr, context.Canceled) && !crossed {
			a.stopGoalOnError(turnErr)
		}
	}
	a.mu.Lock()
	progress := a.turnProgress
	goalID := ""
	if a.goalAtTurn != nil {
		goalID = a.goalAtTurn.GoalID
	}
	if !userOrigin && a.turnOrigin == "goal" {
		if a.autoEmptyGoal != goalID {
			a.autoEmptyGoal = goalID
			a.autoEmpty = 0
		}
		if progress {
			a.autoEmpty = 0
		} else {
			a.autoEmpty++
		}
	}
	empty := a.autoEmpty
	stopped := a.autoStop
	controller := a.opts.Goal
	mode := a.turnMode
	a.mu.Unlock()
	if controller == nil || mode == protocol.ModePlan || stopped || turnErr != nil || accountingErr != nil {
		return false, accountingErr
	}
	g, err := controller.Get()
	if err != nil || g == nil {
		return false, accountingErr
	}
	if empty >= 3 && g.Status == protocol.GoalActive {
		// Empty output is not proof of an external blocker. Pause conservatively
		// rather than falsely claiming the model's three-turn blocked audit.
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal continuation paused after three turns with no text or tool progress"})
		_, _ = controller.SetStatus(g.GoalID, protocol.GoalPaused, false)
		return false, accountingErr
	}
	deferred, _ := controller.Deferred()
	a.mu.RLock()
	toolsAvailable := a.goalToolsAvailableLocked()
	a.mu.RUnlock()
	return toolsAvailable && !deferred && (g.Status == protocol.GoalActive || (crossed && g.Status == protocol.GoalBudgetLimited)), accountingErr
}
func (a *Agent) finishGoalAccounting() (bool, error) {
	a.mu.RLock()
	g := a.goalAtTurn.Clone()
	started, usage, usageSet, mode := a.turnStarted, a.turnUsage, a.usageSet, a.turnMode
	a.mu.RUnlock()
	if g == nil || mode == protocol.ModePlan || a.opts.Goal == nil {
		return false, nil
	}
	tokens := int64(0)
	if usageSet {
		tokens = int64(usage.Total)
		if tokens == 0 {
			tokens = int64(usage.Input + usage.Output)
		}
	}
	updated, crossed, err := a.opts.Goal.AccountDuration(g.GoalID, tokens, time.Since(started), usage.Cost.Clone())
	if err != nil {
		return false, err
	}
	if crossed && updated != nil {
		a.mu.Lock()
		a.budgetWrap = true
		a.mu.Unlock()
	}
	return crossed, nil
}

func (a *Agent) ResetGoalAudit() {
	a.mu.Lock()
	a.goalTurn = 0
	a.goalTurnID = ""
	a.autoEmpty = 0
	a.autoEmptyGoal = ""
	a.mu.Unlock()
}

func (a *Agent) ContinueGoal() {
	a.mu.Lock()
	if a.closed || a.mode == protocol.ModePlan || a.opts.Goal == nil {
		a.mu.Unlock()
		return
	}
	if a.autoRunning {
		a.autoPending = true
		a.mu.Unlock()
		return
	}
	a.autoRunning = true
	a.autoStop = false
	a.autoPending = false
	a.autoDone = make(chan struct{})
	done := a.autoDone
	a.autoWG.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.autoWG.Done()
		a.mu.Lock()
		wrap := a.budgetWrap
		a.budgetWrap = false
		stopped := a.autoStop
		a.mu.Unlock()
		if stopped {
			a.finishAutoWorker(done)
			return
		}
		for {
			a.mu.RLock()
			stopped = a.autoStop
			a.mu.RUnlock()
			if stopped {
				break
			}
			if err := a.internalTurn(context.Background(), wrap); err != nil {
				break
			}
			a.mu.Lock()
			crossed := a.budgetWrap
			a.budgetWrap = false
			a.mu.Unlock()
			if crossed && !wrap {
				wrap = true
				continue
			}
			g, err := a.opts.Goal.Get()
			if err != nil || g == nil || g.Status != protocol.GoalActive {
				break
			}
			if compacted, compactErr := a.autoCompactGoalBoundary(context.Background()); compactErr != nil {
				a.mu.RLock()
				stopped = a.autoStop
				a.mu.RUnlock()
				if stopped || errors.Is(compactErr, context.Canceled) {
					break
				}
				a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal auto-compaction: " + compactErr.Error()})
				_, _ = a.opts.Goal.SetStatus(g.GoalID, protocol.GoalBlocked, false)
				break
			} else if compacted {
				g, err = a.opts.Goal.Get()
				if err != nil || g == nil || g.Status != protocol.GoalActive {
					break
				}
			}
			// Yield between autonomous requests even when a provider returns
			// immediately; productive goals remain unbounded but cannot hot-spin.
			time.Sleep(automaticTurnDelay)
			wrap = false
		}
		a.finishAutoWorker(done)
	}()
}
func (a *Agent) finishAutoWorker(done chan struct{}) {
	a.mu.Lock()
	restart := a.autoPending && !a.autoStop && !a.closed && a.mode == protocol.ModeDefault
	a.autoRunning = false
	a.autoPending = false
	a.autoDone = nil
	close(done)
	a.mu.Unlock()
	if restart {
		a.ContinueGoal()
	}
}

// StopGoal cancels and joins current goal work, including the pre-first-turn window.
func (a *Agent) StopGoal(ctx context.Context, deferGoal bool) error {
	return a.stopWork(ctx, deferGoal, false)
}

func (a *Agent) stopWork(ctx context.Context, deferGoal, anyTurn bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	a.autoStop = true
	a.autoPending = false
	cancel := a.activeCancel
	done := a.autoDone
	activeDone := a.activeDone
	goal := a.goalAtTurn.Clone()
	automatic := a.autoRunning
	compacting := a.turnOrigin == "compact" && a.running
	controlled := automatic || goal != nil || compacting || (anyTurn && a.running)
	if controlled {
		a.queueAccepting = false
	}
	controller := a.opts.Goal
	a.mu.Unlock()
	if cancel != nil && controlled {
		cancel()
	}
	if !controlled {
		activeDone = nil
	}
	var deferErr error
	// Persist the user's intent before joining so a caller deadline cannot
	// leave a pre-first-turn or compaction-suspended goal eligible to restart.
	shouldDefer := automatic || goal != nil
	if deferGoal && compacting && controller != nil {
		if current, err := controller.Get(); err != nil {
			deferErr = err
		} else if current != nil && current.Status == protocol.GoalActive {
			shouldDefer = true
		}
	}
	if deferGoal && shouldDefer && controller != nil && deferErr == nil {
		deferErr = controller.Defer(true)
	}
	wait := func(ch <-chan struct{}) error {
		if ch == nil {
			return nil
		}
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := wait(done); err != nil {
		return errors.Join(err, deferErr)
	}
	if err := wait(activeDone); err != nil {
		return errors.Join(err, deferErr)
	}
	return deferErr
}
func (a *Agent) stopAutomatic(deferGoal bool) {
	a.mu.RLock()
	automatic := a.autoRunning
	a.mu.RUnlock()
	if automatic {
		_ = a.StopGoal(context.Background(), deferGoal)
	}
}
func (a *Agent) Abort()                                 { _ = a.stopWork(context.Background(), true, true) }
func (a *Agent) AbortContext(ctx context.Context) error { return a.stopWork(ctx, true, true) }
func (a *Agent) WaitGoal(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.RLock()
	done := a.autoDone
	a.mu.RUnlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) Close() {
	reentrantEventCallback := a.bus.InCallback()
	unlockAdmission := a.LockAdmission()
	a.mu.Lock()
	a.closed = true
	a.queueAccepting = false
	cancel := a.activeCancel
	if a.autoRunning {
		a.autoStop = true
	}
	a.mu.Unlock()
	unlockAdmission()
	if cancel != nil {
		cancel()
	}
	a.autoWG.Wait()
	a.turnWG.Wait()
	a.mailboxMu.Lock()
	if !a.mailboxClosed {
		a.mailboxClosed = true
		close(a.mailboxActivity)
	}
	a.mailboxMu.Unlock()
	a.bus.Close()
	if !reentrantEventCallback {
		a.bus.Wait()
	}
}

func (a *Agent) prepareToolRouting(ctx context.Context, query string) {
	router := a.opts.Router
	if router == nil || router.DeferredCount() == 0 {
		return
	}
	started := time.Now()
	candidateLimit := max(deferredCandidateK, router.DeferredCount())
	candidates, err := router.Search(ctx, query, candidateLimit)
	latency := time.Since(started).Milliseconds()
	fallback := err != nil
	selected := a.selectPermittedMatches(candidates, defaultDeferredTopK)
	if fallback {
		selected = a.allPermittedDeferred()
	}
	ids := matchIDs(selected)
	a.mu.Lock()
	a.baseDeferred = append([]string(nil), ids...)
	a.searchedDeferred = nil
	a.mu.Unlock()
	a.publishToolRouting("automatic", ids, len(candidates), latency, fallback, err)
}

func (a *Agent) applyDiscoveryDetails(details any) {
	var discovery tools.DiscoveryDetails
	switch value := details.(type) {
	case tools.DiscoveryDetails:
		discovery = value
	case *tools.DiscoveryDetails:
		if value == nil {
			return
		}
		discovery = *value
	default:
		return
	}
	selected := a.selectPermittedMatches(discovery.Matches, defaultDeferredTopK)
	ids := matchIDs(selected)
	a.mu.Lock()
	a.searchedDeferred = append([]string(nil), ids...)
	a.mu.Unlock()
	a.publishToolRouting("search_tools", ids, discovery.CandidateCount, discovery.LatencyMS, false, nil)
}

func (a *Agent) selectPermittedMatches(matches []tools.ToolMatch, limit int) []tools.ToolMatch {
	selected := make([]tools.ToolMatch, 0, limit)
	seen := make(map[string]bool, limit)
	for _, match := range matches {
		if seen[match.ID] {
			continue
		}
		desc, ok := a.opts.Registry.Descriptor(match.ID)
		if !ok || !tools.IsDeferred(desc) || !tools.CanExpose(a.opts.Permission, desc) {
			continue
		}
		seen[match.ID] = true
		selected = append(selected, match)
		if len(selected) == limit {
			break
		}
	}
	return selected
}

func (a *Agent) allPermittedDeferred() []tools.ToolMatch {
	selected := make([]tools.ToolMatch, 0, a.opts.Router.DeferredCount())
	for _, desc := range a.opts.Registry.Descriptors() {
		if tools.IsDeferred(desc) && tools.CanExpose(a.opts.Permission, desc) {
			selected = append(selected, tools.ToolMatch{ID: desc.Schema.Name})
		}
	}
	return selected
}

func matchIDs(matches []tools.ToolMatch) []string {
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}
	return ids
}

func (a *Agent) requestToolSchemas() []protocol.ToolSchema {
	a.mu.RLock()
	mode := a.turnMode
	if !a.running {
		mode = a.mode
	}
	a.mu.RUnlock()
	allowed := func(name string) bool {
		if desc, ok := a.opts.Registry.Descriptor(name); ok && desc.Risk == permission.RiskDelegate && !tools.CanExpose(a.opts.Permission, desc) {
			return false
		}
		if mode == protocol.ModePlan {
			return name != "ask_user" && name != "update_plan"
		}
		return name != "request_user_input"
	}
	if a.opts.Router == nil {
		all := a.opts.Registry.Schemas()
		out := all[:0]
		for _, schema := range all {
			if allowed(schema.Name) {
				out = append(out, schema)
			}
		}
		return out
	}
	a.mu.RLock()
	base := append([]string(nil), a.baseDeferred...)
	searched := append([]string(nil), a.searchedDeferred...)
	a.mu.RUnlock()

	descriptors := a.opts.Registry.Descriptors()
	schemas := make([]protocol.ToolSchema, 0, len(descriptors))
	for _, desc := range descriptors {
		if !tools.IsDeferred(desc) && allowed(desc.Schema.Name) {
			schemas = append(schemas, desc.Schema)
		}
	}
	seen := make(map[string]bool, len(base)+len(searched))
	for _, name := range append(base, searched...) {
		if seen[name] {
			continue
		}
		desc, ok := a.opts.Registry.Descriptor(name)
		if !ok || !tools.IsDeferred(desc) || !tools.CanExpose(a.opts.Permission, desc) || !allowed(desc.Schema.Name) {
			continue
		}
		seen[name] = true
		schemas = append(schemas, desc.Schema)
	}
	return schemas
}

func (a *Agent) publishToolRouting(trigger string, ids []string, candidates int, latency int64, fallback bool, routeErr error) {
	schemas := a.requestToolSchemas()
	eventIDs := append([]string(nil), ids...)
	if len(eventIDs) > maxRoutingEventTools {
		eventIDs = eventIDs[:maxRoutingEventTools]
	}
	event := protocol.AgentEvent{
		Type: protocol.EvToolRouting,
		ToolRouting: &protocol.ToolRouting{
			Trigger:        trigger,
			ToolIDs:        eventIDs,
			CandidateCount: candidates,
			SelectedCount:  len(ids),
			ExposedCount:   len(schemas),
			SchemaBytes:    providerSchemaBytes(schemas),
			LatencyMS:      latency,
			Fallback:       fallback,
		},
	}
	if routeErr != nil {
		event.Message = boundRoutingMessage(routeErr.Error(), 2048)
	}
	a.publish(event)
}

func providerSchemaBytes(schemas []protocol.ToolSchema) int {
	providerSchemas := make([]struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}, len(schemas))
	for i, schema := range schemas {
		providerSchemas[i].Name = schema.Name
		providerSchemas[i].Description = schema.Description
		providerSchemas[i].Parameters = schema.Parameters
	}
	encoded, _ := json.Marshal(providerSchemas)
	return len(encoded)
}

func boundRoutingMessage(message string, max int) string {
	if max <= 0 || len(message) <= max {
		return message
	}
	return message[:max] + "…"
}

func (a *Agent) run(ctx context.Context) error {
	turn := 0
	for {
		if a.opts.MaxTurns > 0 && turn >= a.opts.MaxTurns {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "max turns reached"})
			return errors.New("agent: max turns reached")
		}
		turn++

		if err := a.drainMailboxForProvider(); err != nil {
			return err
		}
		msgs, err := a.contextMessagesCurrent()
		if err != nil {
			return fmt.Errorf("agent: load context: %w", err)
		}
		if compacted, compactErr := a.autoCompactAdmittedGoalBoundary(ctx, msgs); compactErr != nil {
			return fmt.Errorf("goal auto-compaction: %w", compactErr)
		} else if compacted {
			msgs, err = a.contextMessagesCurrent()
			if err != nil {
				return fmt.Errorf("agent: reload compacted context: %w", err)
			}
		}

		internalContext, err := a.goalInternalContext()
		if err != nil {
			return fmt.Errorf("agent: load goal context: %w", err)
		}
		req := protocol.ChatRequest{
			Model:              a.Model(),
			Messages:           msgs,
			Tools:              a.requestToolSchemas(),
			System:             a.requestSystemPrompt(),
			Thinking:           a.requestThinking(),
			ReasoningSummary:   a.ReasoningSummary(),
			TextVerbosity:      a.TextVerbosity(),
			InternalContext:    internalContext,
			SessionAffinityKey: a.requestAffinityKey("turn"),
		}

		// Call the provider (optionally with a merged retry on malformed args).
		stop, err := a.streamTurn(ctx, req)
		if err != nil {
			return err
		}
		naturalStop := false
		switch stop {
		case protocol.StopToolUse:
			// Steering never skips tool calls. Finish the complete serial batch,
			// including cancellation placeholders, before checking the queue.
			if err := a.executeToolCalls(ctx); err != nil {
				if ctx.Err() != nil {
					a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
				}
				return err
			}
		case protocol.StopStop, protocol.StopLength:
			naturalStop = true
		case protocol.StopAborted:
			return nil
		case protocol.StopError:
			return errors.New("agent: provider stopped with error")
		default:
			naturalStop = true
		}

		canContinue := a.opts.MaxTurns == 0 || turn < a.opts.MaxTurns
		queued, ok, limited := a.takeQueuedInput(naturalStop, canContinue)
		if limited {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "max turns reached"})
			return errors.New("agent: max turns reached")
		}
		if ok {
			if err := a.deliverQueuedInput(ctx, queued); err != nil {
				return err
			}
			continue
		}
		if naturalStop {
			// takeQueuedInput atomically closed admission with the final empty
			// check. A concurrent enqueue is therefore either consumed above or
			// rejected with ErrNotRunning, never stranded.
			return nil
		}
		// Tool-use without steering continues the ordinary tool-result chain.
	}
}

// takeQueuedInput selects one safe-boundary input. Steering always wins. A
// follow-up becomes eligible only after a natural provider stop. When a
// naturally stopped run has no eligible input, queue admission closes under
// the same lock as the empty check. limited reports an eligible input that
// cannot be persisted because the next provider request would exceed MaxTurns.
func (a *Agent) takeQueuedInput(naturalStop, canContinue bool) (item protocol.QueuedInput, ok, limited bool) {
	// Hold queuePublishMu across a successful selection and its durable append.
	// This makes the safe-boundary priority decision atomic with delivery: a
	// newly submitted steer cannot slip in after a follow-up was selected but
	// before that follow-up is persisted.
	a.queuePublishMu.Lock()
	a.mu.Lock()
	index := -1
	for i, item := range a.queuedInputs {
		if item.Kind == protocol.QueuedInputSteer {
			index = i
			break
		}
	}
	if index < 0 && naturalStop {
		for i, item := range a.queuedInputs {
			if item.Kind == protocol.QueuedInputFollowUp {
				index = i
				break
			}
		}
	}
	if index < 0 {
		if naturalStop {
			a.queueAccepting = false
		}
		a.mu.Unlock()
		a.queuePublishMu.Unlock()
		return protocol.QueuedInput{}, false, false
	}
	if !canContinue {
		a.mu.Unlock()
		a.queuePublishMu.Unlock()
		return protocol.QueuedInput{}, false, true
	}
	item = a.queuedInputs[index]
	a.mu.Unlock()
	return item, true, false
}

func (a *Agent) deliverQueuedInput(ctx context.Context, item protocol.QueuedInput) error {
	if err := ctx.Err(); err != nil {
		a.queuePublishMu.Unlock()
		return err
	}
	// takeQueuedInput reserved queuePublishMu across selection. Treat persistence
	// and removal as the remainder of that transaction, so interactive abort
	// either restores the item without a durable append or observes it removed.
	index := -1
	a.mu.Lock()
	if a.queueAccepting {
		for i, pending := range a.queuedInputs {
			if pending.ID == item.ID {
				index = i
				break
			}
		}
	}
	a.mu.Unlock()
	if index < 0 {
		a.queuePublishMu.Unlock()
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		a.queuePublishMu.Unlock()
		return err
	}
	msg := protocol.NewUserMessage(newID(), "", item.Text)
	a.mailboxPersistMu.Lock()
	err := a.opts.Session.Append(session.Entry{
		Type: session.EntryMessage, ID: msg.ID, ParentID: "", Message: &msg,
	})
	a.mailboxPersistMu.Unlock()
	if err != nil {
		a.queuePublishMu.Unlock()
		return fmt.Errorf("agent: append queued %s input: %w", item.Kind, err)
	}
	a.mu.Lock()
	// Re-find after persistence defensively, although queue mutation is excluded
	// by queuePublishMu for the whole transaction.
	for i, pending := range a.queuedInputs {
		if pending.ID != item.ID {
			continue
		}
		copy(a.queuedInputs[i:], a.queuedInputs[i+1:])
		a.queuedInputs = a.queuedInputs[:len(a.queuedInputs)-1]
		break
	}
	snapshot := a.inputQueueLocked()
	a.mu.Unlock()
	a.publishInputQueue(snapshot)
	a.queuePublishMu.Unlock()
	a.mu.Lock()
	// A queued Plan-mode instruction starts a fresh plan response inside the
	// same captured collaboration mode.
	a.turnPlanSeen = false
	a.mu.Unlock()
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if err := a.activateExplicitSkillMentions(ctx, item.Text); err != nil {
		return fmt.Errorf("agent: activate explicit skill from queued input: %w", err)
	}
	a.prepareToolRouting(ctx, item.Text)
	return nil
}

// streamTurn calls the provider and persists the assistant message; returns stop reason.
func (a *Agent) requestAffinityKey(purpose string) string {
	if a.opts.Session == nil || a.opts.Session.ID() == "" {
		return ""
	}
	branchID := ""
	if branches, ok := a.opts.Session.(session.ActiveBranchStore); ok {
		branchID = branches.ActiveBranchID()
	}
	sum := sha256.Sum256([]byte(a.opts.Session.ID() + "\x00" + branchID + "\x00" + purpose))
	return hex.EncodeToString(sum[:])
}

func (a *Agent) streamTurn(ctx context.Context, req protocol.ChatRequest) (protocol.StopReason, error) {
	a.mu.Lock()
	a.latestContextTokens = 0
	a.mu.Unlock()
	provider := a.currentProvider()
	creds, err := provider.Resolve(ctx, a.resolveCreds(ctx))
	if err != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
		return protocol.StopError, fmt.Errorf("agent: provider resolve: %w", err)
	}

	stream, err := provider.Chat(ctx, creds, req)
	if err != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
		return protocol.StopError, fmt.Errorf("agent: provider chat: %w", err)
	}
	defer stream.Close()

	asstID := newID()
	parent := a.opts.Session.BranchTip()
	var content []protocol.ContentBlock
	var providerData []protocol.ContentBlock
	var usage *protocol.Usage
	var stop protocol.StopReason = protocol.StopPending
	thinkingBuf := ""
	a.mu.RLock()
	planEnabled := a.turnMode == protocol.ModePlan && !a.turnPlanSeen
	a.mu.RUnlock()
	collector := newPlanStreamCollector(planEnabled, asstID+"-plan", a.publish, func() {
		a.mu.Lock()
		a.turnPlanSeen = true
		a.mu.Unlock()
	})
	toolCalls := map[string]protocol.ContentBlock{} // id -> block
	toolOrder := []string{}                         // first-seen id order

	for {
		ev, err := stream.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				stop = protocol.StopAborted
				collector.Interrupt()
				content = assistantResponseContentWithProviderData(thinkingBuf, providerData, collector.Blocks())
				if perr := a.persistAssistant(asstID, parent, content, stop, usage, ""); perr != nil {
					return protocol.StopAborted, perr
				}
				a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
				return protocol.StopAborted, nil
			}
			// Normal end of stream: io.EOF per the EventStream contract.
			if errors.Is(err, io.EOF) {
				break
			}
			// Stream error event
			stop = protocol.StopError
			collector.Interrupt()
			content = assistantResponseContentWithProviderData(thinkingBuf, providerData, collector.Blocks())
			if perr := a.persistAssistant(asstID, parent, content, stop, usage, err.Error()); perr != nil {
				return protocol.StopError, perr
			}
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
			return protocol.StopError, err
		}

		switch ev.Type {
		case protocol.EvStreamTextDelta:
			if strings.TrimSpace(ev.Text) != "" {
				a.mu.Lock()
				a.turnProgress = true
				a.mu.Unlock()
			}
			collector.Push(ev.Text)
		case protocol.EvStreamThinkingDelta:
			thinkingBuf += ev.Text
			a.publish(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: ev.Text})
		case protocol.EvStreamProviderData:
			if ev.ProviderData != nil && ev.ProviderData.Type == protocol.BlockProviderData {
				block := *ev.ProviderData
				block.Data = append([]byte(nil), ev.ProviderData.Data...)
				providerData = append(providerData, block)
			}
		case protocol.EvStreamToolCallDelta:
			cb, ok := toolCalls[ev.ToolCallID]
			if !ok {
				cb = protocol.ContentBlock{
					Type:       protocol.BlockToolCall,
					ToolCallID: ev.ToolCallID,
					Name:       ev.ToolName,
				}
				toolOrder = append(toolOrder, ev.ToolCallID)
			}
			if ev.Arguments != nil {
				cb.Arguments = append(cb.Arguments, ev.Arguments...)
			}
			toolCalls[ev.ToolCallID] = cb
		case protocol.EvStreamToolCallDone:
			a.mu.Lock()
			a.turnProgress = true
			a.mu.Unlock()
			cb, ok := toolCalls[ev.ToolCallID]
			if !ok {
				cb = protocol.ContentBlock{
					Type:       protocol.BlockToolCall,
					ToolCallID: ev.ToolCallID,
					Name:       ev.ToolName,
				}
				toolOrder = append(toolOrder, ev.ToolCallID)
			}
			if ev.Arguments != nil {
				cb.Arguments = ev.Arguments
			}
			if cb.Name == "" {
				cb.Name = ev.ToolName
			}
			toolCalls[ev.ToolCallID] = cb
		case protocol.EvStreamUsage:
			if ev.Usage != nil {
				normalized := *ev.Usage
				if normalized.Total == 0 {
					normalized.Total = normalized.Input + normalized.Output
				}
				if normalized.Cost == nil {
					normalized.Cost = normalized.CostFor(a.Model().Pricing)
				}
				usage = &normalized
				a.mu.Lock()
				a.latestContextTokens = contextTokensForCompaction(normalized)
				a.mu.Unlock()
				a.publish(protocol.AgentEvent{Type: protocol.EvUsage, Usage: normalized.Clone()})
			}
		case protocol.EvStreamDone:
			stop = ev.StopReason
			if stop == "" {
				stop = protocol.StopStop
			}
		case protocol.EvStreamError:
			stop = protocol.StopError
			errMsg := "provider error"
			if ev.Err != nil {
				errMsg = ev.Err.Error()
			}
			collector.Interrupt()
			content = assistantResponseContentWithProviderData(thinkingBuf, providerData, collector.Blocks())
			if perr := a.persistAssistant(asstID, parent, content, stop, usage, errMsg); perr != nil {
				return protocol.StopError, perr
			}
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: errMsg})
			if ev.Err != nil {
				return protocol.StopError, fmt.Errorf("agent: provider stream: %w", ev.Err)
			}
			return protocol.StopError, fmt.Errorf("agent: %s", errMsg)
		}
	}

	// Assemble final content: thinking first, then ordered text/plan blocks, then tool calls.
	if stop == protocol.StopAborted || stop == protocol.StopError {
		collector.Interrupt()
	} else {
		collector.Finish()
	}
	content = assistantResponseContentWithProviderData(thinkingBuf, providerData, collector.Blocks())
	for _, id := range toolOrder {
		if cb, ok := toolCalls[id]; ok {
			content = append(content, cb)
		}
	}
	if stop == protocol.StopPending {
		stop = protocol.StopStop
	}

	if err := a.persistAssistant(asstID, parent, content, stop, usage, ""); err != nil {
		return stop, err
	}
	collector.PublishCompleted()

	// Stash tool calls for execution (ordered).
	if stop == protocol.StopToolUse {
		a.mu.Lock()
		a.pending = make(map[string]protocol.ContentBlock)
		a.pendingOrder = a.pendingOrder[:0]
		for _, id := range toolOrder {
			if cb, ok := toolCalls[id]; ok && cb.Type == protocol.BlockToolCall {
				a.pending[cb.ToolCallID] = cb
				a.pendingOrder = append(a.pendingOrder, cb.ToolCallID)
			}
		}
		a.mu.Unlock()
	}

	return stop, nil
}

func messageTextBlocks(message protocol.Message) string {
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == protocol.BlockText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func contextTokensForCompaction(usage protocol.Usage) int {
	// Input is the provider's actual context occupancy for this request. Total
	// adds generated output and can cross the threshold even though those output
	// tokens were not all present in the request being measured.
	if usage.Input > 0 {
		return usage.Input
	}
	if usage.Total > 0 {
		return usage.Total
	}
	return usage.Output
}

func (a *Agent) persistAssistant(id, parent string, content []protocol.ContentBlock, stop protocol.StopReason, usage *protocol.Usage, errMsg string) error {
	if usage != nil {
		a.mu.Lock()
		a.turnUsage = a.turnUsage.Add(*usage)
		a.usageSet = true
		a.mu.Unlock()
	}
	msg := protocol.NewAssistantMessage(id, parent, a.Model().Provider, a.Model().ID, content, stop, usage)
	if errMsg != "" {
		msg.Error = errMsg
	}
	if err := a.opts.Session.Append(session.Entry{
		Type:     session.EntryMessage,
		ID:       id,
		ParentID: parent,
		Message:  &msg,
	}); err != nil {
		return fmt.Errorf("agent: persist assistant: %w", err)
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
}

// executeToolCalls runs the pending tool calls serially (in stream order)
// and persists results. Aborts early when ctx is cancelled.
func (a *Agent) executeToolCalls(ctx context.Context) error {
	a.mu.Lock()
	pending := a.pending
	order := append([]string(nil), a.pendingOrder...)
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.mu.Unlock()

	parent := a.opts.Session.BranchTip()
	callCount := 0

	for i, id := range order {
		cb, ok := pending[id]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			// Keep the provider-facing conversation valid even when cancellation
			// lands between serial tool calls: every declared call still gets a
			// result, so a later resume cannot expose dangling tool_calls.
			for _, remainingID := range order[i:] {
				remaining, exists := pending[remainingID]
				if !exists {
					continue
				}
				msg := protocol.NewToolResultMessage(newID(), parent, remaining.ToolCallID, remaining.Name,
					[]protocol.ContentBlock{protocol.NewTextBlock("Error: tool call cancelled: " + err.Error())}, true)
				if appendErr := a.appendToolResult(parent, msg); appendErr != nil {
					return appendErr
				}
				parent = msg.ID
			}
			return err
		}
		if a.opts.CallLimit > 0 && callCount >= a.opts.CallLimit {
			// Emit an error result for skipped calls so the provider never
			// sees tool_calls without results.
			msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
				[]protocol.ContentBlock{protocol.NewTextBlock(
					fmt.Sprintf("Error: tool call skipped (call limit %d reached)", a.opts.CallLimit))}, true)
			if err := a.appendToolResult(parent, msg); err != nil {
				return err
			}
			// Chain: the next result attaches to this one so every tool call
			// result stays on the root→tip path (no dangling tool_calls).
			parent = msg.ID
			continue
		}
		callCount++
		msg, err := a.executeOne(ctx, cb, parent)
		if err != nil {
			return err
		}
		// Chain tool results serially so all of them remain on the branch
		// tip path; otherwise only the last result is visible to Messages().
		parent = msg.ID
	}
	return nil
}

// appendToolResult persists a tool_result message and emits its events.
// Details are private tool metadata used only for UI-facing previews.
func (a *Agent) appendToolResult(parent string, msg protocol.Message, details ...any) error {
	if err := a.opts.Session.Append(session.Entry{
		Type:     session.EntryMessage,
		ID:       msg.ID,
		ParentID: parent,
		Message:  &msg,
	}); err != nil {
		return fmt.Errorf("agent: append tool result: %w", err)
	}
	started := a.takeToolStart(msg.ToolCallID)
	output := toolResultText(msg.Content)
	for _, detail := range details {
		if _, private := detail.(tools.PrivateDetails); private {
			output = "(private goal state updated)"
		}
	}
	if diff, ok := editDiffPreview(details); ok {
		output = diff
	}
	ev := protocol.AgentEvent{
		Type:       protocol.EvToolEnd,
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
		IsError:    msg.IsError,
		ToolOutput: boundEventText(output, 8*1024),
	}
	if !started.IsZero() {
		ev.ToolDurationMS = time.Since(started).Milliseconds()
	}
	if msg.IsError {
		ev.Message = boundEventText(output, 2*1024)
	}
	a.publish(ev)
	return nil
}

func (a *Agent) executeOne(ctx context.Context, cb protocol.ContentBlock, parent string) (protocol.Message, error) {
	// Validate args JSON.
	var args map[string]any
	rawArgs := cb.Arguments
	if len(rawArgs) == 0 || string(rawArgs) == "" {
		rawArgs = json.RawMessage("{}")
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		// Malformed arguments: inject a synthetic tool result telling the model.
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf(
				"Error: tool arguments are not valid JSON: %v. Raw: %s", err, string(rawArgs)))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	mode := a.capturedTurnMode()
	if mode == protocol.ModePlan && cb.Name == "update_plan" {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock("update_plan is a TODO/checklist tool and is not allowed in Plan mode")}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}
	if (mode == protocol.ModePlan && cb.Name == "ask_user") || (mode != protocol.ModePlan && cb.Name == "request_user_input") {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Error: %s is unavailable in %s mode", cb.Name, mode))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	tool, ok := a.opts.Registry.Get(cb.Name)
	if !ok {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Error: unknown tool %q", cb.Name))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	// Permission gate.
	risk := riskFor(cb.Name)
	if descriptors, ok := a.opts.Registry.(tools.DescriptorRegistry); ok {
		if desc, found := descriptors.Descriptor(cb.Name); found && desc.Risk != "" {
			risk = desc.Risk
		}
	}
	permReq := permission.Request{
		Tool:  cb.Name,
		Args:  rawArgs,
		Paths: extractPaths(args),
		Risk:  risk,
		Agent: a.opts.Identity.Clone(),
	}
	decision, err := a.opts.Permission.Authorize(ctx, permReq)
	if err != nil || decision == permission.DecisionDeny {
		reason := "denied by permission policy"
		if err != nil {
			reason = err.Error()
		}
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock("Permission denied: " + reason)}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, err
		}
		return msg, nil
	}

	a.mu.Lock()
	a.toolStarts[cb.ToolCallID] = time.Now()
	a.mu.Unlock()
	a.publish(protocol.AgentEvent{
		Type:       protocol.EvToolStart,
		ToolCallID: cb.ToolCallID,
		ToolName:   cb.Name,
		Message:    toolStartMessage(cb.Name, rawArgs),
	})

	// Run the tool with panic recovery and bridge progress into the agent
	// event stream used by the TUI, SDK, print mode, and RPC.
	tr := a.runTool(ctx, tool, rawArgs, cb.ToolCallID, cb.Name)

	var out []protocol.ContentBlock
	if len(tr.Content) == 0 {
		out = []protocol.ContentBlock{protocol.NewTextBlock("(no output)")}
	} else {
		out = tr.Content
	}
	msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name, out, tr.IsError)
	if err := a.appendToolResult(parent, msg, tr.Details); err != nil {
		return msg, err
	}
	if !tr.IsError {
		a.applyDiscoveryDetails(tr.Details)
		a.applySkillActivationDetails(tr.Details)
		a.applyPlanUpdateDetails(tr.Details)
	}
	return msg, nil
}

func (a *Agent) applyPlanUpdateDetails(details any) {
	var update *protocol.PlanUpdate
	switch value := details.(type) {
	case tools.PlanUpdateDetails:
		copy := value.Update
		update = &copy
	case *tools.PlanUpdateDetails:
		if value != nil {
			copy := value.Update
			update = &copy
		}
	}
	if update != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvPlanUpdate, PlanUpdate: update.Clone()})
	}
}

func (a *Agent) applySkillActivationDetails(details any) {
	var activation tools.SkillActivationDetails
	switch value := details.(type) {
	case tools.SkillActivationDetails:
		activation = value
	case *tools.SkillActivationDetails:
		if value == nil {
			return
		}
		activation = *value
	default:
		return
	}
	if activation.Name == "" || activation.Content == "" || !skillNameAllowed(a.opts.SkillNames, activation.Name) {
		return
	}
	a.mu.Lock()
	if a.activeSkills == nil {
		a.activeSkills = make(map[string]string)
	}
	a.activeSkills[activation.Name] = activation.Content
	a.mu.Unlock()
}

func (a *Agent) publishGoalSnapshot() {
	if a.opts.Goal == nil {
		return
	}
	g, err := a.opts.Goal.Get()
	if err != nil {
		return
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g, Cleared: g == nil}})
}

func (a *Agent) goalInternalContext() ([]protocol.InternalContextFragment, error) {
	a.mu.RLock()
	controller, mode, turn, wrap := a.opts.Goal, a.turnMode, a.goalTurn, a.budgetWrap
	a.mu.RUnlock()
	if controller == nil || mode == protocol.ModePlan {
		return nil, nil
	}
	g, err := controller.Get()
	if err != nil {
		return nil, err
	}
	if g == nil || (g.Status != protocol.GoalActive && !(wrap && g.Status == protocol.GoalBudgetLimited)) {
		return nil, nil
	}
	fragment, err := controller.Fragment(*g, turn, wrap)
	if err != nil {
		return nil, err
	}
	return []protocol.InternalContextFragment{fragment}, nil
}

func (a *Agent) requestSystemPrompt() string {
	a.mu.RLock()
	base := a.opts.SystemPrompt
	mode := a.turnMode
	if !a.running {
		mode = a.mode
	}
	names := make([]string, 0, len(a.activeSkills))
	for name := range a.activeSkills {
		names = append(names, name)
	}
	sort.Strings(names)
	contents := make([]string, 0, len(names))
	for _, name := range names {
		contents = append(contents, a.activeSkills[name])
	}
	a.mu.RUnlock()
	if mode == protocol.ModePlan {
		base += "\n\n<collaboration_mode>\n" + planpkg.Instructions + "\n</collaboration_mode>"
	}
	if len(contents) == 0 {
		return base
	}
	return base + "\n\n<active_agent_skills>\n" + strings.Join(contents, "\n") + "\n</active_agent_skills>"
}

func loadCollaborationMode(st session.Store) (protocol.CollaborationMode, error) {
	mode := protocol.ModeDefault
	if state, ok := st.(session.ThreadStateStore); ok {
		persisted, err := state.CollaborationMode()
		if err != nil {
			return "", err
		}
		mode = persisted
	}
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return "", err
	}
	return parsed, nil
}

func restoreActiveSkills(st session.Store, registry tools.Registry, host tools.ToolHost, allowed map[string]bool) map[string]string {
	active := make(map[string]string)
	if st == nil {
		return active
	}
	messages, err := st.Messages()
	if err != nil {
		return active
	}
	reload := make(map[string]bool)
	for _, message := range messages {
		if message.Role != protocol.RoleTool || message.ToolName != "activate_skill" || message.IsError {
			continue
		}
		for _, block := range message.Content {
			if block.Type != protocol.BlockText || !strings.HasPrefix(block.Text, "<skill_content name=") {
				continue
			}
			line, _, _ := strings.Cut(block.Text, "\n")
			value := strings.TrimSuffix(strings.TrimPrefix(line, "<skill_content name="), ">")
			var name string
			if err := json.Unmarshal([]byte(value), &name); err == nil && name != "" && skillNameAllowed(allowed, name) {
				active[name] = block.Text
				if allowed != nil {
					reload[name] = true
				}
			}
		}
	}
	if entries, ok := st.(session.BranchEntryStore); ok {
		if branch, branchErr := entries.BranchEntries(); branchErr == nil {
			for _, entry := range branch {
				if entry.Type == session.EntryMeta && entry.Key == skillActivationMeta && skillNameAllowed(allowed, entry.Value) {
					reload[entry.Value] = true
				}
			}
		}
	}
	// Rehydrate current catalog content for direct markers and for app-provided
	// policy maps. This prevents stale project instructions from surviving a
	// trust or precedence change while preserving legacy standalone behavior.
	for name := range reload {
		activation, activationErr := runSkillActivation(context.Background(), registry, host, name)
		if activationErr != nil {
			delete(active, name)
			continue
		}
		active[name] = activation.Content
	}
	return active
}

func (a *Agent) activateExplicitSkillMentions(ctx context.Context, text string) error {
	names := explicitSkillNames(text, activationSkillNames(a.opts.Registry, a.opts.SkillNames))
	for _, name := range names {
		callID := newID()
		started := time.Now()
		a.publish(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: callID, ToolName: "activate_skill", Message: "activating explicitly requested skill " + name})
		activation, err := runSkillActivation(ctx, a.opts.Registry, a.opts.ToolHost, name)
		if err == nil {
			a.mailboxPersistMu.Lock()
			err = a.opts.Session.Append(session.Entry{Type: session.EntryMeta, ID: newID(), Key: skillActivationMeta, Value: name})
			a.mailboxPersistMu.Unlock()
		}
		event := protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: callID, ToolName: "activate_skill", ToolDurationMS: time.Since(started).Milliseconds(), IsError: err != nil}
		if err != nil {
			event.Message = boundEventText(err.Error(), 2*1024)
			event.ToolOutput = event.Message
			a.publish(event)
			return err
		}
		a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
		event.ToolOutput = "activated skill " + name
		a.publish(event)
		a.applySkillActivationDetails(activation)
	}
	return nil
}

func runSkillActivation(ctx context.Context, registry tools.Registry, host tools.ToolHost, name string) (tools.SkillActivationDetails, error) {
	if registry == nil {
		return tools.SkillActivationDetails{}, errors.New("skill registry unavailable")
	}
	descriptor, ok := registry.Descriptor("activate_skill")
	if !ok || descriptor.Owner != "skills" || descriptor.Tool == nil {
		return tools.SkillActivationDetails{}, errors.New("activate_skill is unavailable")
	}
	args, _ := json.Marshal(map[string]string{"name": name})
	result, err := descriptor.Tool.Run(ctx, args, host)
	if err != nil {
		return tools.SkillActivationDetails{}, err
	}
	if result.IsError {
		message := "skill activation failed"
		if len(result.Content) > 0 && result.Content[0].Text != "" {
			message = result.Content[0].Text
		}
		return tools.SkillActivationDetails{}, errors.New(message)
	}
	switch value := result.Details.(type) {
	case tools.SkillActivationDetails:
		if value.Name != "" && value.Content != "" {
			return value, nil
		}
	case *tools.SkillActivationDetails:
		if value != nil && value.Name != "" && value.Content != "" {
			return *value, nil
		}
	}
	return tools.SkillActivationDetails{}, errors.New("activate_skill returned no durable content")
}

func activationSkillNames(registry tools.Registry, allowed map[string]bool) []string {
	if registry == nil {
		return nil
	}
	descriptor, ok := registry.Descriptor("activate_skill")
	if !ok || descriptor.Owner != "skills" || descriptor.Tool == nil {
		return nil
	}
	var schema struct {
		Properties struct {
			Name struct {
				Enum []string `json:"enum"`
			} `json:"name"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(descriptor.Schema.Parameters, &schema); err != nil {
		return nil
	}
	names := make([]string, 0, len(schema.Properties.Name.Enum))
	for _, name := range schema.Properties.Name.Enum {
		if skillNameAllowed(allowed, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func skillNameAllowed(allowed map[string]bool, name string) bool {
	if allowed == nil {
		return true
	}
	return allowed[name]
}

func explicitSkillNames(text string, candidates []string) []string {
	available := make(map[string]bool, len(candidates))
	for _, name := range candidates {
		available[name] = true
	}
	rest := strings.TrimLeftFunc(text, unicode.IsSpace)
	var names []string
	seen := make(map[string]bool)
	for strings.HasPrefix(rest, "$") {
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			end = len(rest)
		}
		name := strings.TrimPrefix(rest[:end], "$")
		if !available[name] {
			break
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
		rest = strings.TrimLeftFunc(rest[end:], unicode.IsSpace)
	}
	return names
}

func (a *Agent) runTool(ctx context.Context, tool tools.Tool, rawArgs json.RawMessage, callID, name string) (tr tools.ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			tr = tools.ErrorResult(fmt.Errorf("tool %s panicked: %v", tool.Schema().Name, r))
		}
	}()
	host := a.opts.ToolHost
	if host != nil {
		host = &progressHost{ToolHost: host, agent: a, callID: callID, name: name}
	}
	res, err := tool.Run(ctx, rawArgs, host)
	if err != nil {
		return tools.ErrorResult(err)
	}
	return res
}

// progressHost preserves the host contract while making tool progress visible
// to every surface that subscribes to AgentEvent.
type progressHost struct {
	tools.ToolHost
	agent  *Agent
	callID string
	name   string
}

func (h *progressHost) ToolCallID() string { return h.callID }
func (h *progressHost) CollaborationMode() protocol.CollaborationMode {
	return h.agent.capturedTurnMode()
}

func (h *progressHost) RequestUserInput(ctx context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
	interactive, ok := h.ToolHost.(tools.UserInputHost)
	if !ok {
		return protocol.UserInputResponse{}, errors.New("interactive user input is unavailable on this surface")
	}
	req.ID = h.callID
	req.ToolCallID = h.callID
	return interactive.RequestUserInput(ctx, req)
}

func (h *progressHost) EmitProgress(ev tools.ToolProgressEvent) {
	if ev.ToolCallID == "" {
		ev.ToolCallID = h.callID
	}
	if ev.Name == "" {
		ev.Name = h.name
	}
	h.agent.publish(protocol.AgentEvent{
		Type:       protocol.EvToolProgress,
		ToolCallID: ev.ToolCallID,
		ToolName:   ev.Name,
		Message:    ev.Message,
		IsError:    ev.IsError,
		ToolProgress: &protocol.ToolProgress{
			ToolCallID: ev.ToolCallID,
			Name:       ev.Name,
			Message:    ev.Message,
			Done:       ev.Done,
			IsError:    ev.IsError,
		},
	})
	// Keep the original host observable for embedding/test hosts.
	h.ToolHost.EmitProgress(ev)
}

func (a *Agent) takeToolStart(callID string) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	started := a.toolStarts[callID]
	delete(a.toolStarts, callID)
	return started
}

func toolResultText(content []protocol.ContentBlock) string {
	var b strings.Builder
	for _, block := range content {
		if block.Type != protocol.BlockText && block.Type != protocol.BlockThinking {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(block.Text)
	}
	return b.String()
}

func editDiffPreview(details []any) (string, bool) {
	for _, detail := range details {
		switch d := detail.(type) {
		case tools.DiffDetails:
			if d.Diff != "" {
				return d.Diff, true
			}
		case *tools.DiffDetails:
			if d != nil && d.Diff != "" {
				return d.Diff, true
			}
		}
	}
	return "", false
}

func toolStartMessage(name string, rawArgs json.RawMessage) string {
	if name == "edit" || name == "write" {
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(rawArgs, &input); err == nil && input.Path != "" {
			return input.Path
		}
	}
	return "running"
}

func boundEventText(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	body := []byte(text)[:maxBytes]
	for len(body) > 0 && !utf8.Valid(body) {
		body = body[:len(body)-1]
	}
	return string(body) + "\n… [tool output preview truncated]"
}

// resolveCreds resolves provider credentials: explicit API key → auth.json → env.
// An empty credential is passed through and the provider's Resolve is the
// authority on whether that is acceptable (fake/test providers accept empty).
func (a *Agent) resolveCreds(ctx context.Context) auth.Credential {
	id := a.Model().Provider
	if a.opts.APIKey != "" && a.opts.APIKeyProvider == id {
		return auth.Credential{Type: auth.CredentialAPIKey, Key: a.opts.APIKey}
	}
	if a.opts.Auth != nil {
		if cred, ok := a.opts.Auth.Get(id); ok && cred.Valid() {
			return cred
		}
	}
	// Env fallback for known API-key providers.
	if id == "opencode-go" {
		if k := os.Getenv("OPENCODE_API_KEY"); k != "" {
			return auth.Credential{Type: auth.CredentialAPIKey, Key: k}
		}
	}
	return auth.Credential{}
}

// riskFor maps tool names to permission risk classes.
func riskFor(name string) permission.Risk {
	switch name {
	case "read", "grep", "glob", "search_tools", "ask_user", "request_user_input", "update_plan", "get_goal", "create_goal", "update_goal":
		return permission.RiskRead
	case "write", "edit":
		return permission.RiskWrite
	case "bash":
		return permission.RiskExec
	case "webfetch":
		return permission.RiskNet
	default:
		return permission.RiskExec
	}
}

// extractPaths pulls likely path fields from tool args.
func extractPaths(args map[string]any) []string {
	var paths []string
	for _, k := range []string{"path", "file", "dir", "paths"} {
		switch v := args[k].(type) {
		case string:
			paths = append(paths, v)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					paths = append(paths, s)
				}
			}
		}
	}
	return paths
}

func textBlock(s string) protocol.ContentBlock {
	return protocol.NewTextBlock(s)
}

func assistantResponseContent(thinking string, blocks []protocol.ContentBlock) []protocol.ContentBlock {
	return assistantResponseContentWithProviderData(thinking, nil, blocks)
}

func assistantResponseContentWithProviderData(thinking string, providerData, blocks []protocol.ContentBlock) []protocol.ContentBlock {
	content := make([]protocol.ContentBlock, 0, len(providerData)+len(blocks)+1)
	if thinking != "" {
		content = append(content, protocol.ContentBlock{Type: protocol.BlockThinking, Text: thinking})
	}
	// Opaque reasoning continuity is persisted before output/function calls and
	// is deliberately never published on the AgentEvent bus.
	content = append(content, providerData...)
	return append(content, blocks...)
}

func strings_trim(s string) string { return strings.TrimSpace(s) }

// ---------------------------------------------------------------------------
// Event bus
// ---------------------------------------------------------------------------

type eventBus struct {
	mu           sync.Mutex
	subs         map[int]func(protocol.AgentEvent)
	next         int
	wake         chan struct{}
	items        []any
	closing      bool
	inCallback   bool
	dispatcherID uint64
	closed       chan struct{}
}
type eventBarrier struct{ done chan struct{} }
type eventStop struct{}

func newEventBus() *eventBus {
	b := &eventBus{subs: make(map[int]func(protocol.AgentEvent)), wake: make(chan struct{}, 1), closed: make(chan struct{})}
	go b.dispatch()
	return b
}
func (b *eventBus) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *eventBus) dispatch() {
	atomic.StoreUint64(&b.dispatcherID, currentGoroutineID())
	defer close(b.closed)
	for range b.wake {
		for {
			b.mu.Lock()
			if len(b.items) == 0 {
				b.mu.Unlock()
				break
			}
			item := b.items[0]
			b.items = b.items[1:]
			fns := make([]func(protocol.AgentEvent), 0, len(b.subs))
			if _, ok := item.(protocol.AgentEvent); ok {
				ids := make([]int, 0, len(b.subs))
				for id := range b.subs {
					ids = append(ids, id)
				}
				sort.Ints(ids)
				for _, id := range ids {
					fns = append(fns, b.subs[id])
				}
			}
			b.mu.Unlock()
			switch v := item.(type) {
			case protocol.AgentEvent:
				for _, fn := range fns {
					b.mu.Lock()
					if b.closing {
						b.mu.Unlock()
						break
					}
					b.inCallback = true
					b.mu.Unlock()
					func() {
						defer func() { _ = recover() }()
						fn(v.Clone())
					}()
					b.mu.Lock()
					b.inCallback = false
					b.mu.Unlock()
				}
			case eventBarrier:
				close(v.done)
			case eventStop:
				return
			}
		}
	}
}
func currentGoroutineID() uint64 {
	var stack [64]byte
	n := runtime.Stack(stack[:], false)
	const prefix = "goroutine "
	if n <= len(prefix) || string(stack[:len(prefix)]) != prefix {
		return 0
	}
	end := len(prefix)
	for end < n && stack[end] >= '0' && stack[end] <= '9' {
		end++
	}
	id, _ := strconv.ParseUint(string(stack[len(prefix):end]), 10, 64)
	return id
}

func (b *eventBus) InCallback() bool {
	current := currentGoroutineID()
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inCallback && current != 0 && current == atomic.LoadUint64(&b.dispatcherID)
}

func (b *eventBus) Drain(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return nil
	}
	b.items = append(b.items, eventBarrier{done})
	b.signal()
	b.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *eventBus) Wait() { <-b.closed }

func (b *eventBus) Close() {
	b.mu.Lock()
	if !b.closing {
		b.closing = true
		// Closing suppresses future callbacks. Release callers already waiting
		// on a drain barrier before terminating the dispatcher.
		for _, item := range b.items {
			if barrier, ok := item.(eventBarrier); ok {
				close(barrier.done)
			}
		}
		b.items = []any{eventStop{}}
		b.subs = make(map[int]func(protocol.AgentEvent))
		b.signal()
	}
	b.mu.Unlock()
}

func (b *eventBus) Subscribe(fn func(protocol.AgentEvent)) func() {
	if fn == nil {
		return func() {}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return func() {}
	}
	id := b.next
	b.next++
	b.subs[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}

func (b *eventBus) Publish(ev protocol.AgentEvent) {
	b.mu.Lock()
	if !b.closing {
		b.items = append(b.items, ev.Clone())
		b.signal()
	}
	b.mu.Unlock()
}

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

// idCounter disambiguates IDs generated within the same nanosecond tick.
var idCounter uint64

func newID() string {
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), atomic.AddUint64(&idCounter, 1))
}
