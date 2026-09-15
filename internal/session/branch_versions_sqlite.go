package session

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Oversized database strings are rejected before they cross the SQL boundary.
// Identities are never truncated into a potentially different selection.
func versionSQLText(column string) string {
	return `CASE WHEN length(CAST(` + column + ` AS BLOB))<=256 THEN ` + column + ` ELSE NULL END`
}
func versionColumns() string {
	columns := []string{"branch_id", "branch_name", "parent_branch_id", "forked_from_id", "tip_id"}
	for i := range columns {
		columns[i] = versionSQLText(columns[i])
	}
	return strings.Join(columns, ",") + ",created_at,updated_at,active"
}
func scanVersion(row interface{ Scan(...any) error }) (protocol.RPCBranchVersion, error) {
	var b protocol.SessionBranch
	if err := row.Scan(&b.ID, &b.Name, &b.ParentID, &b.ForkedFromID, &b.TipID, &b.CreatedAt, &b.UpdatedAt, &b.Active); err != nil {
		return protocol.RPCBranchVersion{}, err
	}
	return boundedVersion(b)
}
func probeVersionTx(ctx context.Context, tx *sql.Tx) (BranchVersionIdentity, error) {
	var result BranchVersionIdentity
	rows, err := tx.QueryContext(ctx, `SELECT `+versionSQLText("session_id")+`,`+versionSQLText("branch_id")+`,`+versionSQLText("tip_id")+` FROM session_meta,session_branches WHERE singleton=1 AND active=1 AND tip_id=branch_tip LIMIT 2`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	if !rows.Next() {
		return result, errors.Join(ErrBranchVersionStale, rows.Err())
	}
	if err := rows.Scan(&result.SessionID, &result.BranchID, &result.TipID); err != nil {
		return result, err
	}
	if rows.Next() {
		return result, ErrBranchVersionStale
	}
	if !ValidVersionIdentity(result.SessionID, false) || !ValidVersionIdentity(result.BranchID, false) || !ValidVersionIdentity(result.TipID, true) {
		return result, ErrBranchVersionStale
	}
	return result, rows.Err()
}
func versionGoalTx(ctx context.Context, tx *sql.Tx, source, target string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM thread_goals WHERE branch_id IN (?,?) AND status NOT IN (?,?)`, source, target, protocol.GoalComplete, protocol.GoalBudgetLimited).Scan(&count)
	return count != 0, err
}
func (s *SQLiteStore) BranchVersions(ctx context.Context, after string, limit int) (protocol.RPCBranchesPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result protocol.RPCBranchesPage
	if s.closed || limit < 1 || limit > protocol.RPCBranchesPageMaxItems {
		return result, errors.New("session: bounded branch listing unavailable")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	identity, err := probeVersionTx(ctx, tx)
	if err != nil {
		return result, err
	}
	result = protocol.RPCBranchesPage{SessionID: identity.SessionID, ActiveBranchID: identity.BranchID, ActiveTipID: identity.TipID, Branches: []protocol.RPCBranchVersion{}}
	rows, err := tx.QueryContext(ctx, `SELECT `+versionColumns()+` FROM session_branches WHERE branch_id>? ORDER BY branch_id LIMIT ?`, after, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		branch, err := scanVersion(rows)
		if err != nil {
			return result, err
		}
		result.Branches = append(result.Branches, branch)
	}
	if len(result.Branches) > limit {
		result.Branches = result.Branches[:limit]
		result.NextCursor = result.Branches[limit-1].ID
	}
	return result, rows.Err()
}
func (s *SQLiteStore) BranchVersion(ctx context.Context, branchID string) (BranchVersionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result BranchVersionSnapshot
	if s.closed {
		return result, errors.New("session: store closed")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result.Active, err = probeVersionTx(ctx, tx)
	if err != nil {
		return result, err
	}
	result.Branch, err = scanVersion(tx.QueryRowContext(ctx, `SELECT `+versionColumns()+` FROM session_branches WHERE branch_id=?`, branchID))
	if err != nil {
		return result, err
	}
	result.Mode, err = versionModeTx(ctx, tx, branchID)
	if err != nil {
		return result, err
	}
	result.NonterminalGoal, err = versionGoalTx(ctx, tx, result.Active.BranchID, branchID)
	if err != nil {
		return result, err
	}
	result.Entries, err = boundedVersionEntriesTx(ctx, tx, result.Branch.TipID)
	return result, err
}
func (s *SQLiteStore) RestoreBranchVersion(ctx context.Context, p protocol.RPCBranchRestorePrepareParams, expectedMode protocol.CollaborationMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	if err := ValidateBranchRestoreBinding(p); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the writer reservation before inspecting either identity. The no-op
	// update has no durable semantic effect when the compare fails.
	if _, err = tx.ExecContext(ctx, `UPDATE session_meta SET branch_tip=branch_tip WHERE singleton=1`); err != nil {
		return err
	}
	current, err := probeVersionTx(ctx, tx)
	if err != nil {
		return err
	}
	if current != (BranchVersionIdentity{p.SessionID, p.SourceBranchID, p.SourceTipID}) {
		return ErrBranchVersionStale
	}
	target, err := scanVersion(tx.QueryRowContext(ctx, `SELECT `+versionColumns()+` FROM session_branches WHERE branch_id=?`, p.TargetBranchID))
	if err != nil {
		return err
	}
	if target.TipID != p.TargetTipID {
		return ErrBranchVersionStale
	}
	mode, err := versionModeTx(ctx, tx, p.TargetBranchID)
	if err != nil {
		return err
	}
	if mode != expectedMode {
		return ErrBranchVersionStale
	}
	goal, err := versionGoalTx(ctx, tx, p.SourceBranchID, p.TargetBranchID)
	if err != nil {
		return err
	}
	if goal {
		return errors.New("session: branch restore rejects nonterminal goals")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_branches SET active=0 WHERE branch_id=?`, p.SourceBranchID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_branches SET active=1 WHERE branch_id=?`, p.TargetBranchID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_meta SET branch_tip=? WHERE singleton=1`, p.TargetTipID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.branchID = p.TargetBranchID
	s.tip = p.TargetTipID
	s.invalidateContextCacheLocked()
	return nil
}
func (s *SQLiteStore) ProbeBranchVersion(ctx context.Context) (BranchVersionIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return BranchVersionIdentity{}, errors.New("session: store closed")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BranchVersionIdentity{}, err
	}
	defer tx.Rollback()
	return probeVersionTx(ctx, tx)
}

func (s *SQLiteStore) AdoptBranchVersion(ctx context.Context, expected BranchVersionIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("session: store closed")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	actual, err := probeVersionTx(ctx, tx)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrBranchVersionStale
	}
	s.branchID = actual.BranchID
	s.tip = actual.TipID
	s.invalidateContextCacheLocked()
	return nil
}

func versionModeTx(ctx context.Context, tx *sql.Tx, branchID string) (protocol.CollaborationMode, error) {
	var text string
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(collaboration_mode AS BLOB))<=32 THEN collaboration_mode ELSE NULL END FROM thread_state WHERE branch_id=?`, branchID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.ModeDefault, nil
	}
	if err != nil {
		return "", err
	}
	return protocol.ParseCollaborationMode(text)
}
