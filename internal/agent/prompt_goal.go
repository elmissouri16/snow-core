package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (a *Agent) prompt(ctx context.Context, text string, attachments []protocol.ContentBlock, requestedMode *protocol.CollaborationMode) (retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
	if strings_trim(text) == "" && len(attachments) == 0 {
		return errors.New("agent: empty prompt")
	}

	// Validate without disturbing admitted user work or a running goal. An
	// unsupported prompt must not stop an automatic goal and leave it idle.
	a.mu.RLock()
	closed, running, wasAutomatic := a.closed, a.running, a.autoRunning
	pendingRecovery := len(a.queuedInputs) > 0
	modeBeforeAdmission := a.mode
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
	if pendingRecovery {
		return fmt.Errorf("%w: undelivered queued input is waiting for recovery; call ClearPendingInputs first", ErrPromptRejected)
	}
	if !model.SupportsThinkingLevel(level) {
		return errors.Join(ErrPromptRejected, unsupportedThinkingError(model, level))
	}
	if err := validateUserAttachments(model, attachments); err != nil {
		return errors.Join(ErrPromptRejected, err)
	}
	if wasAutomatic && a.closeAutomaticQueueForPrompt() {
		return fmt.Errorf("%w: undelivered queued input was accepted before automatic work could be preempted; call ClearPendingInputs first", ErrPromptRejected)
	}
	a.stopAutomatic(false)
	if requestedMode != nil && modeBeforeAdmission == protocol.ModePlan && *requestedMode == protocol.ModeDefault {
		cleared, err := a.clearActiveSkillsDurably(true)
		if err != nil {
			return errors.Join(ErrPromptRejected, err)
		}
		skillsCleared = cleared
	}

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
	if len(a.queuedInputs) > 0 {
		a.mu.Unlock()
		return fmt.Errorf("%w: undelivered queued input was accepted while automatic work stopped; call ClearPendingInputs first", ErrPromptRejected)
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
	a.admitTurnIdentityLocked("user")
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
	a.resetTurnExecutionLocked()
	modeChanged := requestedMode != nil
	mode := a.mode
	a.mu.Unlock()
	unlockAdmission()
	admissionHeld = false
	if skillsCleared > 0 {
		a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
		skillsCleared = 0
	}
	if modeChanged {
		a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: level}})
	}
	a.prepareToolRouting(ctx, text)

	// Ensure we stop running on any exit. Operational failures leave accepted
	// queued input closed but recoverable through PendingInputs/ClearPendingInputs.
	defer func() {
		a.closeInputQueue(retErr == nil || ctx.Err() != nil)
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
			a.markTurnIdleLocked()
		}))
		// Queue completion before a continuation can overwrite turn metadata.
		a.publishTurnDone(continuing, origin, turnID, usage)
		a.clearCompletedTurnIdentity(turnID)
		a.turnWG.Done()
		if continuing {
			a.ContinueGoal()
		}
		if !reentrantEventCallback {
			a.drainEventsBestEffort()
		}
	}()
	if err := a.persistTurnMarker(); err != nil {
		return err
	}

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
	userEntry := session.Entry{
		Type:     session.EntryMessage,
		ID:       userMsg.ID,
		ParentID: "",
		Message:  &userMsg,
	}
	var appendErr error
	if titles, ok := a.opts.Session.(session.TitleStore); ok {
		titleSource := text
		if titleSource == "" && len(attachments) > 0 {
			titleSource = "Image prompt"
		}
		appendErr = titles.AppendWithInitialTitle(userEntry, session.SuggestedTitle(titleSource))
	} else {
		appendErr = a.opts.Session.Append(userEntry)
	}
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

func (a *Agent) resetTurnExecutionLocked() {
	a.turnPlanSeen = false
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.pendingToolError = ""
	a.toolStarts = make(map[string]time.Time)
	a.toolDisplays = make(map[string]toolDisplayState)
	a.repeatedTool = repeatedToolCallState{}
	a.turnToolCalls = 0
	a.turnUsage = protocol.Usage{}
	a.usageSet = false
	a.turnProgress = false
	a.baseDeferred = nil
	a.searchedDeferred = nil
}

