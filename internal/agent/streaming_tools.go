package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (a *Agent) streamTurnWithErrors(ctx context.Context, req protocol.ChatRequest, publishErrors bool) (protocol.StopReason, error) {
	a.mu.Lock()
	a.latestContextTokens = 0
	a.mu.Unlock()
	provider := a.currentProvider()
	asstID := newID()
	parent := a.opts.Session.BranchTip()
	stream, err := provider.Chat(ctx, req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			if perr := a.persistAssistant(asstID, parent, nil, protocol.StopAborted, nil, ""); perr != nil {
				return protocol.StopAborted, perr
			}
			a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
			return protocol.StopAborted, nil
		}
		if publishErrors {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
		}
		return protocol.StopError, &providerStartError{err: err}
	}
	defer stream.Close()

	var content []protocol.ContentBlock
	var providerData []protocol.ContentBlock
	var usage *protocol.Usage
	var stop protocol.StopReason = protocol.StopPending
	var thinkingBuf strings.Builder
	a.mu.RLock()
	planEnabled := a.turnMode == protocol.ModePlan && !a.turnPlanSeen
	a.mu.RUnlock()
	collector := newPlanStreamCollector(planEnabled, asstID+"-plan", a.publish, func() {
		a.mu.Lock()
		a.turnPlanSeen = true
		a.mu.Unlock()
	})
	toolCalls := map[string]protocol.ContentBlock{} // id -> block
	toolDone := map[string]bool{}                   // id -> final arguments observed
	toolOrder := []string{}                         // first-seen id order
	sawDone := false
	activity := false

