package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/snow-core/snow/internal/compact"
	planpkg "github.com/snow-core/snow/internal/plan"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

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
		startMessage := "activating explicitly requested skill " + name
		a.publish(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: callID, ToolName: "activate_skill", Message: startMessage})
		activation, activationErr := runSkillActivation(ctx, a.opts.Registry, a.opts.ToolHost, name)
		durationMS := time.Since(started).Milliseconds()
		output := "activated skill " + name
		if activationErr != nil {
			output = boundEventText(activationErr.Error(), 8*1024)
		}
		transcript, marshalErr := json.Marshal(protocol.ToolTranscript{
			ToolName: "activate_skill",
			IsError:  activationErr != nil,
			Display: protocol.ToolDisplay{
				Started:      true,
				StartMessage: startMessage,
				Output:       output,
				DurationMS:   durationMS,
			},
		})
		var persistErr error
		if marshalErr != nil {
			persistErr = marshalErr
		} else {
			entries := make([]session.Entry, 0, 2)
			if activationErr == nil {
				entries = append(entries, session.Entry{Type: session.EntryMeta, ID: newID(), Key: skillActivationMeta, Value: name})
			}
			entries = append(entries, session.Entry{Type: session.EntryMeta, ID: newID(), Key: session.MetaToolTranscript, Value: string(transcript)})
			a.mailboxPersistMu.Lock()
			if batch, ok := a.opts.Session.(session.BatchStore); ok {
				persistErr = batch.AppendBatch(entries)
			} else {
				for _, entry := range entries {
					if persistErr = a.opts.Session.Append(entry); persistErr != nil {
						break
					}
				}
			}
			a.mailboxPersistMu.Unlock()
		}
		if persistErr == nil {
			a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
		}
		err := errors.Join(activationErr, persistErr)
		eventOutput := output
		if activationErr == nil && persistErr != nil {
			eventOutput = boundEventText(persistErr.Error(), 8*1024)
		}
		event := protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: callID, ToolName: "activate_skill", ToolDurationMS: durationMS, ToolOutput: eventOutput, IsError: err != nil}
		if err != nil {
			event.Message = boundEventText(eventOutput, 2*1024)
			a.publish(event)
			return err
		}
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
	// The host owns correlation identity. A tool may provide convenience fields,
	// but allowing it to spoof another call would make live and resumed rows
	// disagree and could misattribute progress to a concurrent surface.
	ev.ToolCallID = h.callID
	ev.Name = h.name
	ev.Message = boundEventText(ev.Message, 2*1024)
	h.agent.recordToolProgress(h.callID, ev.Message)
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

func (a *Agent) recordToolProgress(callID, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.toolDisplays[callID]
	if !ok {
		return
	}
	state.progress = append(state.progress, message)
	state.progressSize += len(message)
	for len(state.progress) > 1 && (len(state.progress) > maxPersistedToolProgressRows || state.progressSize > maxPersistedToolProgressBytes) {
		state.progressSize -= len(state.progress[0])
		state.progress = state.progress[1:]
	}
	a.toolDisplays[callID] = state
}

func (a *Agent) takeToolDisplay(callID string) (time.Time, toolDisplayState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	started := a.toolStarts[callID]
	display := a.toolDisplays[callID]
	delete(a.toolStarts, callID)
	delete(a.toolDisplays, callID)
	display.progress = append([]string(nil), display.progress...)
	return started, display
}

func (a *Agent) spillToolResult(ctx context.Context, toolName, callID string, content []protocol.ContentBlock, details any) []protocol.ContentBlock {
	if a.opts.Artifacts == nil || toolName == "artifact_read" || toolName == "artifact_grep" {
		return content
	}
	if _, private := details.(tools.PrivateDetails); private {
		return content
	}
	if pointer, private := details.(*tools.PrivateDetails); private && pointer != nil {
		return content
	}
	var text strings.Builder
	for _, block := range content {
		if block.Type != protocol.BlockText {
			return content
		}
		text.WriteString(block.Text)
	}
	value := text.String()
	threshold := a.opts.Compaction.ToolResultInlineBytes
	if threshold <= 0 {
		threshold = 16 << 10
	}
	if len(value) <= threshold {
		return content
	}
	a.mu.RLock()
	sessionID := ""
	if a.opts.Session != nil {
		sessionID = a.opts.Session.ID()
	}
	a.mu.RUnlock()
	ref, err := a.opts.Artifacts.SaveText(ctx, sessionID, toolName+"\x00"+callID, value)
	if err != nil {
		return content
	}
	preview := compact.PruneHistoricalToolResultsWithRefs([]protocol.Message{{Role: protocol.RoleTool, Content: content}}, threshold, threshold*3/4, threshold/4,
		func(protocol.Message, string) string { return ref.ID })
	if len(preview) != 1 {
		return content
	}
	return preview[0].Content
}

