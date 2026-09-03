package compact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PlannerWithOptions finds a prefix to compact while retaining complete recent
// turns. Tool calls and results remain in the same retained unit by
// construction. Assistant-originated goal turns may fall back to retaining
// complete recent tool cycles when one oversized goal turn leaves no ordinary
// turn prefix to compact.
func PlannerWithOptions(msgs []protocol.Message, opts PlannerOptions) Plan {
	if opts.MinRetainedTurns < 1 {
		opts.MinRetainedTurns = 2
	}
	if opts.RetainTokens < 1 {
		opts.RetainTokens = 8 * 1024
	}
	if len(msgs) <= 1 {
		return emptyPlan(msgs)
	}

	turnStarts := completeTurnStarts(msgs)
	if plan := planAtStarts(msgs, turnStarts, opts); len(plan.CompactionCandidates) > 0 {
		return plan
	}
	if opts.AllowActiveToolCycles {
		starts := activeToolCycleStarts(msgs, turnStarts)
		if plan := planAtStarts(msgs, starts, opts); len(plan.CompactionCandidates) > 0 {
			return plan
		}
	}
	if opts.AllowGoalToolCycles {
		starts := goalToolCycleStarts(msgs, turnStarts)
		if plan := planAtStarts(msgs, starts, opts); len(plan.CompactionCandidates) > 0 {
			return plan
		}
	}
	return emptyPlan(msgs)
}

func planAtStarts(msgs []protocol.Message, starts []int, opts PlannerOptions) Plan {
	keep := 0
	if len(starts) > opts.MinRetainedTurns {
		keep = starts[len(starts)-opts.MinRetainedTurns]
		suffixTokens := make([]int, len(msgs)+1)
		for i := len(msgs) - 1; i >= 0; i-- {
			suffixTokens[i] = suffixTokens[i+1] + estimateMessageTokens(msgs[i])
		}
		for turn := len(starts) - opts.MinRetainedTurns - 1; turn >= 1; turn-- {
			candidate := starts[turn]
			if suffixTokens[candidate] > opts.RetainTokens {
				break
			}
			keep = candidate
		}
	}
	if keep <= 0 || !toolPairingBalancedAt(msgs, keep) {
		return emptyPlan(msgs)
	}

	plan := Plan{KeepFrom: keep, CompactionCandidates: msgs[:keep], TotalMessages: len(msgs)}
	plan.EstimatedTokens = estimateTokens(plan.CompactionCandidates)
	for i := len(plan.CompactionCandidates) - 1; i >= 0; i-- {
		id := plan.CompactionCandidates[i].ID
		if id != "" && !strings.HasPrefix(id, "compaction-") {
			plan.BoundaryID = id
			break
		}
	}
	if plan.BoundaryID == "" {
		return emptyPlan(msgs)
	}
	return plan
}

func emptyPlan(msgs []protocol.Message) Plan {
	return Plan{EstimatedTokens: estimateTokens(msgs), TotalMessages: len(msgs)}
}

// completeTurnStarts returns complete-turn boundaries in provider context.
// User and mailbox messages begin explicit turns. Automatic goal turns have no
// synthetic user message, so an assistant at the beginning of projected
// history or immediately after a terminal assistant is an implicit turn start.
func completeTurnStarts(msgs []protocol.Message) []int {
	starts := make([]int, 0)
	first := 0
	if len(msgs) > 0 && msgs[0].Role == protocol.RoleCustom {
		first = 1
	}
	if first < len(msgs) && msgs[first].Role == protocol.RoleAssistant {
		starts = append(starts, first)
	}
	for i := first; i < len(msgs); i++ {
		msg := msgs[i]
		switch msg.Role {
		case protocol.RoleUser, protocol.RoleAgent:
			starts = append(starts, i)
		case protocol.RoleAssistant:
			if i > first && terminalAssistant(msgs[i-1]) {
				starts = append(starts, i)
			}
		}
	}
	return starts
}

func terminalAssistant(message protocol.Message) bool {
	return message.Role == protocol.RoleAssistant && message.StopReason != protocol.StopToolUse && message.StopReason != protocol.StopPending
}

// activeToolCycleStarts adds safe call/result-cycle boundaries only when doing
// so cannot consume an exact retained prior turn. The one checkpoint exception
// is a fresh turn directly parented to that checkpoint.
func activeToolCycleStarts(msgs []protocol.Message, turnStarts []int) []int {
	if len(turnStarts) != 1 {
		return turnStarts
	}
	activeStart := turnStarts[0]
	if len(msgs) > 0 && msgs[0].Role == protocol.RoleCustom && (activeStart != 1 || !messageDirectlyParentsCheckpoint(msgs[activeStart], msgs[0])) {
		return turnStarts
	}
	return appendToolCycleStarts(msgs, turnStarts, activeStart)
}

