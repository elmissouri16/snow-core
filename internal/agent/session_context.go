package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SessionIdentityAdmitted returns the active identity while the caller holds
// the admission lock. Unlike IdleSessionAdmitted it does not alter automatic
// goal continuation state.
func (a *Agent) SessionIdentityAdmitted() (id, path string, running bool, err error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return "", "", false, errors.New("agent: closed")
	}
	if a.opts.Session == nil {
		return "", "", a.running, errors.New("agent: session is nil")
	}
	return a.opts.Session.ID(), a.opts.Session.Path(), a.running, nil
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

// IdleSessionAdmitted returns the active store while the caller holds the
// admission lock. It stops automatic goal continuation and rejects ordinary
// running turns, allowing a host to take an immutable cross-session snapshot
// without racing a prompt or session switch.
func (a *Agent) IdleSessionAdmitted(operation string) (session.Store, error) {
	if strings.TrimSpace(operation) == "" {
		operation = "inspect session"
	}
	if err := a.stopAutomaticForControl(context.Background(), operation); err != nil {
		return nil, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return nil, errors.New("agent: closed")
	}
	if a.running {
		return nil, fmt.Errorf("agent: cannot %s while running", operation)
	}
	if a.opts.Session == nil {
		return nil, errors.New("agent: session is nil")
	}
	return a.opts.Session, nil
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
	messageBytes := mailboxMessageBytes(message)
	if a.mailboxUnreadItems >= maxPendingMailboxItems || a.mailboxUnreadBytes+messageBytes > maxPendingMailboxBytes {
		a.mailboxMu.Unlock()
		return fmt.Errorf("agent: mailbox limit reached (%d messages or %d bytes)", maxPendingMailboxItems, maxPendingMailboxBytes)
	}
	a.mailbox = append(a.mailbox, message)
	a.mailboxBytes += messageBytes
	a.mailboxUnreadItems++
	a.mailboxUnreadBytes += messageBytes
	a.mailboxUnread = true
	if running {
		a.mailboxMu.Unlock()
		return nil
	}
	batch := append([]protocol.AgentMessage(nil), a.mailbox...)
	a.mailbox = nil
	a.mailboxBytes = 0
	a.mailboxMu.Unlock()
	return a.persistMailboxBatchLocked(batch)
}

func (a *Agent) resetMailboxUnread() {
	a.mailboxMu.Lock()
	a.mailbox = nil
	a.mailboxBytes = 0
	a.mailboxUnreadItems = 0
	a.mailboxUnreadBytes = 0
	a.mailboxUnread = false
	a.mailboxMu.Unlock()
}

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
	a.mailboxBytes = 0
	a.mailboxMu.Unlock()
	if err := a.persistMailboxBatchLocked(batch); err != nil {
		return err
	}
	a.mailboxMu.Lock()
	a.mailboxUnreadItems = 0
	a.mailboxUnreadBytes = 0
	a.mailboxUnread = false
	a.mailboxMu.Unlock()
	return nil
}

