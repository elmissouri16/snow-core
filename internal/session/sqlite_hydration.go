package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	entryHydrationProjectionVersion  = 2
	hydrationProjectionSchemaVersion = 11
	hydrationBackfillBatchSize       = 128
	hydrationBackfillByteLimit       = 4 << 20
)

var errHydrationProjectionIncomplete = errors.New("session: hydration projection incomplete")

type hydrationProjectionEntryError struct {
	entryID string
	cause   error
}

func (e *hydrationProjectionEntryError) Error() string {
	return fmt.Sprintf("session: hydration projection for entry %q: %v", e.entryID, e.cause)
}

func (e *hydrationProjectionEntryError) Unwrap() error { return errHydrationProjectionIncomplete }

const insertHydrationProjectionSQL = `
	INSERT INTO entry_hydration_projection(
		entry_id, projection_version, role, latest_plan_index, projection)
	VALUES(?, ?, ?, ?, ?)
	ON CONFLICT(entry_id) DO UPDATE SET
		projection_version=excluded.projection_version,
		role=excluded.role,
		latest_plan_index=excluded.latest_plan_index,
		projection=excluded.projection`

type sqliteHydrationExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func hydrationProjectionValues(entry Entry) (entryHydrationRecord, []byte, error) {
	record := summarizeHydrationEntry(entry)
	projection, err := marshalHydrationProjection(record)
	return record, projection, err
}

func insertHydrationProjection(exec sqliteHydrationExecer, entry Entry) error {
	record, projection, err := hydrationProjectionValues(entry)
	if err != nil {
		return err
	}
	_, err = exec.Exec(insertHydrationProjectionSQL,
		entry.ID, entryHydrationProjectionVersion, record.summary.Role,
		record.latestPlanIndex, projection)
	if err != nil {
		return fmt.Errorf("session: write hydration projection: %w", err)
	}
	return nil
}

func insertPreparedHydrationProjection(stmt *sql.Stmt, entry Entry) error {
	record := summarizeHydrationEntry(entry)
	return insertPreparedHydrationRecord(stmt, entry.ID, record)
}

func insertPreparedHydrationRecord(stmt *sql.Stmt, entryID string, record entryHydrationRecord) error {
	projection, err := marshalHydrationProjection(record)
	if err != nil {
		return err
	}
	if _, err := stmt.Exec(entryID, entryHydrationProjectionVersion,
		record.summary.Role, record.latestPlanIndex, projection); err != nil {
		return fmt.Errorf("session: write hydration projection: %w", err)
	}
	return nil
}

func backfillHydrationProjections(tx *sql.Tx) error {
	projectionStmt, err := tx.Prepare(insertHydrationProjectionSQL)
	if err != nil {
		return fmt.Errorf("prepare projections: %w", err)
	}
	defer projectionStmt.Close()
	var cursor int64
	for {
		rows, err := tx.Query(`
			SELECT e.seq, e.id, e.parent_id, e.entry_type, e.message, e.summary,
				e.compacted_through, e.meta_key, e.meta_value
			FROM entries e
			LEFT JOIN entry_hydration_projection h ON h.entry_id = e.id
			WHERE e.seq > ? AND (h.entry_id IS NULL OR h.projection_version <> ?)
			ORDER BY e.seq
			LIMIT ?`, cursor, entryHydrationProjectionVersion, hydrationBackfillBatchSize)
		if err != nil {
			return fmt.Errorf("query projections: %w", err)
		}
		entries, err := scanHydrationBackfillEntries(rows)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		for _, source := range entries {
			if err := insertPreparedHydrationRecord(projectionStmt, source.entry.ID, source.record); err != nil {
				return err
			}
			cursor = source.seq
		}
	}
}

type hydrationBackfillEntry struct {
	seq    int64
	entry  Entry
	record entryHydrationRecord
}

