// Command snow is the snow-core CLI: interactive TUI, print mode, and
// JSON event stream mode.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/rpc"
	"github.com/snow-core/snow/internal/tui"
	"github.com/snow-core/snow/pkg/protocol"
)

var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "snow:", err)
		os.Exit(1)
	}
}

func run() error {
	root := &cobra.Command{
		Use:     "snow",
		Short:   "snow — a minimal modular coding-agent harness in Go",
		Version: version,
		RunE:    runInteractive,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringP("prompt", "p", "", "run in print mode with this prompt")
	root.PersistentFlags().String("mode", "", "output mode: print|json")
	root.PersistentFlags().String("provider", "", "provider id (opencode-go|fake|chatgpt)")
	root.PersistentFlags().String("model", "", "model id")
	root.PersistentFlags().String("api-key", "", "explicit API key (overrides auth.json and env)")
	root.PersistentFlags().String("permission", "", "permission mode: ask|allow|deny")
	root.PersistentFlags().String("session", "", "session file path to resume")
	root.PersistentFlags().Bool("no-session", false, "ephemeral in-memory session")
	root.PersistentFlags().String("base-url", "", "provider base URL override")
	root.PersistentFlags().String("config", "", "config file path")
	root.PersistentFlags().String("auth", "", "auth file path")
	root.PersistentFlags().String("thinking", "", "thinking level: off|low|medium|high")

	root.AddCommand(versionCmd())

	return root.Execute()
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}

func buildOptions(cmd *cobra.Command) app.Options {
	opts := app.Options{}
	opts.CWD = mustCWD()
	opts.Provider, _ = cmd.Flags().GetString("provider")
	opts.Model, _ = cmd.Flags().GetString("model")
	opts.APIKey, _ = cmd.Flags().GetString("api-key")
	opts.Permission, _ = cmd.Flags().GetString("permission")
	opts.SessionPath, _ = cmd.Flags().GetString("session")
	opts.NoSession, _ = cmd.Flags().GetBool("no-session")
	opts.BaseURL, _ = cmd.Flags().GetString("base-url")
	opts.ConfigPath, _ = cmd.Flags().GetString("config")
	opts.AuthPath, _ = cmd.Flags().GetString("auth")
	opts.Thinking, _ = cmd.Flags().GetString("thinking")
	return opts
}

func runInteractive(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := buildOptions(cmd)
	mode, _ := cmd.Flags().GetString("mode")
	prompt, _ := cmd.Flags().GetString("prompt")

	if mode == "rpc" {
		return rpc.Main(ctx, opts)
	}
	if prompt != "" || mode == "print" || mode == "json" {
		return runPrint(ctx, opts, prompt, mode == "json")
	}
	return runTUI(ctx, opts)
}

func runPrint(ctx context.Context, opts app.Options, prompt string, jsonMode bool) error {
	a, err := app.New(ctx, opts)
	if err != nil {
		return err
	}
	defer a.Close()

	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		a.Agent.Subscribe(func(ev protocol.AgentEvent) {
			_ = enc.Encode(ev)
		})
	} else {
		a.Agent.Subscribe(func(ev protocol.AgentEvent) {
			switch ev.Type {
			case protocol.EvTextDelta:
				fmt.Print(ev.Text)
			case protocol.EvError:
				fmt.Fprintf(os.Stderr, "\nsnow: %s\n", ev.Message)
			case protocol.EvToolStart:
				fmt.Printf("\n[tool %s starting]\n", ev.ToolName)
			case protocol.EvToolEnd:
				if ev.IsError {
					fmt.Printf("[tool %s failed]\n", ev.ToolName)
				} else {
					fmt.Printf("[tool %s done]\n", ev.ToolName)
				}
			}
		})
	}

	if prompt == "" {
		return fmt.Errorf("print mode requires -p prompt")
	}
	if err := a.Agent.Prompt(ctx, prompt); err != nil {
		return err
	}
	if !jsonMode {
		fmt.Println()
	}
	return nil
}

func runTUI(ctx context.Context, opts app.Options) error {
	return tui.Run(ctx, opts)
}

func mustCWD() string {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "snow: getwd:", err)
		os.Exit(1)
	}
	return cwd
}
