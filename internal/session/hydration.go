package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// BranchHydrationStore exposes a lightweight exact-branch projection for
// transcript hydration. Large message bodies stay in the store; callers get
// only presentation counts, durable identities, composer inputs, the latest
// plan, and enough context metadata to reproduce the footer usage snapshot.
type BranchHydrationStore interface {
	BranchHydration() (BranchHydrationSnapshot, error)
}

// BranchEntryPager reads a bounded root-to-tip window ending at cursor. Cursor
// must identify an entry on the branch snapshot obtained by the caller.
type BranchEntryPager interface {
	BranchEntryPage(cursor string, limit int) (BranchEntryPage, error)
}

// BranchEntryLookup fetches a bounded set of exact entries by durable ID. TUI
// hydration uses it only for legacy tool-call lookbehind outside the row page.
type BranchEntryLookup interface {
	BranchEntriesByID(ids []string) ([]Entry, error)
}

// BranchEntryPage is ordered from its oldest entry to cursor. OlderCursor is
// the parent of the first entry, or empty when the page reached the root.
type BranchEntryPage struct {
	Entries     []Entry
	OlderCursor string
}

// BranchHydrationSnapshot contains small, caller-owned projections for one
// active branch tip. Entries remain root-to-tip ordered.
type BranchHydrationSnapshot struct {
	TipID        string
	Entries      []BranchEntrySummary
	UserInputs   []string
	LatestPlan   string
	ContextUsage BranchContextUsage
	TurnCount    uint64
	StepCount    uint64
}

// BranchEntrySummary contains the bounded scalar state needed to count exact
// transcript rows without retaining a complete decoded message.
type BranchEntrySummary struct {
	ID               string
	Type             EntryType
	CompactedThrough string

	Role               protocol.Role
	AgentRunMarker     uint8
	UserVisible        bool
	ThinkingVisible    bool
	BodyRows           int
	StopVisible        bool
	ContextChars       int
	Usage              *protocol.Usage
	ToolCallID         string
	ToolName           string
	ToolIsError        bool
	ToolOutputPresent  bool
	ToolDisplayPresent bool
	ToolWasDispatched  bool
	ToolCallIDs        []string

	CompactionActive          bool
	CompactionCheckpointChars int
}

// BranchContextUsage is the message-light equivalent of the provider-facing
// context projection. EstimatedChars is used when Compacted or Usage is nil;
// otherwise TrailingChars covers messages appended after the measured request.
type BranchContextUsage struct {
	Compacted           bool
	EstimatedChars      int
	Usage               *protocol.Usage
	TrailingChars       int
	HasTrailingMessages bool
}

type entryHydrationRecord struct {
	summary         BranchEntrySummary
	userInput       string
	latestPlan      string
	latestPlanIndex int
}

func summarizeHydrationEntry(entry Entry) entryHydrationRecord {
	record := entryHydrationRecord{
		summary: BranchEntrySummary{
			ID:               entry.ID,
			Type:             entry.Type,
			CompactedThrough: entry.CompactedThrough,
		},
		latestPlanIndex: -1,
	}
	switch {
	case IsAgentTurnMarker(entry):
		record.summary.AgentRunMarker = agentRunMarkerTurn
	case IsAgentStepMarker(entry):
		record.summary.AgentRunMarker = agentRunMarkerStep
	}
	if entry.Type == EntryCompaction && strings.TrimSpace(entry.Summary) != "" {
		record.summary.CompactionActive = true
		record.summary.CompactionCheckpointChars = compactedCheckpointContextChars(entry.Summary)
	}
	if entry.Type == EntryMeta && entry.Key == MetaToolTranscript {
		var transcript protocol.ToolTranscript
		if json.Unmarshal([]byte(entry.Value), &transcript) == nil && transcript.ToolName != "" {
			record.summary.Role = protocol.RoleTool
			record.summary.ToolName = transcript.ToolName
			record.summary.ToolIsError = transcript.IsError
			record.summary.ToolOutputPresent = strings.TrimSpace(transcript.Display.Output) != ""
		}
		return record
	}
	if entry.Type != EntryMessage || entry.Message == nil {
		return record
	}

	message := entry.Message
	metadata := hydrationMessageMetadata{
		role: message.Role, stopReason: message.StopReason, errorText: message.Error,
		usage: message.Usage, toolCallID: message.ToolCallID, toolName: message.ToolName,
		toolIsError: message.IsError, toolDisplay: message.ToolDisplay,
	}
	return summarizeHydrationMessage(record, metadata, len(message.Content), func(i int) hydrationBlockView {
		block := message.Content[i]
		return hydrationBlockView{
			typeName: block.Type, text: block.Text, name: block.Name,
			toolCallID: block.ToolCallID, argumentsLen: len(block.Arguments),
		}
	})
}