func (a *Agent) markTurnIdleLocked() {
	a.running = false
	a.baseDeferred = nil
	a.searchedDeferred = nil
	a.activeCancel = nil
	a.goalAtTurn = nil
	if a.activeDone != nil {
		close(a.activeDone)
		a.activeDone = nil
	}
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
	if len(a.queuedInputs) > 0 {
		a.mu.Unlock()
		return errors.New("agent: undelivered queued input is waiting for recovery; call ClearPendingInputs first")
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
	a.admitTurnIdentityLocked("goal")
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
	a.resetTurnExecutionLocked()
	a.mu.Unlock()
	defer func() {
		a.closeInputQueue(retErr == nil || ctx.Err() != nil)
		cancel()
		retErr = errors.Join(retErr, a.drainMailbox())
		continuing, accountingErr := a.finalizeGoalTurn(retErr, false)
		retErr = errors.Join(retErr, accountingErr)
		var origin, turnID string
		var usage *protocol.Usage
		retErr = errors.Join(retErr, a.finishTurnMailbox(func() {
			origin, turnID, usage = a.turnCompletionLocked()
			a.markTurnIdleLocked()
		}))
		a.publishTurnDone(continuing, origin, turnID, usage)
		a.clearCompletedTurnIdentity(turnID)
		a.turnWG.Done()
	}()
	if err := a.persistTurnMarker(); err != nil {
		return err
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g.Clone()}, TurnOrigin: "goal", TurnID: a.turnID, GoalContinuing: true})
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

func (a *Agent) stopGoalOnError(turnErr error) error {
	a.mu.RLock()
	g := a.goalAtTurn.Clone()
	controller := a.opts.Goal
	a.mu.RUnlock()
	if g == nil || controller == nil || g.Status != protocol.GoalActive || errors.Is(turnErr, context.Canceled) {
		return nil
	}
	status := protocol.GoalBlocked
	var limited provider.UsageLimitedError
	if errors.As(turnErr, &limited) && limited.UsageLimited() {
		status = protocol.GoalUsageLimited
	} else if advice, retryable := provider.RetryAdviceFor(turnErr); retryable {
		if advice.Kind == provider.RetryRateLimit {
			status = protocol.GoalUsageLimited
		} else {
			status = protocol.GoalPaused
		}
	}
	// Fail closed before the semantic transition: a process crash or status-write
	// failure must not leave autonomous work eligible to resume unexpectedly.
	if err := controller.Defer(true); err != nil {
		return fmt.Errorf("goal: defer before %s transition: %w", status, err)
	}
	if status == protocol.GoalBlocked {
		if _, err := controller.SetStatusWithReason(g.GoalID, status, false, turnErr.Error()); err != nil {
			return fmt.Errorf("goal: persist %s status: %w", status, err)
		}
	} else if _, err := controller.SetStatus(g.GoalID, status, false); err != nil {
		return fmt.Errorf("goal: persist %s status: %w", status, err)
	}
	return nil
}

