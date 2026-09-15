package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// The reservation precedes ALL authoritative reads, including mode, goals, old
// label, bounded source entries and tool boundaries. A separate open handle
// cannot interleave a write between any predicate and the mutation/capture.
func (s *SQLiteStore) historyControlTx(ctx context.Context, p protocol.RPCHistoryControlBinding, history bool) (*sql.Tx, BranchVersionSnapshot, error) {
	var result BranchVersionSnapshot
	if s.closed {
		return nil, result, errors.New("session: store closed")
	}
	if err := validateHistoryBinding(p); err != nil {
		return nil, result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, result, err
	}
	fail := func(err error) (*sql.Tx, BranchVersionSnapshot, error) { _ = tx.Rollback(); return nil, result, err }
	if _, err = tx.ExecContext(ctx, `UPDATE session_meta SET branch_tip=branch_tip WHERE singleton=1`); err != nil {
		return fail(err)
	}
	result.Active, err = probeVersionTx(ctx, tx)
	if err != nil {
		return fail(err)
	}
	if result.Active != (BranchVersionIdentity{p.SessionID, p.SourceBranchID, p.SourceTipID}) {
		return fail(ErrBranchVersionStale)
	}
	result.Branch, err = scanVersion(tx.QueryRowContext(ctx, `SELECT `+versionColumns()+` FROM session_branches WHERE branch_id=?`, p.TargetBranchID))
	if err != nil {
		return fail(err)
	}
	if result.Branch.TipID != p.TargetTipID {
		return fail(ErrBranchVersionStale)
	}
	result.NonterminalGoal, err = versionGoalTx(ctx, tx, p.SourceBranchID, p.TargetBranchID)
	if err != nil {
		return fail(err)
	}
	if result.NonterminalGoal {
		return fail(errors.New("session: history control rejects nonterminal goals"))
	}
	result.Mode, err = versionModeTx(ctx, tx, p.TargetBranchID)
	if err != nil {
		return fail(err)
	}
	if history {
		result.Entries, err = boundedVersionEntriesTx(ctx, tx, p.TargetTipID)
		if err != nil {
			return fail(err)
		}
		if err = ValidateForkBoundary(result.Entries); err != nil {
			return fail(err)
		}
	}
	return tx, result, nil
}