streamLoop:
	for {
		ev, err := stream.Next(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				stop = protocol.StopAborted
				collector.Interrupt()
				content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
				if perr := a.persistAssistant(asstID, parent, content, stop, usage, ""); perr != nil {
					return protocol.StopAborted, perr
				}
				a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
				return protocol.StopAborted, nil
			}
			if errors.Is(err, io.EOF) && !sawDone {
				err = errors.New("provider stream ended before terminal done event")
			}
			stop = protocol.StopError
			collector.Interrupt()
			content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
			if activity {
				if perr := a.persistAssistant(asstID, parent, content, stop, usage, err.Error()); perr != nil {
					return protocol.StopError, perr
				}
			}
			if publishErrors {
				a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()})
			}
			return protocol.StopError, &providerTurnError{err: err, activity: activity}
		}

		if ev.Type != protocol.EvStreamError && ev.Type != protocol.EvStreamDone {
			activity = true
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
			thinkingBuf.WriteString(ev.Text)
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
			toolDone[ev.ToolCallID] = true
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
				// Positive cache reads are inherently known even for older or custom
				// providers that predate the explicit presence marker.
				if normalized.CacheRead > 0 {
					normalized.CacheReadKnown = true
				}
				if normalized.Total == 0 {
					normalized.Total = normalized.Input + normalized.Output
				}
				if normalized.Cost == nil {
					normalized.Cost = normalized.CostFor(a.Model().Pricing)
				}
				usage = &normalized
				a.mu.Lock()
				a.latestContextTokens = contextTokensForCompaction(normalized)
				if a.latestContextReport != nil {
					a.latestContextReport.Usage = normalized.Clone()
				}
				a.mu.Unlock()
				a.publish(protocol.AgentEvent{Type: protocol.EvUsage, Usage: normalized.Clone()})
			}
		case protocol.EvStreamDone:
			stop = ev.StopReason
			if stop == "" {
				stop = protocol.StopStop
			}
			sawDone = true
			break streamLoop
		case protocol.EvStreamError:
			stop = protocol.StopError
			errMsg := "provider error"
			if ev.Err != nil {
				errMsg = ev.Err.Error()
			}
			collector.Interrupt()
			content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
			if activity {
				if perr := a.persistAssistant(asstID, parent, content, stop, usage, errMsg); perr != nil {
					return protocol.StopError, perr
				}
			}
			if publishErrors {
				a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: errMsg})
			}
			if ev.Err != nil {
				return protocol.StopError, &providerTurnError{err: fmt.Errorf("agent: provider stream: %w", ev.Err), activity: activity}
			}
			return protocol.StopError, &providerTurnError{err: fmt.Errorf("agent: %s", errMsg), activity: activity}
		}
	}

	if !validTerminalStop(stop) {
		protocolErr := fmt.Errorf("provider emitted invalid terminal stop reason %q", stop)
		collector.Interrupt()
		content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
		if perr := a.persistAssistant(asstID, parent, content, protocol.StopError, usage, protocolErr.Error()); perr != nil {
			return protocol.StopError, perr
		}
		if publishErrors {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: protocolErr.Error()})
		}
		return protocol.StopError, &providerTurnError{err: protocolErr, activity: activity}
	}

	// Terminal error/abort signals never commit streamed tool calls. They may be
	// partial, and no execution is safe after the provider has rejected the turn.
	if stop == protocol.StopAborted || stop == protocol.StopError {
		collector.Interrupt()
		content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
		errMsg := ""
		if stop == protocol.StopError {
			errMsg = "provider stopped with error"
		}
		if perr := a.persistAssistant(asstID, parent, content, stop, usage, errMsg); perr != nil {
			return stop, perr
		}
		if stop == protocol.StopAborted {
			a.publish(protocol.AgentEvent{Type: protocol.EvAborted})
			return stop, nil
		}
		if publishErrors {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: errMsg})
		}
		return stop, &providerTurnError{err: errors.New(errMsg), activity: activity}
	}

	// Validate terminal metadata against the actual streamed calls. Content is
	// authoritative when a provider reports stop despite complete calls. Length
	// truncation is different: arguments may parse while being silently partial,
	// so every call receives a synthetic error and none is executed.
	hasToolCalls := len(toolOrder) > 0
	allToolCallsDone := true
	validToolIdentities := true
	for _, id := range toolOrder {
		call := toolCalls[id]
		if strings.TrimSpace(call.ToolCallID) == "" || strings.TrimSpace(call.Name) == "" {
			validToolIdentities = false
		}
		if !toolDone[id] {
			allToolCallsDone = false
		}
	}
	var protocolErr error
	switch {
	case stop == protocol.StopToolUse && !hasToolCalls:
		protocolErr = errors.New("provider stopped for tool use without any tool calls")
	case hasToolCalls && !validToolIdentities:
		protocolErr = errors.New("provider emitted a tool call without both an ID and name")
	case hasToolCalls && stop != protocol.StopLength && !allToolCallsDone:
		protocolErr = errors.New("provider ended with an incomplete tool call")
	}
	if protocolErr != nil {
		collector.Interrupt()
		content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
		if perr := a.persistAssistant(asstID, parent, content, protocol.StopError, usage, protocolErr.Error()); perr != nil {
			return protocol.StopError, perr
		}
		if publishErrors {
			a.publish(protocol.AgentEvent{Type: protocol.EvError, Message: protocolErr.Error()})
		}
		return protocol.StopError, &providerTurnError{err: protocolErr, activity: activity}
	}

	persistedStop := stop
	returnedStop := stop
	pendingToolError := ""
	if hasToolCalls {
		switch stop {
		case protocol.StopLength:
			returnedStop = protocol.StopToolUse
			pendingToolError = "tool call was not executed because model output was truncated; arguments may be incomplete"
		case protocol.StopStop:
			persistedStop = protocol.StopToolUse
			returnedStop = protocol.StopToolUse
		}
	}

	collector.Finish()
	content = assistantResponseContentWithProviderData(thinkingBuf.String(), providerData, collector.Blocks())
	for _, id := range toolOrder {
		if cb, ok := toolCalls[id]; ok {
			// RawMessage must contain valid JSON for SQLite/JSONL persistence.
			// Keep the original in pending so executeOne can return a precise
			// malformed-argument result instead of accidentally running the tool.
			persisted := cb
			if len(persisted.Arguments) > 0 && !json.Valid(persisted.Arguments) {
				persisted.Arguments = json.RawMessage("{}")
			}
			content = append(content, persisted)
		}
	}
	if err := a.persistAssistant(asstID, parent, content, persistedStop, usage, ""); err != nil {
		return persistedStop, err
	}
	collector.PublishCompleted()

	// Stash tool calls for serial execution or synthetic truncated-call results.
	if returnedStop == protocol.StopToolUse {
		a.mu.Lock()
		a.pending = make(map[string]protocol.ContentBlock)
		a.pendingOrder = a.pendingOrder[:0]
		a.pendingToolError = pendingToolError
		for _, id := range toolOrder {
			if cb, ok := toolCalls[id]; ok && cb.Type == protocol.BlockToolCall {
				a.pending[cb.ToolCallID] = cb
				a.pendingOrder = append(a.pendingOrder, cb.ToolCallID)
			}
		}
		a.mu.Unlock()
	}

	return returnedStop, nil
}

