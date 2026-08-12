package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompactionGoalAutoThresholdDefaultsDisablesAndValidates(t *testing.T) {
	cfg := Default()
	if cfg.Compaction.GoalAutoThresholdPercent != 90 {
		t.Fatalf("default threshold=%d, want 90", cfg.Compaction.GoalAutoThresholdPercent)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"compaction":{"goal_auto_threshold_percent":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Compaction.GoalAutoThresholdPercent != 0 {
		t.Fatalf("explicit disabled threshold=%d", loaded.Compaction.GoalAutoThresholdPercent)
	}
	for _, value := range []int{49, 100} {
		candidate := DefaultCompaction()
		candidate.GoalAutoThresholdPercent = value
		if err := candidate.Validate(); err == nil {
			t.Fatalf("threshold %d was accepted", value)
		}
	}
}

func TestApplyProjectCompactionPreferences(t *testing.T) {
	cfg := Default()
	retain, min, max := 9000, 3, 1500
	fallback := "error"
	theme := "ocean"
	err := ApplyProjectPreferences(&cfg, ProjectExtensions{TUI: ProjectTUIConfig{Theme: &theme}, Compaction: ProjectCompactionConfig{RetainTokens: &retain, MinRetainedTurns: &min, SummaryMaxTokens: &max, Fallback: &fallback, Guidance: "project facts"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TUI.Theme != "ocean" || cfg.Compaction.RetainTokens != 9000 || cfg.Compaction.Guidance != "project facts" {
		t.Fatalf("%+v", cfg)
	}
}