type hydrationProjectionMessage struct {
	Role        protocol.Role                   `json:"role"`
	Content     borrowedHydrationJSON           `json:"content"`
	StopReason  protocol.StopReason             `json:"stop_reason,omitempty"`
	Error       string                          `json:"error,omitempty"`
	Usage       *protocol.Usage                 `json:"usage,omitempty"`
	ToolCallID  string                          `json:"tool_call_id,omitempty"`
	ToolName    string                          `json:"tool_name,omitempty"`
	IsError     bool                            `json:"is_error,omitzero"`
	ToolDisplay *hydrationProjectionToolDisplay `json:"tool_display,omitempty"`
}

type hydrationProjectionToolDisplay struct {
	Output string `json:"output,omitempty"`
}

type hydrationProjectionBlock struct {
	Type       protocol.ContentBlockType `json:"type"`
	Text       string                    `json:"text,omitempty"`
	ToolCallID string                    `json:"tool_call_id,omitempty"`
	Name       string                    `json:"name,omitempty"`
	Arguments  borrowedHydrationJSON     `json:"arguments,omitempty"`
}

type borrowedHydrationJSON []byte

func (b *borrowedHydrationJSON) UnmarshalJSON(data []byte) error {
	*b = data
	return nil
}

func decodeHydrationProjectionRecord(raw []byte, entry Entry) (entryHydrationRecord, error) {
	var source hydrationProjectionMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return entryHydrationRecord{}, err
	}
	var toolDisplay *protocol.ToolDisplay
	if source.ToolDisplay != nil {
		toolDisplay = &protocol.ToolDisplay{Output: source.ToolDisplay.Output}
	}
	metadata := hydrationMessageMetadata{
		role: source.Role, stopReason: source.StopReason, errorText: source.Error,
		usage: source.Usage, toolCallID: source.ToolCallID, toolName: source.ToolName,
		toolIsError: source.IsError, toolDisplay: toolDisplay,
	}
	accumulator := newHydrationMessageAccumulator(summarizeHydrationEntry(entry), metadata)
	if err := forEachHydrationProjectionBlock(source.Content, func(i int, block hydrationProjectionBlock) {
		accumulator.addBlock(i, hydrationBlockView{
			typeName: block.Type, text: block.Text, name: block.Name,
			toolCallID: block.ToolCallID, argumentsLen: len(block.Arguments),
		})
	}); err != nil {
		return entryHydrationRecord{}, err
	}
	return accumulator.finish(), nil
}

func forEachHydrationProjectionBlock(data []byte, visit func(int, hydrationProjectionBlock)) error {
	i := skipHydrationJSONSpace(data, 0)
	if i == len(data) {
		return nil
	}
	if len(data)-i >= 4 && data[i] == 'n' && data[i+1] == 'u' && data[i+2] == 'l' && data[i+3] == 'l' {
		if skipHydrationJSONSpace(data, i+4) != len(data) {
			return errors.New("trailing content data")
		}
		return nil
	}
	if data[i] != '[' {
		return errors.New("content is not an array")
	}
	i++
	for index := 0; ; index++ {
		i = skipHydrationJSONSpace(data, i)
		if i >= len(data) {
			return errors.New("unterminated content array")
		}
		if data[i] == ']' {
			i = skipHydrationJSONSpace(data, i+1)
			if i != len(data) {
				return errors.New("trailing content array data")
			}
			return nil
		}
		end, err := hydrationJSONObjectEnd(data, i)
		if err != nil {
			return fmt.Errorf("content block %d: %w", index, err)
		}
		var block hydrationProjectionBlock
		if err := json.Unmarshal(data[i:end], &block); err != nil {
			return fmt.Errorf("content block %d: %w", index, err)
		}
		visit(index, block)
		i = skipHydrationJSONSpace(data, end)
		if i >= len(data) || data[i] != ',' {
			if i < len(data) && data[i] == ']' {
				continue
			}
			return fmt.Errorf("content block %d: expected comma or array end", index)
		}
		i++
	}
}