// goalToolCycleStarts is the pressure fallback for an assistant-originated
// automatic goal turn. Such a turn has no exact user objective in provider
// history: the active goal is injected separately on every request. If that
// single turn grows beyond the context threshold, retaining its newest complete
// cycles is safer than blocking while an old prefix is still compactable.
func goalToolCycleStarts(msgs []protocol.Message, turnStarts []int) []int {
	if len(turnStarts) == 0 {
		return turnStarts
	}
	goalStart := turnStarts[len(turnStarts)-1]
	if msgs[goalStart].Role != protocol.RoleAssistant {
		return turnStarts
	}
	return appendToolCycleStarts(msgs, turnStarts, goalStart)
}

// appendToolCycleStarts returns a new ordered boundary slice. Every added
// boundary begins with an assistant request after a complete tool-result batch,
// so the prefix ends after whole calls/results and provider-private data remains
// attached to its owning assistant message.
func appendToolCycleStarts(msgs []protocol.Message, starts []int, from int) []int {
	withCycles := slices.Clone(starts)
	for i := max(from+1, 1); i < len(msgs); i++ {
		msg := msgs[i]
		if msg.Role != protocol.RoleAssistant || (msg.StopReason != protocol.StopToolUse && msg.StopReason != protocol.StopPending) {
			continue
		}
		if msgs[i-1].Role == protocol.RoleTool || msgs[i-1].Role == protocol.RoleCustom {
			withCycles = append(withCycles, i)
		}
	}
	return withCycles
}

func messageDirectlyParentsCheckpoint(message, checkpoint protocol.Message) bool {
	if message.ParentID == "" || checkpoint.ID == "" {
		return false
	}
	markerID := strings.TrimPrefix(checkpoint.ID, "compaction-")
	return message.ParentID == checkpoint.ID || message.ParentID == markerID
}

// toolPairingBalancedAt rejects a compaction cut that separates a tool call
// from its result. Calls in the compacted prefix must be complete; unresolved
// calls may remain only in the exact retained tail.
func toolPairingBalancedAt(msgs []protocol.Message, cut int) bool {
	if cut <= 0 || cut > len(msgs) {
		return false
	}
	type callSide struct {
		prefix bool
	}
	calls := make(map[string]callSide)
	for i, msg := range msgs {
		prefix := i < cut
		if prefix && msg.Role == protocol.RoleAssistant && msg.StopReason == protocol.StopPending {
			return false
		}
		if msg.Role == protocol.RoleAssistant {
			for _, block := range msg.Content {
				if block.Type != protocol.BlockToolCall || block.ToolCallID == "" {
					continue
				}
				if _, exists := calls[block.ToolCallID]; exists {
					return false
				}
				calls[block.ToolCallID] = callSide{prefix: prefix}
			}
		}
		if msg.Role != protocol.RoleTool || msg.ToolCallID == "" {
			continue
		}
		call, exists := calls[msg.ToolCallID]
		if !exists || call.prefix != prefix {
			return false
		}
		// IDs are provider/request scoped and may be reused by a later complete
		// turn. Remove matched pairs so reuse cannot permanently poison planning.
		delete(calls, msg.ToolCallID)
	}
	for _, call := range calls {
		if call.prefix {
			return false
		}
	}
	return true
}

