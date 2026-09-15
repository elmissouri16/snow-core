//go:build darwin || linux

package web

import (
	"context"
	"encoding/json/v2"
	"path/filepath"
	"time"
	"uuid"
)

func operationPublicSize(op ProjectOperation) int {
	data, err := json.Marshal(op)
	if err != nil {
		return 65536
	}
	return len(data)
}

// registerOperation inserts metadata and completes the operation in one SQLite
// transaction. Known ownership is pinned to the recorded child, never Add(path).
// Explicit review of unknown ownership instead performs ordinary registration
// and preserves the non-success outcome; it cannot fabricate a created success.
func (r *Registry) registerOperation(ctx context.Context, id string, revision int64, review bool) (ProjectOperation, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return ProjectOperation{}, err
	}
	defer leave()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectOperation{}, registryError(ctx)
	}
	defer tx.Rollback()
	op, err := scanOperation(tx.QueryRowContext(ctx, `SELECT id,revision,state,record FROM manager_operations WHERE id=?`, id))
	if err != nil {
		return op, err
	}
	if op.Revision != revision || operationActive(op.State) || op.ProjectID != "" {
		return op, ErrOperationConflict
	}
	known := op.State == "awaiting_registration" && op.Outcome == "observed" && validDirectoryIdentity(op.Child)
	var child DirectoryIdentity
	if known {
		child = op.Child
		if !matchesDirectoryIdentity(op.Parent) || !matchesDirectoryIdentity(child) {
			return op, ErrOperationConflict
		}
	} else {
		if !review {
			return op, ErrOperationConflict
		}
		child, err = openDirectoryIdentity(filepath.Join(op.Parent.Path, op.Name))
		if err != nil {
			return op, err
		}
	}
	var duplicate, count int
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE removed_at IS NULL AND (path=? OR (device=? AND inode=?))`, child.Path, child.Device, child.Inode).Scan(&duplicate) != nil {
		return op, registryError(ctx)
	}
	if duplicate != 0 {
		return op, ErrProjectDuplicate
	}
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE removed_at IS NULL`).Scan(&count) != nil {
		return op, registryError(ctx)
	}
	if count >= MaxProjects {
		return op, ErrProjectLimit
	}
	projectID := uuid.New().String()
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,path,device,inode,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, projectID, op.Name, child.Path, child.Device, child.Inode, now, now); err != nil {
		return op, registryError(ctx)
	}
	previous := op.Revision
	op.Revision++
	op.UpdatedAt = max(op.UpdatedAt, now)
	op.ProjectID = projectID
	if known {
		op.State = "succeeded"
		op.Error = ""
	} else {
		op.State = "needs_review"
		op.Error = "review_required"
	}
	if err := updateOperationTx(ctx, tx, previous, op); err != nil {
		return op, err
	}
	if !matchesDirectoryIdentity(child) || (known && !matchesDirectoryIdentity(op.Parent)) {
		return op, ErrOperationConflict
	}
	if tx.Commit() != nil {
		return op, registryError(ctx)
	}
	return op, nil
}
