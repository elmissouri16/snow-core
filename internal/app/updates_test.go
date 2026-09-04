package app

import (
	"context"
	"testing"

	updatepkg "github.com/elmissouri16/snow-core/internal/update"
)

type recordingUpdateService struct {
	checks   int
	installs int
}

func (s *recordingUpdateService) Check(context.Context) (updatepkg.Status, error) {
	s.checks++
	return updatepkg.Status{LatestVersion: "1.0.1", Available: true, Eligible: true}, nil
}

func (s *recordingUpdateService) Install(ctx context.Context, status updatepkg.Status) (updatepkg.Result, error) {
	return s.InstallWithProgress(ctx, status, nil)
}

func (s *recordingUpdateService) InstallWithProgress(_ context.Context, _ updatepkg.Status, report updatepkg.ProgressFunc) (updatepkg.Result, error) {
	s.installs++
	if report != nil {
		report(updatepkg.Progress{Phase: updatepkg.ProgressDownloading, DownloadedBytes: 2, TotalBytes: 4})
	}
	return updatepkg.Result{InstalledVersion: "1.0.1"}, nil
}

func (*recordingUpdateService) Eligibility() (bool, string) { return true, "" }

func TestAppConstructionDoesNotInvokeUpdater(t *testing.T) {
	service := &recordingUpdateService{}
	a, err := New(t.Context(), Options{
		Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true, Updater: service,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if service.checks != 0 || service.installs != 0 {
		t.Fatalf("app construction invoked updater: checks=%d installs=%d", service.checks, service.installs)
	}
	status, err := a.CheckForUpdate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var progress updatepkg.Progress
	if _, err := a.InstallUpdateWithProgress(t.Context(), status, func(snapshot updatepkg.Progress) {
		progress = snapshot
	}); err != nil {
		t.Fatal(err)
	}
	if service.checks != 1 || service.installs != 1 {
		t.Fatalf("explicit updater calls = checks:%d installs:%d", service.checks, service.installs)
	}
	if progress.Phase != updatepkg.ProgressDownloading || progress.DownloadedBytes != 2 || progress.TotalBytes != 4 {
		t.Fatalf("progress = %+v", progress)
	}
}