type hydrationMessageMetadata struct {
	role        protocol.Role
	stopReason  protocol.StopReason
	errorText   string
	usage       *protocol.Usage
	toolCallID  string
	toolName    string
	toolIsError bool
	toolDisplay *protocol.ToolDisplay
}

type hydrationBlockView struct {
	typeName     protocol.ContentBlockType
	text         string
	name         string
	toolCallID   string
	argumentsLen int
}

type hydrationMessageAccumulator struct {
	record  entryHydrationRecord
	message hydrationMessageMetadata
	text    strings.Builder
}

func newHydrationMessageAccumulator(record entryHydrationRecord, message hydrationMessageMetadata) hydrationMessageAccumulator {
	record.summary.Role = message.role
	record.summary.ContextChars = len(message.role) + 8
	record.summary.Usage = message.usage.Clone()
	record.summary.ToolCallID = message.toolCallID
	record.summary.ToolName = message.toolName
	record.summary.ToolIsError = message.toolIsError
	return hydrationMessageAccumulator{record: record, message: message}
}

func (a *hydrationMessageAccumulator) addBlock(i int, block hydrationBlockView) {
	a.record.summary.ContextChars += len(block.typeName) + len(block.text) + len(block.name) +
		len(block.toolCallID) + block.argumentsLen + 8
	switch block.typeName {
	case protocol.BlockImage:
		if a.message.role == protocol.RoleUser {
			a.record.summary.UserVisible = true
		}
	case protocol.BlockText:
		if a.message.role == protocol.RoleUser || a.message.role == protocol.RoleTool {
			a.text.WriteString(block.text)
		}
		if a.message.role == protocol.RoleUser && strings.TrimSpace(block.text) != "" {
			a.record.summary.UserVisible = true
		} else if a.message.role == protocol.RoleAssistant && strings.TrimSpace(block.text) != "" {
			a.record.summary.BodyRows++
		}
	case protocol.BlockThinking:
		if a.message.role == protocol.RoleAssistant && strings.TrimSpace(block.text) != "" {
			a.record.summary.ThinkingVisible = true
		}
	case protocol.BlockPlan:
		if a.message.role == protocol.RoleAssistant && strings.TrimSpace(block.text) != "" {
			a.record.summary.BodyRows++
			a.record.latestPlan = block.text
			a.record.latestPlanIndex = i
		}
	case protocol.BlockToolCall:
		if a.message.role == protocol.RoleAssistant && block.toolCallID != "" {
			a.record.summary.ToolCallIDs = append(a.record.summary.ToolCallIDs, block.toolCallID)
		}
	}
}

func (a *hydrationMessageAccumulator) finish() entryHydrationRecord {
	messageText := a.text.String()
	if a.message.role == protocol.RoleUser && strings.TrimSpace(messageText) != "" {
		a.record.userInput = messageText
	}
	if a.message.role == protocol.RoleAssistant {
		switch a.message.stopReason {
		case protocol.StopAborted:
			a.record.summary.StopVisible = true
		case protocol.StopError:
			a.record.summary.StopVisible = strings.TrimSpace(a.message.errorText) != ""
		}
	}
	if a.message.role == protocol.RoleTool {
		a.record.summary.ToolDisplayPresent = a.message.toolDisplay != nil
		a.record.summary.ToolWasDispatched = legacyToolResultWasDispatched(a.message.toolIsError, messageText)
		switch {
		case a.message.toolDisplay != nil:
			a.record.summary.ToolOutputPresent = strings.TrimSpace(a.message.toolDisplay.Output) != ""
		case a.message.toolName == "get_goal", a.message.toolName == "create_goal", a.message.toolName == "update_goal":
			a.record.summary.ToolOutputPresent = true
		default:
			a.record.summary.ToolOutputPresent = strings.TrimSpace(messageText) != ""
		}
	}
	return a.record
}