func validTerminalStop(stop protocol.StopReason) bool {
	switch stop {
	case protocol.StopStop, protocol.StopLength, protocol.StopToolUse, protocol.StopError, protocol.StopAborted:
		return true
	default:
		return false
	}
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

func estimateRequestTokens(messages []protocol.Message, system string, schemas []protocol.ToolSchema) int {
	bytes := len(system) + providerSchemaBytes(schemas)
	for _, message := range messages {
		bytes += len(message.Role) + len(message.ToolName) + len(message.ToolCallID)
		for _, block := range message.Content {
			bytes += len(block.Text) + len(block.Arguments) + len(block.Data) + len(block.Name) + len(block.ToolCallID)
		}
	}
	return bytes / 4
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

func (a *Agent) persistAbortedBoundary() error {
	parent := a.opts.Session.BranchTip()
	return a.persistAssistant(newID(), parent, nil, protocol.StopAborted, nil, "")
}

func (a *Agent) persistAssistant(id, parent string, content []protocol.ContentBlock, stop protocol.StopReason, usage *protocol.Usage, errMsg string) error {
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
	if usage != nil {
		a.mu.Lock()
		a.turnUsage = a.turnUsage.Add(*usage)
		a.usageSet = true
		a.mu.Unlock()
		if err := a.accountGoalUsage(*usage); err != nil {
			return fmt.Errorf("agent: account goal usage: %w", err)
		}
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
}

// executeToolCalls runs the pending tool calls serially (in stream order)
// and persists results. Aborts early when ctx is cancelled.
func (a *Agent) executeToolCalls(ctx context.Context) (toolBatchResult, error) {
	a.mu.Lock()
	pending := a.pending
	order := append([]string(nil), a.pendingOrder...)
	forcedError := a.pendingToolError
	a.pending = make(map[string]protocol.ContentBlock)
	a.pendingOrder = a.pendingOrder[:0]
	a.pendingToolError = ""
	a.mu.Unlock()

	parent := a.opts.Session.BranchTip()
	result := toolBatchResult{}

	for i, id := range order {
		cb, ok := pending[id]
		if !ok {
			continue
		}
		result.Calls++
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
					return result, appendErr
				}
				parent = msg.ID
			}
			return result, err
		}
		if forcedError != "" {
			msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
				[]protocol.ContentBlock{protocol.NewTextBlock("Error: " + forcedError)}, true)
			if err := a.appendToolResult(parent, msg); err != nil {
				return result, err
			}
			parent = msg.ID
			continue
		}
		a.mu.Lock()
		limitReached := a.opts.CallLimit > 0 && a.turnToolCalls >= a.opts.CallLimit
		if !limitReached {
			a.turnToolCalls++
		}
		a.mu.Unlock()
		if limitReached {
			// Emit an error result for skipped calls so the provider never
			// sees tool_calls without results.
			msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
				[]protocol.ContentBlock{protocol.NewTextBlock(
					fmt.Sprintf("Error: tool call skipped (call limit %d reached)", a.opts.CallLimit))}, true)
			if err := a.appendToolResult(parent, msg); err != nil {
				return result, err
			}
			// Chain: the next result attaches to this one so every tool call
			// result stays on the root→tip path (no dangling tool_calls).
			parent = msg.ID
			continue
		}
		msg, dispatched, err := a.executeOne(ctx, cb, parent)
		if dispatched {
			result.Dispatched++
		}
		if err != nil {
			return result, err
		}
		a.observeRepeatedToolCall(cb.Name, cb.Arguments)
		// Chain tool results serially so all of them remain on the branch
		// tip path; otherwise only the last result is visible to Messages().
		parent = msg.ID
	}
	return result, nil
}

