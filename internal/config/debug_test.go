package config

import (
	"path/filepath"
	"testing"
)

func TestDebugConfigDefaultsOffAndRoundTrips(t *testing.T) {
	if Default().Debug.Enabled {
		t.Fatal("debug must default off")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Debug.Enabled = true
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Debug.Enabled {
		t.Fatal("debug enablement did not round-trip")
	}
}
