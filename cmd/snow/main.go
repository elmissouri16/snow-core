// Command snow is the snow-core CLI: interactive TUI, print mode, and
// JSON event stream mode.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/config"
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
		Use:           "snow",
		Short:         "snow — a minimal modular coding-agent harness in Go",
		Version:       version,
		RunE:          runInteractive,
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
	root.AddCommand(loginCmd())
	root.AddCommand(logoutCmd())

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

// loginCmd stores an API key credential for a provider in auth.json.
// OAuth flows (ChatGPT) are a Phase 2 follow-up.
func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login <provider>",
		Short: "Store an API key credential for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			switch provider {
			case "opencode-go":
			default:
				return fmt.Errorf("login: unsupported provider %q (supported: opencode-go)", provider)
			}
			key, err := promptSecret("API key: ")
			if err != nil {
				return err
			}
			if key == "" {
				return fmt.Errorf("login: empty API key")
			}
			authPath, _ := cmd.Flags().GetString("auth")
			if authPath == "" {
				_, a, _ := config.DefaultPaths()
				authPath = a
			}
			store, err := auth.NewFileStore(authPath)
			if err != nil {
				return err
			}
			if err := store.Put(provider, auth.Credential{Type: auth.CredentialAPIKey, Key: key}); err != nil {
				return err
			}
			fmt.Printf("stored %s API key in %s (0600)\n", provider, authPath)
			return nil
		},
	}
}

// promptSecret reads a line from stdin without echoing (best-effort; falls
// back to plain readline when the terminal is not interactive).
func promptSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		return string(b), err
	}
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return "", sc.Err()
	}
	return sc.Text(), nil
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

// logoutCmd clears a provider credential from auth.json.
func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout <provider>",
		Short: "Remove a stored credential for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			authPath, _ := cmd.Flags().GetString("auth")
			if authPath == "" {
				_, a, _ := config.DefaultPaths()
				authPath = a
			}
			store, err := auth.NewFileStore(authPath)
			if err != nil {
				return err
			}
			if err := store.Delete(provider); err != nil {
				return err
			}
			fmt.Printf("removed %s credential from %s\n", provider, authPath)
			return nil
		},
	}
}
