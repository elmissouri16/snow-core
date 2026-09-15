//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
)

// Consent is manager-only and absent by default, including on migration. Exact
// registration identity is stored explicitly, independently of mutable labels.
// Triggers revoke consent in the same statement that archives/deletes/rebinds a
// registration, and prevent orphan, inactive, or more than MaxProjects entries.
func migrateProjectTrust(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS project_trust (
  project_id TEXT PRIMARY KEY NOT NULL CHECK(length(project_id)=36),
  path TEXT NOT NULL CHECK(length(path) BETWEEN 1 AND 4096),
  device TEXT NOT NULL CHECK(length(device)>0),
  inode TEXT NOT NULL CHECK(length(inode)>0)
 );
 CREATE TRIGGER IF NOT EXISTS project_trust_active_insert BEFORE INSERT ON project_trust
 WHEN NOT EXISTS (SELECT 1 FROM projects WHERE id=NEW.project_id AND removed_at IS NULL
  AND path=NEW.path AND device=NEW.device AND inode=NEW.inode)
  OR (NOT EXISTS (SELECT 1 FROM project_trust WHERE project_id=NEW.project_id)
      AND (SELECT count(*) FROM project_trust)>=100)
 BEGIN SELECT RAISE(ABORT, 'invalid project trust'); END;
 CREATE TRIGGER IF NOT EXISTS project_trust_active_update BEFORE UPDATE ON project_trust
 WHEN NOT EXISTS (SELECT 1 FROM projects WHERE id=NEW.project_id AND removed_at IS NULL
  AND path=NEW.path AND device=NEW.device AND inode=NEW.inode)
 BEGIN SELECT RAISE(ABORT, 'invalid project trust'); END;
 CREATE TRIGGER IF NOT EXISTS project_trust_remove AFTER UPDATE ON projects
 WHEN NEW.removed_at IS NOT NULL OR OLD.id!=NEW.id OR OLD.path!=NEW.path
  OR OLD.device!=NEW.device OR OLD.inode!=NEW.inode
 BEGIN DELETE FROM project_trust WHERE project_id IN (OLD.id,NEW.id); END;
 CREATE TRIGGER IF NOT EXISTS project_trust_delete AFTER DELETE ON projects
 BEGIN DELETE FROM project_trust WHERE project_id=OLD.id; END;
 `)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}

// loadProjectTrust requires the registry gate. Validate the entire bounded table
// rather than treating malformed/orphan consent as an absent or positive flag.
// Filesystem identity is checked separately immediately before presenting trust.
func (r *Registry) loadProjectTrust(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT t.project_id,t.path,t.device,t.inode,
 p.id,p.path,p.device,p.inode,p.removed_at FROM project_trust t
 LEFT JOIN projects p ON p.id=t.project_id LIMIT ?`, MaxProjects+1)
	if err != nil {
		return nil, registryError(ctx)
	}
	defer rows.Close()
	trusted := make(map[string]bool)
	for rows.Next() {
		var id, path, device, inode string
		var registeredID, registeredPath, registeredDevice, registeredInode sql.NullString
		var removed sql.NullInt64
		if err := rows.Scan(&id, &path, &device, &inode, &registeredID, &registeredPath, &registeredDevice, &registeredInode, &removed); err != nil {
			return nil, registryError(ctx)
		}
		if len(trusted) == MaxProjects || !validProjectID(id) || trusted[id] || removed.Valid ||
			!registeredID.Valid || !registeredPath.Valid || !registeredDevice.Valid || !registeredInode.Valid ||
			id != registeredID.String || path != registeredPath.String || device != registeredDevice.String || inode != registeredInode.String ||
			!validStoredProject(Project{ID: id, Name: "trust", Path: path, device: device, inode: inode}) {
			return nil, ErrRegistryStorage
		}
		trusted[id] = true
	}
	if rows.Err() != nil {
		return nil, registryError(ctx)
	}
	return trusted, nil
}

// RememberProjectTrust records explicit manager activation consent for the exact
// active registration returned by Lookup/List/Add. It does not start a worker,
// grant tool permissions, or change CLI extension trust. Callers must still do a
// fresh Lookup and require Available before each explicit activation.
func (r *Registry) RememberProjectTrust(ctx context.Context, expected Project) error {
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
	if !current.Available || current.ID != expected.ID || current.Path != expected.Path || current.device != expected.device || current.inode != expected.inode {
		return ErrProjectInvalid
	}
	if _, err := r.loadProjectTrust(ctx); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return registryError(ctx)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO project_trust(project_id,path,device,inode)
 SELECT id,path,device,inode FROM projects WHERE id=? AND removed_at IS NULL AND path=? AND device=? AND inode=?
 ON CONFLICT(project_id) DO UPDATE SET path=excluded.path,device=excluded.device,inode=excluded.inode`, current.ID, current.Path, current.device, current.inode)
	if err := organizationResult(ctx, result, err); err != nil {
		return err
	}
	current.checkIdentity()
	if !current.Available {
		return ErrProjectInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return registryError(ctx)
	}
	return nil
}

// RevokeProjectTrust clears only remembered manager activation consent. It is
// idempotent for active registered IDs, including missing or replaced folders.
// Revocation requires neither filesystem availability nor valid prior consent.
func (r *Registry) RevokeProjectTrust(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	if !validProjectID(id) {
		return ErrProjectNotFound
	}
	var active bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=? AND removed_at IS NULL)`, id).Scan(&active); err != nil {
		return registryError(ctx)
	}
	if !active {
		return ErrProjectNotFound
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM project_trust WHERE project_id=?`, id); err != nil {
		return registryError(ctx)
	}
	return nil
}
