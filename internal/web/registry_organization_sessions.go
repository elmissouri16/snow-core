//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"errors"
)

// SessionOrganization contains no title, transcript, filesystem path, or runtime
// authority. Durable session IDs are scoped to an exact project registration.
type SessionOrganization struct{ Pinned, Archived bool }
type OrganizedSession struct {
	SessionSummary
	SessionOrganization
}

// DecorateSessions retains the catalog summary (especially its durable ID) and
// original order. Filtering/pinning these copies never changes catalog offsets.
func (r *Registry) DecorateSessions(ctx context.Context, projectID string, page CatalogSessions) ([]OrganizedSession, error) {
	if !validProjectID(projectID) || !validOrganizationCatalog(page) {
		return nil, ErrProjectInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	result := make([]OrganizedSession, 0, len(page.Sessions))
	for _, summary := range page.Sessions {
		entry := OrganizedSession{SessionSummary: summary}
		err := r.db.QueryRowContext(ctx, `SELECT pinned,archived FROM session_organization WHERE project_id=? AND session_id=?`, projectID, summary.ID).Scan(&entry.Pinned, &entry.Archived)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, registryError(ctx)
		}
		result = append(result, entry)
	}
	return result, nil
}

func validOrganizationCatalog(page CatalogSessions) bool {
	if len(page.Sessions) > OrganizationPageSize {
		return false
	}
	seen := make(map[string]bool, len(page.Sessions))
	for _, entry := range page.Sessions {
		if entry.ID == "" || !runtimeIdentifier(entry.ID) || seen[entry.ID] {
			return false
		}
		seen[entry.ID] = true
	}
	return true
}

// saveSessionOrganization is called only after a fresh bounded catalog page has
// proved membership. Keeping that page in the contract prevents arbitrary IDs
// from consuming manager storage, without copying any session content.
func (r *Registry) saveSessionOrganization(ctx context.Context, projectID, sessionID, action string, page CatalogSessions) error {
	if !validProjectID(projectID) || !validOrganizationCatalog(page) {
		return ErrProjectInvalid
	}
	found := false
	for _, entry := range page.Sessions {
		if entry.ID == sessionID {
			found = true
		}
	}
	if !found {
		return ErrProjectInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	var active int
	if r.db.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=? AND removed_at IS NULL`, projectID).Scan(&active) != nil {
		return registryError(ctx)
	}
	if active != 1 {
		return ErrProjectNotFound
	}
	var state SessionOrganization
	err = r.db.QueryRowContext(ctx, `SELECT pinned,archived FROM session_organization WHERE project_id=? AND session_id=?`, projectID, sessionID).Scan(&state.Pinned, &state.Archived)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return registryError(ctx)
	}
	switch action {
	case "pin":
		state.Pinned = true
	case "unpin":
		state.Pinned = false
	case "archive":
		state.Archived = true
	case "restore":
		state.Archived = false
	default:
		return ErrProjectInvalid
	}
	if !state.Pinned && !state.Archived {
		_, err = r.db.ExecContext(ctx, `DELETE FROM session_organization WHERE project_id=? AND session_id=?`, projectID, sessionID)
		if err != nil {
			return registryError(ctx)
		}
		return nil
	}
	if !exists {
		var total, project int
		if r.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(project_id=?),0) FROM session_organization`, projectID).Scan(&total, &project) != nil {
			return registryError(ctx)
		}
		if total >= MaxSessionOrganization || project >= MaxProjectSessionOrganization {
			return ErrOrganizationLimit
		}
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO session_organization(project_id,session_id,pinned,archived) VALUES(?,?,?,?)
 ON CONFLICT(project_id,session_id) DO UPDATE SET pinned=excluded.pinned,archived=excluded.archived`, projectID, sessionID, state.Pinned, state.Archived)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}