// Apply compacts the session by appending a marker that records the summary and
// the last compacted message. ContextMessages uses that marker to project the
// summary plus retained tail without deleting history.
func Apply(ctx context.Context, st session.Store, summarizer Summarizer, plan Plan) (Result, error) {
	if len(plan.CompactionCandidates) == 0 {
		return Result{}, nil
	}
	summary, err := summarizer(ctx, plan.CompactionCandidates)
	if err != nil {
		return Result{}, fmt.Errorf("compact: summarize: %w", err)
	}

	boundary := plan.BoundaryID
	if boundary == "" {
		boundary = plan.CompactionCandidates[len(plan.CompactionCandidates)-1].ID
	}
	if after, ok := strings.CutPrefix(boundary, "compaction-"); ok {
		markerID := after
		entriesStore, ok := st.(session.BranchEntryStore)
		if !ok {
			return Result{}, errors.New("compact: cannot resolve prior compaction boundary")
		}
		entries, entriesErr := entriesStore.BranchEntries()
		if entriesErr != nil {
			return Result{}, fmt.Errorf("compact: resolve prior compaction boundary: %w", entriesErr)
		}
		resolved := ""
		for _, candidate := range entries {
			if candidate.ID == markerID && candidate.Type == session.EntryCompaction {
				resolved = candidate.CompactedThrough
				break
			}
		}
		if resolved == "" {
			return Result{}, errors.New("compact: prior compaction boundary is unavailable")
		}
		boundary = resolved
	}
	entry := session.Entry{
		Type:             session.EntryCompaction,
		Summary:          summary,
		CompactedThrough: boundary,
	}
	if err := st.Append(entry); err != nil {
		return Result{}, err
	}

	totalMessages := max(plan.TotalMessages, len(plan.CompactionCandidates))
	return Result{
		SummarizedMessages: plan.KeepFrom,
		RetainedMessages:   max(0, totalMessages-plan.KeepFrom),
		Summary:            summary,
		BeforeEntries:      totalMessages,
		// The marker entry is appended but is not a message.
		AfterEntries: totalMessages + 1,
	}, nil
}

// estimateTokens is a rough chars/4 heuristic; provider-reported usage is
// authoritative at runtime.
func estimateMessageTokens(message protocol.Message) int {
	n := 0
	for _, content := range message.Content {
		n += len(content.Text) / 4
		n += len(content.Arguments) / 4
	}
	return n
}

func estimateTokens(msgs []protocol.Message) int {
	n := 0
	for _, message := range msgs {
		n += estimateMessageTokens(message)
	}
	return n
}

// FormatWorkingStateCheckpoint gives every durable compaction summary a stable,
// structured model-facing identity. Missing provider sections are filled
// deterministically so resume and repeated compaction always receive the same
// working-state contract.
func FormatWorkingStateCheckpoint(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = "No checkpoint details were produced."
	}
	if !strings.Contains(summary, "\n## ") {
		detail := strings.TrimSpace(strings.TrimPrefix(summary, WorkingStateTitle))
		var checkpoint strings.Builder
		checkpoint.WriteString(WorkingStateTitle)
		for _, section := range WorkingStateSections {
			checkpoint.WriteString("\n\n## ")
			checkpoint.WriteString(section)
			checkpoint.WriteByte('\n')
			if section == "Current working state" && detail != "" {
				checkpoint.WriteString(detail)
			} else {
				checkpoint.WriteString("- None recorded.")
			}
		}
		return checkpoint.String()
	}
	if !strings.HasPrefix(summary, WorkingStateTitle) {
		summary = WorkingStateTitle + "\n\n" + summary
	}
	for _, section := range WorkingStateSections {
		heading := "## " + section
		if !strings.Contains(summary, heading) {
			summary += "\n\n" + heading + "\n- None recorded."
		}
	}
	return summary
}

