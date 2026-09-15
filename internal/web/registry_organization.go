//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	OrganizationPageSize          = 25
	MaxOrganizationOffset         = 10_000
	MaxSessionOrganization        = 10_000
	MaxProjectSessionOrganization = 1_000
)

var ErrOrganizationLimit = errors.New("web: manager organization metadata limit reached")

// Additive schema leaves the registration identity and active unique indexes
// untouched. The UPDATE trigger also bounds explicit archived-ID restoration.
func migrateOrganization(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS project_organization (
  project_id TEXT PRIMARY KEY NOT NULL,
  pinned INTEGER NOT NULL CHECK(pinned IN (0,1))
 );
 CREATE TABLE IF NOT EXISTS session_organization (
  project_id TEXT NOT NULL,
  session_id TEXT NOT NULL CHECK(length(session_id) BETWEEN 1 AND 128),
  pinned INTEGER NOT NULL CHECK(pinned IN (0,1)),
  archived INTEGER NOT NULL CHECK(archived IN (0,1)),
  PRIMARY KEY(project_id,session_id)
 );
 CREATE TRIGGER IF NOT EXISTS projects_restore_limit BEFORE UPDATE OF removed_at ON projects
 WHEN OLD.removed_at IS NOT NULL AND NEW.removed_at IS NULL AND
  (SELECT count(*) FROM projects WHERE removed_at IS NULL) >= 100
 BEGIN SELECT RAISE(ABORT, 'project limit'); END;
 CREATE TRIGGER IF NOT EXISTS organization_session_limit BEFORE INSERT ON session_organization
 WHEN NOT EXISTS (SELECT 1 FROM session_organization WHERE project_id=NEW.project_id AND session_id=NEW.session_id) AND
  ((SELECT count(*) FROM session_organization) >= 10000 OR
   (SELECT count(*) FROM session_organization WHERE project_id=NEW.project_id) >= 1000)
 BEGIN SELECT RAISE(ABORT, 'organization limit'); END;
 `)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}

// RenameProject changes only the manager label, including for archived entries.
func (r *Registry) RenameProject(ctx context.Context, id, name string) error {
	if len(name) > 128 {
		return ErrProjectInvalid
	}
	name = strings.TrimSpace(name)
	if !validProjectID(id) || !validProjectName(name) {
		return ErrProjectInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	result, err := r.db.ExecContext(ctx, `UPDATE projects SET name=?,updated_at=? WHERE id=?`, name, time.Now().UnixMilli(), id)
	return organizationResult(ctx, result, err)
}

func organizationResult(ctx context.Context, result sql.Result, err error) error {
	if err != nil {
		return registryError(ctx)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return registryError(ctx)
	}
	if n != 1 {
		return ErrProjectNotFound
	}
	return nil
}

func (r *Registry) PinProject(ctx context.Context, id string, pinned bool) error {
	if !validProjectID(id) {
		return ErrProjectNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	result, err := r.db.ExecContext(ctx, `INSERT INTO project_organization(project_id,pinned)
 SELECT id,? FROM projects WHERE id=? AND removed_at IS NULL
 ON CONFLICT(project_id) DO UPDATE SET pinned=excluded.pinned`, pinned, id)
	return organizationResult(ctx, result, err)
}

// RestoreProject never replaces identity or allocates a new ID. Both canonical
// path and device/inode must still match; an active alias or full registry fails.
func (r *Registry) RestoreProject(ctx context.Context, id string) (Project, error) {
	if !validProjectID(id) {
		return Project{}, ErrProjectNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return Project{}, err
	}
	defer leave()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, registryError(ctx)
	}
	defer tx.Rollback()
	var p Project
	err = tx.QueryRowContext(ctx, `SELECT id,name,path,device,inode FROM projects WHERE id=? AND removed_at IS NOT NULL`, id).Scan(&p.ID, &p.Name, &p.Path, &p.device, &p.inode)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, registryError(ctx)
	}
	if !validStoredProject(p) {
		return Project{}, ErrRegistryStorage
	}
	p.checkIdentity()
	if !p.Available {
		return Project{}, ErrProjectInvalid
	}
	var count int
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE removed_at IS NULL AND (path=? OR (device=? AND inode=?))`, p.Path, p.device, p.inode).Scan(&count) != nil {
		return Project{}, registryError(ctx)
	}
	if count != 0 {
		return Project{}, ErrProjectDuplicate
	}
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE removed_at IS NULL`).Scan(&count) != nil {
		return Project{}, registryError(ctx)
	}
	if count >= MaxProjects {
		return Project{}, ErrProjectLimit
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET removed_at=NULL,updated_at=? WHERE id=?`, time.Now().UnixMilli(), id); err != nil {
		return Project{}, registryError(ctx)
	}
	p.checkIdentity()
	if !p.Available {
		return Project{}, ErrProjectInvalid
	}
	if err := tx.Commit(); err != nil {
		return Project{}, registryError(ctx)
	}
	return p, nil
}

// ArchivedProjects is a bounded manager inventory, not a filesystem walk. Old
// remove actions are archives too; entries stay archived until explicit restore.
type ArchivedProjects struct {
	Projects   []Project
	HasMore    bool
	NextOffset int
}

func (r *Registry) ListArchivedProjects(ctx context.Context, offset int) (ArchivedProjects, error) {
	if offset < 0 || offset > MaxOrganizationOffset {
		return ArchivedProjects{}, ErrProjectInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return ArchivedProjects{}, err
	}
	defer leave()
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.name,p.path,p.device,p.inode,COALESCE(o.pinned,0)
 FROM projects p LEFT JOIN project_organization o ON o.project_id=p.id
 WHERE p.removed_at IS NOT NULL ORDER BY p.removed_at DESC,p.id LIMIT ? OFFSET ?`, OrganizationPageSize+1, offset)
	if err != nil {
		return ArchivedProjects{}, registryError(ctx)
	}
	defer rows.Close()
	var page ArchivedProjects
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.device, &p.inode, &p.Pinned); err != nil {
			return ArchivedProjects{}, registryError(ctx)
		}
		if !validStoredProject(p) {
			return ArchivedProjects{}, ErrRegistryStorage
		}
		if len(page.Projects) == OrganizationPageSize {
			page.HasMore = offset+OrganizationPageSize <= MaxOrganizationOffset
			break
		}
		p.Archived = true
		p.checkIdentity()
		page.Projects = append(page.Projects, p)
	}
	if rows.Err() != nil {
		return ArchivedProjects{}, registryError(ctx)
	}
	page.NextOffset = offset + OrganizationPageSize
	return page, nil
}

// DecorateProjects copies presentation metadata without reconstituting Project:
// unexported canonical device/inode identity survives this decoration unchanged.
func (r *Registry) DecorateProjects(ctx context.Context, projects []Project) ([]Project, error) {
	if len(projects) > MaxProjects {
		return nil, ErrProjectLimit
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	result := make([]Project, 0, len(projects))
	for _, p := range projects {
		if !validStoredProject(p) {
			return nil, ErrProjectInvalid
		}
		err := r.db.QueryRowContext(ctx, `SELECT COALESCE(o.pinned,0),p.removed_at IS NOT NULL FROM projects p LEFT JOIN project_organization o ON p.id=o.project_id WHERE p.id=?`, p.ID).Scan(&p.Pinned, &p.Archived)
		if err != nil {
			return nil, registryError(ctx)
		}
		result = append(result, p)
	}
	return result, nil
}