func hydrationJSONObjectEnd(data []byte, start int) (int, error) {
	if start >= len(data) || data[start] != '{' {
		return 0, errors.New("content block is not an object")
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(data); i++ {
		char := data[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, errors.New("unterminated content block")
}

func skipHydrationJSONSpace(data []byte, offset int) int {
	for offset < len(data) {
		switch data[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		default:
			return offset
		}
	}
	return offset
}

func scanHydrationBackfillEntries(rows *sql.Rows) ([]hydrationBackfillEntry, error) {
	defer rows.Close()
	entries := make([]hydrationBackfillEntry, 0, hydrationBackfillBatchSize)
	decodedBytes := 0
	for rows.Next() {
		var source hydrationBackfillEntry
		var raw sql.RawBytes
		entry := &source.entry
		if err := rows.Scan(&source.seq, &entry.ID, &entry.ParentID, &entry.Type, &raw,
			&entry.Summary, &entry.CompactedThrough, &entry.Key, &entry.Value); err != nil {
			return nil, fmt.Errorf("scan projection source: %w", err)
		}
		entryBytes := len(raw) + len(entry.Summary) + len(entry.Value)
		if len(entries) > 0 && decodedBytes+entryBytes > hydrationBackfillByteLimit {
			break
		}
		decodedBytes += entryBytes
		if len(raw) > 0 {
			record, err := decodeHydrationProjectionRecord(raw, *entry)
			if err != nil {
				return nil, fmt.Errorf("decode projection source: %w", err)
			}
			source.record = record
		} else {
			source.record = summarizeHydrationEntry(*entry)
		}
		entries = append(entries, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan projection source: %w", err)
	}
	return entries, nil
}

// BranchHydration implements BranchHydrationStore without transferring old
// message blobs. The derived projection is append-atomic and rebuilt from the
// authoritative entry log during schema migration.
func (s *SQLiteStore) BranchHydration() (BranchHydrationSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BranchHydrationSnapshot{}, errors.New("session: store closed")
	}
	tip := s.tip
	repaired := make(map[string]struct{})
	for {
		snapshot, err := sqliteBranchHydrationSummaries(s.db, tip)
		if err == nil {
			snapshot.ContextUsage = summarizeBranchContextUsage(snapshot.Entries)
			return snapshot, nil
		}
		incomplete, ok := errors.AsType[*hydrationProjectionEntryError](err)
		if !ok {
			return BranchHydrationSnapshot{}, err
		}
		if _, duplicate := repaired[incomplete.entryID]; duplicate {
			return BranchHydrationSnapshot{}, err
		}
		if repairErr := repairHydrationProjectionEntry(s.db, incomplete.entryID); repairErr != nil {
			return BranchHydrationSnapshot{}, repairErr
		}
		repaired[incomplete.entryID] = struct{}{}
	}
}

func repairHydrationProjectionEntry(db *sql.DB, entryID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: begin hydration projection repair: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT seq, id, parent_id, entry_type, message, summary,
			compacted_through, meta_key, meta_value
		FROM entries WHERE id = ?`, entryID)
	if err != nil {
		return fmt.Errorf("session: query hydration projection repair: %w", err)
	}
	entries, err := scanHydrationBackfillEntries(rows)
	if err != nil {
		return fmt.Errorf("session: decode hydration projection repair: %w", err)
	}
	if len(entries) != 1 {
		return fmt.Errorf("session: hydration projection source %q: %w", entryID, ErrNotFound)
	}
	stmt, err := tx.Prepare(insertHydrationProjectionSQL)
	if err != nil {
		return fmt.Errorf("session: prepare hydration projection repair: %w", err)
	}
	if err := insertPreparedHydrationRecord(stmt, entryID, entries[0].record); err != nil {
		_ = stmt.Close()
		return err
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("session: close hydration projection repair: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: commit hydration projection repair: %w", err)
	}
	return nil
}

func sqliteBranchHydrationSummaries(db *sql.DB, tip string) (BranchHydrationSnapshot, error) {
	rows, err := db.Query(`
		WITH RECURSIVE branch(seq, id, parent_id, entry_type, meta_key, meta_value) AS (
			SELECT seq, id, parent_id, entry_type, meta_key, meta_value FROM entries WHERE id = ?
			UNION ALL
			SELECT e.seq, e.id, e.parent_id, e.entry_type, e.meta_key, e.meta_value
			FROM entries e JOIN branch b ON e.id = b.parent_id
		)
		SELECT b.id,
			CASE
				WHEN b.entry_type='meta' AND b.meta_key='agent_turn_v1'
					AND b.meta_value IN ('user', 'goal', 'subagent') THEN 1
				WHEN b.entry_type='meta' AND b.meta_key='agent_step_v1'
					AND b.meta_value='provider' THEN 2
				ELSE 0
			END,
			COALESCE(h.projection_version, 0), h.projection
		FROM branch b
		LEFT JOIN entry_hydration_projection h ON h.entry_id = b.id
		ORDER BY b.seq`, tip)
	if err != nil {
		return BranchHydrationSnapshot{}, fmt.Errorf("session: sqlite hydration summary: %w", err)
	}
	snapshot := BranchHydrationSnapshot{TipID: tip}
	for rows.Next() {
		var id string
		var markerKind, projectionVersion int
		var projection sql.RawBytes
		if err := rows.Scan(&id, &markerKind, &projectionVersion, &projection); err != nil {
			_ = rows.Close()
			return BranchHydrationSnapshot{}, fmt.Errorf("session: sqlite hydration scan: %w", err)
		}
		if projectionVersion != entryHydrationProjectionVersion || len(projection) == 0 {
			_ = rows.Close()
			return BranchHydrationSnapshot{}, &hydrationProjectionEntryError{
				entryID: id, cause: errors.New("missing or version-mismatched projection"),
			}
		}
		record, err := unmarshalHydrationProjection(projection)
		if err != nil {
			_ = rows.Close()
			return BranchHydrationSnapshot{}, &hydrationProjectionEntryError{entryID: id, cause: err}
		}
		record.summary.ID = id
		switch markerKind {
		case 1:
			record.summary.AgentRunMarker = agentRunMarkerTurn
		case 2:
			record.summary.AgentRunMarker = agentRunMarkerStep
		}
		snapshot.Entries = append(snapshot.Entries, record.summary)
		if record.userInput != "" {
			snapshot.UserInputs = append(snapshot.UserInputs, record.userInput)
		}
		if record.latestPlan != "" {
			snapshot.LatestPlan = record.latestPlan
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return BranchHydrationSnapshot{}, fmt.Errorf("session: sqlite hydration scan: %w", err)
	}
	if err := rows.Close(); err != nil {
		return BranchHydrationSnapshot{}, fmt.Errorf("session: sqlite hydration close: %w", err)
	}
	if tip != "" && len(snapshot.Entries) == 0 {
		return BranchHydrationSnapshot{}, ErrNotFound
	}
	stats := agentRunStatsFromSummaries(snapshot.Entries)
	snapshot.TurnCount = stats.Turns
	snapshot.StepCount = stats.Steps
	return snapshot, nil
}

// BranchEntriesByID implements BranchEntryLookup.
func (s *SQLiteStore) BranchEntriesByID(ids []string) ([]Entry, error) {
	if len(ids) > maxBranchEntryLookupIDs {
		return nil, fmt.Errorf("session: branch entry lookup exceeds %d ids", maxBranchEntryLookupIDs)
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("session: store closed")
	}
	args := make([]any, len(unique))
	for i := range unique {
		args[i] = unique[i]
	}
	rows, err := s.db.Query(`
		SELECT id, parent_id, entry_type, message, summary, compacted_through, meta_key, meta_value
		FROM entries WHERE id IN (`+sqlValuePlaceholders(len(unique))+`) ORDER BY seq`, args...)
	if err != nil {
		return nil, fmt.Errorf("session: sqlite hydration lookup: %w", err)
	}
	entries, err := scanBranchEntries(rows)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(unique) {
		return nil, ErrNotFound
	}
	return entries, nil
}
