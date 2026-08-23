package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRetryDefaultsAndPartialGlobalOverride(t *testing.T) {
	defaults := DefaultRetry()
	if defaults.Normal.MaxAttempts != 12 || defaults.Normal.MaxElapsedMS != 300_000 || defaults.Goal.MaxAttempts != 30 || defaults.Goal.MaxElapsedMS != 1_800_000 {
		t.Fatalf("defaults=%+v", defaults)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"retry":{"normal":{"max_attempts":4}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retry.Normal.MaxAttempts != 4 || cfg.Retry.Normal.InitialDelayMS != defaults.Normal.InitialDelayMS || cfg.Retry.Goal != defaults.Goal {
		t.Fatalf("partial override=%+v", cfg.Retry)
	}
}

func TestRetryValidation(t *testing.T) {
	for _, mutate := range []func(*RetryConfig){
		func(c *RetryConfig) { c.Normal.MaxAttempts = 0 },
		func(c *RetryConfig) { c.Normal.MaxElapsedMS = 0 },
		func(c *RetryConfig) { c.Goal.InitialDelayMS = c.Goal.MaxDelayMS + 1 },
		func(c *RetryConfig) { c.Goal.JitterPercent = 101 },
	} {
		cfg := DefaultRetry()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid retry config accepted: %+v", cfg)
		}
	}
}

func TestProjectExtensionsCannotOverrideRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, []byte(`{"retry":{"normal":{"max_attempts":99}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	extensions, err := LoadProjectExtensions(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	before := cfg.Retry
	if err := ApplyProjectPreferences(&cfg, extensions); err != nil {
		t.Fatal(err)
	}
	if cfg.Retry != before {
		t.Fatalf("project changed retry policy: before=%+v after=%+v", before, cfg.Retry)
	}
}
