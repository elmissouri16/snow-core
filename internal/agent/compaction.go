package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func latestPersistedContextTokens(messages []protocol.Message) int {
	// Usage before a projected compaction summary describes the old full
	// request. Usage after that marker is valid for the newly grown context.
	start := 0
	if len(messages) > 0 && messages[0].Role == protocol.RoleCustom && isCompactionCheckpointText(messageTextBlocks(messages[0])) {
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

func (a *Agent) autoThresholdPercent() int {
	threshold := a.opts.Compaction.AutoThresholdPercent
	if threshold == 0 && a.opts.Compaction.GoalAutoThresholdPercent > 0 {
		threshold = a.opts.Compaction.GoalAutoThresholdPercent
	}
	return threshold
}

func (a *Agent) pressureCompactionDue(messages []protocol.Message) bool {
	threshold := a.autoThresholdPercent()
	model := a.Model()
	if threshold == 0 || model.ContextWindow <= 0 {
		return false
	}
	estimated := estimateRequestTokens(messages, a.requestSystemPrompt(), a.requestToolSchemas())
	a.mu.Lock()
	if a.latestContextTokens <= 0 {
		a.latestContextTokens = latestPersistedContextTokens(messages)
	}
	current := a.latestContextTokens
	// Provider usage is authoritative for the prior request. Add only estimated
	// growth since that request so system/schema overhead is not double-counted.
	minGrowth := a.opts.Compaction.HistoricalToolResultThreshold / 4
	if minGrowth <= 0 {
		minGrowth = compact.HistoricalToolResultThreshold / 4
	}
	if growth := estimated - a.latestRequestEstimate; current > 0 && a.latestRequestEstimate > 0 && growth >= minGrowth {
		current += growth
	}
	stopped := a.autoStop
	a.mu.Unlock()
	return current > 0 && !stopped && int64(current)*100 >= int64(model.ContextWindow)*int64(threshold)
}

func (a *Agent) toolHistoryCompactionDue(messages []protocol.Message) bool {
	percent := a.opts.Compaction.ToolHistoryBudgetPercent
	model := a.Model()
	if percent <= 0 || model.ContextWindow <= 0 || len(messages) == 0 {
		return false
	}
	a.mu.RLock()
	stopped := a.autoStop
	a.mu.RUnlock()
	if stopped {
		return false
	}
	threshold := a.opts.Compaction.HistoricalToolResultThreshold
	if threshold <= 0 {
		threshold = compact.HistoricalToolResultThreshold
	}
	projected := messages
	if compact.NeedsHistoricalToolResultPruning(messages, threshold) {
		projected = compact.PruneHistoricalToolResults(messages, threshold, compact.HistoricalToolResultHead, compact.HistoricalToolResultTail)
	}
	plan := compact.PlannerWithOptions(projected, a.compactionPlannerOptions(model, projected))
	if len(plan.CompactionCandidates) == 0 {
		return false
	}
	eligible := estimateToolHistoryTokens(plan.CompactionCandidates)
	return int64(eligible)*100 >= int64(model.ContextWindow)*int64(percent)
}

func estimateToolHistoryTokens(messages []protocol.Message) int {
	bytes := 0
	for _, message := range messages {
		switch message.Role {
		case protocol.RoleAssistant:
			for _, block := range message.Content {
				if block.Type == protocol.BlockToolCall {
					bytes += len(block.Name) + len(block.ToolCallID) + len(block.Arguments)
				}
			}
		case protocol.RoleTool:
			bytes += len(message.ToolName) + len(message.ToolCallID)
			for _, block := range message.Content {
				if block.Type == protocol.BlockImage {
					bytes += estimateImageTokens(block.Data) * 4
					continue
				}
				bytes += len(block.Text) + len(block.Arguments)
			}
		}
	}
	return (bytes + 3) / 4
}

func (a *Agent) autoCompactionTriggerFor(messages []protocol.Message) compactionTrigger {
	if a.pressureCompactionDue(messages) {
		return compactionPressure
	}
	if a.toolHistoryCompactionDue(messages) {
		return compactionToolHistory
	}
	return ""
}

func (a *Agent) autoCompactionDue(messages []protocol.Message) bool {
	return a.autoCompactionTriggerFor(messages) != ""
}

// autoCompactAdmittedBoundary compacts at the safe top-of-cycle boundary of
// any admitted turn. If the turn captured a goal, the goal snapshot must still
// be active and matching before its context may be replaced.
func (a *Agent) autoCompactAdmittedBoundary(ctx context.Context, messages []protocol.Message) (bool, error) {
	a.mu.RLock()
	admittedGoal := a.goalAtTurn.Clone()
	a.mu.RUnlock()
	trigger := a.autoCompactionTriggerFor(messages)
	if trigger == "" {
		return false, nil
	}
	if admittedGoal != nil {
		if a.opts.Goal == nil {
			return false, nil
		}
		goal, err := a.opts.Goal.Get()
		if err != nil || goal == nil || goal.GoalID != admittedGoal.GoalID || goal.Status != protocol.GoalActive {
			return false, err
		}
	}
	result, err := a.compactActiveContext(ctx, trigger)
	if err != nil {
		return true, err
	}
	if result.SummarizedMessages > 0 {
		a.mu.Lock()
		a.latestContextTokens = 0
		a.latestRequestEstimate = 0
		a.mu.Unlock()
	}
	return true, nil
}

// Deprecated compatibility wrapper for internal embedders and older tests.
func (a *Agent) autoCompactAdmittedGoalBoundary(ctx context.Context, messages []protocol.Message) (bool, error) {
	return a.autoCompactAdmittedBoundary(ctx, messages)
}

func (a *Agent) autoCompactGoalBoundary(ctx context.Context) (bool, error) {
	messages, err := a.contextMessagesCurrent()
	if err != nil {
		return false, err
	}
	trigger := a.autoCompactionTriggerFor(messages)
	if trigger == "" {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.Lock()
	if a.closed || a.running || a.autoStop || a.mode != protocol.ModeDefault || len(a.queuedInputs) > 0 {
		a.mu.Unlock()
		return false, nil
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = false
	a.admitTurnIdentityLocked("compact")
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

	result, err := a.compactActiveContext(runCtx, trigger)
	if err != nil {
		return true, err
	}
	if result.SummarizedMessages > 0 {
		a.mu.Lock()
		a.latestContextTokens = 0
		a.latestRequestEstimate = 0
		a.mu.Unlock()
	}
	return true, nil
}

func (a *Agent) compactionPlannerOptions(model protocol.Model, messages []protocol.Message) compact.PlannerOptions {
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
	if compactionTailIsActive(messages) {
		// The in-flight user/tool cycle is retained in addition to the configured
		// number of complete recent turns; it must not consume that quality floor.
		minTurns++
	}
	return compact.PlannerOptions{
		RetainTokens:          budget,
		MinRetainedTurns:      minTurns,
		AllowActiveToolCycles: compactionTailIsActive(messages),
	}
}

func compactionTailIsActive(messages []protocol.Message) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	switch last.Role {
	case protocol.RoleUser, protocol.RoleAgent, protocol.RoleTool:
		return true
	case protocol.RoleAssistant:
		return last.StopReason == protocol.StopToolUse || last.StopReason == protocol.StopPending
	default:
		return false
	}
}

func (a *Agent) compactActiveContext(ctx context.Context, trigger compactionTrigger) (protocol.CompactionResult, error) {
	automatic := trigger != compactionManual
	msgs, err := a.contextMessagesCurrent()
	if err != nil {
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact load context: %w", err)
	}
	model := a.Model()
	plan := compact.PlannerWithOptions(msgs, a.compactionPlannerOptions(model, msgs))
	result := protocol.CompactionResult{SummarizedMessages: len(plan.CompactionCandidates), RetainedMessages: len(msgs) - len(plan.CompactionCandidates), Automatic: automatic}
	message := fmt.Sprintf("compacting %d messages", result.SummarizedMessages)
	if trigger == compactionPressure {
		message = fmt.Sprintf("context reached %d%%; %s", a.autoThresholdPercent(), message)
	} else if trigger == compactionToolHistory {
		message = fmt.Sprintf("completed tool history reached %d%% of the model window; %s", a.opts.Compaction.ToolHistoryBudgetPercent, message)
	} else if trigger == compactionOverflow {
		message = "provider rejected the context as too large; " + message + " before one retry"
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

	retainedRefs, referenceVerificationErr := a.verifiedCompactedArtifactReferences(ctx, plan.CompactionCandidates)
	summaryInput := a.pruneHistoricalToolResults(ctx, plan.CompactionCandidates)
	summary, summaryErr := a.summarizeForCompaction(ctx, summaryInput)
	usedFallback := false
	if summaryErr == nil && strings.TrimSpace(summary) == "" {
		summaryErr = errors.New("provider returned a blank compaction summary")
	}
	if summaryErr != nil && a.opts.Compaction.Fallback != "error" {
		if ctx.Err() != nil {
			a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: ctx.Err().Error(), IsError: true, Compaction: &result})
			return protocol.CompactionResult{}, ctx.Err()
		}
		summary, summaryErr = compact.DefaultSummarizer(ctx, summaryInput)
		usedFallback = summaryErr == nil
	}
	if summaryErr != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: summaryErr.Error(), IsError: true, Compaction: &result})
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact summary: %w", summaryErr)
	}
	summary, repairedSummary, normalizeErr := compact.NormalizeWorkingStateCheckpoint(ctx, summary, summaryInput)
	if normalizeErr != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: normalizeErr.Error(), IsError: true, Compaction: &result})
		return protocol.CompactionResult{}, fmt.Errorf("agent: normalize compact summary: %w", normalizeErr)
	}
	if repairedSummary && a.opts.Compaction.Fallback == "error" {
		summaryErr = errors.New("provider compaction summary contained tool-protocol markup")
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: summaryErr.Error(), IsError: true, Compaction: &result})
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact summary quality: %w", summaryErr)
	}
	usedFallback = usedFallback || repairedSummary
	transcriptRef, transcriptErr := a.saveCompactedToolTranscript(ctx, plan.CompactionCandidates, plan.BoundaryID)
	manifestRefs := boundedCompactionReferences(retainedRefs, transcriptRef)
	summary = rebuildCompactionRetrievalSection(summary, manifestRefs)
	result.Summary = summary
	result.UsedFallback = usedFallback
	if _, err = compact.Apply(ctx, a.opts.Session, func(context.Context, []protocol.Message) (string, error) { return summary, nil }, plan); err != nil {
		a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: err.Error(), IsError: true, Compaction: &result})
		return protocol.CompactionResult{}, fmt.Errorf("agent: compact apply: %w", err)
	}
	a.mu.Lock()
	a.latestContextReport = nil
	a.mu.Unlock()
	var completionNotes []string
	if usedFallback {
		completionNotes = append(completionNotes, "provider summary required deterministic local fallback or repair")
	}
	if referenceVerificationErr != nil {
		completionNotes = append(completionNotes, "working-state checkpoint saved; some prior retrieval references could not be verified")
	}
	if transcriptErr != nil || (estimateToolHistoryTokens(plan.CompactionCandidates) > 0 && transcriptRef == "") {
		completionNotes = append(completionNotes, "working-state checkpoint saved; exact compacted tool transcript is unavailable")
	}
	message = strings.Join(completionNotes, "; ")
	// Persisted session mutation is observable before the terminal compaction
	// boundary. Keeping EvCompactionDone last prevents consumers from settling
	// the turn and then being resurrected by a trailing attributed update.
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	a.publish(protocol.AgentEvent{Type: protocol.EvCompactionDone, Message: message, Compaction: &result})
	return result, nil
}

