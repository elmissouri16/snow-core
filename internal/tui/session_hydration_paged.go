package tui

import (
	"encoding/json"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const sessionHydrationPageSize = 256

func (m *Model) hydrateSession() {
	if m.hydrateSessionPaginated() {
		return
	}
	m.hydrateSessionLegacy()
}

// hydrateSessionPaginated keeps complete lightweight ancestry state while
// fetching large message blobs only for the bounded visible suffix. False
// selects the legacy custom-store path without partially mutating the model.
func (m *Model) hydrateSessionPaginated() bool {
	if m.app == nil || m.app.Agent == nil || m.app.Session == nil {
		return false
	}
	hydrationStore, ok := m.app.Session.(session.BranchHydrationStore)
	if !ok {
		return false
	}
	entryLookup, ok := m.app.Session.(session.BranchEntryLookup)
	if !ok {
		return false
	}
	snapshot, err := hydrationStore.BranchHydration()
	if err != nil {
		return false
	}
	defer m.clearFinalizedMarkdownCaches()
	m.clearTranscriptSelection()

	durableIDs := make([]string, len(snapshot.Entries))
	rowCounts := make([]int, len(snapshot.Entries))
	totalRows := 0
	for i, summary := range snapshot.Entries {
		durableIDs[i] = summary.ID
		rowCounts[i] = hydrationSummaryRowCount(summary)
		totalRows += rowCounts[i]
	}
	usageSnapshot := hydrationContextUsage(snapshot.ContextUsage)
	key := ""
	if m.inlineTranscript {
		key = m.inlineSessionKey()
	}
	if m.inlineTranscript && key != "" && key == m.inlineHistoryKey {
		m.hydrateInputHistoryValues(snapshot.UserInputs)
		m.inlineDurableMessageIDs = durableIDs
		m.applyContextUsageSnapshot(usageSnapshot)
		m.applyTurnCount(snapshot.TurnCount)
		return true
	}

	hadPrintedHistory := m.inlineTranscript && m.inlineEverCommitted
	commonEntries := 0
	if hadPrintedHistory {
		limit := min(len(m.inlineDurableMessageIDs), len(durableIDs))
		for commonEntries < limit && m.inlineDurableMessageIDs[commonEntries] == durableIDs[commonEntries] {
			commonEntries++
		}
	}

	globalSkipRows := 0
	hydrationOmitted := 0
	inlineOmitted := 0
	if m.inlineTranscript {
		commonRows := sumHydrationRows(rowCounts[:commonEntries])
		changedRows := totalRows - commonRows
		if changedRows > hydrationSegmentLimit {
			inlineOmitted = changedRows - hydrationSegmentLimit
		}
		globalSkipRows = commonRows + inlineOmitted
	} else if totalRows > maxTranscriptEntries {
		hydrationOmitted = totalRows - (maxTranscriptEntries - 1)
		globalSkipRows = hydrationOmitted
	}

	start := hydrationEntryAtRowOffset(rowCounts, globalSkipRows)
	prefixRows := sumHydrationRows(rowCounts[:start])
	localSkipRows := globalSkipRows - prefixRows
	if localSkipRows < 0 {
		return false
	}

	visibleIDs := make([]string, 0, min(maxTranscriptEntries, len(snapshot.Entries)-start))
	visibleSet := make(map[string]struct{}, cap(visibleIDs))
	for i := start; i < len(snapshot.Entries); i++ {
		if rowCounts[i] == 0 {
			continue
		}
		id := snapshot.Entries[i].ID
		visibleIDs = append(visibleIDs, id)
		visibleSet[id] = struct{}{}
	}
	entriesByID, ok := loadHydrationEntries(entryLookup, visibleIDs)
	if !ok {
		return false
	}

	latestToolCallEntry := make(map[string]string)
	toolCallEntryForResult := make(map[string]string)
	for _, summary := range snapshot.Entries {
		for _, callID := range summary.ToolCallIDs {
			latestToolCallEntry[callID] = summary.ID
		}
		if summary.Role == protocol.RoleTool && summary.ToolCallID != "" {
			toolCallEntryForResult[summary.ID] = latestToolCallEntry[summary.ToolCallID]
		}
	}
	lookbehindIDs := make([]string, 0)
	lookbehindSet := make(map[string]struct{})
	for i := start; i < len(snapshot.Entries); i++ {
		summary := snapshot.Entries[i]
		if rowCounts[i] == 0 || summary.Role != protocol.RoleTool || summary.ToolCallID == "" ||
			summary.ToolDisplayPresent || !toolCallArgumentsAffectStartMessage(summary.ToolName) ||
			!summary.ToolWasDispatched {
			continue
		}
		entryID := toolCallEntryForResult[summary.ID]
		if entryID == "" {
			continue
		}
		if _, visible := visibleSet[entryID]; visible {
			continue
		}
		if _, duplicate := lookbehindSet[entryID]; duplicate {
			continue
		}
		lookbehindSet[entryID] = struct{}{}
		lookbehindIDs = append(lookbehindIDs, entryID)
	}
	lookbehind, ok := loadHydrationEntries(entryLookup, lookbehindIDs)
	if !ok {
		return false
	}
	for id, entry := range lookbehind {
		entriesByID[id] = entry
	}

	toolCallsByEntry := make(map[string]map[string]protocol.ContentBlock)
	for _, entry := range entriesByID {
		if entry.Message == nil || entry.Message.Role != protocol.RoleAssistant {
			continue
		}
		calls := make(map[string]protocol.ContentBlock)
		for _, block := range entry.Message.Content {
			if block.Type == protocol.BlockToolCall && block.ToolCallID != "" {
				calls[block.ToolCallID] = block
			}
		}
		if len(calls) > 0 {
			toolCallsByEntry[entry.ID] = calls
		}
	}

	renderMessages := make([]protocol.Message, 0, len(visibleIDs))
	for i := start; i < len(snapshot.Entries); i++ {
		if rowCounts[i] == 0 {
			continue
		}
		entry, found := entriesByID[snapshot.Entries[i].ID]
		if !found {
			return false
		}
		message, found := hydrationRenderMessage(entry)
		if !found {
			return false
		}
		renderMessages = append(renderMessages, message)
	}

	actualRows := 0
	toolCallForMessage := make(map[int]protocol.ContentBlock)
	for i, message := range renderMessages {
		callEntryID := toolCallEntryForResult[message.ID]
		call := toolCallsByEntry[callEntryID][message.ToolCallID]
		if call.Type == protocol.BlockToolCall {
			toolCallForMessage[i] = call
		}
		actualRows += m.hydrationMessageRowCount(message, call)
	}
	if actualRows != sumHydrationRows(rowCounts[start:]) {
		// The derived projection is rebuildable. Falling back keeps custom or
		// stale stores correct until their projection version is refreshed.
		return false
	}

	m.hydrateInputHistoryValues(snapshot.UserInputs)
	m.inlineCommitted = 0
	m.inlinePrintEnd = 0
	m.inlinePrintInFlight = false
	m.inlinePrintGeneration++
	m.latestPlan = snapshot.LatestPlan
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.applyContextUsageSnapshot(usageSnapshot)
	m.applyTurnCount(snapshot.TurnCount)

	hydrated := make([]string, 0, min(actualRows, maxTranscriptEntries))
	skipRows := localSkipRows
	for i, message := range renderMessages {
		rows := m.hydrationMessageRows(message, toolCallForMessage[i])
		if skipRows >= len(rows) {
			skipRows -= len(rows)
			continue
		}
		if skipRows > 0 {
			rows = rows[skipRows:]
			skipRows = 0
		}
		hydrated = append(hydrated, rows...)
	}
	if skipRows != 0 {
		return false
	}

	m.lines = nil
	m.transcriptBase = ""
	m.transcriptBaseSynced = 0
	m.transcriptDropped = 0
	m.transcriptBytes = 0
	if m.inlineTranscript {
		if hadPrintedHistory {
			m.lines = append(m.lines, styleFooter.Render("── switched transcript ──"))
		}
		if inlineOmitted > 0 {
			m.lines = append(m.lines, styleFooter.Render(fmt.Sprintf("── %d older transcript segments omitted ──", inlineOmitted)))
		}
		m.lines = append(m.lines, hydrated...)
		m.inlineHistoryKey = key
		m.inlineDurableMessageIDs = durableIDs
		m.inlineHeaderPending = true
	} else {
		if hydrationOmitted > 0 {
			m.transcriptDropped = hydrationOmitted
			marker := styleFooter.Render(fmt.Sprintf("── %d older transcript entries omitted ──", hydrationOmitted))
			m.lines = append(m.lines, marker)
			m.transcriptBytes += len(marker)
		}
		for _, row := range hydrated {
			m.appendTranscriptLine(row)
		}
	}
	m.refreshTranscript()
	return true
}

func loadHydrationEntries(lookup session.BranchEntryLookup, ids []string) (map[string]session.Entry, bool) {
	entries := make(map[string]session.Entry, len(ids))
	for start := 0; start < len(ids); start += sessionHydrationPageSize {
		end := min(start+sessionHydrationPageSize, len(ids))
		page, err := lookup.BranchEntriesByID(ids[start:end])
		if err != nil || len(page) != end-start {
			return nil, false
		}
		for _, entry := range page {
			entries[entry.ID] = entry
		}
	}
	return entries, len(entries) == len(ids)
}

func hydrationRenderMessage(entry session.Entry) (protocol.Message, bool) {
	switch {
	case entry.Type == session.EntryMessage && entry.Message != nil:
		return *entry.Message, true
	case entry.Type == session.EntryMeta && entry.Key == session.MetaToolTranscript:
		var transcript protocol.ToolTranscript
		if json.Unmarshal([]byte(entry.Value), &transcript) != nil || transcript.ToolName == "" {
			return protocol.Message{}, false
		}
		return protocol.Message{
			ID:          entry.ID,
			Role:        protocol.RoleTool,
			ToolName:    transcript.ToolName,
			IsError:     transcript.IsError,
			ToolDisplay: transcript.Display.Clone(),
		}, true
	default:
		return protocol.Message{}, false
	}
}

func hydrationSummaryRowCount(summary session.BranchEntrySummary) int {
	switch summary.Role {
	case protocol.RoleUser:
		if summary.UserVisible {
			return 1
		}
	case protocol.RoleAssistant:
		rows := summary.BodyRows
		if summary.ThinkingVisible {
			rows++
		}
		if summary.StopVisible {
			rows++
		}
		return rows
	case protocol.RoleTool:
		if summary.ToolIsError {
			return 1
		}
		rows := 1
		if summary.ToolName == "spawn_agent" {
			rows = 0
		}
		if hydrationToolOutputVisible(summary.ToolName, summary.ToolOutputPresent) {
			rows++
		}
		return rows
	}
	return 0
}

func hydrationToolOutputVisible(toolName string, outputPresent bool) bool {
	if toolName == "activate_skill" {
		return true
	}
	switch toolName {
	case "spawn_agent", "send_message", "followup_task", "interrupt_agent", "close_agent", "resume_agent":
		return false
	default:
		return outputPresent
	}
}

func hydrationEntryAtRowOffset(rowCounts []int, offset int) int {
	for i, rows := range rowCounts {
		if offset < rows {
			return i
		}
		offset -= rows
	}
	return len(rowCounts)
}

func sumHydrationRows(rows []int) int {
	total := 0
	for _, count := range rows {
		total += count
	}
	return total
}

func hydrationContextUsage(context session.BranchContextUsage) contextUsageSnapshot {
	if context.Compacted || context.Usage == nil {
		tokens := contextCharsToTokens(context.EstimatedChars)
		return contextUsageSnapshot{tokens: tokens, estimated: tokens > 0}
	}
	snapshot := contextUsageSnapshot{
		usage:  context.Usage.Clone(),
		tokens: contextTokensFromUsage(*context.Usage),
	}
	if context.HasTrailingMessages {
		snapshot.tokens += contextCharsToTokens(context.TrailingChars)
		snapshot.estimated = true
	}
	return snapshot
}

func contextCharsToTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}
