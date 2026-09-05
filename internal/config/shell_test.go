package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestShellProtectedPathsRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := []string{filepath.Join(t.TempDir(), "private")}
	cfg := Default()
	cfg.ShellProtectedPaths = want
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(loaded.ShellProtectedPaths, want) {
		t.Fatal("operator paths lost in roundtrip")
	}
	// Restricted project decoding deliberately ignores unknown global fields.
	if _, err := LoadProjectExtensions(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"shell_protected_paths":["relative"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("relative protected path accepted")
	}
}
