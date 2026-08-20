package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tempfile"
	"github.com/elmissouri16/snow-core/internal/trust"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// startupConfig contains the dependency-ordered state created before tools.
// Keeping this phase together makes New the sole runtime-wiring owner while
// giving configuration and trust setup an explicit boundary.
type startupConfig struct {
	absCWD, globalDir, configPath string
	persistedCfg, cfg             config.Config
	permMode                      permission.Mode
	thinking                      protocol.ThinkingLevel
	reasoningSummary              protocol.ReasoningSummary
	textVerbosity                 protocol.TextVerbosity
	collaborationMode             protocol.CollaborationMode
	planThinking                  *protocol.ThinkingLevel
	authPath                      string
	authStore                     auth.Store
	trust                         *trust.Store
	authService                   *auth.Service
	projectPlugins                []publicplugin.PluginSpec
	projectMCPServers             map[string]publicmcp.ServerSpec
	projectSkills                 config.ProjectSkillsConfig
	projectSystemPrompt           bool
	searchPolicy                  config.EffectiveSearchPolicy
	configDiagnostics             []config.Diagnostic
	projectAllowed                bool
	projectInputRoot              string
}

func initializeStartup(ctx context.Context, opts Options) (startupConfig, error) {
	var startup startupConfig
	cwd := opts.CWD
	if cwd == "" {
		var err error
		cwd, err = getwd()
		if err != nil {
			return startupConfig{}, fmt.Errorf("app: cwd: %w", err)
		}
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: abs cwd: %w", err)
	}
	globalDir := config.GlobalDir()
	tempfile.SweepStale(globalDir, []string{".auth-", ".snow-config-", ".snow-trust-"}, 24*time.Hour)
	tempfile.SweepStale(filepath.Join(globalDir, "cache", "chatgpt-models"), []string{".models-"}, 24*time.Hour)
	tempfile.SweepStale(filepath.Join(globalDir, "cache", "opencode-models"), []string{".models-"}, 24*time.Hour)

	configPath := opts.ConfigPath
	if configPath == "" {
		c, _, _ := config.DefaultPaths()
		configPath = c
	}
	loadedCfg, err := config.Load(configPath)
	if err != nil {
		return startupConfig{}, err
	}
	persistedCfg := loadedCfg
	cfg, err := config.Clone(loadedCfg)
	if err != nil {
		return startupConfig{}, err
	}
	if _, err := config.ApplyProjectSelection(&cfg, absCWD); err != nil {
		return startupConfig{}, fmt.Errorf("app: project model selection: %w", err)
	}
	// Apply CLI/SDK overrides after the remembered project selection. A provider
	// override should not carry a model selected for a different provider; that
	// otherwise turns a valid project effort/model pair into an accidental
	// unsupported pair.
	if opts.Provider != "" {
		if cfg.DefaultProvider != "" && cfg.DefaultProvider != opts.Provider && opts.Model == "" {
			cfg.DefaultModel = ""
		}
		cfg.DefaultProvider = opts.Provider
	}
	if opts.Model != "" {
		cfg.DefaultModel = opts.Model
	}
	if cfg.DefaultModel != "" && strings.TrimSpace(cfg.DefaultModel) == "" {
		return startupConfig{}, errors.New("app: model id must not be blank")
	}
	if opts.Permission != "" {
		cfg.PermissionMode = opts.Permission
	}
	if opts.Thinking != "" {
		cfg.Thinking = opts.Thinking
	}
	if opts.ReasoningSummary != "" {
		cfg.ReasoningSummary = opts.ReasoningSummary
	}
	if opts.TextVerbosity != "" {
		cfg.TextVerbosity = opts.TextVerbosity
	}
	if opts.CollaborationMode != "" {
		cfg.CollaborationMode = opts.CollaborationMode
	}
	if opts.PlanModeReasoningEffort != "" {
		cfg.PlanModeReasoningEffort = opts.PlanModeReasoningEffort
	}
	if opts.Subagents != nil {
		cfg.Subagents.Enabled = *opts.Subagents
	}
	if opts.SubagentProvider != "" {
		cfg.Subagents.DefaultProvider = opts.SubagentProvider
	}
	if opts.SubagentModel != "" {
		cfg.Subagents.DefaultModel = opts.SubagentModel
	}
	if opts.SubagentMaxConcurrency > 0 {
		cfg.Subagents.MaxConcurrentThreads = opts.SubagentMaxConcurrency
		if opts.SubagentMaxAgents == 0 && cfg.Subagents.MaxAgentsPerSession < opts.SubagentMaxConcurrency {
			cfg.Subagents.MaxAgentsPerSession = opts.SubagentMaxConcurrency
		}
	}
	if opts.SubagentMaxAgents > 0 {
		cfg.Subagents.MaxAgentsPerSession = opts.SubagentMaxAgents
	}
	if opts.SubagentMaxDepth > 0 {
		cfg.Subagents.MaxDepth = opts.SubagentMaxDepth
	}
	if err := cfg.Subagents.ValidateSubagents(); err != nil {
		return startupConfig{}, err
	}
	permMode := permission.Mode(cfg.PermissionMode)
	if permMode != permission.ModeAsk && permMode != permission.ModeAllow && permMode != permission.ModeDeny {
		return startupConfig{}, fmt.Errorf("app: invalid permission mode %q (want ask, allow, or deny)", cfg.PermissionMode)
	}
	if opts.BaseURL != "" {
		if cfg.Providers == nil {
			cfg.Providers = map[string]config.ProviderConfig{}
		}
		pc := cfg.Providers[cfg.DefaultProvider]
		pc.BaseURL = opts.BaseURL
		cfg.Providers[cfg.DefaultProvider] = pc
	}
	thinking, err := protocol.ParseThinkingLevel(cfg.Thinking)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: thinking: %w", err)
	}
	reasoningSummary, err := protocol.ParseReasoningSummary(cfg.ReasoningSummary)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: reasoning summary: %w", err)
	}
	textVerbosity, err := protocol.ParseTextVerbosity(cfg.TextVerbosity)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: text verbosity: %w", err)
	}
	collaborationMode, err := protocol.ParseCollaborationMode(cfg.CollaborationMode)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: collaboration mode: %w", err)
	}
	var planThinking *protocol.ThinkingLevel
	if cfg.PlanModeReasoningEffort != "" {
		parsed, err := protocol.ParseThinkingLevel(cfg.PlanModeReasoningEffort)
		if err != nil {
			return startupConfig{}, fmt.Errorf("app: plan mode reasoning effort: %w", err)
		}
		planThinking = &parsed
	}
	// Keep the in-memory config normalized even when older files omit these
	// newly introduced fields.
	cfg.ReasoningSummary = string(reasoningSummary)
	cfg.TextVerbosity = string(textVerbosity)

	authPath := opts.AuthPath
	if authPath == "" {
		_, a, _ := config.DefaultPaths()
		authPath = a
	}
	// Session persistence and credential persistence are independent. Ephemeral
	// conversations still use the configured auth store so OAuth refresh and
	// account-scoped model discovery work with --no-session and the SDK.
	fs, err := auth.NewFileStore(authPath)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: auth store: %w", err)
	}
	var authStore auth.Store = fs
	authService := auth.NewService(authStore)

	// persistedCfg was captured before project and CLI/SDK overlays. Interactive
	// writes mutate the latest operator config transactionally rather than
	// leaking runtime-only overrides into it.

	// Project trust store. Decisions persist to ~/.snow/trust.json.
	// NOTE: DefaultPaths returns (configPath, authPath, trustPath); the trust
	// store must get the THIRD value — wiring it to authPath corrupted
	// auth.json after the first stored credential and broke every startup.
	_, _, trustPath := config.DefaultPaths()
	tr, err := trust.New(trustPath)
	if err != nil {
		return startupConfig{}, fmt.Errorf("app: trust store: %w", err)
	}
	// Project configuration is input, not an execution boundary. Its restricted
	// extension and preference fields are read only after an allow decision.
	var projectPlugins []publicplugin.PluginSpec
	projectMCPServers := map[string]publicmcp.ServerSpec{}
	projectSkills := config.ProjectSkillsConfig{Overrides: map[string]bool{}}
	projectSystemPrompt := false
	trustResolution, err := trust.Resolve(absCWD, cfg.DefaultProjectTrust, tr)
	if err != nil {
		return startupConfig{}, err
	}
	// Non-interactive ask is deliberately fail-closed. Only an effective allow
	// reads project-local configuration and resources.
	projectAllowed := !trustResolution.Prompt && trustResolution.Level == trust.LevelAllow
	// Keep every trust-gated read pinned to the canonical path that was
	// authorized. A launch path may be a symlink and must not be able to retarget
	// the decision between resolution and resource loading.
	projectInputRoot := trustResolution.Path
	projectConfigPath := filepath.Join(projectInputRoot, ".snow", "config.json")
	if projectAllowed {
		var extensions config.ProjectExtensions
		extensions, err = config.LoadProjectExtensions(projectConfigPath)
		if err != nil {
			return startupConfig{}, err
		}
		projectPlugins = extensions.Plugins
		projectMCPServers = extensions.MCPServers
		projectSkills = extensions.Skills
		projectSystemPrompt = extensions.SystemPromptFile != nil
		if err := config.ApplyProjectPreferences(&cfg, extensions); err != nil {
			return startupConfig{}, err
		}
	}

	searchPolicy, configDiagnostics := config.LoadSearchPolicy(config.GlobalDir(), projectInputRoot, projectAllowed)

	startup = startupConfig{
		absCWD: absCWD, globalDir: globalDir, configPath: configPath,
		persistedCfg: persistedCfg, cfg: cfg, permMode: permMode,
		thinking: thinking, reasoningSummary: reasoningSummary, textVerbosity: textVerbosity,
		collaborationMode: collaborationMode, planThinking: planThinking,
		authPath: authPath, authStore: authStore, authService: authService, trust: tr, projectPlugins: projectPlugins,
		projectMCPServers: projectMCPServers, projectSkills: projectSkills,
		projectSystemPrompt: projectSystemPrompt, searchPolicy: searchPolicy,
		configDiagnostics: configDiagnostics, projectAllowed: projectAllowed,
		projectInputRoot: projectInputRoot,
	}
	return startup, nil
}
