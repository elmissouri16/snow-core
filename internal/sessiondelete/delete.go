// Package sessiondelete shares inactive-session deletion and managed cleanup
// between the application and the runtime-free control worker. It creates no
// runtime, configuration, session, provider, or extension objects.
package sessiondelete

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/session"
)

// CleanupError means the database deletion committed, but managed cleanup failed.
// Other errors may also leave an uncertain outcome (for example failed rollback);
// callers must not promise that every non-CleanupError leaves files unchanged.
type CleanupError struct{ Err error }

func (e *CleanupError) Error() string { return "session deleted; cleanup warning: " + e.Err.Error() }
func (e *CleanupError) Unwrap() error { return e.Err }

// Delete uses the existing indexed identity checks, root pins, exclusive database
// leases and quarantine rollback. The caller owns any live-runtime admission gate.
func Delete(ctx context.Context, index *session.FileIndex, cwd, path, id, home string, artifacts artifact.SessionDeleter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ids, err := index.DeleteWithIDs(cwd, path, id)
	if err != nil && len(ids) == 0 {
		return err
	}
	errs := []error{err}
	// Once committed, finish cleanup even if the initiating browser disconnected.
	for _, owned := range ids {
		if artifacts != nil {
			errs = append(errs, artifacts.DeleteSession(context.WithoutCancel(ctx), owned))
		}
		errs = append(errs, goal.DeleteSessionData(home, owned))
	}
	if err := errors.Join(errs...); err != nil {
		return &CleanupError{Err: err}
	}
	return nil
}

// ByID resolves only exact indexed membership; browser paths never reach Delete.
func ByID(ctx context.Context, root, cwd, id, home string, artifacts artifact.SessionDeleter) error {
	if !ValidID(id) {
		return session.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	index := session.NewFileIndex(root)
	infos, err := index.List(cwd)
	if err != nil {
		return err
	}
	for _, info := range infos {
		if info.ID == id {
			return Delete(ctx, index, cwd, info.Path, id, home, artifacts)
		}
	}
	return session.ErrNotFound
}

func ValidID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
