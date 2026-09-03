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

// UpdateService is the application boundary for checking and installing Snow
// releases. Constructing a service performs no network or filesystem mutation.
type UpdateService interface {
	Check(context.Context) (updatepkg.Status, error)
	Install(context.Context, updatepkg.Status) (updatepkg.Result, error)
	Eligibility() (bool, string)
}

// CheckForUpdate performs one explicit release lookup.
func (a *App) CheckForUpdate(ctx context.Context) (updatepkg.Status, error) {
	if a == nil || a.updater == nil {
		return updatepkg.Status{}, errors.New("app: updater unavailable")
	}
	return a.updater.Check(ctx)
}

// InstallUpdate verifies and installs a release returned by CheckForUpdate.
func (a *App) InstallUpdate(ctx context.Context, status updatepkg.Status) (updatepkg.Result, error) {
	if a == nil || a.updater == nil {
		return updatepkg.Result{}, errors.New("app: updater unavailable")
	}
	return a.updater.Install(ctx, status)
}

// UpdateEligibility reports whether this process can replace its executable.
func (a *App) UpdateEligibility() (bool, string) {
	if a == nil || a.updater == nil {
		return false, "updater unavailable"
	}
	return a.updater.Eligibility()
}