// canonicalizeWorkingStateCheckpoint emits every known section exactly once
// while preserving unique provider content and unknown extension sections.
func canonicalizeWorkingStateCheckpoint(summary string) string {
	known := make(map[string]bool, len(WorkingStateSections))
	for _, section := range WorkingStateSections {
		known[section] = true
	}
	type chunk struct {
		heading string
		body    string
	}
	var preamble []string
	var chunks []chunk
	var current *chunk
	flush := func() {
		if current != nil {
			chunks = append(chunks, *current)
			current = nil
		}
	}
	for line := range strings.SplitSeq(summary, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = &chunk{heading: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if current == nil {
			if strings.TrimSpace(line) != WorkingStateTitle {
				preamble = append(preamble, line)
			}
			continue
		}
		if current.body != "" {
			current.body += "\n"
		}
		current.body += line
	}
	flush()

	sections := make(map[string]string, len(WorkingStateSections))
	var extras []chunk
	for _, item := range chunks {
		body := strings.TrimSpace(item.body)
		if !known[item.heading] {
			extras = append(extras, chunk{heading: item.heading, body: body})
			continue
		}
		prior, exists := sections[item.heading]
		switch {
		case !exists:
			sections[item.heading] = body
		case checkpointBodyEmpty(prior) && !checkpointBodyEmpty(body):
			sections[item.heading] = body
		case !checkpointBodyEmpty(body) && !strings.Contains(prior, body):
			sections[item.heading] = strings.TrimSpace(prior) + "\n" + body
		}
	}

	var out strings.Builder
	out.WriteString(WorkingStateTitle)
	if text := strings.TrimSpace(strings.Join(preamble, "\n")); text != "" {
		out.WriteString("\n\n")
		out.WriteString(text)
	}
	for _, item := range extras {
		if item.heading == "" {
			continue
		}
		out.WriteString("\n\n## ")
		out.WriteString(item.heading)
		if item.body != "" {
			out.WriteByte('\n')
			out.WriteString(item.body)
		}
	}
	for _, section := range WorkingStateSections {
		body := strings.TrimSpace(sections[section])
		if body == "" {
			body = "- None recorded."
		}
		out.WriteString("\n\n## ")
		out.WriteString(section)
		out.WriteByte('\n')
		out.WriteString(body)
	}
	return out.String()
}

// NormalizeWorkingStateCheckpoint validates provider output and deterministically
// repairs critical empty sections from the exact compacted prefix. A provider
// that emits tool-protocol markup instead of a summary is discarded entirely.
// The returned bool reports that local fallback content was used.
func NormalizeWorkingStateCheckpoint(ctx context.Context, summary string, msgs []protocol.Message) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	local, err := DefaultSummarizer(ctx, msgs)
	if err != nil {
		return "", false, err
	}
	if containsProviderToolMarkup(summary) {
		return local, true, nil
	}
	checkpoint := canonicalizeWorkingStateCheckpoint(FormatWorkingStateCheckpoint(summary))
	local = canonicalizeWorkingStateCheckpoint(FormatWorkingStateCheckpoint(local))
	// Objective/prior-state and verification evidence are factual rather than
	// interpretive. Always merge the deterministic extraction so provider prose
	// cannot silently omit an old constraint or erase a failed command by
	// claiming the suite is clean. Duplicate bodies are suppressed for repeated
	// compaction.
	for _, section := range []string{
		"Objective and constraints",
		"Prior working-state checkpoints",
		"Commands and verification",
		"Important tool results",
		"Errors and failed approaches",
	} {
		body := checkpointSection(local, section)
		if checkpointBodyEmpty(body) {
			continue
		}
		current := checkpointSection(checkpoint, section)
		if checkpointBodyEmpty(current) {
			checkpoint = replaceCheckpointSection(checkpoint, section, body)
			continue
		}
		if !strings.Contains(current, body) {
			checkpoint = replaceCheckpointSection(checkpoint, section, current+"\n"+body)
		}
	}
	if failures := checkpointSection(local, "Errors and failed approaches"); !checkpointBodyEmpty(failures) {
		const warning = "- Deterministic verification status: failures were recorded; do not treat an unverified provider claim that all checks passed as authoritative."
		current := checkpointSection(checkpoint, "Commands and verification")
		if !strings.Contains(current, warning) {
			checkpoint = replaceCheckpointSection(checkpoint, "Commands and verification", current+"\n"+warning)
		}
	}
	return deduplicateCheckpointBullets(checkpoint), false, nil
}

