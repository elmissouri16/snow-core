package app

import (
	"context"
	"errors"

	updatepkg "github.com/elmissouri16/snow-core/internal/update"
)

// UpdateStatus is the bounded release-check result exposed to interactive surfaces.
type UpdateStatus = updatepkg.Status

// UpdateResult describes a successfully installed release.
type UpdateResult = updatepkg.Result

// UpdateProgress is one bounded snapshot from an explicitly approved install.
type UpdateProgress = updatepkg.Progress

// UpdateProgressFunc receives synchronous installation progress snapshots.
type UpdateProgressFunc = updatepkg.ProgressFunc

const (
	UpdateProgressPreparing   = updatepkg.ProgressPreparing
	UpdateProgressDownloading = updatepkg.ProgressDownloading
	UpdateProgressVerifying   = updatepkg.ProgressVerifying
	UpdateProgressInstalling  = updatepkg.ProgressInstalling
)

// UpdateService is the application boundary for checking and installing Snow
// releases. Constructing a service performs no network or filesystem mutation.
type UpdateService interface {
	Check(context.Context) (updatepkg.Status, error)
	Install(context.Context, updatepkg.Status) (updatepkg.Result, error)
	InstallWithProgress(context.Context, updatepkg.Status, updatepkg.ProgressFunc) (updatepkg.Result, error)
	Eligibility() (bool, string)
}

// CheckForUpdate performs one explicit release lookup.
func (a *App) CheckForUpdate(ctx context.Context) (updatepkg.Status, error) {
	if a == nil || a.updater == nil {
		return updatepkg.Status{}, errors.New("app: updater unavailable")
	}
	return a.updater.Check(ctx)
}

// InstallUpdateWithProgress verifies and installs a checked release while
// reporting progress only after an interactive surface approves installation.
func (a *App) InstallUpdateWithProgress(ctx context.Context, status updatepkg.Status, report updatepkg.ProgressFunc) (updatepkg.Result, error) {
	if a == nil || a.updater == nil {
		return updatepkg.Result{}, errors.New("app: updater unavailable")
	}
	return a.updater.InstallWithProgress(ctx, status, report)
}
