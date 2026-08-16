package config

import "testing"

func TestWorktreeWorkerDefaultsAndBounds(t *testing.T) {
	cfg := Default()
	if cfg.TUI.WorktreeSidebar || cfg.WorktreeWorkers.MaxConcurrent != 4 || cfg.WorktreeWorkers.ShutdownTimeoutMS != 5000 {
		t.Fatalf("worktree defaults = %+v, tui=%+v", cfg.WorktreeWorkers, cfg.TUI)
	}
	if err := cfg.WorktreeWorkers.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := cfg.WorktreeWorkers
	bad.MaxConcurrent = 9
	if err := bad.Validate(); err == nil {
		t.Fatal("oversized worktree worker concurrency accepted")
	}
	bad = cfg.WorktreeWorkers
	bad.ShutdownTimeoutMS = 99
	if err := bad.Validate(); err == nil {
		t.Fatal("undersized worktree shutdown timeout accepted")
	}
}
