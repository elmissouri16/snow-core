package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func newRuntimeControlsTestApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	t.Setenv("SNOW_HOME", filepath.Join(home, "snow-home"))
	a, err := New(t.Context(), Options{
		Provider: "fake", Permission: "ask", NoSession: true, CWD: cwd,
		NoMCP: true, NoPlugins: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestRuntimePermissionModeFacade(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	if got, err := a.PermissionMode(); err != nil || got != permission.ModeAsk {
		t.Fatalf("permission mode = %q, err=%v", got, err)
	}
	if err := a.SetPermissionMode(permission.ModeDeny); err != nil {
		t.Fatal(err)
	}
	if got, err := a.PermissionMode(); err != nil || got != permission.ModeDeny {
		t.Fatalf("permission mode after set = %q, err=%v", got, err)
	}
}

func TestProjectTrustFacadePersistsForRestartWithoutMutatingLoadedInputs(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	before, err := a.ProjectTrust()
	if err != nil {
		t.Fatal(err)
	}
	if before.Level != trust.LevelAsk || !before.Prompt || before.Loaded || before.RestartRequired {
		t.Fatalf("initial trust = %+v", before)
	}
	wantPath, err := trust.CanonicalPath(a.CWD())
	if err != nil {
		t.Fatal(err)
	}
	if before.Path != wantPath {
		t.Fatalf("trust path = %q, want %q", before.Path, wantPath)
	}

	after, err := a.SetProjectTrust(trust.LevelAllow)
	if err != nil {
		t.Fatal(err)
	}
	if after.Level != trust.LevelAllow || after.Prompt || after.Loaded || !after.RestartRequired {
		t.Fatalf("allow trust = %+v", after)
	}
	if a.ProjectAllowed {
		t.Fatal("trust mutation loaded project inputs into the running app")
	}
	if level, ok := a.Trust.Get(wantPath); !ok || level != trust.LevelAllow {
		t.Fatalf("persisted trust = %q, ok=%v", level, ok)
	}

	after, err = a.SetProjectTrust(trust.LevelDeny)
	if err != nil {
		t.Fatal(err)
	}
	if after.Level != trust.LevelDeny || after.RestartRequired {
		t.Fatalf("deny trust = %+v", after)
	}
}

func TestPrepareProjectInitOwnsPromptAndRejectsPlanMode(t *testing.T) {
	a := newRuntimeControlsTestApp(t)
	prompt, err := a.PrepareProjectInit()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "AGENTS.md") || !strings.Contains(prompt, ".snow/config.json") {
		t.Fatalf("init prompt missing canonical outputs: %q", prompt)
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PrepareProjectInit(); err == nil || !strings.Contains(err.Error(), "Default mode") {
		t.Fatalf("plan-mode error = %v", err)
	}
}