func summarizeHydrationMessage(
	record entryHydrationRecord,
	message hydrationMessageMetadata,
	blockCount int,
	blockAt func(int) hydrationBlockView,
) entryHydrationRecord {
	accumulator := newHydrationMessageAccumulator(record, message)
	for i := range blockCount {
		accumulator.addBlock(i, blockAt(i))
	}
	return accumulator.finish()
}

func userTextForBlocks(blocks []protocol.ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == protocol.BlockText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

// LegacyToolResultWasDispatched identifies pre-ToolDisplay synthetic results.
// Its result is persisted in hydration projections; semantic changes require a
// projection-version bump so old sessions are rebuilt consistently.
func LegacyToolResultWasDispatched(message protocol.Message) bool {
	return legacyToolResultWasDispatched(message.IsError, userTextForBlocks(message.Content))
}

func legacyToolResultWasDispatched(isError bool, messageText string) bool {
	if !isError {
		return true
	}
	text := strings.TrimSpace(messageText)
	for _, prefix := range []string{
		"Permission denied:",
		"Error: tool arguments are not valid JSON:",
		"Error: unknown tool ",
		"Error: tool call cancelled:",
		"Error: tool call skipped ",
	} {
		if strings.HasPrefix(text, prefix) {
			return false
		}
	}
	return !strings.Contains(text, " is unavailable in ")
}

func compactedCheckpointContextChars(summary string) int {
	return len(protocol.RoleCustom) + 8 + len(protocol.BlockText) +
		len(compactedCheckpointPrefix) + len(summary) + 8
}

func buildBranchHydrationSnapshot(tip string, entries []Entry) BranchHydrationSnapshot {
	stats := agentRunStatsFromEntries(entries)
	snapshot := BranchHydrationSnapshot{
		TipID:     tip,
		Entries:   make([]BranchEntrySummary, 0, len(entries)),
		TurnCount: stats.Turns,
		StepCount: stats.Steps,
	}
	for _, entry := range entries {
		record := summarizeHydrationEntry(entry)
		snapshot.Entries = append(snapshot.Entries, record.summary)
		if record.userInput != "" {
			snapshot.UserInputs = append(snapshot.UserInputs, strings.Clone(record.userInput))
		}
		if record.latestPlan != "" {
			snapshot.LatestPlan = strings.Clone(record.latestPlan)
		}
	}
	snapshot.ContextUsage = summarizeBranchContextUsage(snapshot.Entries)
	return snapshot
}

func summarizeBranchContextUsage(entries []BranchEntrySummary) BranchContextUsage {
	lastCompaction, boundaryPos := latestHydrationCompaction(entries)
	if lastCompaction >= 0 {
		chars := entries[lastCompaction].CompactionCheckpointChars
		for i := boundaryPos + 1; i < len(entries); i++ {
			if entries[i].Type == EntryMessage {
				chars += entries[i].ContextChars
			}
		}
		return BranchContextUsage{Compacted: true, EstimatedChars: chars}
	}

	lastUsage := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == EntryMessage && entries[i].Usage != nil {
			lastUsage = i
			break
		}
	}
	if lastUsage >= 0 {
		context := BranchContextUsage{Usage: entries[lastUsage].Usage.Clone()}
		for i := lastUsage + 1; i < len(entries); i++ {
			if entries[i].Type != EntryMessage {
				continue
			}
			context.HasTrailingMessages = true
			context.TrailingChars += entries[i].ContextChars
		}
		return context
	}
	context := BranchContextUsage{}
	for i := range entries {
		if entries[i].Type == EntryMessage {
			context.EstimatedChars += entries[i].ContextChars
		}
	}
	return context
}

