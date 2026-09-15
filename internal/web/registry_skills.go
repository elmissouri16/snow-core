//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
)

// The startup preference belongs to the registration, not project trust or the
// worker's active skills. Existing registrations default to disabled.
func migrateProjectSkills(ctx context.Context, tx *sql.Tx) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('projects') WHERE name='skills_enabled')`).Scan(&exists); err != nil {
		return registryError(ctx)
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE projects ADD COLUMN skills_enabled INTEGER NOT NULL DEFAULT 0 CHECK(skills_enabled IN (0,1))`); err != nil {
			return registryError(ctx)
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS project_skills_reset AFTER UPDATE ON projects
 WHEN NEW.removed_at IS NOT NULL OR OLD.id!=NEW.id OR OLD.path!=NEW.path
  OR OLD.device!=NEW.device OR OLD.inode!=NEW.inode
 BEGIN UPDATE projects SET skills_enabled=0 WHERE id=NEW.id AND skills_enabled!=0; END;`)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}

// SetProjectSkills saves the next-start preference for an available, unchanged
// registration. It never starts a worker or changes trust or tool permissions.
func (r *Registry) SetProjectSkills(ctx context.Context, expected Project, enabled bool) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	if !validStoredProject(expected) || !expected.Available || expected.Archived {
		return ErrProjectInvalid
	}
	current, err := r.lookupRegistration(ctx, expected.ID)
	if err != nil {
		return err
	}
	if !current.Available || current.Path != expected.Path || current.device != expected.device || current.inode != expected.inode {
		return ErrProjectInvalid
	}
	if current.SkillsEnabled == enabled {
		return nil
	}
	result, err := r.db.ExecContext(ctx, `UPDATE projects SET skills_enabled=? WHERE id=? AND removed_at IS NULL AND path=? AND device=? AND inode=?`, enabled, current.ID, current.Path, current.device, current.inode)
	return organizationResult(ctx, result, err)
}
