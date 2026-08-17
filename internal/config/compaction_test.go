package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompactionAutoThresholdDefaultsDisablesAndValidates(t *testing.T) {
	cfg := Default()
	if cfg.Compaction.AutoThresholdPercent != 80 {
		t.Fatalf("default threshold=%d, want 80", cfg.Compaction.AutoThresholdPercent)
	}
	if cfg.Compaction.ToolHistoryBudgetPercent != 20 {
		t.Fatalf("default tool history budget=%d, want 20", cfg.Compaction.ToolHistoryBudgetPercent)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"compaction":{"goal_auto_threshold_percent":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Compaction.AutoThresholdPercent != 0 {
		t.Fatalf("explicit disabled threshold=%d", loaded.Compaction.AutoThresholdPercent)
	}
	if loaded.Compaction.ToolHistoryBudgetPercent != 0 {
		t.Fatalf("legacy automatic-compaction disable unexpectedly enabled tool history budget=%d", loaded.Compaction.ToolHistoryBudgetPercent)
	}
	for _, value := range []int{49, 100} {
		candidate := DefaultCompaction()
		candidate.AutoThresholdPercent = value
		candidate.GoalAutoThresholdPercent = 0
		if err := candidate.Validate(); err == nil {
			t.Fatalf("threshold %d was accepted", value)
		}
	}

	toolPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(toolPath, []byte(`{"compaction":{"tool_history_budget_percent":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	toolDisabled, err := Load(toolPath)
	if err != nil {
		t.Fatal(err)
	}
	if toolDisabled.Compaction.ToolHistoryBudgetPercent != 0 {
		t.Fatalf("explicit disabled tool history budget=%d", toolDisabled.Compaction.ToolHistoryBudgetPercent)
	}
	for _, value := range []int{4, 51} {
		candidate := DefaultCompaction()
		candidate.ToolHistoryBudgetPercent = value
		if err := candidate.Validate(); err == nil {
			t.Fatalf("tool history budget %d was accepted", value)
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
