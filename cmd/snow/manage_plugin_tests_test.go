package main

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
)

func TestPluginFixtureCLIUsesOnlyMockHost(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "demo")
	if err := javascript.Scaffold(dir, "demo", true); err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join(dir, "tests/plugin.json")
	output, err := runCLI(t, "snow", "--provider", "nonexistent-provider", "plugin", "test", dir, "--fixtures", fixtures, "--json")
	var report javascript.FixtureReport
	if err != nil || jsonv2.Unmarshal([]byte(output), &report) != nil || report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("output=%s err=%v", output, err)
	}
	// Deliberately wrong expected content must produce a failed JSON report and
	// a nonzero command error, not silently succeed or contact a provider.
	raw, err := os.ReadFile(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), `"expect":{"content":[{"type":"text","text":"Hello world · visit 1"}]}`, `"expect":{"content":[{"type":"text","text":"wrong"}]}`))
	if err := os.WriteFile(fixtures, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output, err = runCLI(t, "snow", "plugin", "test", dir, "--fixtures", fixtures, "--json")
	if err == nil || !strings.Contains(output, `"failed":1`) {
		t.Fatalf("failed output=%s err=%v", output, err)
	}
}
func TestPluginFixtureCLIRequiresExplicitFixtureAndHonorsDisable(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	for _, args := range [][]string{
		{"snow", "plugin", "test", "/missing"},
		{"snow", "--no-plugins", "plugin", "test", "/missing", "--fixtures", "/missing"},
		{"snow", "plugin", "test", "/missing", "--fixtures", t.TempDir()},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