func latestHydrationCompaction(entries []BranchEntrySummary) (lastCompaction, boundaryPos int) {
	lastCompaction, boundaryPos = -1, -1
	hasCompaction := false
	for i := range entries {
		if entries[i].Type == EntryCompaction && entries[i].CompactionActive {
			hasCompaction = true
			break
		}
	}
	if !hasCompaction {
		return lastCompaction, boundaryPos
	}
	positions := make(map[string]int, len(entries))
	for i := range entries {
		positions[entries[i].ID] = i
	}
	for i, entry := range entries {
		if entry.Type != EntryCompaction || !entry.CompactionActive {
			continue
		}
		resolved := i - 1
		if pos, ok := positions[entry.CompactedThrough]; ok && pos < i {
			resolved = pos
		}
		if resolved > boundaryPos || (resolved == boundaryPos && i > lastCompaction) {
			lastCompaction = i
			boundaryPos = resolved
		}
	}
	return lastCompaction, boundaryPos
}

const maxBranchHydrationPage = 4096

func validateHydrationPage(limit int) error {
	if limit <= 0 || limit > maxBranchHydrationPage {
		return fmt.Errorf("session: branch page limit must be between 1 and %d", maxBranchHydrationPage)
	}
	return nil
}

func branchEntriesByID(entries []Entry, byID map[string]int, ids []string) ([]Entry, error) {
	if len(ids) > maxBranchHydrationPage {
		return nil, fmt.Errorf("session: branch entry lookup exceeds %d ids", maxBranchHydrationPage)
	}
	out := make([]Entry, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		index, ok := byID[id]
		if !ok {
			return nil, ErrNotFound
		}
		out = append(out, cloneEntry(entries[index]))
	}
	return out, nil
}

func branchEntryPage(entries []Entry, byID map[string]int, cursor string, limit int) (BranchEntryPage, error) {
	if err := validateHydrationPage(limit); err != nil {
		return BranchEntryPage{}, err
	}
	if cursor == "" {
		return BranchEntryPage{}, nil
	}
	reversed := make([]Entry, 0, limit)
	current := cursor
	for len(reversed) < limit && current != "" {
		index, ok := byID[current]
		if !ok {
			if len(reversed) == 0 {
				return BranchEntryPage{}, ErrNotFound
			}
			break
		}
		entry := cloneEntry(entries[index])
		reversed = append(reversed, entry)
		current = entry.ParentID
	}
	page := BranchEntryPage{Entries: make([]Entry, len(reversed)), OlderCursor: current}
	for i := range reversed {
		page.Entries[len(reversed)-1-i] = reversed[i]
	}
	return page, nil
}

// BranchHydration implements BranchHydrationStore.
func (s *MemoryStore) BranchHydration() (BranchHydrationSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return BranchHydrationSnapshot{}, errors.New("session: store closed")
	}
	return buildBranchHydrationSnapshot(s.tip, pathFrom(s.entries, s.byID, s.tip)), nil
}

// BranchEntryPage implements BranchEntryPager.
func (s *MemoryStore) BranchEntryPage(cursor string, limit int) (BranchEntryPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return BranchEntryPage{}, errors.New("session: store closed")
	}
	return branchEntryPage(s.entries, s.byID, cursor, limit)
}

// BranchEntriesByID implements BranchEntryLookup.
func (s *MemoryStore) BranchEntriesByID(ids []string) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return branchEntriesByID(s.entries, s.byID, ids)
}

// BranchHydration implements BranchHydrationStore for legacy fixtures.
func (s *JSONLStore) BranchHydration() (BranchHydrationSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return BranchHydrationSnapshot{}, errors.New("session: store closed")
	}
	return buildBranchHydrationSnapshot(s.tip, pathFrom(s.entries, s.byID, s.tip)), nil
}

// BranchEntryPage implements BranchEntryPager for legacy fixtures.
func (s *JSONLStore) BranchEntryPage(cursor string, limit int) (BranchEntryPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return BranchEntryPage{}, errors.New("session: store closed")
	}
	return branchEntryPage(s.entries, s.byID, cursor, limit)
}

// BranchEntriesByID implements BranchEntryLookup for legacy fixtures.
func (s *JSONLStore) BranchEntriesByID(ids []string) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	return branchEntriesByID(s.entries, s.byID, ids)
}
