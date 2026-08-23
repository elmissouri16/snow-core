package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tui"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

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
		Args:          cobra.NoArgs,
		Version:       version,
		RunE:          runInteractive,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringP("prompt", "p", "", "run in print mode with this prompt")
	root.PersistentFlags().String("mode", "", "output mode: print|json|rpc")
	root.PersistentFlags().String("collaboration-mode", "", "collaboration mode: default|plan")
	root.PersistentFlags().String("provider", "", "provider id or named OpenAI-compatible profile")
	root.PersistentFlags().String("model", "", "model id")
	root.PersistentFlags().String("api-key", "", "explicit API key (overrides auth.json and env)")
	root.PersistentFlags().String("permission", "", "permission mode: ask|allow|deny")
	root.PersistentFlags().String("session", "", "SQLite session database path to resume")
	root.PersistentFlags().Bool("no-session", false, "ephemeral in-memory session")
	root.PersistentFlags().String("base-url", "", "provider base URL override")
	root.PersistentFlags().String("config", "", "config file path")
	root.PersistentFlags().String("auth", "", "auth file path")
	root.PersistentFlags().String("thinking", "", "thinking level: off|minimal|low|medium|high|xhigh|max|ultra")
	root.PersistentFlags().StringSlice("tools", nil, "restrict built-in tools to a comma-separated allowlist")
	root.PersistentFlags().StringArray("plugin", nil, "load an explicit plugin manifest or executable (repeatable)")
	root.PersistentFlags().Bool("no-plugins", false, "disable all plugin loading")
	root.PersistentFlags().StringArray("mcp", nil, "connect an MCP manifest, Streamable HTTP URL, or stdio executable (repeatable)")
	root.PersistentFlags().Bool("no-mcp", false, "disable all configured MCP servers")
	root.PersistentFlags().StringArray("skill-dir", nil, "add a trusted Agent Skills directory (repeatable)")
	root.PersistentFlags().Bool("no-skills", false, "disable Agent Skills discovery")
	root.PersistentFlags().Bool("usage", false, "print token/cache usage after print-mode prompts")
	root.PersistentFlags().Bool("subagents", false, "enable role-scoped Codex-style subagents")
	root.PersistentFlags().Bool("no-subagents", false, "disable configured subagents")
	root.PersistentFlags().String("subagent-provider", "", "default provider for subagents")
	root.PersistentFlags().String("subagent-model", "", "default model for subagents")
	root.PersistentFlags().Int("subagent-max-concurrency", 0, "maximum concurrently running subagents")
	root.PersistentFlags().Int("subagent-max-agents", 0, "maximum subagent identities per session")
	root.PersistentFlags().Int("subagent-max-depth", 0, "maximum subagent nesting depth")

	root.AddCommand(versionCmd())
	root.AddCommand(resumeCmd())
	root.AddCommand(forkCmd())
	root.AddCommand(forkWorktreeCmd())
	root.AddCommand(authCmd())
	root.AddCommand(loginCmd())
	root.AddCommand(logoutCmd())
	root.AddCommand(skillsCmd())
	root.AddCommand(mcpCmd())
	root.AddCommand(pluginCmd())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root.SetContext(ctx)
	return root.Execute()
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [session-path]",
		Short: "Pick a session for this directory, or resume an explicit session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			noSession, _ := cmd.Flags().GetBool("no-session")
			if noSession {
				return errors.New("resume: --no-session cannot be used when resuming a session")
			}

			path, _ := cmd.Flags().GetString("session")
			if len(args) == 1 {
				if cmd.Flags().Changed("session") {
					return errors.New("resume: session path provided both as an argument and with --session")
				}
				path = args[0]
			}
			if path == "" {
				cwd := mustCWD()
				infos, err := session.NewFileIndex(session.DefaultSessionsRoot()).List(cwd)
				if err != nil {
					return fmt.Errorf("resume: list sessions for %s: %w", cwd, err)
				}
				if len(infos) == 0 {
					return fmt.Errorf("resume: no saved sessions for %s; run snow to start one", cwd)
				}
				mode, _ := cmd.Flags().GetString("mode")
				prompt, _ := cmd.Flags().GetString("prompt")
				if mode == "" && prompt == "" {
					return runInteractiveWithSessionPicker(cmd, nil)
				}
				// Headless modes cannot show an interactive picker. Preserve their
				// useful no-path behavior by resuming the newest indexed session.
				path = infos[0].Path
			}

			if err := session.ValidateSQLiteSession(path); err != nil {
				return fmt.Errorf("resume: session %q: %w", path, err)
			}
			if err := cmd.Flags().Set("session", path); err != nil {
				return fmt.Errorf("resume: select session: %w", err)
			}
			return runInteractiveWithExistingSession(cmd, nil)
		},
	}
}

