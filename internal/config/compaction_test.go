package config

import "testing"

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