func compactedArtifactReferences(messages []protocol.Message) []string {
	const marker = "Full retained tool result:"
	seen := make(map[string]bool)
	var refs []string
	collect := func(value string) {
		for offset := 0; ; {
			index := strings.Index(value[offset:], marker)
			if index < 0 {
				return
			}
			index += offset
			start := index + len(marker)
			for start < len(value) && (value[start] == ' ' || value[start] == '\t') {
				start++
			}
			end := start + len("artifact-") + 32
			if end <= len(value) {
				candidate := value[start:end]
				valid := strings.HasPrefix(candidate, "artifact-")
				for _, r := range candidate[len("artifact-"):] {
					if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
						valid = false
						break
					}
				}
				if valid && !seen[candidate] {
					seen[candidate] = true
					refs = append(refs, candidate)
				}
			}
			offset = index + len(marker)
		}
	}
	for _, message := range messages {
		collect(message.Error)
		for _, block := range message.Content {
			collect(block.Text)
		}
	}
	return refs
}

func (a *Agent) verifiedCompactedArtifactReferences(ctx context.Context, messages []protocol.Message) ([]string, error) {
	if a.opts.Artifacts == nil {
		return nil, nil
	}
	a.mu.RLock()
	sessionID := ""
	if a.opts.Session != nil {
		sessionID = a.opts.Session.ID()
	}
	a.mu.RUnlock()
	if sessionID == "" {
		return nil, errors.New("artifact verification requires a session ID")
	}
	ownedIDs, err := a.opts.Artifacts.ListIDs(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(ownedIDs))
	for _, id := range ownedIDs {
		owned[id] = true
	}
	candidates := compactedArtifactReferences(messages)
	verified := make([]string, 0, min(len(candidates), maxCompactionRetrievalReferences))
	// References are ordered oldest to newest. Intersect with the structurally
	// enumerated session namespace, then retain the newest 24. Forged markers
	// cannot consume verification work or crowd out older valid references.
	for i := len(candidates) - 1; i >= 0 && len(verified) < maxCompactionRetrievalReferences; i-- {
		if owned[candidates[i]] {
			verified = append(verified, candidates[i])
		}
	}
	for left, right := 0, len(verified)-1; left < right; left, right = left+1, right-1 {
		verified[left], verified[right] = verified[right], verified[left]
	}
	return verified, nil
}

func boundedCompactionReferences(retained []string, transcriptRef string) []string {
	if len(retained) > maxCompactionRetrievalReferences {
		retained = retained[len(retained)-maxCompactionRetrievalReferences:]
	}
	out := append([]string(nil), retained...)
	if transcriptRef == "" {
		return out
	}
	if len(out) >= maxCompactionRetrievalReferences {
		out = out[1:]
	}
	return append(out, transcriptRef)
}

func rebuildCompactionRetrievalSection(summary string, refs []string) string {
	const (
		heading = "## Retrieval references"
		marker  = "Full retained tool result:"
	)
	// Model-generated summaries are untrusted. Remove every marker they echo,
	// regardless of section, then rebuild the one canonical section exclusively
	// from session-owned references.
	lines := strings.Split(summary, "\n")
	for i := 0; i < len(lines); i++ {
		removed := false
		for {
			index := strings.Index(lines[i], marker)
			if index < 0 {
				break
			}
			end := index + len(marker)
			for end < len(lines[i]) && (lines[i][end] == ' ' || lines[i][end] == '\t') {
				end++
			}
			candidateEnd := end + len("artifact-") + 32
			if candidateEnd <= len(lines[i]) && strings.HasPrefix(lines[i][end:candidateEnd], "artifact-") {
				end = candidateEnd
			}
			lines[i] = strings.TrimSpace(lines[i][:index] + lines[i][end:])
			removed = true
		}
		if removed {
			if lines[i] == "" {
				lines[i] = "- Unverified artifact reference omitted."
			}
			if i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "Use artifact_read or artifact_grep") {
				lines[i+1] = ""
			}
		}
	}
	// Remove every model-supplied top-level retrieval section, not just the
	// first. Canonical content is inserted once at the required position below.
	filtered := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) != heading {
			filtered = append(filtered, lines[i])
			i++
			continue
		}
		i++
		for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			i++
		}
	}
	canonical := []string{heading}
	if len(refs) == 0 {
		canonical = append(canonical, "- None recorded.")
	} else {
		for _, ref := range refs {
			canonical = append(canonical,
				marker+" "+ref,
				"Use artifact_read or artifact_grep to inspect retained compacted tool evidence.",
			)
		}
	}
	insertAt := len(filtered)
	for i, line := range filtered {
		if strings.TrimSpace(line) == "## Unresolved next steps" {
			insertAt = i
			break
		}
	}
	result := append([]string(nil), filtered[:insertAt]...)
	if len(result) > 0 && result[len(result)-1] != "" {
		result = append(result, "")
	}
	result = append(result, canonical...)
	if insertAt < len(filtered) && (len(result) == 0 || result[len(result)-1] != "") {
		result = append(result, "")
	}
	result = append(result, filtered[insertAt:]...)
	return strings.Join(result, "\n")
}

