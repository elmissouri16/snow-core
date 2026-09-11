package session

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"uuid"
)

var _ WorkflowStateStore = (*SQLiteStore)(nil)

// WorkflowState reads a consistent snapshot without materializing conversational
// messages. The metadata journal remains authoritative across reopen and forks.
func (s *SQLiteStore) WorkflowState(ctx context.Context, pluginID string) (WorkflowState, error) {
	if err := ctx.Err(); err != nil {
		return WorkflowState{}, err
	}
	if err := validateWorkflowOwner(pluginID); err != nil {
		return WorkflowState{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return WorkflowState{}, errors.New("session: store closed")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return WorkflowState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	records, err := sqliteWorkflowRecords(ctx, tx, s.tip, 0)
	if err != nil {
		return WorkflowState{}, err
	}
	state, err := projectWorkflow(ctx, pluginID, s.branchID, s.tip, records)
	if err != nil {
		return WorkflowState{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkflowState{}, err
	}
	return state, nil
}

// ApplyWorkflowState commits the entry, hydration projection, and branch/session
// tips in one transaction. The database CAS also protects separate store handles.
func (s *SQLiteStore) ApplyWorkflowState(ctx context.Context, pluginID, expectedBranchID, expectedTipID string, update WorkflowUpdate) (WorkflowState, error) {
	if err := ctx.Err(); err != nil {
		return WorkflowState{}, err
	}
	raw, detached, err := encodeWorkflowUpdate(pluginID, update)
	if err != nil {
		return WorkflowState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return WorkflowState{}, errors.New("session: store closed")
	}
	if s.branchID != expectedBranchID || s.tip != expectedTipID {
		return WorkflowState{}, ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var durableTip string
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT tip_id,active FROM session_branches WHERE branch_id=?`, s.branchID).Scan(&durableTip, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkflowState{}, ErrConflict
		}
		return WorkflowState{}, err
	}
	if !active || durableTip != expectedTipID {
		return WorkflowState{}, ErrConflict
	}
	records, err := sqliteWorkflowRecords(ctx, tx, s.tip, len(raw))
	if err != nil {
		return WorkflowState{}, err
	}
	state, err := projectWorkflow(ctx, pluginID, s.branchID, s.tip, records)
	if err != nil {
		return WorkflowState{}, err
	}
	if err := applyWorkflowUpdate(&state, detached); err != nil {
		return WorkflowState{}, err
	}
	entry := Entry{Type: EntryMeta, ID: uuid.New().String(), ParentID: s.tip, Key: MetaPluginWorkflow, Value: raw}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entries(id,parent_id,entry_type,meta_key,meta_value) VALUES(?,?,?,?,?)`, entry.ID, entry.ParentID, entry.Type, entry.Key, entry.Value); err != nil {
		return WorkflowState{}, err
	}
	if err := insertHydrationProjection(tx, entry); err != nil {
		return WorkflowState{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE session_branches SET tip_id=?,updated_at=? WHERE branch_id=? AND tip_id=? AND active=1`, entry.ID, time.Now().UnixMilli(), s.branchID, expectedTipID)
	if err != nil {
		return WorkflowState{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return WorkflowState{}, err
	}
	if count != 1 {
		return WorkflowState{}, ErrConflict
	}
	result, err = tx.ExecContext(ctx, `UPDATE session_meta SET branch_tip=? WHERE singleton=1 AND EXISTS (SELECT 1 FROM session_branches WHERE branch_id=? AND active=1)`, entry.ID, s.branchID)
	if err != nil {
		return WorkflowState{}, err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return WorkflowState{}, err
	}
	if count != 1 {
		return WorkflowState{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return WorkflowState{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkflowState{}, err
	}
	s.tip = entry.ID
	s.advanceContextCacheLocked(expectedTipID, []Entry{entry})
	state.TipID = entry.ID
	return state, nil
}

func sqliteWorkflowRecords(ctx context.Context, tx *sql.Tx, tip string, addedBytes int) ([]string, error) {
	var count, size int
	// Bound the aggregate scan by one record beyond the journal quota. Byte
	// lengths are measured as BLOBs so UTF-8 cannot undercount persisted input.
	if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(length(CAST(meta_value AS BLOB))),0) FROM (SELECT meta_value FROM entries WHERE entry_type=? AND meta_key=? LIMIT ?)`, EntryMeta, MetaPluginWorkflow, MaxWorkflowHistoryRecords+1).Scan(&count, &size); err != nil {
		return nil, err
	}
	if err := checkWorkflowHistory(count, size, addedBytes); err != nil {
		return nil, err
	}
	// UNION (not UNION ALL) terminates even if a damaged database has a cycle.
	// Valid entries always follow their parents in sequence order.
	const ancestry = `WITH RECURSIVE ancestry(id,parent_id) AS (
 SELECT id,parent_id FROM entries WHERE id=?
 UNION SELECT e.id,e.parent_id FROM entries e JOIN ancestry a ON e.id=a.parent_id
 ) `
	var valid int
	if err := tx.QueryRowContext(ctx, ancestry+`SELECT count(*) FROM ancestry WHERE id='root' AND (parent_id='' OR parent_id IS NULL)`, tip).Scan(&valid); err != nil {
		return nil, err
	}
	if valid != 1 {
		return nil, errors.New("session: malformed workflow ancestry")
	}
	rows, err := tx.QueryContext(ctx, ancestry+`SELECT e.meta_value FROM ancestry a JOIN entries e ON e.id=a.id WHERE e.entry_type=? AND e.meta_key=? ORDER BY e.seq`, tip, EntryMeta, MetaPluginWorkflow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		records = append(records, raw)
	}
	return records, rows.Err()
}