// authCmd contains non-interactive authentication inspection commands.
func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect provider authentication",
	}
	cmd.AddCommand(authCheckCmd())
	return cmd
}

func authCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <provider>",
		Short: "Check whether a provider credential is configured",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			authPath, _ := cmd.Flags().GetString("auth")
			if authPath == "" {
				_, authPath, _ = config.DefaultPaths()
			}
			store, err := auth.NewFileStore(authPath)
			if err != nil {
				return err
			}
			cfg, _, err := loadCLIAuthConfig(cmd)
			if err != nil {
				return err
			}
			service, _, err := newCLIAuthService(store, cfg.Providers)
			if err != nil {
				return err
			}
			status, err := service.Status(cmd.Context(), provider)
			if err != nil {
				return fmt.Errorf("auth check %s: %w", provider, err)
			}
			if !status.Configured() {
				return fmt.Errorf("auth check %s: not authenticated", provider)
			}
			fmt.Printf("%s: %s\n", provider, status.Summary)
			if status.State == auth.StateExpired {
				return fmt.Errorf("auth check %s: credential expired", provider)
			}
			return nil
		},
	}
}

// loginCmd stores API-key or OAuth credentials in auth.json.
func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login <provider>",
		Short: "Sign in to a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			authPath, _ := cmd.Flags().GetString("auth")
			if authPath == "" {
				_, authPath, _ = config.DefaultPaths()
			}
			store, err := auth.NewFileStore(authPath)
			if err != nil {
				return err
			}
			cfg, configPath, err := loadCLIAuthConfig(cmd)
			if err != nil {
				return err
			}
			profileName, _ := cmd.Flags().GetString("name")
			profileName = strings.TrimSpace(profileName)
			if profileName != "" {
				if provider != openaicompat.ProviderID {
					return errors.New("login: --name is only valid with openai-compatible")
				}
				if err := config.ValidateProviderProfileID(profileName); err != nil {
					return err
				}
				baseURL, _ := cmd.Flags().GetString("base-url")
				baseURL = strings.TrimSpace(baseURL)
				cfg, err = config.Update(configPath, func(latest *config.Config) error {
					if latest.Providers == nil {
						latest.Providers = map[string]config.ProviderConfig{}
					}
					profileConfig := latest.Providers[profileName]
					if baseURL != "" {
						profileConfig.BaseURL = baseURL
					}
					if profileConfig.BaseURL == "" {
						return errors.New("login: named OpenAI-compatible profile requires --base-url on first login")
					}
					profileConfig.Type = config.ProviderTypeOpenAICompatible
					latest.Providers[profileName] = profileConfig
					return nil
				})
				if err != nil {
					return fmt.Errorf("login: save named profile: %w", err)
				}
				provider = profileName
			}
			service, _, err := newCLIAuthService(store, cfg.Providers)
			if err != nil {
				return err
			}
			device, _ := cmd.Flags().GetBool("device-code")
			noOpen, _ := cmd.Flags().GetBool("no-open")
			method := "api_key"
			if provider == chatgpt.ProviderID {
				method = string(chatgpt.LoginBrowser)
				if device {
					method = string(chatgpt.LoginDevice)
				}
			}
			loginCtx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			status, err := service.Login(loginCtx, provider, auth.LoginRequest{Method: method}, cliAuthInteraction{openBrowser: !noOpen})
			if err != nil {
				return err
			}
			if provider == chatgpt.ProviderID {
				fmt.Println(provider + ": " + status.Summary)
				catalog := chatgpt.New(chatgpt.Config{Store: store, CacheRoot: filepath.Join(config.GlobalDir(), "cache", "chatgpt-models")})
				if models, catalogErr := catalog.RefreshModels(loginCtx); catalogErr == nil {
					fmt.Printf("loaded %d ChatGPT models\n", len(models))
				} else {
					fmt.Fprintln(os.Stderr, "chatgpt: signed in; model catalog is using an offline fallback")
				}
				return nil
			}
			fmt.Printf("stored %s API key in %s (0600)\n", provider, authPath)
			return nil
		},
	}
	cmd.Flags().String("name", "", "save an OpenAI-compatible endpoint as this provider profile")
	cmd.Flags().Bool("device-code", false, "use ChatGPT device-code login")
	cmd.Flags().Bool("no-open", false, "do not launch a browser automatically")
	return cmd
}

