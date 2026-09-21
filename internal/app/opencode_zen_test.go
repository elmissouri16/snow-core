package app

import (
	"strings"
	"testing"
)

func TestOpenCodeZenIsDisabled(t *testing.T) {
	app, err := New(t.Context(), Options{
		Provider:   "opencode-zen",
		NoSession:  true,
		Permission: "deny",
		CWD:        t.TempDir(),
		NoPlugins:  true,
		NoMCP:      true,
		NoSkills:   true,
	})
	if err == nil {
		_ = app.Close()
		t.Fatal("OpenCode Zen started after the provider was disabled")
	}
	if !strings.Contains(err.Error(), "opencode-zen") || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %q, want actionable disabled-provider error", err)
	}
}
