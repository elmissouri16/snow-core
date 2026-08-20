package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (e *providerStartError) Error() string { return "agent: provider chat: " + e.err.Error() }

func (e *providerStartError) Unwrap() error { return e.err }

func (e *providerStartError) providerFailure() {}

func (e *providerTurnError) Error() string { return e.err.Error() }

func (e *providerTurnError) Unwrap() error { return e.err }

func (e *providerTurnError) providerFailure() {}

func isProviderFailure(err error) bool {
	var marked providerFailure
	return errors.As(err, &marked)
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
	if _, err := repairInterruptedToolCalls(opts.Session, opts.Registry); err != nil {
		return nil, fmt.Errorf("agent: recover interrupted tool calls: %w", err)
	}
	a := &Agent{opts: opts, model: opts.Model, bus: newEventBus(), mode: mode, turnMode: mode, rootEpoch: 1}
	a.pending = make(map[string]protocol.ContentBlock)
	a.toolStarts = make(map[string]time.Time)
	a.toolDisplays = make(map[string]toolDisplayState)
	a.activeSkills = restoreActiveSkills(opts.Session, opts.Registry, opts.ToolHost, opts.SkillNames)
	if state, ok := opts.Session.(session.ThreadStateStore); ok {
		if err := state.SetCollaborationMode(mode); err != nil {
			a.bus.Close()
			a.bus.Wait()
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
	skillsCleared := 0
	reentrantEventCallback := a.bus.InCallback()
	defer func() {
		if admissionHeld {
			unlockAdmission()
		}
		if skillsCleared > 0 {
			a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
			if !reentrantEventCallback {
				a.drainEventsBestEffort()
			}
		}
	}()
	parsed, err := protocol.ParseCollaborationMode(string(mode))
	if err != nil {
		return err
	}
	// A mode switch is never an implicit abort of an admitted turn.
	a.mu.RLock()
	running := a.running
	wasAutomatic := a.autoRunning
	previousMode := a.mode
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
	if previousMode == protocol.ModePlan && parsed == protocol.ModeDefault {
		cleared, err := a.clearActiveSkillsDurably(true)
		if err != nil {
			return resumeAfterFailure(err)
		}
		skillsCleared = cleared
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
	if skillsCleared > 0 {
		a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
		skillsCleared = 0
	}
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
		a.drainEventsBestEffort()
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
	if err := a.setSessionAdmitted(st, true); err != nil {
		return err
	}
	a.ResetTurnIdentityAdmitted()
	return nil
}

// SetSessionQuietAdmitted participates in an App transaction that already
// holds LockAdmission.
func (a *Agent) SetSessionQuietAdmitted(st session.Store) error {
	return a.setSessionAdmitted(st, false)
}

// ResetTurnIdentityAdmitted commits the reconciliation boundary after a
// compound session transaction succeeds. The caller must hold admission or
// otherwise guarantee that the agent is idle.
func (a *Agent) ResetTurnIdentityAdmitted() {
	a.mu.Lock()
	a.resetTurnIdentityLocked()
	a.mu.Unlock()
}

func (a *Agent) resetTurnIdentityLocked() {
	a.rootEpoch++
	if a.rootEpoch == 0 {
		a.rootEpoch = 1
	}
	a.turnOrigin, a.turnID = "", ""
	a.activeTurnSequence = 0
	a.latestTurnOrigin, a.latestTurnID = "", ""
	a.latestTurnSequence = 0
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
	if _, err := repairInterruptedToolCalls(st, a.opts.Registry); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("agent: recover interrupted tool calls: %w", err)
	}
	a.opts.Session = st
	a.activeSkills = restoreActiveSkills(st, a.opts.Registry, a.opts.ToolHost, a.opts.SkillNames)
	a.mode = mode
	a.turnMode = mode
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	effort := a.effectiveThinkingLocked(mode)
	a.mu.Unlock()
	a.resetMailboxUnread()
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
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
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
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	a.mu.Unlock()
	unlock()
	reentrant := a.bus.InCallback()
	a.publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	if !reentrant {
		a.drainEventsBestEffort()
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
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	a.mu.Unlock()
	unlock()
	reentrant := a.bus.InCallback()
	a.publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	if !reentrant {
		a.drainEventsBestEffort()
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
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	a.mu.Unlock()
	unlock()
	reentrantEventCallback := a.bus.InCallback()
	a.publish(protocol.AgentEvent{Type: protocol.EvModelChanged, Model: &m})
	if !reentrantEventCallback {
		a.drainEventsBestEffort()
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

// LatestTurn returns the identity of the most recently admitted root
// operation, including after it has completed. It is empty until the first
// operation is admitted and supports delayed UI event reconciliation.
func (a *Agent) LatestTurn() (origin, id string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestTurnOrigin, a.latestTurnID
}

// TurnSequenceWatermark returns the latest process-local admission sequence.
// It remains monotonic across session and branch reconciliation boundaries.
func (a *Agent) TurnSequenceWatermark() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.turnSequence
}

// RootEpoch returns the process-local session/branch reconciliation epoch.
func (a *Agent) RootEpoch() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rootEpoch
}

func (a *Agent) admitTurnIdentityLocked(origin string) {
	a.turnSequence++
	a.turnOrigin, a.turnID = origin, newID()
	a.activeTurnSequence = a.turnSequence
	a.latestTurnOrigin, a.latestTurnID = a.turnOrigin, a.turnID
	a.latestTurnSequence = a.turnSequence
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

// closeAutomaticQueueForPrompt atomically closes queue admission against a
// racing QueueInput and reports whether accepted work must finish/recover before
// an ordinary prompt may preempt the automatic run.
func (a *Agent) closeAutomaticQueueForPrompt() bool {
	a.queuePublishMu.Lock()
	defer a.queuePublishMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.autoRunning {
		a.queueAccepting = false
	}
	return len(a.queuedInputs) > 0
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
// Subscribe registers an ordered event callback. Callbacks must return
// promptly; a callback that runs longer than the bounded subscriber timeout is
// evicted so it cannot strand delivery or agent shutdown.
func (a *Agent) Subscribe(fn func(protocol.AgentEvent)) func() { return a.bus.Subscribe(fn) }

func (a *Agent) DrainEvents(ctx context.Context) error { return a.bus.Drain(ctx) }

func (a *Agent) InEventCallback() bool { return a.bus.InCallback() }

func (a *Agent) drainEventsBestEffort() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.bus.Drain(ctx)
}

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
	if ev.Agent == nil {
		a.mu.RLock()
		if ev.RootEpoch == 0 {
			ev.RootEpoch = a.rootEpoch
		}
		if ev.TurnID == "" && a.running {
			ev.TurnID = a.turnID
			ev.TurnOrigin = a.turnOrigin
		}
		if ev.TurnSequence == 0 && ev.TurnID != "" {
			switch ev.TurnID {
			case a.turnID:
				ev.TurnSequence = a.activeTurnSequence
			case a.latestTurnID:
				ev.TurnSequence = a.latestTurnSequence
			}
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
