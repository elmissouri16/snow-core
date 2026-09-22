package web

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"unicode"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const runtimeHistoryBytes = 256 << 10
const runtimeMessageBytes = 64 << 10
const runtimeErrorBytes = 8 << 10
const runtimeHistoryCount = 100

// RuntimeMessage is a bounded public timeline segment. The live-only
// tool_activity role is an empty step marker owned by RuntimeActivity.MessageID;
// its local ID never claims a persisted assistant SourceID.
type RuntimeMessage struct {
	imageTurnMarker bool                      // Live-only evidence: run stats follow the persisted turn marker.
	Images          []MessageImage            `json:"images,omitempty"`
	SourceSpanID    string                    `json:"-"`
	SourceTurnID    string                    `json:"-"`
	CanRegenerate   bool                      `json:"can_regenerate"`
	CanEdit         bool                      `json:"can_edit"`
	ID              string                    `json:"id"`
	SourceID        string                    `json:"source_id,omitempty"`
	Role            string                    `json:"role"`
	Text            string                    `json:"text"`
	HTML            string                    `json:"html,omitempty"`
	Truncated       bool                      `json:"truncated"`
	Tools           []protocol.RPCHistoryTool `json:"tools,omitempty"`
}

func (s RuntimeSnapshot) clone() RuntimeSnapshot {
	s.Compaction = s.Compaction.clone()
	if s.CompactionACK != nil {
		s.CompactionACK = new(*s.CompactionACK)
	}
	s.Steer = s.Steer.clone()
	if s.SteerACK != nil {
		s.SteerACK = new(*s.SteerACK)
	}
	s.Goal = s.Goal.clone()
	if s.GoalRunACK != nil {
		s.GoalRunACK = new(*s.GoalRunACK)
	}
	if s.Queue != nil {
		s.Queue = new(*s.Queue)
		s.Queue.Items = slices.Clone(s.Queue.Items)
	}
	s.Messages = slices.Clone(s.Messages)
	for i := range s.Messages {
		s.Messages[i].Tools = slices.Clone(s.Messages[i].Tools)
		s.Messages[i].Images = slices.Clone(s.Messages[i].Images)
	}
	s.Activities = slices.Clone(s.Activities)
	if s.Permission != nil {
		p := *s.Permission
		p.Paths = slices.Clone(p.Paths)
		p.Capabilities = slices.Clone(p.Capabilities)
		p.Effects = slices.Clone(p.Effects)
		s.Permission = &p
	}
	if s.Telemetry != nil {
		s.Telemetry = cloneRuntimeTelemetry(s.Telemetry)
	}
	s.Input = cloneRuntimeInput(s.Input)
	return s
}
func cloneRuntimeInput(input *protocol.UserInputRequest) *protocol.UserInputRequest {
	if input == nil {
		return nil
	}
	result := *input
	result.Questions = slices.Clone(input.Questions)
	for i := range result.Questions {
		result.Questions[i].Options = slices.Clone(result.Questions[i].Options)
	}
	return &result
}

// runtimeText bounds valid UTF-8 plain text and strips terminal control bytes.
// This is not HTML sanitization: browser consumers must still use textContent.
func runtimeText(text string, limit int) string {
	if len(text) > limit {
		text = text[:limit]
	}
	// Remove malformed bytes (including a clipped final rune) rather than
	// expanding them to replacement runes beyond the caller's byte budget.
	text = strings.ToValidUTF8(text, "")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}

func runtimeErrorText(text string) string {
	text = strings.TrimSpace(runtimeText(text, runtimeErrorBytes))
	for _, prefix := range []string{"agent: provider stream: ", "agent: provider chat: ", "agent: provider resolve: ", "agent: "} {
		text = strings.TrimPrefix(text, prefix)
	}
	return strings.TrimSpace(text)
}

func runtimePromptFailureText(detail string) string {
	const (
		guidance  = "Prompt failed. Review the saved session before explicitly retrying."
		separator = "\n\n"
	)
	detail = strings.TrimSpace(runtimeText(detail, runtimeErrorBytes-len(separator)-len(guidance)))
	if detail == "" {
		return guidance
	}
	return detail + separator + guidance
}