func uniqueHistoryNameTx(ctx context.Context, tx *sql.Tx, name, except string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_branches WHERE branch_name=? COLLATE NOCASE AND branch_id!=?`, name, except).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("session: branch name already exists")
	}
	return nil
}

// commitHistoryControl clears any transaction the driver retains after a
// rejected COMMIT (for example a deferred constraint failure). SQLiteStore owns
// one connection and the caller holds s.mu, so this rollback cannot affect a
// different operation. An actually committed transaction is not undone. Without
// this cleanup a same-handle probe could mistake uncommitted rows for a durable
// outcome. Commit errors remain unknown even when cleanup succeeds.
func (s *SQLiteStore) commitHistoryControl(ctx context.Context, tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_, _ = s.db.ExecContext(cleanupCtx, `ROLLBACK`)
		return errors.Join(ErrHistoryControlUnknown, err)
	}
	return nil
}

func (s *SQLiteStore) ForkHistoryBranch(ctx context.Context, p protocol.RPCManagedBranchForkParams, expectedMode protocol.CollaborationMode) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty protocol.SessionBranch
	if err := validateHistoryName(p.Name, 64); err != nil {
		return empty, err
	}
	tx, snapshot, err := s.historyControlTx(ctx, p.RPCHistoryControlBinding, true)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if snapshot.Mode != expectedMode {
		return empty, ErrBranchVersionStale
	}
	if err := uniqueHistoryNameTx(ctx, tx, p.Name, ""); err != nil {
		return empty, err
	}
	now := time.Now().UnixMilli()
	branch := protocol.SessionBranch{ID: "branch-" + randomSuffix(), Name: p.Name, ParentID: p.TargetBranchID, ForkedFromID: p.TargetTipID, TipID: p.TargetTipID, CreatedAt: now, UpdatedAt: now, Active: true}
	writes := []struct {
		sql  string
		args []any
	}{
		{`UPDATE session_branches SET active=0`, nil},
		{`INSERT INTO session_branches(branch_id,branch_name,parent_branch_id,forked_from_id,tip_id,created_at,updated_at,active) VALUES(?,?,?,?,?,?,?,1)`, []any{branch.ID, p.Name, p.TargetBranchID, p.TargetTipID, p.TargetTipID, now, now}},
		{`INSERT INTO thread_state(branch_id,collaboration_mode) VALUES(?,?)`, []any{branch.ID, snapshot.Mode}},
		{`INSERT INTO thread_goals(branch_id,goal_id,objective,status,blocked_reason,token_budget,tokens_used,seconds_used,created_at,updated_at) SELECT ?,?,objective,status,blocked_reason,token_budget,tokens_used,seconds_used,created_at,? FROM thread_goals WHERE branch_id=?`, []any{branch.ID, newID(), now, p.TargetBranchID}},
		{`INSERT INTO thread_goal_costs(branch_id,currency,input_cost,output_cost,cache_read_cost,cache_write_cost,total_cost) SELECT ?,currency,input_cost,output_cost,cache_read_cost,cache_write_cost,total_cost FROM thread_goal_costs WHERE branch_id=?`, []any{branch.ID, p.TargetBranchID}},
		{`INSERT INTO thread_goal_deferrals(branch_id,deferred) SELECT ?,deferred FROM thread_goal_deferrals WHERE branch_id=?`, []any{branch.ID, p.TargetBranchID}},
		{`UPDATE session_meta SET branch_tip=? WHERE singleton=1`, []any{p.TargetTipID}},
	}
	for _, write := range writes {
		if _, err := tx.ExecContext(ctx, write.sql, write.args...); err != nil {
			return empty, err
		}
	}
	if err := s.commitHistoryControl(ctx, tx); err != nil {
		return branch, err
	}
	s.branchID, s.tip = branch.ID, p.TargetTipID
	s.invalidateContextCacheLocked()
	branch.Messages, branch.Preview = branchStats(snapshot.Entries)
	return branch, nil
}

func (s *SQLiteStore) RenameHistoryBranch(ctx context.Context, p protocol.RPCManagedBranchRenameParams) (protocol.SessionBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty protocol.SessionBranch
	if err := validateHistoryName(p.Name, 64); err != nil {
		return empty, err
	}
	if err := validateHistoryName(p.OldName, 64); err != nil {
		return empty, err
	}
	tx, snapshot, err := s.historyControlTx(ctx, p.RPCHistoryControlBinding, false)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if snapshot.Branch.Name != p.OldName {
		return empty, ErrBranchVersionStale
	}
	if err := uniqueHistoryNameTx(ctx, tx, p.Name, p.TargetBranchID); err != nil {
		return empty, err
	}
	branch := historySessionBranch(snapshot.Branch)
	branch.Name, branch.UpdatedAt = p.Name, time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE session_branches SET branch_name=?,updated_at=? WHERE branch_id=?`, branch.Name, branch.UpdatedAt, branch.ID); err != nil {
		return empty, err
	}
	if err := s.commitHistoryControl(ctx, tx); err != nil {
		return empty, err
	}
	return branch, nil
}

func (s *SQLiteStore) CaptureHistoryFork(ctx context.Context, p protocol.RPCManagedSessionForkParams, expectedMode protocol.CollaborationMode) (*HistoryForkSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateHistoryName(p.Name, 72); err != nil {
		return nil, err
	}
	tx, snapshot, err := s.historyControlTx(ctx, p.RPCHistoryControlBinding, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if snapshot.Mode != expectedMode {
		return nil, ErrBranchVersionStale
	}
	// The no-op reservation never commits. Everything consumed by publication is
	// already owned by this immutable capture; no source lock survives the call.
	return capturedHistoryFork(p, snapshot), nil
}

var _ HistoryControlStore = (*SQLiteStore)(nil)