func (a *Agent) finalizeGoalTurn(turnErr error, userOrigin bool) (bool, error) {
	crossed, accountingErr := a.finishGoalAccounting()
	var transitionErr error
	if accountingErr != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal accounting: " + accountingErr.Error()})
		transitionErr = a.stopGoalOnError(accountingErr)
	}
	if turnErr != nil || accountingErr != nil {
		// Accounting always precedes terminal classification, and budget crossing
		// keeps precedence. Provider retries have already exhausted their central
		// policy by the time an error reaches this boundary.
		a.mu.Lock()
		a.budgetWrap = false
		a.mu.Unlock()
		if accountingErr == nil && !errors.Is(turnErr, context.Canceled) && !crossed {
			transitionErr = errors.Join(transitionErr, a.stopGoalOnError(turnErr))
		}
	}
	a.mu.Lock()
	progress := a.turnProgress
	goalID := ""
	if a.goalAtTurn != nil {
		goalID = a.goalAtTurn.GoalID
	}
	if !userOrigin && a.turnOrigin == "goal" && turnErr == nil {
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
	if transitionErr != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal transition: " + transitionErr.Error()})
	}
	finalErr := errors.Join(accountingErr, transitionErr)
	if controller == nil || mode == protocol.ModePlan || stopped || finalErr != nil || turnErr != nil {
		return false, finalErr
	}
	g, err := controller.Get()
	if err != nil || g == nil {
		return false, errors.Join(finalErr, err)
	}
	if empty >= 3 && g.Status == protocol.GoalActive {
		// Empty output is not proof of an external blocker. Pause conservatively
		// rather than falsely claiming the model's three-turn blocked audit.
		a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal continuation paused after three turns with no text or tool progress"})
		if err := controller.Defer(true); err != nil {
			transitionErr := fmt.Errorf("goal: defer before no-progress pause: %w", err)
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: transitionErr.Error()})
			return false, transitionErr
		}
		if _, err := controller.SetStatus(g.GoalID, protocol.GoalPaused, false); err != nil {
			transitionErr := fmt.Errorf("goal: persist paused status: %w", err)
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: transitionErr.Error()})
			return false, transitionErr
		}
		return false, finalErr
	}
	deferred, deferErr := controller.Deferred()
	if deferErr != nil {
		return false, deferErr
	}
	a.mu.RLock()
	toolsAvailable := a.goalToolsAvailableLocked()
	a.mu.RUnlock()
	return toolsAvailable && !deferred && (g.Status == protocol.GoalActive || (crossed && g.Status == protocol.GoalBudgetLimited)), finalErr
}

func (a *Agent) accountGoalUsage(usage protocol.Usage) error {
	a.mu.RLock()
	g := a.goalAtTurn.Clone()
	mode := a.turnMode
	controller := a.opts.Goal
	a.mu.RUnlock()
	if g == nil || mode == protocol.ModePlan || controller == nil {
		return nil
	}
	tokens := int64(usage.Total)
	if tokens == 0 {
		tokens = int64(usage.Input + usage.Output)
	}
	updated, crossed, err := controller.AccountDuration(g.GoalID, tokens, 0, usage.Cost.Clone())
	if err != nil {
		return err
	}
	if crossed && updated != nil {
		a.mu.Lock()
		a.budgetWrap = true
		a.mu.Unlock()
	}
	return nil
}

func (a *Agent) finishGoalAccounting() (bool, error) {
	a.mu.RLock()
	g := a.goalAtTurn.Clone()
	started, mode, alreadyCrossed := a.turnStarted, a.turnMode, a.budgetWrap
	a.mu.RUnlock()
	if g == nil || mode == protocol.ModePlan || a.opts.Goal == nil {
		return false, nil
	}
	updated, durationCrossed, err := a.opts.Goal.AccountDuration(g.GoalID, 0, time.Since(started), nil)
	if err != nil {
		return false, err
	}
	crossed := alreadyCrossed || durationCrossed
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
				if deferErr := a.opts.Goal.Defer(true); deferErr != nil {
					a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal auto-compaction deferral: " + deferErr.Error()})
				} else if _, statusErr := a.opts.Goal.SetStatusWithReason(g.GoalID, protocol.GoalBlocked, false, "Automatic compaction failed: "+compactErr.Error()); statusErr != nil {
					a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "goal auto-compaction status: " + statusErr.Error()})
				}
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
	if anyTurn {
		a.closeInputQueue(true)
	}
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

func (a *Agent) Abort() { _ = a.stopWork(context.Background(), true, true) }

func (a *Agent) AbortContext(ctx context.Context) error { return a.stopWork(ctx, true, true) }
