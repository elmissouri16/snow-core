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

func (s *recordingUpdateService) Install(context.Context, updatepkg.Status) (updatepkg.Result, error) {
	s.installs++
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
	if _, err := a.InstallUpdate(t.Context(), status); err != nil {
		t.Fatal(err)
	}
	if service.checks != 1 || service.installs != 1 {
		t.Fatalf("explicit updater calls = checks:%d installs:%d", service.checks, service.installs)
	}
}
