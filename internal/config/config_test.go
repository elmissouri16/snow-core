package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultProvider != "opencode-go" {
		t.Fatalf("default provider = %q", cfg.DefaultProvider)
	}
	if cfg.PermissionMode != "ask" {
		t.Fatalf("default permission = %q", cfg.PermissionMode)
	}
}

func TestLoadAndSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.DefaultProvider = "fake"
	cfg.DefaultModel = "m2"
	cfg.PermissionMode = "deny"
	cfg.Providers = map[string]ProviderConfig{
		"opencode-go": {BaseURL: "https://example.com/v1", DefaultModel: "kimi-k2.6"},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultProvider != "fake" || got.DefaultModel != "m2" || got.PermissionMode != "deny" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	pc, ok := got.Providers["opencode-go"]
	if !ok || pc.BaseURL != "https://example.com/v1" || pc.DefaultModel != "kimi-k2.6" {
		t.Fatalf("provider config mismatch: %+v", got.Providers)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.ToolOutputLimit() != DefaultToolOutputBytes {
		t.Fatal("wrong tool output limit")
	}
	if cfg.BashTimeout() != DefaultBashTimeout {
		t.Fatal("wrong bash timeout")
	}
}

func TestOverridesFromEnv(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	dir := GlobalDir()
	if dir == "" {
		t.Fatal("expected global dir")
	}
}
