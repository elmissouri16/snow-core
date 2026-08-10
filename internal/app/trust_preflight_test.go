package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/trust"
)

func TestProjectTrustPreflightAndImmediateResourceLoading(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(home, "sessions"))
	cwd := t.TempDir()
	opts := Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: cwd, NoMCP: true}

	preflight, err := InspectProjectTrust(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.Resolution.Prompt || preflight.Resolution.Path == "" {
		t.Fatalf("unknown empty project did not prompt: %+v", preflight.Resolution)
	}

	skillDir := filepath.Join(cwd, ".snow", "skills", "local")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: local\ndescription: Local project skill.\n---\nUse local instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(cwd, ".snow", "config.json")
	if err := os.WriteFile(projectConfig, []byte(`{"skills":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Headless ask is fail-closed and does not discover project skills.
	denied, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := denied.Skills.Get("local"); ok {
		denied.Close()
		t.Fatal("ask policy loaded a project skill headlessly")
	}
	denied.Close()

	if err := preflight.Store.Set(cwd, trust.LevelAllow); err != nil {
		t.Fatal(err)
	}
	allowed, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if skill, ok := allowed.Skills.Get("local"); !ok || skill.Scope != "project" {
		allowed.Close()
		t.Fatalf("allowed project skill = %+v %v", skill, ok)
	}
	allowed.Close()

	// Deny must not parse malformed project configuration; allow must report it.
	if err := preflight.Store.Set(cwd, trust.LevelDeny); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectConfig, []byte(`{"plugins":`), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked, err := New(context.Background(), opts)
	if err != nil {
		t.Fatalf("deny parsed malformed project config: %v", err)
	}
	blocked.Close()
	if err := preflight.Store.Set(cwd, trust.LevelAllow); err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "parse project") {
		t.Fatalf("allow malformed project config error = %v", err)
	}
}

func TestSymlinkProjectTrustCannotAuthorizeRetargetedResources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	realA, realB := t.TempDir(), t.TempDir()
	writeSkill := func(root, name string) {
		dir := filepath.Join(root, ".snow", "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: test\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(realA, "from-a")
	writeSkill(realB, "from-b")
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(realA, alias); err != nil {
		t.Fatal(err)
	}
	opts := Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: alias, NoMCP: true}
	preflight, err := InspectProjectTrust(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.Store.Set(preflight.Resolution.Path, trust.LevelAllow); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realB, alias); err != nil {
		t.Fatal(err)
	}
	retargeted, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := retargeted.Skills.Get("from-b"); ok {
		retargeted.Close()
		t.Fatal("allow for original symlink target authorized retargeted skill")
	}
	retargeted.Close()

	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realA, alias); err != nil {
		t.Fatal(err)
	}
	allowed, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer allowed.Close()
	skill, ok := allowed.Skills.Get("from-a")
	if !ok {
		t.Fatal("authorized canonical target skill was not loaded")
	}
	canonicalA, err := filepath.EvalSymlinks(realA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(skill.Directory, canonicalA+string(filepath.Separator)) {
		t.Fatalf("skill directory %q is not pinned beneath canonical target %q", skill.Directory, canonicalA)
	}
}

func TestInvalidAndLegacyDefaultProjectTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	cwd := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	cfg := config.Default()
	cfg.DefaultProjectTrust = "always"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	preflight, err := InspectProjectTrust(Options{CWD: cwd, ConfigPath: configPath})
	if err != nil || preflight.Resolution.Prompt || preflight.Resolution.Level != trust.LevelAllow {
		t.Fatalf("always resolution = %+v, %v", preflight.Resolution, err)
	}
	cfg.DefaultProjectTrust = "invalid"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectProjectTrust(Options{CWD: cwd, ConfigPath: configPath}); err == nil {
		t.Fatal("invalid default trust accepted by preflight")
	}
	if _, err := New(context.Background(), Options{Provider: "fake", NoSession: true, CWD: cwd, ConfigPath: configPath}); err == nil {
		t.Fatal("invalid default trust accepted by app.New")
	}
}
