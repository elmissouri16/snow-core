package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
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
	overflowRecovered := false
	providerStartRetries := 0
	syntheticOnlyBatches := 0
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
		msgs = a.pruneHistoricalToolResults(ctx, msgs)

		internalContext, err := a.goalInternalContext()
		if err != nil {
			return fmt.Errorf("agent: load goal context: %w", err)
		}
		if reminder := a.takeRepeatedToolReminder(); reminder != "" {
			internalContext = append(internalContext, protocol.InternalContextFragment{Source: "loop-guard", Text: reminder})
		}
		req := protocol.ChatRequest{
			Model:              a.Model(),
			Messages:           providerMessages(msgs),
			Tools:              a.requestToolSchemas(),
			System:             a.requestSystemPrompt(),
			Thinking:           a.requestThinking(),
			ReasoningSummary:   a.ReasoningSummary(),
			TextVerbosity:      a.TextVerbosity(),
			InternalContext:    internalContext,
			SessionAffinityKey: a.requestAffinityKey("turn"),
		}

		requestEstimate := estimateRequestTokens(req.Messages, req.System, req.Tools)
		contextReport := buildContextReport(req, true)
		a.mu.Lock()
		a.latestRequestEstimate = requestEstimate
		a.latestContextReport = &contextReport
		a.mu.Unlock()

		stop, err := a.streamTurnWithErrors(ctx, req, overflowRecovered)
		if err != nil {
			var startErr *providerStartError
			startFailure := errors.As(err, &startErr)
			if !startFailure {
				providerStartRetries = 0
			}
			if !overflowRecovered && a.autoThresholdPercent() > 0 && ctx.Err() == nil && provider.IsContextWindowExceeded(err) {
				result, compactErr := a.compactActiveContext(ctx, compactionOverflow)
				if compactErr == nil && result.SummarizedMessages > 0 {
					overflowRecovered = true
					a.mu.Lock()
					a.latestContextTokens = 0
					a.latestRequestEstimate = 0
					a.mu.Unlock()
					turn-- // the failed oversized request did not complete a model round
					continue
				}
				if compactErr != nil {
					a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
					return errors.Join(err, fmt.Errorf("agent: overflow recovery compaction: %w", compactErr))
				}
			}

			// A provider failure before a stream exists is safe to retry once:
			// no model output or tool side effect can have escaped that attempt.
			if ctx.Err() == nil && providerStartRetries < 1 && startFailure && provider.IsTransientError(err) {
				providerStartRetries++
				a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: "transient provider startup failure; retrying once"})
				if waitErr := waitForContext(ctx, goalTransientRetryDelay); waitErr != nil {
					waitErr = errors.Join(waitErr, a.persistAbortedBoundary())
					a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
					return waitErr
				}
				turn-- // a pre-stream transport failure did not complete a model round
				continue
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
					providerStartRetries = 0
					syntheticOnlyBatches = 0
					continue
				}
			}
			if !overflowRecovered {
				a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
			}
			return err
		}
		providerStartRetries = 0
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