func promptLine(prompt string) (string, error) {
	fmt.Print(prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		value, err := term.ReadPassword(fd)
		fmt.Println()
		return string(value), err
	}
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return "", sc.Err()
	}
	return sc.Text(), nil
}

func openBrowser(ctx context.Context, target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	default:
		name, args = "xdg-open", []string{target}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
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

func buildOptions(cmd *cobra.Command) (app.Options, error) {
	opts := app.Options{BuildVersion: version}
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
	opts.Tools, _ = cmd.Flags().GetStringSlice("tools")
	opts.CollaborationMode, _ = cmd.Flags().GetString("collaboration-mode")
	opts.NoPlugins, _ = cmd.Flags().GetBool("no-plugins")
	opts.NoMCP, _ = cmd.Flags().GetBool("no-mcp")
	opts.SkillDirs, _ = cmd.Flags().GetStringArray("skill-dir")
	opts.NoSkills, _ = cmd.Flags().GetBool("no-skills")
	enableSubagents, _ := cmd.Flags().GetBool("subagents")
	disableSubagents, _ := cmd.Flags().GetBool("no-subagents")
	if enableSubagents && disableSubagents {
		return opts, errors.New("--subagents and --no-subagents are mutually exclusive")
	}
	if cmd.Flags().Changed("subagents") || cmd.Flags().Changed("no-subagents") {
		enabled := enableSubagents && !disableSubagents
		opts.Subagents = &enabled
	}
	opts.SubagentProvider, _ = cmd.Flags().GetString("subagent-provider")
	opts.SubagentModel, _ = cmd.Flags().GetString("subagent-model")
	opts.SubagentMaxConcurrency, _ = cmd.Flags().GetInt("subagent-max-concurrency")
	opts.SubagentMaxAgents, _ = cmd.Flags().GetInt("subagent-max-agents")
	opts.SubagentMaxDepth, _ = cmd.Flags().GetInt("subagent-max-depth")
	args, _ := cmd.Flags().GetStringArray("plugin")
	for _, arg := range args {
		spec, err := parsePluginSpec(arg)
		if err != nil {
			return opts, err
		}
		opts.Plugins = append(opts.Plugins, spec)
	}
	mcpArgs, _ := cmd.Flags().GetStringArray("mcp")
	for _, arg := range mcpArgs {
		specs, err := parseMCPSpecs(arg)
		if err != nil {
			return opts, err
		}
		opts.MCPServers = append(opts.MCPServers, specs...)
	}
	return opts, nil
}

func parseMCPSpecs(arg string) ([]publicmcp.ServerSpec, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil, fmt.Errorf("mcp: empty argument")
	}
	var data []byte
	if strings.HasPrefix(arg, "{") {
		data = []byte(arg)
	} else if b, err := os.ReadFile(arg); err == nil {
		trimmed := strings.TrimSpace(string(b))
		if strings.HasSuffix(strings.ToLower(arg), ".json") || strings.HasPrefix(trimmed, "{") {
			data = b
		}
	}
	if len(data) > 0 {
		var common struct {
			MCPServers  map[string]publicmcp.ServerSpec `json:"mcpServers"`
			SnowServers map[string]publicmcp.ServerSpec `json:"mcp_servers"`
		}
		if err := json.Unmarshal(data, &common); err != nil {
			return nil, fmt.Errorf("mcp %s: parse manifest: %w", arg, err)
		}
		servers := common.MCPServers
		if len(servers) == 0 {
			servers = common.SnowServers
		}
		if len(servers) > 0 {
			ids := make([]string, 0, len(servers))
			for id := range servers {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			out := make([]publicmcp.ServerSpec, 0, len(ids))
			for _, id := range ids {
				spec := servers[id]
				if spec.ID == "" {
					spec.ID = sanitizeMCPID(id)
				}
				if err := spec.Validate(); err != nil {
					return nil, fmt.Errorf("mcp %s: %w", id, err)
				}
				out = append(out, spec)
			}
			return out, nil
		}
		var spec publicmcp.ServerSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("mcp %s: parse server: %w", arg, err)
		}
		if spec.ID == "" {
			spec.ID = deriveMCPID(spec.Command, spec.URL)
		}
		if err := spec.Validate(); err != nil {
			return nil, err
		}
		return []publicmcp.ServerSpec{spec}, nil
	}
	if parsed, err := url.Parse(arg); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		spec := publicmcp.ServerSpec{ID: sanitizeMCPID(parsed.Hostname()), Transport: publicmcp.TransportStreamableHTTP, URL: arg}
		return []publicmcp.ServerSpec{spec}, spec.Validate()
	}
	spec := publicmcp.ServerSpec{ID: deriveMCPID(arg, ""), Transport: publicmcp.TransportStdio, Command: arg}
	return []publicmcp.ServerSpec{spec}, spec.Validate()
}

