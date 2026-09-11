package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/skills"
)

func skillManagementApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	writePluginNamedUserSkill(t, home)
	return &App{ConfigPath: filepath.Join(home, "config.json"), ProjectInputRoot: t.TempDir(),
		Skills: skills.Discover(skills.Options{Home: home, SnowHome: t.TempDir()})}
}

func TestSkillPolicySavePreservesRuntimeAndUnrelatedConfiguration(t *testing.T) {
	a := skillManagementApp(t)
	if err := os.WriteFile(a.ConfigPath, []byte(`{"custom":{"keep":true},"skills":{"overrides":{"other":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := a.SetSkillEnabled(t.Context(), "snow-js-plugin", false)
	if err != nil || status.Enabled || !status.RestartRequired || status.PolicyScope != "global" {
		t.Fatalf("save=%+v err=%v", status, err)
	}
	if _, enabled := a.Skills.Get("snow-js-plugin"); !enabled {
		t.Fatal("save changed running catalog")
	}
	cfg, err := config.Load(a.ConfigPath)
	if err != nil || cfg.Skills.Overrides["snow-js-plugin"] || cfg.Skills.Overrides["other"] {
		t.Fatalf("saved policy=%+v err=%v", cfg.Skills, err)
	}
	data, err := os.ReadFile(a.ConfigPath)
	if err != nil || !strings.Contains(string(data), `"custom"`) {
		t.Fatal("lost unrelated configuration")
	}
	statuses, err := a.SkillStatuses()
	if err != nil || len(statuses) != 1 || statuses[0].Enabled || !statuses[0].RestartRequired {
		t.Fatalf("refresh=%+v err=%v", statuses, err)
	}
	// Reopening discovery applies the saved policy without loading skill bodies.
	restarted := skills.Discover(skills.Options{Home: filepath.Dir(a.ConfigPath), SnowHome: t.TempDir(), Overrides: cfg.Skills.Overrides})
	if got, exists := restarted.Lookup("snow-js-plugin"); !exists || got.Enabled {
		t.Fatal("saved disable did not survive restart")
	}
	status, err = a.SetSkillEnabled(t.Context(), "snow-js-plugin", true)
	if err != nil || !status.Enabled || status.RestartRequired {
		t.Fatalf("re-enable=%+v err=%v", status, err)
	}
}

func TestSkillPolicyScopeFollowsTrustedProjectPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		trusted      bool
		wantScope    string
		wantEnabled  bool
	}{
		{"global", `{"skills":{"overrides":{"snow-js-plugin":true}}}`, false, "global", false},
		{"project named", `{"skills":{"overrides":{"snow-js-plugin":true}}}`, true, "project", true},
		{"project default resets global", `{"skills":{"disabled":false}}`, true, "project", true},
		{"project inherit", `{"skills":{}}`, true, "global", false},
		{"untrusted malformed", `not json`, false, "global", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := skillManagementApp(t)
			a.ProjectAllowed = tc.trusted
			if err := config.UpdateSkills(a.ConfigPath, func(s *config.SkillsConfig) error {
				s.Overrides["snow-js-plugin"] = false
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			projectPath := filepath.Join(a.ProjectInputRoot, ".snow", "config.json")
			if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(projectPath, []byte(tc.policy), 0o600); err != nil {
				t.Fatal(err)
			}
			statuses, err := a.SkillStatuses()
			if err != nil || len(statuses) != 1 || statuses[0].Enabled != tc.wantEnabled || statuses[0].PolicyScope != tc.wantScope {
				t.Fatalf("statuses=%+v err=%v", statuses, err)
			}
			status, err := a.SetSkillEnabled(t.Context(), "snow-js-plugin", !tc.wantEnabled)
			if err != nil || status.Enabled == tc.wantEnabled {
				t.Fatalf("toggle=%+v err=%v", status, err)
			}
			if tc.wantScope == "global" {
				data, err := os.ReadFile(projectPath)
				if err != nil || string(data) != tc.policy {
					t.Fatal("global action altered project policy")
				}
			}
			statuses, err = a.SkillStatuses()
			if err != nil || statuses[0].Enabled == tc.wantEnabled {
				t.Fatalf("saved effective policy=%+v err=%v", statuses, err)
			}
		})
	}
}

func TestSkillPolicySaveRejectsFailuresWithoutChangingRuntime(t *testing.T) {
	a := skillManagementApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.SetSkillEnabled(ctx, "snow-js-plugin", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if _, err := a.SetSkillEnabled(t.Context(), "missing", false); err == nil {
		t.Fatal("unknown skill accepted")
	}
	if _, err := os.Stat(a.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected mutation wrote config: %v", err)
	}
	a.ConfigPath = t.TempDir()
	if _, err := a.SetSkillEnabled(t.Context(), "snow-js-plugin", false); err == nil {
		t.Fatal("invalid config destination accepted")
	}
	if _, enabled := a.Skills.Get("snow-js-plugin"); !enabled {
		t.Fatal("failed save changed runtime")
	}
}
