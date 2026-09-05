package agent

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

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
	a.mailboxClosed = true
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
	candidates, selected, err := a.searchPermittedDeferred(ctx, query, defaultDeferredTopK)
	latency := time.Since(started).Milliseconds()
	fallback := err != nil
	if fallback {
		selected = a.fallbackDeferred(query, defaultDeferredTopK)
	}
	ids := a.filterRequestDeferredIDs(a.expandDeferredIDs(matchIDs(selected), true))
	a.mu.Lock()
	a.baseDeferred = slices.Clone(ids)
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
	ids := a.filterRequestDeferredIDs(a.expandDeferredIDs(matchIDs(selected), true))
	a.mu.Lock()
	a.searchedDeferred = slices.Clone(ids)
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
		desc, ok := tools.Metadata(a.opts.Registry, match.ID)
		if !ok || !desc.Deferred || !tools.CanExposeMetadata(a.opts.Permission, desc) {
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

func (a *Agent) searchPermittedDeferred(ctx context.Context, query string, limit int) ([]tools.ToolMatch, []tools.ToolMatch, error) {
	count := a.opts.Router.DeferredCount()
	candidateLimit := min(count, max(deferredCandidateK, limit))
	var candidates, selected []tools.ToolMatch
	for candidateLimit > 0 {
		matches, err := a.opts.Router.Search(ctx, query, candidateLimit)
		candidates = matches
		if err != nil {
			return candidates, nil, err
		}
		selected = a.selectPermittedMatches(matches, limit)
		if len(selected) >= limit || candidateLimit >= count {
			return candidates, selected, nil
		}
		candidateLimit = min(count, candidateLimit*2)
	}
	return nil, nil, nil
}

func (a *Agent) fallbackDeferred(query string, limit int) []tools.ToolMatch {
	terms := strings.Fields(strings.ToLower(query))
	type ranked struct {
		match tools.ToolMatch
		score float64
	}
	var rankedMatches []ranked
	for _, desc := range tools.SelectMetadata(a.opts.Registry, func(desc tools.DescriptorMetadata) bool {
		return desc.Deferred && tools.CanExposeMetadata(a.opts.Permission, desc)
	}) {
		name := strings.ToLower(desc.Name)
		originalName := strings.ToLower(desc.OriginalName)
		namespace := strings.ToLower(desc.Namespace)
		description := strings.ToLower(desc.Description)
		keywords := strings.ToLower(strings.Join(desc.Keywords, " "))
		score := 0.0
		normalizedQuery := strings.TrimSpace(strings.ToLower(query))
		if normalizedQuery == name {
			score += 20
		}
		if originalName != "" && normalizedQuery == originalName {
			score += 20
		}
		for _, term := range terms {
			if strings.Contains(name, term) {
				score += 8
			}
			if strings.Contains(originalName, term) {
				score += 8
			}
			if strings.Contains(namespace, term) {
				score += 6
			}
			if strings.Contains(keywords, term) {
				score += 5
			}
			if strings.Contains(description, term) {
				score++
			}
		}
		rankedMatches = append(rankedMatches, ranked{match: tools.ToolMatch{ID: desc.Name, Namespace: desc.Namespace, Description: desc.Description, Score: score}, score: score})
	}
	slices.SortStableFunc(rankedMatches, func(a, b ranked) int {
		if byScore := cmp.Compare(b.score, a.score); byScore != 0 {
			return byScore
		}
		return cmp.Compare(a.match.ID, b.match.ID)
	})
	selected := make([]tools.ToolMatch, 0, min(limit, len(rankedMatches)))
	schemaBytes := 0
	for _, candidate := range rankedMatches {
		desc, ok := a.opts.Registry.Descriptor(candidate.match.ID)
		if !ok {
			continue
		}
		candidateBytes := providerSchemaBytes([]protocol.ToolSchema{desc.Schema})
		if schemaBytes+candidateBytes > maxDeferredFallbackSchemaBytes {
			continue
		}
		schemaBytes += candidateBytes
		selected = append(selected, candidate.match)
		if len(selected) == limit {
			break
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

func (a *Agent) expandDeferredIDs(ids []string, includeSticky bool) []string {
	out := slices.Clone(ids)
	seen := make(map[string]bool, len(out))
	for _, id := range out {
		seen[id] = true
	}
	for _, bundle := range a.opts.DeferredBundles {
		active := includeSticky && bundle.Sticky != nil && bundle.Sticky()
		if !active {
			for _, member := range bundle.Members {
				if seen[member] {
					active = true
					break
				}
			}
		}
		if !active {
			continue
		}
		for _, member := range bundle.Members {
			if member == "" || seen[member] {
				continue
			}
			seen[member] = true
			out = append(out, member)
		}
	}
	return out
}

func (a *Agent) requestToolPolicy() func(tools.DescriptorMetadata) bool {
	a.mu.RLock()
	mode := a.turnMode
	origin := a.turnOrigin
	if !a.running {
		mode = a.mode
		origin = ""
	}
	a.mu.RUnlock()
	return func(desc tools.DescriptorMetadata) bool {
		name := desc.Name
		if origin == "goal" && (name == "ask_user" || name == "request_user_input") {
			return false
		}
		if desc.Risk == permission.RiskDelegate && !tools.CanExposeMetadata(a.opts.Permission, desc) {
			return false
		}
		if mode == protocol.ModePlan {
			return name != "ask_user" && name != "update_plan" && collaborationToolAllowed(mode, desc)
		}
		return name != "request_user_input"
	}
}

func (a *Agent) filterRequestDeferredIDs(ids []string) []string {
	allowed := a.requestToolPolicy()
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, name := range ids {
		if seen[name] {
			continue
		}
		metadata, ok := tools.Metadata(a.opts.Registry, name)
		if !ok || !metadata.Deferred || !allowed(metadata) || !tools.CanExposeMetadata(a.opts.Permission, metadata) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func (a *Agent) requestToolSchemasWithTurnRouting(includeTurnRouting bool) []protocol.ToolSchema {
	a.mu.RLock()
	var base, searched []string
	if includeTurnRouting {
		base = slices.Clone(a.baseDeferred)
		searched = slices.Clone(a.searchedDeferred)
	}
	a.mu.RUnlock()
	allowed := a.requestToolPolicy()
	if a.opts.Router == nil {
		return tools.SelectSchemas(a.opts.Registry, allowed)
	}
	schemas := tools.SelectSchemas(a.opts.Registry, func(desc tools.DescriptorMetadata) bool {
		return !desc.Deferred && allowed(desc)
	})
	deferred := a.filterRequestDeferredIDs(a.expandDeferredIDs(append(base, searched...), true))
	for _, name := range deferred {
		desc, ok := a.opts.Registry.Descriptor(name)
		if !ok {
			continue
		}
		schemas = append(schemas, desc.Schema)
	}
	return schemas
}

func (a *Agent) requestToolSchemas() []protocol.ToolSchema {
	return a.requestToolSchemasWithTurnRouting(true)
}

func (a *Agent) publishToolRouting(trigger string, ids []string, candidates int, latency int64, fallback bool, routeErr error) {
	schemas := a.requestToolSchemas()
	eventIDs := slices.Clone(ids)
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

func hasProviderContinuity(messages []protocol.Message) bool {
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Type == protocol.BlockProviderData {
				return true
			}
		}
	}
	return false
}

func (a *Agent) run(ctx context.Context) error {
	stableTools := a.requestToolSchemasWithTurnRouting(false)
	stableSystem := a.requestSystemPromptForTools(stableTools)
	admittedFixedTokens := fixedContextTokensWithSchemaBytes(stableSystem, providerSchemaBytes(stableTools))
	turn := 0
	overflowRecovered := false
	providerAttempts := 0
	var retryStarted time.Time
	retryRecovery := false
	syntheticOnlyBatches := 0
	stepID := ""
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
		if compacted, compactErr := a.autoCompactAdmittedBoundary(ctx, msgs); compactErr != nil {
			if _, fatal := errors.AsType[*compactionAccountingError](compactErr); fatal {
				return compactErr
			}
			a.mu.RLock()
			goalTurn := a.goalAtTurn != nil
			a.mu.RUnlock()
			if goalTurn {
				return fmt.Errorf("goal auto-compaction: %w", compactErr)
			}
			// Pressure compaction is best-effort for direct user work. A provider
			// request may still fit, and overflow recovery gets one final chance.
		} else if compacted {
			msgs, err = a.contextMessagesCurrent()
			if err != nil {
				return fmt.Errorf("agent: reload compacted context: %w", err)
			}
		}
		msgs = a.pruneHistoricalToolResultsOwned(ctx, msgs)

		internalContext, err := a.goalInternalContext()
		if err != nil {
			return fmt.Errorf("agent: load goal context: %w", err)
		}
		// Reassert Default mode after provider-private reasoning continuity. Opaque
		// state was produced under the mode active for its owning turn and may retain
		// that instruction context across an attached Plan-to-Default transition.
		// Internal context is encoded after history by every provider adapter while
		// the complete owning turns and their private state remain intact.
		if a.capturedTurnMode() == protocol.ModeDefault && hasProviderContinuity(msgs) {
			internalContext = append(internalContext, defaultModeContext())
		}
		if reminder := a.takeRepeatedToolReminder(); reminder != "" {
			internalContext = append(internalContext, protocol.InternalContextFragment{Source: "loop-guard", Text: reminder})
		}
		if retryRecovery {
			internalContext = append(internalContext, protocol.InternalContextFragment{Source: "provider-recovery", Text: "The previous provider response was interrupted. Continue from the durable conversation and tool results. Do not assume unrecorded work completed, and do not repeat completed side effects unless the recorded result proves a retry is needed."})
		}
		requestTools := a.requestToolSchemas()
		req := protocol.ChatRequest{
			Model:                   a.Model(),
			Messages:                providerMessages(msgs),
			Tools:                   requestTools,
			System:                  a.requestSystemPromptForTools(requestTools),
			Thinking:                a.requestThinking(),
			ReasoningSummary:        a.ReasoningSummary(),
			TextVerbosity:           a.TextVerbosity(),
			InternalContext:         internalContext,
			SessionAffinityKey:      a.requestAffinityKey("turn"),
			ConversationAffinityKey: a.conversationAffinityKey(),
		}

		schemaBytes := providerSchemaBytes(req.Tools)
		fixedTokens := fixedContextTokensWithSchemaBytes(req.System, schemaBytes)
		if budget := a.fixedContextBudgetTokens(req.Model); budget > 0 && fixedTokens > budget && fixedTokens > admittedFixedTokens {
			return fixedContextBudgetError(fixedTokens, budget, a.opts.FixedContextBudgetPercent)
		}
		if fixedTokens > admittedFixedTokens {
			admittedFixedTokens = fixedTokens
		}

		requestEstimate := estimateRequestTokensWithSchemaBytes(req.Messages, req.System, schemaBytes)
		contextReport := buildContextReportWithSchemaBytes(req, true, schemaBytes)
		a.applyFixedContextBudget(&contextReport, req.Model)
		a.mu.Lock()
		a.latestRequestEstimate = requestEstimate
		a.latestContextReport = &contextReport
		a.mu.Unlock()

		// Persist one crash-durable logical step boundary immediately before the
		// first provider attempt. Overflow recovery and transport retries reuse the
		// same ID; ordinary tool-result continuations allocate a new step.
		if stepID == "" {
			stepID = newID()
			if err := a.persistStepMarker(stepID); err != nil {
				return err
			}
			a.publish(protocol.AgentEvent{Type: protocol.EvRunStatsUpdated})
		}
		providerAttempts++
		stop, err := a.streamTurnWithErrors(ctx, req, false)
		if err != nil {
			if !overflowRecovered && a.autoThresholdPercent() > 0 && ctx.Err() == nil && provider.IsContextWindowExceeded(err) {
				if !providerFailureActivity(err) {
					parent := a.opts.Session.BranchTip()
					if persistErr := a.persistAssistant(newID(), parent, nil, protocol.StopError, nil, err.Error()); persistErr != nil {
						return errors.Join(err, persistErr)
					}
				}
				result, compactErr := a.compactActiveContext(ctx, compactionOverflow)
				if compactErr == nil && result.SummarizedMessages > 0 {
					overflowRecovered = true
					a.mu.Lock()
					a.latestContextTokens = 0
					a.latestRequestEstimate = 0
					a.mu.Unlock()
					providerAttempts = 0
					retryStarted = time.Time{}
					retryRecovery = false
					turn-- // the failed oversized request did not complete a model round
					continue
				}
				if compactErr != nil {
					a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
					return errors.Join(err, fmt.Errorf("agent: overflow recovery compaction: %w", compactErr))
				}
			}

			// Provider adapters classify but never schedule retries. Before activity,
			// this repeats the side-effect-free request. After activity, the failed
			// assistant boundary has been persisted and the next request is a durable
			// continuation; incomplete tool calls were never dispatched.
			if advice, retryable := provider.RetryAdviceFor(err); retryable && isProviderFailure(err) && ctx.Err() == nil {
				if retryStarted.IsZero() {
					retryStarted = time.Now()
				}
				retryRecovery = retryRecovery || providerFailureActivity(err)
				profile := a.retryProfile()
				if delay, ok := a.scheduleProviderRetry(ctx, profile, retryStarted, providerAttempts, retryRecovery, advice); ok {
					if waitErr := waitForRetry(ctx, delay); waitErr != nil {
						waitErr = errors.Join(waitErr, a.persistAbortedBoundary())
						a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
						return waitErr
					}
					turn-- // a failed provider attempt did not complete a model round
					continue
				}
			}

			// Accepted steering/follow-up work must not disappear behind an
			// ordinary provider failure. Persist one eligible item and let it
			// start a fresh request; repeated failures consume the finite queue.
			// Internal persistence/accounting errors are never masked this way.
			if isProviderFailure(err) {
				canContinue := a.opts.MaxTurns == 0 || turn < a.opts.MaxTurns
				queued, ok, limited := a.takeQueuedInput(true, canContinue)
				if limited {
					a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "max turns reached"})
					return errors.New("agent: max turns reached")
				}
				if ok {
					if deliverErr := a.deliverQueuedInput(ctx, queued); deliverErr != nil {
						return errors.Join(err, deliverErr)
					}
					providerAttempts = 0
					retryStarted = time.Time{}
					retryRecovery = false
					syntheticOnlyBatches = 0
					stepID = ""
					continue
				}
			}
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
			return err
		}
		stepID = ""
		providerAttempts = 0
		retryStarted = time.Time{}
		retryRecovery = false
		naturalStop := false
		switch stop {
		case protocol.StopToolUse:
			// Steering never skips tool calls. Finish the complete serial batch,
			// including cancellation placeholders, before checking the queue.
			batch, batchErr := a.executeToolCalls(ctx)
			if batchErr != nil {
				if ctx.Err() != nil {
					batchErr = errors.Join(batchErr, a.persistAbortedBoundary())
					a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
				}
				return batchErr
			}
			if batch.Calls > 0 && batch.Dispatched == 0 {
				syntheticOnlyBatches++
				if syntheticOnlyBatches > maxConsecutiveSyntheticBatches && (a.opts.MaxTurns == 0 || turn < a.opts.MaxTurns) {
					err := errors.New("agent: repeated tool batches produced only synthetic error results")
					a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
					return err
				}
			} else {
				syntheticOnlyBatches = 0
			}
			if err := ctx.Err(); err != nil {
				err = errors.Join(err, a.persistAbortedBoundary())
				a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
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
			syntheticOnlyBatches = 0
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
	// A queued input is a fresh user instruction even though it reuses the
	// admitted run, so it resets both Plan response state and loop detection.
	a.turnPlanSeen = false
	a.repeatedTool = repeatedToolCallState{}
	a.mu.Unlock()
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if err := a.activateExplicitSkillMentions(ctx, item.Text); err != nil {
		return fmt.Errorf("agent: activate explicit skill from queued input: %w", err)
	}
	a.prepareToolRouting(ctx, item.Text)
	return nil
}

func (a *Agent) requestAffinityKey(purpose string) string {
	identity := a.sessionBranchAffinityIdentity()
	if identity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(identity + "\x00" + purpose))
	return hex.EncodeToString(sum[:])
}

func (a *Agent) conversationAffinityKey() string {
	identity := a.sessionBranchAffinityIdentity()
	if identity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func (a *Agent) sessionBranchAffinityIdentity() string {
	if a.opts.Session == nil || a.opts.Session.ID() == "" {
		return ""
	}
	branchID := ""
	if branches, ok := a.opts.Session.(session.ActiveBranchStore); ok {
		branchID = branches.ActiveBranchID()
	}
	return a.opts.Session.ID() + "\x00" + branchID
}
