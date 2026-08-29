package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func debugOptionsCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	flags := cmd.Flags()
	flags.String("provider", "", "")
	flags.String("model", "", "")
	flags.String("api-key", "", "")
	flags.String("permission", "", "")
	flags.String("session", "", "")
	flags.Bool("no-session", false, "")
	flags.String("base-url", "", "")
	flags.String("config", "", "")
	flags.String("auth", "", "")
	flags.String("thinking", "", "")
	flags.StringSlice("tools", nil, "")
	flags.String("collaboration-mode", "", "")
	flags.Bool("no-plugins", false, "")
	flags.Bool("no-mcp", false, "")
	flags.StringArray("skill-dir", nil, "")
	flags.Bool("no-skills", false, "")
	flags.Bool("debug", false, "")
	flags.Bool("no-debug", false, "")
	flags.String("debug-dump", "", "")
	flags.Bool("subagents", false, "")
	flags.Bool("no-subagents", false, "")
	flags.String("subagent-provider", "", "")
	flags.String("subagent-model", "", "")
	flags.Int("subagent-max-concurrency", 0, "")
	flags.Int("subagent-max-agents", 0, "")
	flags.Int("subagent-max-depth", 0, "")
	flags.StringArray("plugin", nil, "")
	flags.StringArray("mcp", nil, "")
	if err := flags.Parse(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestBuildOptionsDebugOverrides(t *testing.T) {
	opts, err := buildOptions(debugOptionsCommand(t, "--debug-dump", "diagnostic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if opts.Debug == nil || !*opts.Debug || opts.DebugDumpPath != "diagnostic.json" {
		t.Fatalf("debug options=%+v enabled=%v", opts, opts.Debug)
	}

	_, err = buildOptions(debugOptionsCommand(t, "--no-debug", "--debug-dump", "diagnostic.json"))
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("conflict error=%v", err)
	}
}