func (a *Agent) saveCompactedToolTranscript(ctx context.Context, messages []protocol.Message, boundaryID string) (string, error) {
	if a.opts.Artifacts == nil || estimateToolHistoryTokens(messages) == 0 {
		return "", nil
	}
	a.mu.RLock()
	sessionID := ""
	if a.opts.Session != nil {
		sessionID = a.opts.Session.ID()
	}
	a.mu.RUnlock()
	if sessionID == "" {
		return "", errors.New("compacted tool transcript requires a session ID")
	}
	var transcript strings.Builder
	transcript.WriteString("Compacted tool transcript. Opaque provider continuity and private reasoning are intentionally omitted.\n")
	for _, message := range messages {
		hasToolContent := message.Role == protocol.RoleTool
		if message.Role == protocol.RoleAssistant {
			for _, block := range message.Content {
				if block.Type == protocol.BlockToolCall {
					hasToolContent = true
					break
				}
			}
		}
		if !hasToolContent {
			continue
		}
		fmt.Fprintf(&transcript, "\nmessage %s role=%s", message.ID, message.Role)
		if message.ParentID != "" {
			fmt.Fprintf(&transcript, " parent=%s", message.ParentID)
		}
		if message.Role == protocol.RoleTool {
			fmt.Fprintf(&transcript, " tool=%s call_id=%s error=%t", message.ToolName, message.ToolCallID, message.IsError)
		}
		transcript.WriteByte('\n')
		for _, block := range message.Content {
			switch block.Type {
			case protocol.BlockToolCall:
				fmt.Fprintf(&transcript, "tool_call name=%s call_id=%s arguments=%s\n", block.Name, block.ToolCallID, strings.TrimSpace(string(block.Arguments)))
			case protocol.BlockText, protocol.BlockPlan:
				transcript.WriteString(block.Text)
				if !strings.HasSuffix(block.Text, "\n") {
					transcript.WriteByte('\n')
				}
			case protocol.BlockImage:
				fmt.Fprintf(&transcript, "[image %s, %d bytes]\n", block.MIMEType, len(block.Data))
			}
		}
	}
	value := transcript.String()
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	ref, err := a.opts.Artifacts.SaveText(ctx, sessionID, "compaction-tool-history\x00"+boundaryID, value)
	if err != nil {
		return "", fmt.Errorf("save compacted tool transcript: %w", err)
	}
	return ref.ID, nil
}

func (a *Agent) pruneHistoricalToolResults(ctx context.Context, messages []protocol.Message) []protocol.Message {
	threshold := a.opts.Compaction.HistoricalToolResultThreshold
	if threshold <= 0 {
		threshold = compact.HistoricalToolResultThreshold
	}
	return compact.PruneHistoricalToolResultsWithRefs(messages, threshold, compact.HistoricalToolResultHead, compact.HistoricalToolResultTail,
		func(message protocol.Message, text string) string {
			if a.opts.Artifacts == nil || message.ToolName == "artifact_read" || message.ToolName == "artifact_grep" {
				return ""
			}
			a.mu.RLock()
			sessionID := ""
			if a.opts.Session != nil {
				sessionID = a.opts.Session.ID()
			}
			a.mu.RUnlock()
			ref, err := a.opts.Artifacts.SaveText(ctx, sessionID, message.ToolName+"\x00"+message.ToolCallID+"\x00"+message.ID, text)
			if err != nil {
				return ""
			}
			return ref.ID
		})
}

// providerMessages removes surface-only transcript metadata before crossing a
// provider boundary. In particular, ToolDisplay may contain a private edit diff
// that is intentionally visible to the local UI but absent from model context.
func providerMessages(messages []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].ToolDisplay = nil
	}
	return out
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