func (a *Agent) observeRepeatedToolCall(name string, raw json.RawMessage) {
	canonical := canonicalToolArguments(raw)
	a.mu.Lock()
	defer a.mu.Unlock()
	if repeatedToolCallExcluded(name) {
		return
	}
	state := &a.repeatedTool
	if state.name != name || state.canonicalArgs != canonical {
		*state = repeatedToolCallState{name: name, canonicalArgs: canonical, count: 1}
		return
	}
	state.count++
	switch state.count {
	case repeatedToolFirstThreshold:
		state.reminders = append(state.reminders, "You are repeating the exact same tool call with identical arguments. Carefully analyze the previous result before calling it again: if the task is incomplete, use different arguments or a different approach; otherwise finish the task.")
	case repeatedToolNextThreshold, repeatedToolLastThreshold:
		preview := state.canonicalArgs
		if len(preview) > repeatedToolArgsPreview {
			omitted := len(preview) - repeatedToolArgsPreview
			body := []byte(preview)[:repeatedToolArgsPreview]
			for len(body) > 0 && !utf8.Valid(body) {
				body = body[:len(body)-1]
			}
			preview = string(body) + fmt.Sprintf("… (+%d more bytes)", omitted)
		}
		state.reminders = append(state.reminders, fmt.Sprintf("Repeated tool call detected:\n- tool: %s\n- consecutive_calls: %d\n- arguments: %s\nThe repeated calls are not making progress. Do not call this tool with these exact arguments again. Inspect the latest result and choose a different action, different arguments, or finish if enough evidence has been gathered.", name, state.count, preview))
	}
}

func (a *Agent) takeRepeatedToolReminder() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	reminder := strings.Join(a.repeatedTool.reminders, "\n\n")
	a.repeatedTool.reminders = nil
	return reminder
}

func repeatedToolCallExcluded(name string) bool {
	switch name {
	case "update_plan", "get_goal", "wait_agent":
		return true
	default:
		return false
	}
}