func deriveMCPID(command, endpoint string) string {
	value := command
	if endpoint != "" {
		if parsed, err := url.Parse(endpoint); err == nil && parsed.Hostname() != "" {
			value = parsed.Hostname()
		}
	}
	return sanitizeMCPID(filepath.Base(value))
}

func sanitizeMCPID(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	value = strings.Trim(b.String(), "-_")
	if value == "" {
		value = "server"
	}
	if len(value) > 64 {
		value = strings.Trim(value[:64], "-_")
	}
	return value
}

func parsePluginSpec(arg string) (publicplugin.PluginSpec, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return publicplugin.PluginSpec{}, fmt.Errorf("plugin: empty argument")
	}
	var data []byte
	if strings.HasPrefix(arg, "{") {
		data = []byte(arg)
	} else if b, err := os.ReadFile(arg); err == nil {
		trimmed := strings.TrimSpace(string(b))
		if strings.HasSuffix(strings.ToLower(arg), ".json") || strings.HasPrefix(trimmed, "{") {
			data = b
		}
	}
	if len(data) > 0 {
		var spec publicplugin.PluginSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			return spec, fmt.Errorf("plugin %s: parse manifest: %w", arg, err)
		}
		if spec.ID == "" {
			return spec, fmt.Errorf("plugin %s: manifest id required", arg)
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		if _, present := fields["enabled"]; !present {
			spec.Enabled = true
		}
		return spec, nil
	}
	id := filepath.Base(arg)
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			id = strings.ReplaceAll(id, string(r), "-")
		}
	}
	id = strings.ToLower(strings.Trim(id, "-"))
	if id == "" {
		id = "plugin"
	}
	return publicplugin.PluginSpec{ID: id, Command: []string{arg}, Enabled: true}, nil
}

func runInteractive(cmd *cobra.Command, args []string) error {
	return runInteractiveOptions(cmd, false, false)
}

func runInteractiveWithSessionPicker(cmd *cobra.Command, args []string) error {
	return runInteractiveOptions(cmd, true, false)
}

func runInteractiveWithExistingSession(cmd *cobra.Command, args []string) error {
	return runInteractiveOptions(cmd, false, true)
}

func runInteractiveOptions(cmd *cobra.Command, sessionPicker, requireExistingSession bool) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts, err := buildOptions(cmd)
	if err != nil {
		return err
	}
	if sessionPicker {
		// The startup picker needs an app for configuration and rendering, but it
		// must not create a throwaway persisted database that session discovery
		// could open and delete while still active. Switch this memory placeholder
		// to the selected durable store before accepting prompts.
		opts.NoSession = true
	}
	opts.RequireExistingSession = requireExistingSession
	mode, _ := cmd.Flags().GetString("mode")
	prompt, _ := cmd.Flags().GetString("prompt")
	if mode != "" && mode != "print" && mode != "json" && mode != "rpc" {
		return fmt.Errorf("unknown mode %q (want print, json, or rpc)", mode)
	}

	if mode == "rpc" {
		return rpc.Main(ctx, opts)
	}
	if prompt != "" || mode == "print" || mode == "json" {
		showUsage, _ := cmd.Flags().GetBool("usage")
		return runPrint(ctx, opts, prompt, mode == "json", showUsage)
	}
	return runTUI(ctx, opts, sessionPicker)
}