func (r *liveRuntime) addMessage(message RuntimeMessage) {
	if message.ID == "" {
		r.messageSequence++
		message.ID = fmt.Sprintf("live-%s-%d", r.instanceID, r.messageSequence)
	}
	message.Truncated = message.Truncated || len(message.Text) > runtimeMessageBytes
	message.CanEdit = message.Role == "user" && len(message.Images) == 0 && !message.Truncated && (message.SourceID != "" || message.SourceTurnID != "")
	message.Text = runtimeText(message.Text, runtimeMessageBytes)
	r.snapshot.Messages = append(r.snapshot.Messages, message)
	r.trimMessages()
}
func (r *liveRuntime) trimMessages() {
	bytes := 0
	for _, message := range r.snapshot.Messages {
		bytes += len(message.Text)
	}
	for len(r.snapshot.Messages) > runtimeHistoryCount || bytes > runtimeHistoryBytes {
		bytes -= len(r.snapshot.Messages[0].Text)
		r.snapshot.Messages = slices.Delete(r.snapshot.Messages, 0, 1)
		r.snapshot.HistoryTruncated = true
		if r.assistant >= 0 {
			r.assistant--
		}
		if r.plan >= 0 {
			r.plan--
		}
	}
}

func (r *liveRuntime) loadHistory() error {
	var page protocol.RPCMessagesPage
	publicHistory := slices.Contains(r.worker.Client.Ready().Capabilities, "messages_public_history")
	if err := r.call(protocol.RPCRequest{Type: "messages_page"}, protocol.RPCMessagesPageParams{Limit: 100, MaxBytes: runtimeHistoryBytes, PublicHistory: publicHistory}, &page); err != nil {
		return err
	}
	if publicHistory && page.HistoryTools == nil {
		// Empty authoritative maps must not fall back to partial raw-page pairing.
		page.HistoryTools = make(map[string][]protocol.RPCHistoryTool)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projectHistory(page)
	r.publishLocked()
	return nil
}

// projectHistory keeps each displayed text run and plan independently keyed.
// The persisted message identity remains available separately for correlation.
// Callers hold r.mu. Block positions are stable across history reloads.
func (r *liveRuntime) projectHistory(page protocol.RPCMessagesPage) {
	r.snapshot.HistoryTruncated = page.HasMore || page.Start > 0
	tools, toolsTruncated := page.HistoryTools, page.HistoryToolsTruncated
	if tools == nil {
		tools, toolsTruncated = protocol.ProjectHistoryTools(page.Messages)
		toolsTruncated = toolsTruncated || page.HasMore
	}
	r.snapshot.HistoryToolsTruncated = toolsTruncated
	for messageIndex, message := range page.Messages {
		if message.Role != protocol.RoleUser && message.Role != protocol.RoleAssistant {
			continue
		}
		source := runtimeText(message.ID, 128)
		if source != message.ID {
			// A transformed identity cannot authorize a durable source lookup.
			source = ""
		}
		identity := message.ID
		if identity == "" {
			identity = fmt.Sprintf("missing-%d", page.Start+messageIndex)
		}
		key := fmt.Sprintf("history-%x", sha256.Sum256([]byte(identity)))
		metadata := page.HistoryImages[message.ID]
		if page.HistoryImages == nil {
			metadata = protocol.ProjectMessageImages(message)
		}
		images := messageImages(metadata)
		hasImages := len(images) > 0
		text, truncated, firstBlock := "", false, 0
		flush := func() {
			if text != "" || len(images) > 0 {
				r.addMessage(RuntimeMessage{ID: fmt.Sprintf("%s-%d", key, firstBlock), SourceID: source, Role: string(message.Role), Text: text, Truncated: truncated, Images: images})
				images = nil
			}
			text, truncated = "", false
		}
		for blockIndex, block := range message.Content {
			if block.Type == protocol.BlockPlan && block.Text != "" {
				flush()
				r.addMessage(RuntimeMessage{ID: fmt.Sprintf("%s-%d", key, blockIndex), SourceID: source, Role: "plan", Text: block.Text})
				continue
			}
			if block.Type != protocol.BlockText {
				continue
			}
			if text == "" {
				firstBlock = blockIndex
			}
			room := runtimeMessageBytes - len(text)
			truncated = truncated || len(block.Text) > room
			text += runtimeText(block.Text, room)
		}
		flush()
		if saved := tools[message.ID]; len(saved) > 0 {
			// Tool-only assistant entries still own their durable calls. Attach
			// once, to the final projected segment, never to an adjacent turn.
			last := len(r.snapshot.Messages) - 1
			if last >= 0 && r.snapshot.Messages[last].Role == "assistant" && strings.HasPrefix(r.snapshot.Messages[last].ID, key+"-") {
				r.snapshot.Messages[last].Tools = slices.Clone(saved)
			} else {
				r.addMessage(RuntimeMessage{ID: key + "-tools", SourceID: source, Role: "assistant", Tools: slices.Clone(saved)})
			}
		}
		if hasImages {
			for i := range r.snapshot.Messages {
				if strings.HasPrefix(r.snapshot.Messages[i].ID, key+"-") {
					r.snapshot.Messages[i].CanEdit = false
				}
			}
		}
		r.markHistoryRegenerateReply(message, source, key)
	}
}

func projectPermission(request protocol.PermissionRequest) (*RuntimePermission, bool) {
	if !runtimeIdentifier(request.ID) {
		return nil, false
	}
	result := &RuntimePermission{ID: request.ID, Tool: runtimeText(request.Tool, 128), Risk: runtimeText(request.Risk, 128), Reason: runtimeText(request.Reason, 2048), ScopeLabel: runtimeText(request.ScopeLabel, 256), Unknown: request.Unknown}
	result.Truncated = request.PathsTruncated || request.EffectsTruncated || request.CapabilitiesTruncated || len(request.Paths) > 16 || len(request.Effects) > 16 || len(request.Capabilities) > 16 || len(request.Reason) > 2048 || len(request.Tool) > 128 || len(request.Risk) > 128 || len(request.ScopeLabel) > 256
	for _, path := range request.Paths[:min(len(request.Paths), 16)] {
		result.Paths = append(result.Paths, runtimeText(path, 2048))
		result.Truncated = result.Truncated || len(path) > 2048
	}
	for _, capability := range request.Capabilities[:min(len(request.Capabilities), 16)] {
		result.Capabilities = append(result.Capabilities, runtimeText(capability, 128))
		result.Truncated = result.Truncated || len(capability) > 128
	}
	for _, effect := range request.Effects[:min(len(request.Effects), 16)] {
		fields := []*string{&effect.Type, &effect.Capability, &effect.Operation, &effect.Resource, &effect.Command, &effect.Reason, &effect.Confidence}
		for _, field := range fields {
			result.Truncated = result.Truncated || len(*field) > 2048
			*field = runtimeText(*field, 2048)
		}
		result.Effects = append(result.Effects, effect)
	}
	return result, true
}

func projectInput(request *protocol.UserInputRequest) (*protocol.UserInputRequest, bool) {
	if request == nil || !runtimeIdentifier(request.ID) || len(request.Questions) == 0 || len(request.Questions) > 16 {
		return nil, false
	}
	result := cloneRuntimeInput(request)
	result.ToolCallID = ""
	total := 0
	seen := make(map[string]bool, len(result.Questions))
	for i := range result.Questions {
		q := &result.Questions[i]
		if !runtimeIdentifier(q.ID) || seen[q.ID] || len(q.Header) > 256 || len(q.Question) > 8192 || len(q.Options) > 32 {
			return nil, false
		}
		seen[q.ID] = true
		total += len(q.Header) + len(q.Question)
		q.Header = runtimeText(q.Header, 256)
		q.Question = runtimeText(q.Question, 8192)
		for j := range q.Options {
			o := &q.Options[j]
			if len(o.Label) > 512 || len(o.Description) > 2048 || runtimeText(o.Label, 512) != o.Label {
				return nil, false
			}
			total += len(o.Label) + len(o.Description)
			o.Description = runtimeText(o.Description, 2048)
		}
	}
	if total > 64<<10 {
		return nil, false
	}
	return result, true
}

func runtimeRootAgent(ref *protocol.AgentRef) bool {
	return ref == nil || ref.Path == protocol.RootAgentPath && ref.Depth == 0 && ref.ParentPath == "" && ref.ParentThreadID == "" && ref.Validate() == nil
}

func runtimeChildAgent(ref *protocol.AgentRef) bool {
	return ref != nil && ref.Path != protocol.RootAgentPath && ref.Depth > 0 && ref.Validate() == nil
}

func (r *liveRuntime) clearPermissionLocked() {
	r.snapshot.Permission = nil
	r.permissionAgent = nil
}

// consumeChildPermission is the one child event projected independently of a
// root prompt. The shared permission broker emits one FIFO request at a time,
// so the browser's single attention card cannot overwrite another request.
func (r *liveRuntime) consumeChildPermission(e protocol.AgentEvent) {
	permission, ok := projectPermission(e.Permission.Request)
	if !ok {
		r.fail()
		return
	}
	permission.AgentPath = runtimeText(string(e.Agent.Path), protocol.MaxAgentPathBytes)
	permission.AgentRole = runtimeText(e.Agent.Role, 64)

	r.mu.Lock()
	if r.ctx.Err() != nil || r.transitioning || r.snapshot.Status == "closing" || r.snapshot.Status == "failed" {
		r.mu.Unlock()
		return
	}
	// The shared broker publishes only its current FIFO head. A different ID is
	// therefore authoritative replacement after the prior request was resolved
	// or canceled, even if its terminal child event has not drained yet.
	r.snapshot.Permission = permission
	r.permissionAgent = e.Agent.Clone()
	r.snapshot.Status = "permission"
	r.publishLocked()
	r.mu.Unlock()
}

func sameRuntimeAgent(a, b *protocol.AgentRef) bool {
	return a != nil && b != nil && a.ThreadID == b.ThreadID && a.Path == b.Path
}

func (r *liveRuntime) consumeChildStatus(e protocol.AgentEvent) {
	r.mu.Lock()
	if e.Subagent.Status.Terminal() {
		delete(r.activeChildren, e.Agent.ThreadID)
	} else {
		if r.activeChildren == nil {
			r.activeChildren = make(map[string]protocol.AgentStatus)
		}
		r.activeChildren[e.Agent.ThreadID] = e.Subagent.Status
	}
	if e.Subagent.Status.Terminal() && sameRuntimeAgent(r.permissionAgent, e.Agent) {
		r.clearPermissionLocked()
		if r.busy {
			r.snapshot.Status = "running"
		} else {
			r.snapshot.Status = "idle"
		}
		r.publishLocked()
	}
	r.mu.Unlock()
}

func (r *liveRuntime) consumeChildInteraction(e *protocol.AgentEvent) bool {
	if e == nil {
		return false
	}
	switch e.Type {
	case protocol.EvPermissionRequest:
		if runtimeRootAgent(e.Agent) {
			return false
		}
		if !runtimeChildAgent(e.Agent) || e.Permission == nil {
			r.fail()
			return true
		}
		r.consumeChildPermission(*e)
		return true
	case protocol.EvSubagentStarted, protocol.EvSubagentStatus:
		if !runtimeChildAgent(e.Agent) || e.Subagent == nil || e.Subagent.Validate() != nil || !sameRuntimeAgent(e.Agent, &e.Subagent.Agent) {
			r.fail()
			return true
		}
		r.consumeChildStatus(*e)
		return true
	default:
		return false
	}
}

func (r *liveRuntime) drain() {
	defer close(r.drained)
	for event := range r.worker.Client.Events() {
		r.eventMu.Lock()
		if r.consumeChildInteraction(event.AgentEvent) {
			r.eventMu.Unlock()
			continue
		}
		r.mu.Lock()
		buffered, valid := r.bufferCompactionEventLocked(event)
		if !buffered && valid {
			buffered, valid = r.bufferGoalEventLocked(event)
		}
		if !buffered && valid {
			buffered, valid = r.bufferMessageEditEventLocked(event)
		}
		r.mu.Unlock()
		if !valid {
			r.eventMu.Unlock()
			r.fail()
			continue
		}
		if !buffered {
			r.consumeEvent(event)
		}
		r.eventMu.Unlock()
	}
	r.fail()
}

func (r *liveRuntime) consumeEvent(event clientrpc.Event) {
	if r.consumeCompactionEvent(event) {
		return
	}
	if r.consumeGoalEvent(event) {
		return
	}
	if event.PromptCompleted != nil {
		r.consumePromptCompletion(*event.PromptCompleted)
		return
	}
	if event.AgentEvent == nil {
		return
	}
	e := event.AgentEvent
	// Child output, thinking, tool arguments/private previews, raw worker or
	// completion errors, and arbitrary future events are never copied to
	// browser-facing state. Child interaction handling is limited to projecting a
	// valid permission request and clearing it on matching terminal status; Web
	// must resolve the shared FIFO broker or a shell-capable child would deadlock
	// in ask mode. Accepted root EvError
	// diagnostics are projected below after bounding. Retain support for legacy
	// untagged root events, but reject inconsistent identities.
	if r.consumeChildInteraction(e) {
		return
	}
	if !runtimeRootAgent(e.Agent) {
		return
	}
	r.mu.Lock()
	// A modern prompt error may establish the initial observed epoch, but once a
	// root is bound it cannot use an otherwise monotonic future epoch to retarget
	// browser-visible diagnostics. Session/history changes occur while idle and
	// establish their new epoch through their dedicated transaction.
	if e.Type == protocol.EvError && e.RootEpoch != 0 && r.rootEpoch != 0 && e.RootEpoch != r.rootEpoch {
		r.mu.Unlock()
		return
	}
	if e.Type == protocol.EvQueueUpdated && e.QueueControl != nil {
		if !r.queueSupported {
			r.mu.Unlock()
			return
		}
		// Queue lifecycle snapshots can close after prompt completion. They still
		// require this exact root/session/epoch, never monotonic retargeting.
		accepted := false
		if e.RootEpoch != 0 && r.busy && !r.transitioning && r.snapshot.Status != "failed" && r.snapshot.Status != "closing" {
			accepted = r.acceptEvent(*e)
		} else {
			accepted = !r.transitioning && e.TurnID != "" && e.TurnID == r.turnID && e.RootEpoch != 0 && e.RootEpoch == r.rootEpoch
		}
		valid := true
		if accepted {
			valid = e.QueueControl.TurnID == e.TurnID && r.applySteerControlLocked(e.QueueControl) && r.applyQueueControlLocked(e.QueueControl, true)
		}
		r.mu.Unlock()
		if !valid {
			r.fail()
		}
		return
	}
	if !r.acceptEvent(*e) || !r.busy || r.transitioning || r.snapshot.Status == "closing" || r.snapshot.Status == "failed" {
		r.mu.Unlock()
		return
	}
	r.bindPromptUserLocked(*e)
	invalid := false
	switch e.Type {
	case protocol.EvPlanStarted, protocol.EvPlanDelta, protocol.EvPlanCompleted:
		r.projectPlan(*e)
	case protocol.EvUsage, protocol.EvTurnDone:
		r.projectUsage(*e)
	case protocol.EvModeChanged:
		if e.Mode != nil {
			r.snapshot.Mode = runtimeMode(e.Mode.Mode)
			r.snapshot.Thinking = string(protocol.NormalizeThinkingLevel(e.Mode.ReasoningEffort))
			r.publishLocked()
		}
	case protocol.EvTextDelta:
		if e.Text != "" {
			if r.assistant < 0 {
				r.addMessage(RuntimeMessage{Role: "assistant", SourceTurnID: r.assistantSourceTurn(*e), SourceSpanID: r.queueSourceSpanLocked()})
				r.assistant = len(r.snapshot.Messages) - 1
			}
			message := &r.snapshot.Messages[r.assistant]
			room := runtimeMessageBytes - len(message.Text)
			message.Truncated = message.Truncated || len(e.Text) > room
			message.Text += runtimeText(e.Text, room)
			r.trimMessages()
			r.publishLocked()
		}
	case protocol.EvToolStart, protocol.EvToolEnd:
		r.projectActivity(*e)
	case protocol.EvError:
		if message := runtimeErrorText(e.Message); message != "" {
			r.snapshot.Error = message
			r.publishLocked()
		}
	case protocol.EvAborted:
		r.cancelActivities()
		r.publishLocked()
	case protocol.EvPermissionRequest:
		if e.Permission == nil {
			invalid = true
			break
		}
		permission, ok := projectPermission(e.Permission.Request)
		invalid = !ok
		if ok {
			r.snapshot.Permission = permission
			r.permissionAgent = nil
			r.snapshot.Status = "permission"
			r.publishLocked()
		}
	case protocol.EvUserInputRequest:
		input, ok := projectInput(e.UserInput)
		invalid = !ok
		if ok {
			r.snapshot.Input = input
			r.snapshot.Status = "input"
			r.publishLocked()
		}
	}
	r.mu.Unlock()
	if invalid {
		r.fail()
	}
}
