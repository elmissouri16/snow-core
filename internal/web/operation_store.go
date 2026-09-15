//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

func migrateProjectOperations(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS manager_operations (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=36),
 revision INTEGER NOT NULL CHECK(revision>0),
 state TEXT NOT NULL CHECK(state IN ('admitted','creating','running','cancel_requested','awaiting_registration','succeeded','failed','canceled','interrupted','needs_review')),
 record TEXT NOT NULL CHECK(length(record) BETWEEN 1 AND 24576));
 CREATE UNIQUE INDEX IF NOT EXISTS manager_operations_one_active ON manager_operations((1)) WHERE state IN ('admitted','creating','running','cancel_requested');
 CREATE TRIGGER IF NOT EXISTS manager_operations_limit BEFORE INSERT ON manager_operations WHEN (SELECT count(*) FROM manager_operations)>=128 BEGIN SELECT RAISE(ABORT,'operation limit'); END;`)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}
func validOperation(op ProjectOperation) bool {
	if !validProjectID(op.ID) || !validOperationName(op.Name) || !validDirectoryIdentity(op.Parent) || op.Revision < 1 || op.CreatedAt <= 0 || op.UpdatedAt < op.CreatedAt || len(op.Fingerprint) != 64 {
		return false
	}
	for _, r := range op.Fingerprint {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	if op.Child != (DirectoryIdentity{}) && (!validDirectoryIdentity(op.Child) || op.Child.Path != filepath.Join(op.Parent.Path, op.Name)) {
		return false
	}
	if op.ProjectID != "" && !validProjectID(op.ProjectID) {
		return false
	}
	if op.Kind == "clone" {
		if _, err := reviewedOperationRemote(op.Remote); err != nil {
			return false
		}
	} else if op.Kind != "create" || op.Remote != "" {
		return false
	}
	switch op.State {
	case "admitted", "creating", "running", "cancel_requested", "awaiting_registration", "succeeded", "failed", "canceled", "interrupted", "needs_review":
	default:
		return false
	}
	switch op.Outcome {
	case "not_observed", "observed", "unknown":
	default:
		return false
	}
	if (op.State == "succeeded" || op.State == "awaiting_registration" || op.State == "running") && (!validDirectoryIdentity(op.Child) || op.Outcome != "observed") {
		return false
	}
	if op.State == "succeeded" && op.ProjectID == "" {
		return false
	}
	if op.State == "awaiting_registration" && op.ProjectID != "" {
		return false
	}
	switch op.Error {
	case "", "worker_unavailable", "worker_lost", "prepare_failed", "clone_failed", "canceled", "interrupted", "identity_changed", "registration_failed", "review_required", "storage_failed":
	default:
		return false
	}
	return true
}
func decodeOperation(id, state string, revision int64, data string) (ProjectOperation, error) {
	var op ProjectOperation
	if len(data) > 24576 || json.Unmarshal([]byte(data), &op, json.RejectUnknownMembers(true)) != nil || !validOperation(op) || op.ID != id || op.State != state || op.Revision != revision {
		return op, ErrRegistryStorage
	}
	return op, nil
}
func scanOperation(row interface{ Scan(...any) error }) (ProjectOperation, error) {
	var id, state, data string
	var revision int64
	if err := row.Scan(&id, &revision, &state, &data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectOperation{}, ErrOperationNotFound
		}
		return ProjectOperation{}, ErrRegistryStorage
	}
	return decodeOperation(id, state, revision, data)
}
func (r *Registry) operationGet(ctx context.Context, id string) (ProjectOperation, error) {
	if !validProjectID(id) {
		return ProjectOperation{}, ErrOperationNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return ProjectOperation{}, err
	}
	defer leave()
	return scanOperation(r.db.QueryRowContext(ctx, `SELECT id,revision,state,record FROM manager_operations WHERE id=?`, id))
}
func (r *Registry) operationList(ctx context.Context, offset, limit int) ([]ProjectOperation, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	rows, err := r.db.QueryContext(ctx, `SELECT id,revision,state,record FROM manager_operations ORDER BY rowid DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, registryError(ctx)
	}
	defer rows.Close()
	ops := []ProjectOperation{}
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	if rows.Err() != nil {
		return nil, registryError(ctx)
	}
	return ops, nil
}
func (r *Registry) operationInsert(ctx context.Context, op ProjectOperation) error {
	if !validOperation(op) {
		return ErrOperationInvalid
	}
	data, err := json.Marshal(op)
	if err != nil || len(data) > 24576 {
		return ErrOperationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	var count, active int
	if r.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(state IN ('admitted','creating','running','cancel_requested')),0) FROM manager_operations`).Scan(&count, &active) != nil {
		return registryError(ctx)
	}
	if count >= 128 {
		return ErrOperationLimit
	}
	if active != 0 {
		return ErrOperationBusy
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO manager_operations(id,revision,state,record) VALUES(?,?,?,?)`, op.ID, op.Revision, op.State, string(data)); err != nil {
		return registryError(ctx)
	}
	return nil
}
func updateOperationTx(ctx context.Context, tx *sql.Tx, previous int64, op ProjectOperation) error {
	if !validOperation(op) || op.Revision != previous+1 {
		return ErrOperationInvalid
	}
	data, err := json.Marshal(op)
	if err != nil || len(data) > 24576 {
		return ErrOperationInvalid
	}
	result, err := tx.ExecContext(ctx, `UPDATE manager_operations SET revision=?,state=?,record=? WHERE id=? AND revision=?`, op.Revision, op.State, string(data), op.ID, previous)
	if err != nil {
		return registryError(ctx)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return registryError(ctx)
	}
	if n != 1 {
		return ErrOperationConflict
	}
	return nil
}
func (r *Registry) operationUpdate(ctx context.Context, previous int64, op ProjectOperation) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return registryError(ctx)
	}
	defer tx.Rollback()
	if err := updateOperationTx(ctx, tx, previous, op); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return registryError(ctx)
	}
	return nil
}
func (r *Registry) interruptProjectOperations(ctx context.Context) error {
	ops, err := r.operationList(ctx, 0, 128)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if !operationActive(op.State) && op.State != "awaiting_registration" {
			continue
		}
		previous := op.Revision
		op.Revision++
		op.UpdatedAt = max(op.UpdatedAt, time.Now().UnixMilli())
		op.State = "interrupted"
		op.Error = "interrupted"
		op.Outcome = "unknown"
		if err := r.operationUpdate(ctx, previous, op); err != nil {
			return err
		}
	}
	return nil
}
func (r *Registry) operationDismiss(ctx context.Context, id string, revision int64) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	result, err := r.db.ExecContext(ctx, `DELETE FROM manager_operations WHERE id=? AND revision=? AND state NOT IN ('admitted','creating','running','cancel_requested')`, id, revision)
	if err != nil {
		return registryError(ctx)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return registryError(ctx)
	}
	if n != 1 {
		return ErrOperationConflict
	}
	return nil
}