func (a *Agent) summarizeForCompaction(ctx context.Context, msgs []protocol.Message) (string, error) {
	p := a.currentProvider()
	contract := `Create a factual working-state checkpoint for a coding agent, not a conversational recap. Return bounded Markdown using exactly these headings, in this order:
# Working State Checkpoint
## Objective and constraints
## Current working state
## Decisions and rationale
## Files and symbols
## Commands and verification
## Important tool results
## Errors and failed approaches
## Attributed agent updates
## Prior working-state checkpoints
## Retrieval references
## Unresolved next steps
## Active tool batch

Preserve exact identifiers, paths, artifact IDs, commands, test outcomes, failure state, and pending work when known. Carry forward still-relevant facts and retrieval references from prior checkpoints. The compaction boundary is safe, so Active tool batch should normally say none; never invent one. Do not invent facts or call tools.`
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
		Messages:           providerMessages(msgs),
		System:             contract,
		MaxTokens:          maxTokens,
		Thinking:           protocol.ThinkingOff,
		SessionAffinityKey: a.requestAffinityKey("compaction"),
	}
	profile := a.retryProfile()
	attempt := 0
	var retryStarted time.Time
	recovery := false
	for {
		attempt++
		stream, err := p.Chat(ctx, req)
		activity := false
		var summary string
		if err == nil {
			summary, activity, err = readCompactionSummary(ctx, stream)
		}
		if err == nil {
			return summary, nil
		}
		advice, retryable := provider.RetryAdviceFor(err)
		if !retryable || ctx.Err() != nil {
			return "", err
		}
		if retryStarted.IsZero() {
			retryStarted = time.Now()
		}
		recovery = recovery || activity
		delay, ok := a.scheduleProviderRetry(ctx, profile, retryStarted, attempt, recovery, advice)
		if !ok {
			return "", err
		}
		if waitErr := waitForRetry(ctx, delay); waitErr != nil {
			return "", waitErr
		}
	}
}