func runPrint(ctx context.Context, opts app.Options, prompt string, jsonMode, showUsage bool) (err error) {
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("print mode requires -p prompt")
	}
	a, err := app.New(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	for _, diagnostic := range a.Diagnostics {
		fmt.Fprintf(os.Stderr, "config warning: %s: %s\n", diagnostic.Path, diagnostic.Message)
	}

	var outputMu sync.Mutex
	var outputErr error
	writeOut := func(format string, args ...any) {
		outputMu.Lock()
		defer outputMu.Unlock()
		if outputErr == nil {
			_, outputErr = fmt.Fprintf(os.Stdout, format, args...)
		}
	}
	writeErrOut := func(format string, args ...any) {
		outputMu.Lock()
		defer outputMu.Unlock()
		if outputErr == nil {
			_, outputErr = fmt.Fprintf(os.Stderr, format, args...)
		}
	}
	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		a.Agent.Subscribe(func(ev protocol.AgentEvent) {
			outputMu.Lock()
			defer outputMu.Unlock()
			if outputErr == nil {
				outputErr = enc.Encode(ev)
			}
		})
		outputMu.Lock()
		if outputErr == nil {
			outputErr = enc.Encode(a.Agent.StateEvent())
		}
		outputMu.Unlock()
	} else {
		lastGoalStatus := protocol.ThreadGoalStatus("")
		a.Agent.Subscribe(func(ev protocol.AgentEvent) {
			if ev.Agent != nil && ev.Type != protocol.EvSubagentStarted && ev.Type != protocol.EvSubagentStatus {
				return
			}
			switch ev.Type {
			case protocol.EvTextDelta:
				writeOut("%s", ev.Text)
			case protocol.EvPlanDelta:
				writeOut("%s", ev.Text)
			case protocol.EvThreadGoalUpdated:
				if ev.ThreadGoal != nil && ev.ThreadGoal.Cleared {
					if lastGoalStatus != "" {
						writeOut("\n[goal cleared]\n")
					}
					lastGoalStatus = ""
				} else if ev.ThreadGoal != nil && ev.ThreadGoal.Goal != nil && ev.ThreadGoal.Goal.Status != lastGoalStatus {
					g := ev.ThreadGoal.Goal
					writeOut("\n[goal %s · %d tokens]\n", g.Status, g.TokensUsed)
					lastGoalStatus = g.Status
				}
			case protocol.EvProviderRetry:
				if retry := ev.ProviderRetry; retry != nil {
					writeErrOut("\nsnow: provider retry %d/%d in %dms (%s)\n", retry.Attempt, retry.MaxAttempts, retry.DelayMS, retry.Kind)
				}
			case protocol.EvError:
				writeErrOut("\nsnow: %s\n", ev.Message)
			case protocol.EvToolStart:
				writeOut("\n[tool %s starting]\n", ev.ToolName)
			case protocol.EvSubagentStarted:
				if ev.Subagent != nil {
					writeOut("\n[agent %s started]\n", ev.Subagent.Agent.Path)
				}
			case protocol.EvSubagentStatus:
				if ev.Subagent != nil && ev.Subagent.Status.TerminalOutcome() {
					writeOut("\n[agent %s %s]\n", ev.Subagent.Agent.Path, ev.Subagent.Status)
				}
			case protocol.EvToolEnd:
				if ev.IsError {
					writeOut("[tool %s failed]\n", ev.ToolName)
				} else {
					writeOut("[tool %s done]\n", ev.ToolName)
				}
			}
		})
	}
	if err := a.ReadySubagents(); err != nil {
		return err
	}
	// Print/JSON always has an explicit prompt. Publish the restored snapshot
	// after subscribing, but let Prompt itself supersede any idle continuation
	// so we do not launch and immediately cancel a redundant provider request.
	goalState, err := a.GoalState()
	if err != nil {
		return err
	}
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: goalState, Cleared: goalState == nil}})
	if err := a.Agent.DrainEvents(ctx); err != nil {
		return err
	}

	if err := a.Agent.Prompt(ctx, prompt); err != nil {
		return err
	}
	if err := a.Agent.WaitGoal(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if err := a.WaitSubagentsIdle(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if err := a.Agent.DrainEvents(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if !jsonMode {
		writeOut("\n")
		if showUsage {
			usage, usageErr := a.Agent.Usage()
			if usageErr != nil {
				return usageErr
			}
			writeOut("usage: %d input · %d output · %d cached · %d total", usage.Input, usage.Output, usage.CacheRead, usage.Total)
			if usage.Cost != nil {
				writeOut(" · %s %.6f", usage.Cost.Currency, usage.Cost.Total)
			}
			writeOut("\n")
		}
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	return outputErr
}

func runTUI(ctx context.Context, opts app.Options, sessionPicker bool) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive TUI requires terminal stdin/stdout; use -p, --mode json, or --mode rpc")
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return errors.New("interactive TUI requires cursor-addressable terminal support; TERM=dumb is unsupported")
	}
	if sessionPicker {
		return tui.RunWithSessionPicker(ctx, opts)
	}
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
