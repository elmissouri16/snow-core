//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Recovery metadata is an additive manager-only table. No session store, prompt
// body, browser authority or worker instance is persisted here. The primary key
// plus active-project gate bounds retained hints to MaxProjects.
func migrateRecovery(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS recovery_hints (
  project_id TEXT PRIMARY KEY NOT NULL CHECK(length(project_id)=36),
  session_id TEXT NOT NULL CHECK(length(session_id) BETWEEN 1 AND 128),
  state TEXT NOT NULL CHECK(state IN ('bound','admission_unknown','admitted','completed','failed','canceled','rejected')),
  updated_at INTEGER NOT NULL CHECK(updated_at>0)
 );
 CREATE TRIGGER IF NOT EXISTS recovery_active_insert BEFORE INSERT ON recovery_hints
 WHEN NOT EXISTS (SELECT 1 FROM projects WHERE id=NEW.project_id AND removed_at IS NULL)
 BEGIN SELECT RAISE(ABORT, 'inactive project'); END;
 CREATE TRIGGER IF NOT EXISTS recovery_remove AFTER UPDATE OF removed_at ON projects
 WHEN NEW.removed_at IS NOT NULL
 BEGIN DELETE FROM recovery_hints WHERE project_id=NEW.id; END;
 `)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}

// LoadRecovery reads one bounded navigation hint without inspecting or opening a
// session. Missing hints and removed registrations convey no execution authority.
func (r *Registry) LoadRecovery(ctx context.Context, projectID string) (RecoveryHint, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return RecoveryHint{}, false, err
	}
	defer leave()
	if !validProjectID(projectID) {
		return RecoveryHint{}, false, ErrProjectNotFound
	}
	var hint RecoveryHint
	var millis int64
	err = r.db.QueryRowContext(ctx, `SELECT h.session_id,h.state,h.updated_at FROM recovery_hints h JOIN projects p ON p.id=h.project_id WHERE h.project_id=? AND p.removed_at IS NULL`, projectID).Scan(&hint.SessionID, &hint.State, &millis)
	if errors.Is(err, sql.ErrNoRows) {
		return RecoveryHint{}, false, nil
	}
	if err != nil {
		return RecoveryHint{}, false, registryError(ctx)
	}
	hint.UpdatedAt = time.UnixMilli(millis).UTC()
	if !hint.valid() {
		return RecoveryHint{}, false, ErrRegistryStorage
	}
	return hint, true, nil
}

// SaveRecovery replaces the project's single hint while holding the same pinned
// storage checks, exclusive lease and context-aware gate as project operations.
func (r *Registry) SaveRecovery(ctx context.Context, projectID string, hint RecoveryHint) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	if !validProjectID(projectID) {
		return ErrProjectNotFound
	}
	if !hint.valid() {
		return ErrRegistryStorage
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO recovery_hints(project_id,session_id,state,updated_at)
 SELECT id,?,?,? FROM projects WHERE id=? AND removed_at IS NULL
 ON CONFLICT(project_id) DO UPDATE SET session_id=excluded.session_id,state=excluded.state,updated_at=excluded.updated_at`, hint.SessionID, string(hint.State), hint.UpdatedAt.UnixMilli(), projectID)
	if err != nil {
		return registryError(ctx)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return registryError(ctx)
	}
	if count != 1 {
		return ErrProjectNotFound
	}
	return nil
}