func (a *Agent) drainMailbox() error {
	a.mailboxPersistMu.Lock()
	defer a.mailboxPersistMu.Unlock()
	a.mailboxMu.Lock()
	batch := append([]protocol.AgentMessage(nil), a.mailbox...)
	a.mailbox = nil
	a.mailboxBytes = 0
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

func mailboxMessageBytes(message protocol.AgentMessage) int {
	return len(message.ID) + len(message.Author) + len(message.Recipient) + len(message.Kind) + len(message.Content)
}

func (a *Agent) requeueMailbox(batch []protocol.AgentMessage) {
	a.mailboxMu.Lock()
	a.mailbox = append(append([]protocol.AgentMessage(nil), batch...), a.mailbox...)
	for _, message := range batch {
		a.mailboxBytes += mailboxMessageBytes(message)
	}
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
	a.mailboxBytes = 0
	a.mailboxMu.Unlock()
	return a.persistMailboxBatchLocked(batch)
}

// Usage returns the aggregate usage for the active session branch.
func (a *Agent) Usage() (total protocol.Usage, err error) {
	err = a.withSessionRead(func(store session.Store) error {
		if aggregate, ok := store.(interface {
			AggregateUsage() (protocol.Usage, error)
		}); ok {
			var aggregateErr error
			total, aggregateErr = aggregate.AggregateUsage()
			return aggregateErr
		}
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
	var (
		messages []protocol.Message
		err      error
	)
	if projected, ok := store.(session.ContextStore); ok {
		messages, err = projected.ContextMessages()
	} else {
		messages, err = store.Messages()
	}
	if err != nil {
		return nil, err
	}
	// Failed provider attempts remain durable for diagnostics but are not valid
	// conversational input. In particular, an overflow retry must not replay a
	// partial assistant response or leave the next request ending in assistant.
	out := make([]protocol.Message, 0, len(messages))
	skippedToolCalls := make(map[string]bool)
	for _, message := range messages {
		if message.Role == protocol.RoleAssistant && message.StopReason == protocol.StopError {
			for _, block := range message.Content {
				if block.Type == protocol.BlockToolCall && block.ToolCallID != "" {
					skippedToolCalls[block.ToolCallID] = true
				}
			}
			continue
		}
		if message.Role == protocol.RoleTool {
			if skippedToolCalls[message.ToolCallID] {
				continue
			}
		} else if len(skippedToolCalls) > 0 {
			clear(skippedToolCalls)
		}
		out = append(out, message)
	}
	return out, nil
}

// repairInterruptedToolCalls balances only the final incomplete tool batch. A
// crash can occur after the assistant call is committed but before every tool
// result is appended; later ordinary messages would make the outcome ambiguous
// for reasons other than an interrupted dispatch, so older unmatched calls are
// deliberately left untouched.
func repairInterruptedToolCalls(store session.Store, registry tools.Registry) (int, error) {
	if store == nil {
		return 0, errors.New("session is nil")
	}
	messages, err := store.Messages()
	if err != nil {
		return 0, err
	}
	if len(messages) == 0 {
		return 0, nil
	}
	assistantIndex := len(messages) - 1
	for assistantIndex >= 0 && messages[assistantIndex].Role == protocol.RoleTool {
		assistantIndex--
	}
	if assistantIndex < 0 || messages[assistantIndex].Role != protocol.RoleAssistant {
		return 0, nil
	}
	assistant := messages[assistantIndex]
	calls := make([]protocol.ContentBlock, 0)
	for _, block := range assistant.Content {
		if block.Type == protocol.BlockToolCall && block.ToolCallID != "" {
			calls = append(calls, block)
		}
	}
	if len(calls) == 0 {
		return 0, nil
	}
	resolved := make(map[string]bool, len(messages)-assistantIndex-1)
	for _, message := range messages[assistantIndex+1:] {
		if message.Role != protocol.RoleTool {
			return 0, nil
		}
		resolved[message.ToolCallID] = true
	}
	entries := make([]session.Entry, 0, len(calls))
	parent := store.BranchTip()
	for _, call := range calls {
		if resolved[call.ToolCallID] {
			continue
		}
		risk := riskFor(call.Name)
		if registry != nil {
			if descriptor, ok := registry.Descriptor(call.Name); ok && descriptor.Risk != "" {
				risk = descriptor.Risk
			}
		}
		text := interruptedToolResultText(risk)
		message := protocol.NewToolResultMessage(newID(), parent, call.ToolCallID, call.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(text)}, true)
		entries = append(entries, session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: parent, Message: &message})
		parent = message.ID
	}
	if len(entries) == 0 {
		return 0, nil
	}
	if batch, ok := store.(session.BatchStore); ok {
		if err := batch.AppendBatch(entries); err != nil {
			return 0, err
		}
		return len(entries), nil
	}
	for _, entry := range entries {
		if err := store.Append(entry); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

func interruptedToolResultText(risk permission.Risk) string {
	if risk == permission.RiskRead {
		return "Error: the previous Snow process ended before this tool result was recorded. The operation may be retried."
	}
	return "Error: the previous Snow process ended after this tool was dispatched, so its external outcome is unknown. Inspect the current state before retrying; for potentially harmful or costly repetition, ask the user first."
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
			a.resetTurnIdentityLocked()
			a.latestContextTokens = 0
			a.latestRequestEstimate = 0
			a.latestContextReport = nil
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
	a.resetMailboxUnread()
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
	a.resetTurnIdentityLocked()
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	effort := a.effectiveThinkingLocked(mode)
	a.mu.Unlock()
	a.resetMailboxUnread()
	a.publish(protocol.AgentEvent{Type: protocol.EvModeChanged, Mode: &protocol.CollaborationModeState{Mode: mode, ReasoningEffort: effort}})
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	a.publishGoalSnapshot()
	return branch, nil
}

// SessionTitle returns the synchronized session-wide display title.
func (a *Agent) SessionTitle() (string, error) {
	var title string
	err := a.withSessionRead(func(store session.Store) error {
		titles, ok := store.(session.TitleStore)
		if !ok {
			return errors.New("agent: session does not support titles")
		}
		var err error
		title, err = titles.SessionTitle()
		return err
	})
	return title, err
}

// RenameSession changes the display title without changing the session ID,
// path, branches, or conversation tips.
func (a *Agent) RenameSession(title string) error {
	unlock := a.LockAdmission()
	defer unlock()
	return a.RenameSessionAdmitted(title)
}

func (a *Agent) RenameSessionAdmitted(title string) error {
	a.mu.RLock()
	running := a.running
	store := a.opts.Session
	a.mu.RUnlock()
	if running {
		return errors.New("agent: cannot rename session while running")
	}
	titles, ok := store.(session.TitleStore)
	if !ok {
		return errors.New("agent: session does not support titles")
	}
	if err := titles.RenameSession(title); err != nil {
		return err
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
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
	deferActiveGoal := false
	if a.opts.Goal != nil {
		goal, err := a.opts.Goal.Get()
		if err != nil {
			return protocol.CompactionResult{}, fmt.Errorf("agent: inspect goal before compact: %w", err)
		}
		deferActiveGoal = goal != nil && goal.Status == protocol.GoalActive
	}
	if err := a.stopAutomaticForControl(ctx, "compact"); err != nil {
		if deferActiveGoal && a.opts.Goal != nil {
			_ = a.opts.Goal.Defer(true)
		}
		return protocol.CompactionResult{}, err
	}
	// Manual compaction is an explicit control boundary. Suppress every active
	// goal, not only one whose automatic worker happened to be running at this
	// instant; later readiness or mode transitions must not
	// silently restart work after the summary completes.
	if deferActiveGoal && a.opts.Goal != nil {
		if err := a.opts.Goal.Defer(true); err != nil {
			return protocol.CompactionResult{}, fmt.Errorf("agent: pause goal after compact: %w", err)
		}
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return protocol.CompactionResult{}, errors.New("agent: cannot compact while running")
	}
	if len(a.queuedInputs) > 0 {
		a.mu.Unlock()
		return protocol.CompactionResult{}, errors.New("agent: undelivered queued input is waiting for recovery; call ClearPendingInputs first")
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = false
	a.autoStop = false
	a.admitTurnIdentityLocked("compact")
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
		if deferActiveGoal && wasCanceled && a.opts.Goal != nil {
			_ = a.opts.Goal.Defer(true)
		}
	}()
	ctx = runCtx

	return a.compactActiveContext(ctx, compactionManual)
}

func isCompactionCheckpointText(text string) bool {
	return strings.HasPrefix(text, "Working-state checkpoint for compacted history:") ||
		strings.HasPrefix(text, "Conversation summary:")
}