func canonicalToolArguments(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

// appendToolResult persists a tool_result message and emits its events.
// Details are private tool metadata used only for UI-facing previews.
func (a *Agent) appendToolResult(parent string, msg protocol.Message, details ...any) error {
	started, display := a.takeToolDisplay(msg.ToolCallID)
	output := toolResultText(msg.Content)
	for _, detail := range details {
		switch value := detail.(type) {
		case tools.PrivateDetails:
			output = "(private goal state updated)"
		case *tools.PrivateDetails:
			if value != nil {
				output = "(private goal state updated)"
			}
		}
	}
	if diff, ok := editDiffPreview(details); ok {
		output = diff
	}
	preview := boundEventText(output, 8*1024)
	durationMS := int64(0)
	if !started.IsZero() {
		durationMS = time.Since(started).Milliseconds()
	}
	msg.ToolDisplay = &protocol.ToolDisplay{
		Started:      !started.IsZero(),
		StartMessage: display.startMessage,
		Progress:     display.progress,
		Output:       preview,
		DurationMS:   durationMS,
	}
	messageEntry := session.Entry{
		Type:     session.EntryMessage,
		ID:       msg.ID,
		ParentID: parent,
		Message:  &msg,
	}
	deactivation := ""
	if !msg.IsError {
		for _, detail := range details {
			if name, ok := skillDeactivationName(detail); ok {
				deactivation = name
				break
			}
		}
	}
	var persistErr error
	if deactivation == "" {
		persistErr = a.opts.Session.Append(messageEntry)
	} else {
		batch, batchOK := a.opts.Session.(session.BatchStore)
		_, branchOK := a.opts.Session.(session.BranchEntryStore)
		if !batchOK || !branchOK {
			return errors.New("agent: deactivate_skill requires atomic branch-aware session storage")
		}
		markerID := newID()
		messageEntry.ParentID = markerID
		persistErr = batch.AppendBatch([]session.Entry{
			{Type: session.EntryMeta, ID: markerID, ParentID: parent, Key: skillDeactivationMeta, Value: deactivation},
			messageEntry,
		})
	}
	if persistErr != nil {
		return fmt.Errorf("agent: append tool result: %w", persistErr)
	}
	ev := protocol.AgentEvent{
		Type:           protocol.EvToolEnd,
		ToolCallID:     msg.ToolCallID,
		ToolName:       msg.ToolName,
		IsError:        msg.IsError,
		ToolOutput:     preview,
		ToolDurationMS: durationMS,
	}
	if msg.IsError {
		ev.Message = boundEventText(output, 2*1024)
	}
	a.publish(ev)
	return nil
}

func (a *Agent) executeOne(ctx context.Context, cb protocol.ContentBlock, parent string) (protocol.Message, bool, error) {
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
			return msg, false, err
		}
		return msg, false, nil
	}

	a.mu.RLock()
	mode, origin := a.turnMode, a.turnOrigin
	a.mu.RUnlock()
	if origin == "goal" && (cb.Name == "ask_user" || cb.Name == "request_user_input") {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock("Error: interactive user input is unavailable during automatic goal turns")}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, false, err
		}
		return msg, false, nil
	}
	if mode == protocol.ModePlan && cb.Name == "update_plan" {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock("update_plan is a TODO/checklist tool and is not allowed in Plan mode")}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, false, err
		}
		return msg, false, nil
	}
	if (mode == protocol.ModePlan && cb.Name == "ask_user") || (mode != protocol.ModePlan && cb.Name == "request_user_input") {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Error: %s is unavailable in %s mode", cb.Name, mode))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, false, err
		}
		return msg, false, nil
	}

	tool, ok := a.opts.Registry.Get(cb.Name)
	if !ok {
		msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
			[]protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Error: unknown tool %q", cb.Name))}, true)
		if err := a.appendToolResult(parent, msg); err != nil {
			return msg, false, err
		}
		return msg, false, nil
	}
	if cb.Name == "deactivate_skill" {
		_, batchOK := a.opts.Session.(session.BatchStore)
		_, branchOK := a.opts.Session.(session.BranchEntryStore)
		if !batchOK || !branchOK {
			msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name,
				[]protocol.ContentBlock{protocol.NewTextBlock("Error: deactivate_skill requires atomic branch-aware session storage")}, true)
			if err := a.appendToolResult(parent, msg); err != nil {
				return msg, false, err
			}
			return msg, false, nil
		}
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
			return msg, false, err
		}
		return msg, false, nil
	}

	startMessage := toolStartMessage(cb.Name, rawArgs)
	a.mu.Lock()
	a.toolStarts[cb.ToolCallID] = time.Now()
	a.toolDisplays[cb.ToolCallID] = toolDisplayState{startMessage: startMessage}
	a.mu.Unlock()
	a.publish(protocol.AgentEvent{
		Type:       protocol.EvToolStart,
		ToolCallID: cb.ToolCallID,
		ToolName:   cb.Name,
		Message:    startMessage,
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
	out = a.spillToolResult(ctx, cb.Name, cb.ToolCallID, out, tr.Details)
	msg := protocol.NewToolResultMessage(newID(), parent, cb.ToolCallID, cb.Name, out, tr.IsError)
	if err := a.appendToolResult(parent, msg, tr.Details); err != nil {
		return msg, true, err
	}
	if !tr.IsError {
		a.applyDiscoveryDetails(tr.Details)
		a.applySkillActivationDetails(tr.Details)
		a.applySkillDeactivationDetails(tr.Details)
		a.applyPlanUpdateDetails(tr.Details)
	}
	return msg, true, nil
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