func readCompactionSummary(ctx context.Context, stream protocol.EventStream) (string, bool, error) {
	if stream == nil {
		return "", false, errors.New("provider summary returned a nil stream")
	}
	defer stream.Close()
	var out strings.Builder
	activity := false
	for {
		ev, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			cause := errors.New("provider summary stream ended before terminal done event")
			return "", activity, &provider.AdvisedError{Err: cause, Advice: provider.RetryAdvice{Kind: provider.RetryTransient}}
		}
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", activity, err
			}
			if _, retryable := provider.RetryAdviceFor(err); retryable {
				return "", activity, err
			}
			return "", activity, &provider.AdvisedError{Err: err, Advice: provider.RetryAdvice{Kind: provider.RetryTransient}}
		}
		switch ev.Type {
		case protocol.EvStreamTextDelta:
			activity = true
			out.WriteString(ev.Text)
		case protocol.EvStreamThinkingDelta, protocol.EvStreamProviderData, protocol.EvStreamUsage,
			protocol.EvStreamToolCallDelta, protocol.EvStreamToolCallDone:
			activity = true
		case protocol.EvStreamDone:
			if ev.StopReason == protocol.StopError || ev.StopReason == protocol.StopAborted {
				return "", activity, fmt.Errorf("provider summary stopped with %s", ev.StopReason)
			}
			summary := strings.TrimSpace(out.String())
			if summary == "" {
				return "", activity, errors.New("provider summary returned no text")
			}
			return summary, activity, nil
		case protocol.EvStreamError:
			if ev.Err != nil {
				return "", activity, ev.Err
			}
			return "", activity, errors.New("provider summary failed")
		}
	}
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
		a.activeTurnSequence = 0
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
	if len(a.queuedInputs) > 0 {
		a.mu.Unlock()
		return errors.New("agent: undelivered queued input is waiting for recovery; call ClearPendingInputs first")
	}
	a.running = true
	a.queuedInputs = nil
	a.queueAccepting = true
	a.turnWG.Add(1)
	a.turnMode = a.mode
	a.admitTurnIdentityLocked("subagent")
	a.turnStarted = time.Now()
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
		a.closeInputQueue(retErr == nil || ctx.Err() != nil)
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
