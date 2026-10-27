//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const projectsDirKey = "projects_dir"

// Manager-wide preferences are a small key/value table beside the registry.
// Nothing here grants activation authority; values are plain host paths.
func migrateManagerSettings(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS manager_settings (
  key TEXT PRIMARY KEY NOT NULL CHECK(length(key) BETWEEN 1 AND 64),
  value TEXT NOT NULL CHECK(length(value) <= 4096),
  updated_at INTEGER NOT NULL CHECK(updated_at>0)
 );`)
	if err != nil {
		return registryError(ctx)
	}
	return nil
}

// ProjectsDir returns the configured folder new workspaces are created under,
// or "" when the host default applies.
func (r *Registry) ProjectsDir(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return "", err
	}
	defer leave()
	var value string
	err = r.db.QueryRowContext(ctx, `SELECT value FROM manager_settings WHERE key=?`, projectsDirKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", registryError(ctx)
	}
	if !validProjectsDir(value) {
		return "", nil
	}
	return value, nil
}

// SetProjectsDir stores an absolute host folder, or clears it with "".
func (r *Registry) SetProjectsDir(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	path = strings.TrimSpace(path)
	if path == "" {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM manager_settings WHERE key=?`, projectsDirKey); err != nil {
			return registryError(ctx)
		}
		return nil
	}
	path = filepath.Clean(path)
	if !validProjectsDir(path) {
		return ErrProjectInvalid
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO manager_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		projectsDirKey, path, time.Now().UnixMilli())
	if err != nil {
		return registryError(ctx)
	}
	return nil
}

func validProjectsDir(path string) bool {
	return path != "" && len(path) <= 4096 && utf8.ValidString(path) && !strings.ContainsRune(path, 0) && filepath.IsAbs(path) && filepath.Clean(path) == path
}

// workspaceName accepts a single folder name for creation under the projects
// folder: no separators, no traversal, no control characters.
func workspaceName(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	return !strings.ContainsAny(value, "/\\\x00") && !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
