package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCLIWebRejectsUnsupportedFlagsBeforeStartup(t *testing.T) {
	for _, args := range [][]string{
		{"--mode", "web", "--provider", "fake"},
		{"--mode", "web", "--no-session"},
		{"--mode", "web", "-p", "hello"},
		{"--mode", "web", "--mcp", "/missing/manifest.json"},
		{"--mode", "web", "--config", "/missing/config.json"},
		{"--mode", "web", "resume", "/missing/session.db"},
		{"--mode", "web", "login", "chatgpt"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home, sessions := t.TempDir(), t.TempDir()
			t.Setenv("SNOW_HOME", home)
			t.Setenv("SNOW_SESSIONS_DIR", sessions)
			oldArgs := os.Args
			os.Args = append([]string{"snow"}, args...)
			t.Cleanup(func() { os.Args = oldArgs })
			if err := run(); err == nil || !strings.Contains(err.Error(), "web") {
				t.Fatalf("expected web validation error, got %v", err)
			}
			for _, root := range []string{home, sessions} {
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("validation mutated storage: %v (%d entries)", err, len(entries))
				}
			}
		})
	}
}

func TestWebDispatchDoesNotLoadProjectOrRuntime(t *testing.T) {
	home, project, sessions := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("SNOW_HOME", home)
	t.Setenv("SNOW_SESSIONS_DIR", sessions)
	t.Chdir(project)
	// Runtime/configuration construction would fail on this malformed input.
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{Use: "snow"}
	cmd.Flags().String("mode", "web", "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd.SetContext(ctx)
	if err := runInteractiveOptions(cmd, false, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("web constructed runtime before handling cancellation: %v", err)
	}
	for _, root := range []string{project, sessions} {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("web mutated project/session storage: %v", err)
		}
	}
}