func deduplicateCheckpointBullets(checkpoint string) string {
	for _, section := range []string{"Current working state", "Decisions and rationale", "Commands and verification", "Important tool results", "Errors and failed approaches", "Unresolved next steps"} {
		seen := make(map[string]bool)
		body := checkpointSection(checkpoint, section)
		if checkpointBodyEmpty(body) {
			continue
		}
		lines := strings.Split(body, "\n")
		filtered := lines[:0]
		for _, line := range lines {
			key := strings.TrimSpace(line)
			if strings.HasPrefix(key, "- ") && key != "- None recorded." {
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			filtered = append(filtered, line)
		}
		if len(filtered) == 0 {
			filtered = append(filtered, "- None recorded.")
		}
		checkpoint = replaceCheckpointSection(checkpoint, section, strings.Join(filtered, "\n"))
	}
	return checkpoint
}

func containsProviderToolMarkup(summary string) bool {
	lower := strings.ToLower(summary)
	for _, marker := range providerToolMarkupMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func sanitizeProviderToolMarkup(value string) string {
	const replacement = "[provider tool-protocol markup removed]"
	for {
		lower := strings.ToLower(value)
		start := -1
		for _, marker := range providerToolMarkupMarkers {
			if index := strings.Index(lower, marker); index >= 0 && (start < 0 || index < start) {
				start = index
			}
		}
		if start < 0 {
			return value
		}
		end := len(value)
		for _, boundary := range []string{
			"<｜dsml｜/tool_calls>",
			"</tool_calls>",
			"</tool_call>",
			"</function_call>",
			"\n## ",
		} {
			if offset := strings.Index(lower[start:], boundary); offset >= 0 {
				candidate := start + offset
				if boundary != "\n## " {
					candidate += len(boundary)
				}
				if candidate > start && candidate < end {
					end = candidate
				}
			}
		}
		value = strings.TrimSpace(value[:start]) + "\n" + replacement + "\n" + strings.TrimSpace(value[end:])
	}
}

func checkpointSection(summary, section string) string {
	heading := "\n## " + section + "\n"
	start := strings.Index(summary, heading)
	if start < 0 {
		return ""
	}
	start += len(heading)
	end := len(summary)
	if next := strings.Index(summary[start:], "\n## "); next >= 0 {
		end = start + next
	}
	return strings.TrimSpace(summary[start:end])
}

func checkpointBodyEmpty(body string) bool {
	body = strings.TrimSpace(body)
	return body == "" || body == "- None recorded." || body == "None recorded."
}

func replaceCheckpointSection(summary, section, body string) string {
	heading := "\n## " + section + "\n"
	start := strings.Index(summary, heading)
	if start < 0 {
		return strings.TrimRight(summary, "\n") + heading + strings.TrimSpace(body)
	}
	bodyStart := start + len(heading)
	bodyEnd := len(summary)
	if next := strings.Index(summary[bodyStart:], "\n## "); next >= 0 {
		bodyEnd = bodyStart + next
	}
	return summary[:bodyStart] + strings.TrimSpace(body) + summary[bodyEnd:]
}

// DefaultSummarizer produces a bounded role/tool-aware continuation checkpoint.
func commandEvidenceTool(name string) bool {
	return name == "bash" || strings.HasPrefix(name, "process_")
}

func DefaultSummarizer(ctx context.Context, msgs []protocol.Message) (string, error) {
	const maxRunes = 8000
	type pinnedFailure struct {
		command bool
		line    string
	}
	var users, assistants, commands, important, failures, priorCheckpoints, agentUpdates, filesSymbols []string
	var failurePins []pinnedFailure
	fileSymbolSeen := make(map[string]bool)
	for _, m := range msgs {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for _, c := range m.Content {
			text := strings.TrimSpace(sanitizeProviderToolMarkup(c.Text))
			hintSources := make([]string, 0, 2)
			if text != "" {
				hintSources = append(hintSources, text)
			}
			if len(c.Arguments) > 0 {
				argumentText := strings.TrimSpace(sanitizeProviderToolMarkup(string(c.Arguments)))
				if text == "" {
					text = argumentText
				}
				hintSources = append(hintSources, jsonStringValues(c.Arguments)...)
			}
			if text == "" {
				continue
			}
			for _, source := range hintSources {
				for _, hint := range extractFileAndSymbolHints(source) {
					if !fileSymbolSeen[hint] {
						fileSymbolSeen[hint] = true
						filesSymbols = append(filesSymbols, hint)
					}
				}
			}
			text = compactText(text, 500)
			if m.Role == protocol.RoleAssistant && c.Type == protocol.BlockToolCall {
				ref := c.Name
				if c.ToolCallID != "" {
					ref += "/" + c.ToolCallID
				}
				call := strings.TrimSpace(ref + " " + text)
				if call != "" {
					if commandEvidenceTool(c.Name) {
						commands = append(commands, call)
					} else {
						important = append(important, call)
					}
				}
				continue
			}
			switch m.Role {
			case protocol.RoleUser:
				users = append(users, text)
			case protocol.RoleTool:
				ref := m.ToolName
				if m.ToolCallID != "" {
					ref += "/" + m.ToolCallID
				}
				line := ref + ": " + compactText(text, 260)
				section := "Important tool results"
				commandEvidence := commandEvidenceTool(m.ToolName)
				if commandEvidence {
					commands = append(commands, line)
					section = "Commands and verification"
				} else {
					important = append(important, line)
				}
				if m.IsError || strings.Contains(strings.ToLower(text), "error") || strings.Contains(strings.ToLower(text), "failed") {
					failures = append(failures, "See "+section+" entry "+ref+" for the recorded failure.")
					failurePins = append(failurePins, pinnedFailure{command: commandEvidence, line: line})
				}
			case protocol.RoleAssistant:
				assistants = append(assistants, text)
			case protocol.RoleCustom:
				priorCheckpoints = append(priorCheckpoints, text)
			case protocol.RoleAgent:
				agentUpdates = append(agentUpdates, text)
			}
		}
	}
	if len(failurePins) > 4 {
		failurePins = failurePins[len(failurePins)-4:]
	}
	var failedCommands, failedImportant []string
	for _, pinned := range failurePins {
		if pinned.command {
			failedCommands = append(failedCommands, pinned.line)
		} else {
			failedImportant = append(failedImportant, pinned.line)
		}
	}
	pinEvidence := func(values, pinned []string) []string {
		pinnedSet := make(map[string]bool, len(pinned))
		for _, value := range pinned {
			pinnedSet[value] = true
		}
		out := values[:0]
		for _, value := range values {
			if !pinnedSet[value] {
				out = append(out, value)
			}
		}
		remaining := max(8-len(pinned), 0)
		if len(out) > remaining {
			out = out[len(out)-remaining:]
		}
		prioritized := slices.Clone(pinned)
		return append(prioritized, out...)
	}
	commands = pinEvidence(commands, failedCommands)
	important = pinEvidence(important, failedImportant)

	var b strings.Builder
	b.WriteString(WorkingStateTitle + "\n\nGenerated by the bounded local fallback. Exact history remains durable.\n")
	writeRecent := func(title string, values []string, maxItems, sectionRunes int) {
		b.WriteString("\n## " + title + "\n")
		if len(values) == 0 || maxItems <= 0 || sectionRunes <= 0 {
			b.WriteString("- None recorded.\n")
			return
		}
		start := max(0, len(values)-maxItems)
		remaining := sectionRunes
		wrote := false
		for _, value := range values[start:] {
			valueRunes := []rune(value)
			if len(valueRunes)+3 > remaining {
				limit := max(0, remaining-4)
				if limit == 0 {
					break
				}
				value = string(valueRunes[:min(limit, len(valueRunes))]) + "…"
			}
			line := "- " + value + "\n"
			b.WriteString(line)
			remaining -= utf8.RuneCountInString(line)
			wrote = true
			if remaining <= 4 {
				break
			}
		}
		if !wrote {
			b.WriteString("- None recorded.\n")
		}
	}
	writeRecent("Objective and constraints", users, 4, 700)
	writeRecent("Current working state", assistants, 4, 800)
	writeRecent("Decisions and rationale", []string{"Local fallback does not infer decisions; inspect Current working state and exact durable history."}, 1, 180)
	writeRecent("Files and symbols", filesSymbols, 8, 600)
	writeRecent("Commands and verification", commands, 8, 1300)
	writeRecent("Important tool results", important, 8, 1300)
	writeRecent("Errors and failed approaches", failures, 4, 600)
	writeRecent("Attributed agent updates", agentUpdates, 4, 500)
	writeRecent("Prior working-state checkpoints", priorCheckpoints, 2, 700)
	// Retrieval markers are appended only after session-scoped ownership
	// verification in agent.compactActiveContext.
	writeRecent("Retrieval references", nil, 0, 0)
	writeRecent("Unresolved next steps", []string{"Local fallback does not infer next steps; inspect Current working state and exact durable history."}, 1, 180)
	writeRecent("Active tool batch", nil, 0, 0)
	out := []rune(b.String())
	if len(out) > maxRunes {
		return "", errors.New("compact: local checkpoint exceeded its fixed bound")
	}
	return string(out), nil
}

func jsonStringValues(raw json.RawMessage) []string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case string:
			out = append(out, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			keys := slices.Sorted(maps.Keys(typed))
			for _, key := range keys {
				walk(typed[key])
			}
		}
	}
	walk(value)
	return out
}

func extractFileAndSymbolHints(value string) []string {
	var hints []string
	for field := range strings.FieldsSeq(value) {
		raw := field
		candidate := strings.Trim(field, "`[](){}<>,.;:\"'")
		if candidate == "" || len(candidate) > 200 || strings.Contains(candidate, "://") {
			continue
		}
		pathLike := strings.Contains(candidate, "/") || strings.Contains(candidate, "\\")
		if !pathLike {
			for _, suffix := range []string{".go", ".md", ".json", ".yaml", ".yml", ".toml", ".sh", ".sql", ".db"} {
				if strings.HasSuffix(candidate, suffix) {
					pathLike = true
					break
				}
			}
		}
		quotedSymbol := strings.HasPrefix(raw, "`") && strings.Contains(strings.TrimPrefix(raw, "`"), "`")
		if pathLike || quotedSymbol {
			hints = append(hints, candidate)
		}
	}
	return hints
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	r := []rune(value)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return value
}
