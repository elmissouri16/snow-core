// Package app wires configuration, auth, session, tools, providers, and the
// agent into ready-to-run surfaces (CLI, TUI, print, SDK, RPC).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/artifact"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/config"
	ctxpkg "github.com/snow-core/snow/internal/context"
	goalpkg "github.com/snow-core/snow/internal/goal"
	internalmcp "github.com/snow-core/snow/internal/mcp"
	"github.com/snow-core/snow/internal/permission"
	internalplugin "github.com/snow-core/snow/internal/plugin"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/provider/chatgpt"
	"github.com/snow-core/snow/internal/provider/fake"
	"github.com/snow-core/snow/internal/provider/openaicompat"
	"github.com/snow-core/snow/internal/provider/opencodego"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/skills"
	"github.com/snow-core/snow/internal/subagent"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	toolrouter "github.com/snow-core/snow/internal/tools/router"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/internal/userinput"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

// App is the assembled runtime.
type App struct {
	// Cfg is the effective runtime configuration after trusted project overlays.
	// PersistedCfg is the global/operator configuration and must be the base for
	// every write to ConfigPath so project-only values never leak globally.
	Cfg          config.Config
	PersistedCfg config.Config
	ConfigPath   string
	AuthPath     string

	Auth       auth.Store
	Registry   *tools.SimpleRegistry
	Router     tools.Router
	Provider   provider.Provider
	ProviderID string
	Providers  map[string]provider.Provider
	// Models is the active provider catalog; AllModels is the combined live
	// snapshot used by the TUI picker and replaced on catalog refresh.
	Models            []protocol.Model
	AllModels         []protocol.Model
	Model             protocol.Model
	Perm              *permission.SimpleService
	Session           session.Store
	Agent             *agent.Agent
	Goal              *goalpkg.Controller
	Trust             *trust.Store
	PluginManager     *internalplugin.Manager
	PluginDiagnostics []internalplugin.Diagnostic
	MCPManager        *internalmcp.Manager
	MCPStatuses       []publicmcp.Status
	Skills            *skills.Registry
	SkillDiagnostics  []skills.Diagnostic
	Subagents         *subagent.Manager
	Diagnostics       []config.Diagnostic
	SearchPolicy      config.EffectiveSearchPolicy
	ProjectAllowed    bool
	ProjectInputRoot  string

	stateMu                sync.Mutex
	permissionDefault      permission.Mode
	permissionOverride     bool
	explicitAPIKey         string
	explicitAPIKeyProvider string
	modelCatalog           map[string][]protocol.Model
	runtimeSelection       *liveRuntimeSelection
	cwd                    string
	userInput              *userinput.Broker
	toolGuard              *builtin.PathGuard
	sessionHistory         *builtin.SessionBinding
	artifacts              artifact.Store
}

type liveRuntimeSelection struct {
	mu        sync.RWMutex
	provider  string
	model     protocol.Model
	providers map[string]provider.Provider
	catalogs  map[string][]protocol.Model
}

func (s *liveRuntimeSelection) childSelection(providerID, modelID string) (provider.Provider, protocol.Model, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if providerID == "" {
		providerID = s.provider
	}
	p, ok := s.providers[providerID]
	if !ok {
		return nil, protocol.Model{}, fmt.Errorf("app: subagent provider %q is unavailable", providerID)
	}
	if modelID == "" && s.model.Provider == providerID {
		modelID = s.model.ID
	}
	if modelID == "" {
		if defaults, ok := p.(interface{ DefaultModel() protocol.Model }); ok {
			modelID = defaults.DefaultModel().ID
		}
		if modelID == "" && len(s.catalogs[providerID]) > 0 {
			modelID = s.catalogs[providerID][0].ID
		}
	}
	for _, candidate := range s.catalogs[providerID] {
		if candidate.ID == modelID {
			return p, candidate, nil
		}
	}
	// The active model may be an explicit custom ID intentionally preserved
	// when discovery is unavailable. Children inheriting that exact selection
	// must not require it to appear in the remote catalog.
	if s.model.Provider == providerID && s.model.ID == modelID {
		return p, s.model.Clone(), nil
	}
	return nil, protocol.Model{}, fmt.Errorf("app: subagent model %q is unavailable for provider %s", modelID, providerID)
}

func (s *liveRuntimeSelection) availableModels() []protocol.Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	providers := make([]string, 0, len(s.catalogs))
	for id := range s.catalogs {
		providers = append(providers, id)
	}
	sort.Strings(providers)
	var out []protocol.Model
	for _, id := range providers {
		for _, model := range s.catalogs[id] {
			out = append(out, model.Clone())
		}
	}
	return out
}

// Options control app assembly.
type Options struct {
	CWD                     string
	ConfigPath              string
	AuthPath                string
	Provider                string
	Model                   string
	APIKey                  string
	Permission              string   // ask|allow|deny
	SessionPath             string   // empty → create new; or existing .db to resume
	RequireExistingSession  bool     // reject missing/non-Snow SessionPath instead of creating it
	Tools                   []string // subset allowlist; empty = all builtins
	SystemPrompt            string
	Thinking                string
	ReasoningSummary        string
	TextVerbosity           string
	CollaborationMode       string
	PlanModeReasoningEffort string
	NoSession               bool   // in-memory session (SDK ephemeral)
	UseFake                 bool   // force fake provider (demo/tests)
	BaseURL                 string // active provider base URL override
	Plugins                 []publicplugin.PluginSpec
	GoPlugins               []publicplugin.Plugin
	NoPlugins               bool
	MCPServers              []publicmcp.ServerSpec
	NoMCP                   bool
	SkillDirs               []string
	NoSkills                bool
	// UserInputHandler answers ask_user calls for embedded/headless clients.
	// Nil keeps the tool directly visible but makes calls fail fast until an
	// interactive surface enables manual replies.
	UserInputHandler userinput.Handler
	// Subagents overrides config enablement when non-nil. Enabling never implies
	// recursive spawning or mutation.
	Subagents              *bool
	SubagentProvider       string
	SubagentModel          string
	SubagentMaxConcurrency int
	SubagentMaxAgents      int
	SubagentMaxDepth       int
}

func skillNamesForRegistry(catalog *skills.Registry, registry tools.Registry) map[string]bool {
	names := make(map[string]bool)
	if catalog == nil || registry == nil {
		return names
	}
	descriptor, ok := registry.Descriptor("activate_skill")
	if !ok || descriptor.Owner != "skills" {
		return names
	}
	for _, skill := range catalog.List() {
		names[skill.Name] = true
	}
	return names
}

func skillPromptForRegistry(catalog *skills.Registry, registry tools.Registry) string {
	if len(skillNamesForRegistry(catalog, registry)) == 0 {
		return ""
	}
	reader, ok := registry.Descriptor("read_skill_resource")
	return catalog.CatalogPromptForTools(ok && reader.Owner == "skills")
}

// DefaultPaths resolves config/auth paths from the environment.
func DefaultPaths() (configPath, authPath string) {
	c, a, _ := config.DefaultPaths()
	return c, a
}

// ProjectTrustPreflight is the side-effect-free trust decision needed before
// an interactive surface constructs the runtime. Store persists the eventual
// exact-project choice.
type ProjectTrustPreflight struct {
	Resolution trust.Resolution
	Store      *trust.Store
}

// InspectProjectTrust loads only global policy and the user trust store. It
// never reads project-local configuration or resources.
func InspectProjectTrust(opts Options) (ProjectTrustPreflight, error) {
	cwd := opts.CWD
	if cwd == "" {
		var err error
		cwd, err = getwd()
		if err != nil {
			return ProjectTrustPreflight{}, fmt.Errorf("app: cwd: %w", err)
		}
	}
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath, _, _ = config.DefaultPaths()
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return ProjectTrustPreflight{}, err
	}
	_, _, trustPath := config.DefaultPaths()
	store, err := trust.New(trustPath)
	if err != nil {
		return ProjectTrustPreflight{}, fmt.Errorf("app: trust store: %w", err)
	}
	resolution, err := trust.Resolve(cwd, cfg.DefaultProjectTrust, store)
	if err != nil {
		return ProjectTrustPreflight{}, err
	}
	return ProjectTrustPreflight{Resolution: resolution, Store: store}, nil
}

// New assembles the app.
func New(ctx context.Context, opts Options) (result *App, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cwd := opts.CWD
	if cwd == "" {
		var err error
		cwd, err = getwd()
		if err != nil {
			return nil, fmt.Errorf("app: cwd: %w", err)
		}
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("app: abs cwd: %w", err)
	}

	configPath := opts.ConfigPath
	if configPath == "" {
		c, _, _ := config.DefaultPaths()
		configPath = c
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	// Apply CLI/SDK overrides. A provider override should not carry a model
	// selected for a different configured provider; that otherwise turns a
	// valid global effort/model pair into an accidental unsupported pair.
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
		return nil, errors.New("app: model id must not be blank")
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
		return nil, err
	}
	permMode := permission.Mode(cfg.PermissionMode)
	if permMode != permission.ModeAsk && permMode != permission.ModeAllow && permMode != permission.ModeDeny {
		return nil, fmt.Errorf("app: invalid permission mode %q (want ask, allow, or deny)", cfg.PermissionMode)
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
		return nil, fmt.Errorf("app: thinking: %w", err)
	}
	reasoningSummary, err := protocol.ParseReasoningSummary(cfg.ReasoningSummary)
	if err != nil {
		return nil, fmt.Errorf("app: reasoning summary: %w", err)
	}
	textVerbosity, err := protocol.ParseTextVerbosity(cfg.TextVerbosity)
	if err != nil {
		return nil, fmt.Errorf("app: text verbosity: %w", err)
	}
	collaborationMode, err := protocol.ParseCollaborationMode(cfg.CollaborationMode)
	if err != nil {
		return nil, fmt.Errorf("app: collaboration mode: %w", err)
	}
	var planThinking *protocol.ThinkingLevel
	if cfg.PlanModeReasoningEffort != "" {
		parsed, err := protocol.ParseThinkingLevel(cfg.PlanModeReasoningEffort)
		if err != nil {
			return nil, fmt.Errorf("app: plan mode reasoning effort: %w", err)
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
		return nil, fmt.Errorf("app: auth store: %w", err)
	}
	var authStore auth.Store = fs

	// Preserve the operator/global layer before applying any trust-gated project
	// preferences. CLI/SDK overrides retain their historical persistence behavior
	// when the interactive settings panel later saves a value.
	persistedCfg := cfg

	// Project trust store. Decisions persist to ~/.snow/trust.json.
	// NOTE: DefaultPaths returns (configPath, authPath, trustPath); the trust
	// store must get the THIRD value — wiring it to authPath corrupted
	// auth.json after the first stored credential and broke every startup.
	_, _, trustPath := config.DefaultPaths()
	tr, err := trust.New(trustPath)
	if err != nil {
		return nil, fmt.Errorf("app: trust store: %w", err)
	}
	// Project configuration is input, not an execution boundary. Its restricted
	// extension and preference fields are read only after an allow decision.
	var projectPlugins []publicplugin.PluginSpec
	projectMCPServers := map[string]publicmcp.ServerSpec{}
	projectSkills := config.ProjectSkillsConfig{Overrides: map[string]bool{}}
	projectSystemPrompt := false
	var pluginDiagnostics []internalplugin.Diagnostic
	trustResolution, err := trust.Resolve(absCWD, cfg.DefaultProjectTrust, tr)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		projectPlugins = extensions.Plugins
		projectMCPServers = extensions.MCPServers
		projectSkills = extensions.Skills
		projectSystemPrompt = extensions.SystemPromptFile != nil
		if err := config.ApplyProjectPreferences(&cfg, extensions); err != nil {
			return nil, err
		}
	} else if _, statErr := os.Stat(projectConfigPath); statErr == nil {
		pluginDiagnostics = append(pluginDiagnostics, internalplugin.Diagnostic{PluginID: ".snow/config.json", Status: "trust-blocked", Message: "project configuration requires an explicit trust allow"})
	}

	searchPolicy, configDiagnostics := config.LoadSearchPolicy(config.GlobalDir(), projectInputRoot, projectAllowed)

	// Tools. Pin the canonical root once so later launch-path replacement cannot
	// retarget the file capability.
	reg := tools.NewRegistry()
	toolGuard := builtin.NewPathGuard([]string{absCWD}, absCWD)
	guardCommitted := false
	defer func() {
		if !guardCommitted {
			retErr = errors.Join(retErr, toolGuard.Close())
		}
	}()
	toolOpts := builtin.Options{
		MaxOutputBytes: cfg.ToolOutputLimit(),
		BashTimeout:    cfg.BashTimeout(),
		Roots:          []string{absCWD},
		CWD:            absCWD,
		Guard:          toolGuard,
		SearchPolicy:   searchPolicy,
	}
	// Register builtins. The explicit tool allowlist is applied after Agent
	// Skills register their built-in capabilities so it remains a true upper
	// bound for every built-in tool.
	builtin.RegisterBuiltins(reg, toolOpts)

	// Agent Skills use metadata-only startup discovery. Project locations are
	// trust-gated; full SKILL.md bodies and resources load only through the
	// dedicated tools after the model or user activates a skill.
	var skillCatalog *skills.Registry
	if !opts.NoSkills {
		skillDirs := append(append([]string(nil), cfg.Skills.Dirs...), opts.SkillDirs...)
		skillDisabled := cfg.Skills.Disabled
		skillDisabledReason := "disabled by global skills policy"
		skillOverrides := make(map[string]bool, len(cfg.Skills.Overrides)+len(projectSkills.Overrides))
		skillOverrideReasons := make(map[string]string, len(skillOverrides))
		for name, enabled := range cfg.Skills.Overrides {
			skillOverrides[name] = enabled
			skillOverrideReasons[name] = "disabled by global named skill policy"
		}
		if projectSkills.Disabled != nil {
			skillDisabled = *projectSkills.Disabled
			skillDisabledReason = "disabled by project skills policy"
			clear(skillOverrides)
			clear(skillOverrideReasons)
		}
		for name, enabled := range projectSkills.Overrides {
			skillOverrides[name] = enabled
			skillOverrideReasons[name] = "disabled by project named skill policy"
		}
		skillCatalog = skills.Discover(skills.Options{
			CWD: projectInputRoot, SnowHome: config.GlobalDir(), ProjectTrusted: projectAllowed,
			ExtraDirs: skillDirs, IncludeClaude: cfg.Skills.IncludeClaude, IncludeBuiltins: true,
			Disabled: skillDisabled, DisabledReason: skillDisabledReason,
			Overrides: skillOverrides, OverrideReasons: skillOverrideReasons,
		})
		if err := skills.RegisterTools(reg, skillCatalog); err != nil {
			return nil, fmt.Errorf("app: skills: %w", err)
		}
	}
	if len(opts.Tools) > 0 {
		allowed := make(map[string]bool, len(opts.Tools))
		for _, name := range opts.Tools {
			allowed[name] = true
		}
		activationAllowed := allowed["activate_skill"]
		filtered := tools.NewRegistry()
		for _, descriptor := range reg.Descriptors() {
			// The resource reader is meaningful only with tier-one disclosure and
			// activation, and otherwise leaks a names-only enum surface.
			if descriptor.Schema.Name == "read_skill_resource" && !activationAllowed {
				continue
			}
			if allowed[descriptor.Schema.Name] {
				if err := filtered.RegisterDescriptor(descriptor); err != nil {
					return nil, fmt.Errorf("app: filter built-in tool %s: %w", descriptor.Schema.Name, err)
				}
			}
		}
		reg = filtered
		if !activationAllowed && skillCatalog != nil {
			skillCatalog.DisableAll("activate_skill disabled by explicit tool allowlist")
		}
	}
	defer func() {
		if retErr != nil && skillCatalog != nil {
			retErr = errors.Join(retErr, skillCatalog.Close())
		}
	}()
	// Provider.
	providerID := cfg.DefaultProvider
	if providerID == "" {
		providerID = "opencode-go"
	}
	newOpenCode := func() (provider.Provider, error) {
		ocCfg := opencodego.Config{}
		if providerID == opencodego.ProviderID {
			ocCfg.APIKey = opts.APIKey
		}
		if ocCfg.APIKey == "" {
			if stored, ok := authStore.Get(opencodego.ProviderID); ok {
				// ListModels has no credential argument in the stable provider
				// interface, so provide the stored API key as the adapter's
				// lowest-priority fallback for startup discovery. Chat still
				// receives the original credential through Agent.resolveCreds.
				ocCfg.APIKey = stored.Key
			}
		}
		if pc, ok := cfg.Providers["opencode-go"]; ok {
			ocCfg.BaseURL = pc.BaseURL
			ocCfg.DefaultModel = pc.DefaultModel
		}
		if opts.BaseURL != "" && providerID == "opencode-go" {
			ocCfg.BaseURL = opts.BaseURL
		}
		oc, err := opencodego.New(ocCfg)
		if err != nil {
			return nil, fmt.Errorf("app: opencode-go: %w", err)
		}
		return oc, nil
	}

	newChatGPT := func() *chatgpt.Provider {
		cgCfg := chatgpt.Config{Store: authStore, CacheRoot: filepath.Join(config.GlobalDir(), "cache", "chatgpt-models")}
		if pc, ok := cfg.Providers["chatgpt"]; ok {
			cgCfg.BaseURL = pc.BaseURL
		}
		if providerID == "chatgpt" && opts.BaseURL != "" {
			cgCfg.BaseURL = opts.BaseURL
		}
		return chatgpt.New(cgCfg)
	}

	newOpenAICompatible := func() (*openaicompat.Provider, error) {
		compatibleCfg := openaicompat.Config{}
		if pc, ok := cfg.Providers[openaicompat.ProviderID]; ok {
			compatibleCfg.BaseURL = pc.BaseURL
			compatibleCfg.DefaultModel = pc.DefaultModel
		}
		if providerID == openaicompat.ProviderID {
			if opts.BaseURL != "" {
				compatibleCfg.BaseURL = opts.BaseURL
			}
			compatibleCfg.APIKey = opts.APIKey
		}
		if stored, ok := authStore.Get(openaicompat.ProviderID); ok {
			// Startup discovery has no credential argument. Keep this key out of
			// the adapter's runtime fallback so logout takes effect immediately.
			compatibleCfg.DiscoveryAPIKey = stored.Key
		}
		return openaicompat.New(compatibleCfg)
	}

	var prov provider.Provider
	switch providerID {
	case "fake":
		prov = fake.NewWithModels(nil)
	case "chatgpt":
		prov = newChatGPT()
	case "opencode-go":
		prov, err = newOpenCode()
		if err != nil {
			return nil, err
		}
	case openaicompat.ProviderID:
		compatible, compatibleErr := newOpenAICompatible()
		if compatibleErr != nil {
			return nil, fmt.Errorf("app: %s: %w", openaicompat.ProviderID, compatibleErr)
		}
		if !compatible.Configured() {
			return nil, errors.New("app: openai-compatible base URL is required; pass --base-url or configure providers.openai-compatible.base_url")
		}
		prov = compatible
	default:
		return nil, fmt.Errorf("app: unsupported provider %q", providerID)
	}

	// Keep catalogs/providers for every user-facing provider so the model picker
	// and subagent runtime can switch without rebuilding the app.
	providers := map[string]provider.Provider{providerID: prov}
	if providerID != "fake" {
		if _, ok := providers["chatgpt"]; !ok {
			providers["chatgpt"] = newChatGPT()
		}
		if _, ok := providers["opencode-go"]; !ok {
			other, openCodeErr := newOpenCode()
			if openCodeErr != nil {
				return nil, openCodeErr
			}
			providers["opencode-go"] = other
		}
		if _, ok := providers[openaicompat.ProviderID]; !ok {
			compatible, compatibleErr := newOpenAICompatible()
			if compatibleErr != nil {
				return nil, fmt.Errorf("app: %s: %w", openaicompat.ProviderID, compatibleErr)
			}
			providers[openaicompat.ProviderID] = compatible
		}
	}

	// Session.
	var st session.Store
	if opts.NoSession {
		st = session.NewMemoryStore(session.Options{CWD: absCWD})
	} else if opts.SessionPath != "" {
		if opts.RequireExistingSession {
			st, err = session.OpenSQLiteStore(opts.SessionPath, absCWD, session.Options{})
		} else {
			st, err = session.NewSQLiteStore(opts.SessionPath, absCWD, session.Options{})
		}
		if err != nil {
			return nil, fmt.Errorf("app: open session: %w", err)
		}
	} else {
		idx := session.NewFileIndex(session.DefaultSessionsRoot())
		st, err = idx.Create(absCWD)
		if err != nil {
			return nil, fmt.Errorf("app: create session: %w", err)
		}
	}

	var (
		inputBroker *userinput.Broker
		manager     *internalplugin.Manager
		mcpManager  *internalmcp.Manager
		subManager  *subagent.Manager
		router      tools.Router
		ag          *agent.Agent
	)
	committed := false
	defer func() {
		if committed {
			return
		}
		var cleanupErrs []error
		if subManager != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			cleanupErrs = append(cleanupErrs, subManager.Close(closeCtx))
			cancel()
		}
		if ag != nil {
			ag.Close()
		}
		if inputBroker != nil {
			inputBroker.Close()
		}
		if mcpManager != nil {
			cleanupErrs = append(cleanupErrs, mcpManager.Close())
		}
		if manager != nil {
			cleanupErrs = append(cleanupErrs, manager.Close(context.Background()))
		}
		if router != nil {
			cleanupErrs = append(cleanupErrs, router.Close())
		}
		cleanupErrs = append(cleanupErrs, st.Close())
		retErr = errors.Join(retErr, errors.Join(cleanupErrs...))
	}()

	goalController, err := goalpkg.New(st, config.GlobalDir(), nil)
	if err != nil {
		return nil, fmt.Errorf("app: goal controller: %w", err)
	}
	allowedGoalTool := func(name string) bool {
		if len(opts.Tools) == 0 {
			return true
		}
		for _, v := range opts.Tools {
			if v == name {
				return true
			}
		}
		return false
	}
	for _, tool := range goalpkg.Tools(goalController) {
		if allowedGoalTool(tool.Schema().Name) {
			if err := reg.Register(tool); err != nil {
				return nil, err
			}
		}
	}
	// Session history and private-artifact capabilities are deferred and read-only.
	// Artifacts remain outside project roots and are addressable only by opaque IDs
	// scoped to the currently bound session.
	sessionQuery := session.NewQueryEngine(session.NewFileIndex(session.DefaultSessionsRoot()), absCWD)
	sessionHistory := builtin.NewSessionBinding(st)
	artifactStore, err := artifact.NewLocalStore(filepath.Join(config.GlobalDir(), "artifacts"), cfg.Compaction.ArtifactMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("app: artifacts: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, artifactStore.Close())
		}
	}()
	for _, tool := range []tools.Tool{
		builtin.NewSessionSearch(sessionQuery, sessionHistory),
		builtin.NewSessionReference(sessionQuery, sessionHistory),
		builtin.NewArtifactRead(artifactStore, sessionHistory),
		builtin.NewArtifactGrep(artifactStore, sessionHistory),
	} {
		if allowedGoalTool(tool.Schema().Name) {
			if err := reg.Register(tool); err != nil {
				return nil, err
			}
		}
	}

	// Permission service (deny-by-default headless; TUI replaces asker).
	perm := permission.NewService(permMode, permission.DenyAll{})

	// Fetch every available provider catalog during startup. Providers may return
	// cached/bundled fallbacks; authenticated refreshes replace these snapshots.
	modelCatalog := make(map[string][]protocol.Model, len(providers))
	modelCatalogErrors := make(map[string]error, len(providers))
	for id, p := range providers {
		models, listErr := p.ListModels(ctx)
		modelCatalog[id] = normalizeProviderModels(id, models)
		modelCatalogErrors[id] = listErr
	}
	models := modelCatalog[providerID]
	if providerID == openaicompat.ProviderID && cfg.DefaultModel == "" && len(models) == 0 {
		if listErr := modelCatalogErrors[providerID]; listErr != nil {
			return nil, fmt.Errorf("app: openai-compatible model discovery failed; pass --model or configure default_model: %w", listErr)
		}
		return nil, errors.New("app: openai-compatible model discovery returned no models; pass --model or configure default_model")
	}
	var allModels []protocol.Model
	seenProviders := make(map[string]bool)
	for _, id := range []string{providerID, "opencode-go", "openai-compatible", "chatgpt", "fake"} {
		if seenProviders[id] {
			continue
		}
		if catalog, ok := modelCatalog[id]; ok {
			allModels = append(allModels, catalog...)
			seenProviders[id] = true
		}
	}
	// Include any future/custom providers after built-ins.
	for id, catalog := range modelCatalog {
		if !seenProviders[id] {
			allModels = append(allModels, catalog...)
		}
	}

	// Model resolution.
	model := protocol.Model{Provider: providerID, ID: cfg.DefaultModel, SupportsTools: true}
	configuredFound := false
	if model.ID != "" {
		for _, candidate := range models {
			if candidate.ID == model.ID {
				model = candidate
				configuredFound = true
				break
			}
		}
	}
	// Account-scoped providers must not retain a configured model omitted by
	// the selected account catalog. Other providers preserve explicit unknown
	// model IDs for compatible custom gateways and CLI use.
	if model.ID != "" && !configuredFound && modelCatalogAuthoritative(prov) {
		model.ID = ""
	}
	// Prefer the provider's documented default when it exists in the catalog;
	// otherwise fall back to the first catalog entry. Without this, the default
	// silently became whatever the live catalog listed first.
	if model.ID == "" {
		if dm, ok := prov.(interface{ DefaultModel() protocol.Model }); ok {
			dflt := dm.DefaultModel()
			if dflt.ID != "" {
				for _, candidate := range models {
					if candidate.ID == dflt.ID {
						model = candidate
						break
					}
				}
			}
		}
		if model.ID == "" {
			if len(models) > 0 {
				model = models[0]
			} else {
				model.ID = "default"
			}
		}
	}

	runtimeSelection := &liveRuntimeSelection{provider: providerID, model: model, providers: providers, catalogs: modelCatalog}

	// Host (path roots + progress bridge).
	inputBroker = userinput.New(opts.UserInputHandler)
	host := &toolHost{cwd: absCWD, roots: []string{absCWD}, perm: perm, reg: reg, userInput: inputBroker}

	// Extensions are initialized after builtins and the session exist, but
	// before the agent is constructed. This makes registration deterministic
	// and lets every plugin receive the same session/cwd capabilities. When
	// project input was authorized, pin relative extension execution to the same
	// canonical root rather than the possibly retargetable launch alias.
	extensionCWD := absCWD
	if projectAllowed {
		extensionCWD = projectInputRoot
	}
	manager = internalplugin.NewManager(reg, internalplugin.ManagerOptions{
		CWD: extensionCWD, SessionID: st.ID(), HostVersion: "snow-core", HostCapabilities: []string{"tools", "events"},
		MaxProgressBytes: cfg.ToolOutputLimit(), MaxOutputBytes: cfg.ToolOutputLimit(),
	})
	var allPluginSpecs []publicplugin.PluginSpec
	if opts.NoPlugins {
		allPluginSpecs = mergeDisabledPluginSpecs(cfg.Plugins, projectPlugins, opts.Plugins)
	} else {
		allPluginSpecs, err = mergePluginSpecs(cfg.Plugins, projectPlugins, opts.Plugins)
		if err != nil {
			return nil, fmt.Errorf("app: plugin configuration: %w", err)
		}
	}
	if !opts.NoPlugins {
		for _, p := range opts.GoPlugins {
			if err := manager.LoadGo(p); err != nil {
				return nil, fmt.Errorf("app: plugin: %w", err)
			}
		}
		for _, spec := range allPluginSpecs {
			if err := manager.LoadExternal(spec); err != nil {
				return nil, fmt.Errorf("app: plugin %s: %w", spec.ID, err)
			}
		}
		if err := manager.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("app: plugin initialization: %w", err)
		}
	} else {
		for _, spec := range allPluginSpecs {
			pluginDiagnostics = append(pluginDiagnostics, internalplugin.Diagnostic{PluginID: spec.ID, Status: "disabled", Message: "plugin loading disabled by --no-plugins"})
		}
	}

	// MCP servers are independent of Snow's plugin protocol. The official Go
	// SDK performs protocol negotiation and lifecycle handling; negotiated
	// tools/resources/prompts are adapted into the same permissioned registry.
	mcpSpecs := mergeMCPServers(cfg.MCPServers, projectMCPServers, opts.MCPServers)
	var mcpStatuses []publicmcp.Status
	if !opts.NoMCP {
		mcpManager = internalmcp.NewManager(reg, internalmcp.Options{
			CWD: extensionCWD, Roots: []string{absCWD}, HostName: "snow", HostVersion: "0.1.0-dev", MaxOutputBytes: cfg.ToolOutputLimit(),
		})
		mcpManager.ConnectAll(ctx, mcpSpecs)
		mcpStatuses = mcpManager.Statuses()
	} else {
		for _, spec := range mcpSpecs {
			mcpStatuses = append(mcpStatuses, publicmcp.Status{ID: spec.ID, Transport: spec.EffectiveTransport(), Message: "disabled by --no-mcp"})
		}
	}

	// Collaboration tools are direct and registered before deferred-router
	// indexing so the model always receives the complete control set together.
	if cfg.Subagents.Enabled {
		validateChildSelection := func(label, providerOverride, modelID string) error {
			if providerOverride == "" && modelID == "" {
				return nil
			}
			childProvider := providerOverride
			if childProvider == "" {
				childProvider = providerID
			}
			if _, _, err := runtimeSelection.childSelection(childProvider, modelID); err != nil {
				return fmt.Errorf("app: %s references unavailable selection %s/%s: %w", label, childProvider, modelID, err)
			}
			return nil
		}
		if err := validateChildSelection("subagent defaults", cfg.Subagents.DefaultProvider, cfg.Subagents.DefaultModel); err != nil {
			return nil, err
		}
		for name, role := range cfg.Subagents.Roles {
			roleProvider := role.Provider
			if roleProvider == "" {
				roleProvider = cfg.Subagents.DefaultProvider
			}
			if err := validateChildSelection(fmt.Sprintf("subagent role %q", name), roleProvider, role.Model); err != nil {
				return nil, err
			}
		}
		roles := make(map[string]subagent.Role, len(cfg.Subagents.Roles))
		for name, role := range cfg.Subagents.Roles {
			roles[name] = subagent.Role{Name: name, Description: role.Description, System: role.System, Provider: role.Provider, Model: role.Model, Thinking: role.Thinking, Tools: append([]string(nil), role.Tools...), AllowMutation: role.AllowMutation}
		}
		subManager = subagent.New(ctx, subagent.Limits{
			MaxConcurrentThreads: cfg.Subagents.MaxConcurrentThreads, MaxAgentsPerSession: cfg.Subagents.MaxAgentsPerSession,
			MaxDepth: cfg.Subagents.MaxDepth, MinWait: time.Duration(cfg.Subagents.MinWaitTimeoutMS) * time.Millisecond,
			DefaultWait: time.Duration(cfg.Subagents.DefaultWaitTimeoutMS) * time.Millisecond, MaxWait: time.Duration(cfg.Subagents.MaxWaitTimeoutMS) * time.Millisecond,
			TaskTimeout: time.Duration(cfg.Subagents.TaskTimeoutMS) * time.Millisecond, MaxResultBytes: cfg.Subagents.MaxResultBytes,
			Recursive: cfg.Subagents.Recursive, Durable: cfg.Subagents.Durable, AllowMutation: cfg.Subagents.AllowMutation,
			ExposeChildToolEvents: cfg.Subagents.ExposeChildToolEvents, DefaultProvider: cfg.Subagents.DefaultProvider, DefaultModel: cfg.Subagents.DefaultModel, DefaultRole: cfg.Subagents.DefaultRole, Roles: roles,
		})
		subManager.SetModelCatalog(runtimeSelection.availableModels)
		subManager.SetModelSelection(func(providerID, modelID string) (protocol.Model, error) {
			_, selected, err := runtimeSelection.childSelection(providerID, modelID)
			return selected, err
		})
		for _, tool := range subagent.Tools(subManager, subagent.Caller{Path: protocol.RootAgentPath}) {
			if err := reg.RegisterDescriptor(subagent.ToolDescriptor(tool)); err != nil {
				return nil, fmt.Errorf("app: register subagent tool: %w", err)
			}
		}
	}

	// Deferred schemas are indexed only after every startup registrar has
	// completed. Existing tools have no discovery metadata and remain direct.
	descriptors := reg.Descriptors()
	needsRouter := false
	for _, desc := range descriptors {
		if tools.IsDeferred(desc) {
			needsRouter = true
			break
		}
	}
	// A connected MCP server may advertise a mutable tools catalog that starts
	// empty. Create the router now so a later tools/list_changed notification can
	// make its first tool discoverable without restarting Snow.
	if !needsRouter {
		for _, status := range mcpStatuses {
			if status.Connected && slices.Contains(status.Capabilities, "tools") {
				needsRouter = true
				break
			}
		}
	}
	if needsRouter {
		router = toolrouter.New(descriptors)
		if err := reg.Register(builtin.NewSearchTools(router, reg)); err != nil {
			return nil, fmt.Errorf("app: register search_tools: %w", err)
		}
	}
	if mcpManager != nil && router != nil {
		catalogChanged := func(candidate []tools.ToolDescriptor) error {
			if refreshable, ok := router.(tools.RefreshableRouter); ok {
				return refreshable.Refresh(candidate)
			}
			return nil
		}
		mcpManager.SetCatalogChanged(catalogChanged)
		// Reconcile after installing the callback while holding the registry's
		// catalog boundary. A list_changed event can therefore occur before this
		// snapshot or after it, but cannot overtake it and leave a stale router.
		if err := reg.PrepareCurrent(catalogChanged); err != nil {
			return nil, fmt.Errorf("app: reconcile tool router: %w", err)
		}
	}

	// Context assembly. An explicit SDK prompt wins without touching a configured
	// file. Trusted project paths override the global file and remain confined to
	// the canonical project root; otherwise relative paths use the global config
	// directory. The embedded Markdown preamble remains the final fallback.
	preamble := opts.SystemPrompt
	if preamble == "" && cfg.SystemPromptFile != "" {
		promptBase := filepath.Dir(configPath)
		promptRoot := ""
		if projectSystemPrompt {
			promptBase = projectInputRoot
			promptRoot = projectInputRoot
		}
		preamble, err = loadSystemPromptFile(cfg.SystemPromptFile, promptBase, promptRoot, cfg.ContextCapBytes)
		if err != nil {
			return nil, fmt.Errorf("app: system prompt file: %w", err)
		}
	}
	loader := ctxpkg.NewLoader(cfg.ContextCapBytes, false)
	assembly := loader.Assemble(absCWD, preamble, "")
	baseSystemPrompt := assembly.Render()
	if mcpManager != nil {
		if catalog := mcpManager.CatalogPrompt(); catalog != "" {
			baseSystemPrompt += "\n\n" + catalog
		}
	}
	if subManager != nil {
		baseSystemPrompt += "\n\n" + subagentPromptGuidance
	}
	systemPrompt := baseSystemPrompt
	skillNames := skillNamesForRegistry(skillCatalog, reg)
	if catalog := skillPromptForRegistry(skillCatalog, reg); catalog != "" {
		systemPrompt += "\n\n" + catalog
	}

	initialMode := collaborationMode
	if opts.CollaborationMode == "" && opts.SessionPath != "" {
		initialMode = "" // restore the persisted active-branch mode
	}
	ag, err = agent.New(agent.Options{
		Provider:          prov,
		Registry:          reg,
		Session:           st,
		Permission:        perm,
		ToolHost:          host,
		Router:            router,
		SystemPrompt:      systemPrompt,
		Model:             model,
		Thinking:          thinking,
		ReasoningSummary:  reasoningSummary,
		TextVerbosity:     textVerbosity,
		CollaborationMode: initialMode,
		PlanThinking:      planThinking,
		Goal:              goalController,
		Auth:              authStore,
		APIKey:            opts.APIKey,
		APIKeyProvider:    providerID,
		SkillNames:        skillNames,
		Artifacts:         artifactStore,
		Compaction: agent.CompactionOptions{RetainTokens: cfg.Compaction.RetainTokens, MinRetainedTurns: cfg.Compaction.MinRetainedTurns,
			SummaryMaxTokens: cfg.Compaction.SummaryMaxTokens, Fallback: cfg.Compaction.Fallback, Guidance: cfg.Compaction.Guidance,
			AutoThresholdPercent: cfg.Compaction.AutoThresholdPercent, ToolResultInlineBytes: cfg.Compaction.ToolResultInlineBytes,
			HistoricalToolResultThreshold: cfg.Compaction.HistoricalToolResultThreshold},
	})
	if err != nil {
		return nil, fmt.Errorf("app: agent: %w", err)
	}
	host.emitUserInput = ag.EmitUserInputRequest
	host.inEventCallback = ag.InEventCallback
	goalController.SetEmitter(func(ev protocol.AgentEvent) { ag.Publish(ev) })

	if subManager != nil {
		factory := subagent.ChildFactoryFunc(func(childCtx context.Context, spec subagent.ChildSpec) (subagent.ChildRuntime, error) {
			var childStore session.Store
			if spec.Restore {
				if spec.SessionPath == "" {
					return nil, errors.New("app: restored subagent has no child session path")
				}
				if err := ensurePrivateChildDirectory(filepath.Dir(spec.SessionPath)); err != nil {
					return nil, err
				}
				if info, err := os.Stat(spec.SessionPath); err != nil || info.IsDir() {
					if err == nil {
						err = errors.New("child session path is a directory")
					}
					return nil, fmt.Errorf("app: restored child session unavailable: %w", err)
				}
				opened, err := session.NewSQLiteStore(spec.SessionPath, absCWD, session.Options{})
				if err != nil {
					return nil, err
				}
				childStore = opened
			} else {
				forked, err := subagent.ForkContext(spec.ParentMessages, spec.ForkTurns, absCWD, spec.State.Agent.ThreadID)
				if err != nil {
					return nil, err
				}
				if cfg.Subagents.Durable && spec.SessionPath != "" {
					messages, err := forked.Messages()
					_ = forked.Close()
					if err != nil {
						return nil, err
					}
					if err := ensurePrivateChildDirectory(filepath.Dir(spec.SessionPath)); err != nil {
						return nil, err
					}
					opened, err := session.NewSQLiteStore(spec.SessionPath, absCWD, session.Options{ID: spec.State.Agent.ThreadID})
					if err != nil {
						return nil, err
					}
					parent := opened.BranchTip()
					for i := range messages {
						msg := messages[i]
						msg.ParentID = parent
						if err := opened.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, ParentID: parent, Message: &msg}); err != nil {
							_ = opened.Close()
							return nil, err
						}
						parent = msg.ID
					}
					childStore = opened
				} else {
					childStore = forked
				}
			}

			childReg, capabilities, err := cloneChildRegistry(reg, spec.Role, cfg.Subagents.AllowMutation)
			if err != nil {
				_ = childStore.Close()
				return nil, err
			}
			if cfg.Subagents.Recursive && spec.State.Agent.Depth < cfg.Subagents.MaxDepth {
				caller := subagent.Caller{ThreadID: spec.State.Agent.ThreadID, Path: spec.State.Agent.Path}
				for _, tool := range subagent.Tools(subManager, caller) {
					if err := childReg.RegisterDescriptor(subagent.ToolDescriptor(tool)); err != nil {
						_ = childStore.Close()
						return nil, err
					}
				}
			}
			// Shell, mutation, and recursive delegation calls use the live root
			// operator policy. Read-only children can retain deny-all because
			// RiskRead is allowed by that service; this keeps headless children
			// safe while preserving normal TUI attribution for bash approvals.
			childPerm := childPermissionService(perm, capabilities, cfg.Subagents.Recursive)
			childHost := &toolHost{cwd: absCWD, roots: []string{absCWD}, perm: childPerm, reg: childReg}
			childProvider, childModel, err := runtimeSelection.childSelection(spec.State.Provider, spec.State.Model)
			if err != nil {
				_ = childStore.Close()
				return nil, err
			}
			childSkillNames := skillNamesForRegistry(skillCatalog, childReg)
			childSystem := baseSystemPrompt
			if catalog := skillPromptForRegistry(skillCatalog, childReg); catalog != "" {
				childSystem += "\n\n" + catalog
			}
			childSystem += "\n\n<subagent>\nYou are " + string(spec.State.Agent.Path) + ", an independent child agent. Complete the assigned task and return a concise final answer to your parent. The filesystem is shared with peers and is not a sandbox; do not overwrite peer work.\n"
			if spec.Role.System != "" {
				childSystem += spec.Role.System + "\n"
			}
			childSystem += "</subagent>"
			child, err := agent.New(agent.Options{Provider: childProvider, Registry: childReg, Session: childStore, Permission: childPerm, ToolHost: childHost,
				SystemPrompt: childSystem, Model: childModel, Thinking: spec.State.Thinking, ReasoningSummary: reasoningSummary,
				TextVerbosity: textVerbosity, CollaborationMode: protocol.ModeDefault, Auth: authStore, APIKey: opts.APIKey, APIKeyProvider: providerID, Identity: spec.State.Agent.Clone(),
				SkillNames: childSkillNames, Artifacts: artifactStore, Compaction: agent.CompactionOptions{RetainTokens: cfg.Compaction.RetainTokens, MinRetainedTurns: cfg.Compaction.MinRetainedTurns,
					SummaryMaxTokens: cfg.Compaction.SummaryMaxTokens, Fallback: cfg.Compaction.Fallback, Guidance: cfg.Compaction.Guidance,
					AutoThresholdPercent: cfg.Compaction.AutoThresholdPercent, ToolResultInlineBytes: cfg.Compaction.ToolResultInlineBytes,
					HistoricalToolResultThreshold: cfg.Compaction.HistoricalToolResultThreshold}})
			if err != nil {
				_ = childStore.Close()
				return nil, err
			}
			return &childAgentRuntime{Agent: child, store: childStore}, nil
		})
		taskStore, ok := st.(session.SubagentTaskStore)
		if !ok {
			return nil, errors.New("app: session does not support subagent topology")
		}
		if err := subManager.Bind(ag, factory, ag.Publish, taskStore); err != nil {
			return nil, err
		}
	}
	// Plugins observe the same normalized event stream as every other surface.
	// Emit the explicit state snapshot only after plugin subscriptions exist.
	ag.Subscribe(manager.Emit)
	manager.Emit(ag.StateEvent())

	a := &App{
		Cfg:                    cfg,
		PersistedCfg:           persistedCfg,
		ConfigPath:             configPath,
		AuthPath:               authPath,
		Auth:                   authStore,
		Registry:               reg,
		Router:                 router,
		Provider:               prov,
		ProviderID:             providerID,
		Providers:              providers,
		Models:                 append([]protocol.Model(nil), models...),
		AllModels:              allModels,
		modelCatalog:           modelCatalog,
		runtimeSelection:       runtimeSelection,
		Model:                  model,
		Perm:                   perm,
		Session:                st,
		Agent:                  ag,
		Goal:                   goalController,
		Trust:                  tr,
		PluginManager:          manager,
		PluginDiagnostics:      append(pluginDiagnostics, manager.Diagnostics()...),
		MCPManager:             mcpManager,
		MCPStatuses:            append([]publicmcp.Status(nil), mcpStatuses...),
		Skills:                 skillCatalog,
		Subagents:              subManager,
		Diagnostics:            append([]config.Diagnostic(nil), configDiagnostics...),
		SearchPolicy:           searchPolicy,
		ProjectAllowed:         projectAllowed,
		ProjectInputRoot:       projectInputRoot,
		permissionDefault:      permMode,
		permissionOverride:     opts.Permission != "",
		explicitAPIKey:         opts.APIKey,
		explicitAPIKeyProvider: providerID,
		cwd:                    absCWD,
		userInput:              inputBroker,
		toolGuard:              toolGuard,
		sessionHistory:         sessionHistory,
		artifacts:              artifactStore,
	}
	if skillCatalog != nil {
		a.SkillDiagnostics = skillCatalog.Diagnostics()
	}
	if err := a.bindPermissionSession(st); err != nil {
		return nil, fmt.Errorf("app: permission state: %w", err)
	}
	committed = true
	guardCommitted = true
	return a, nil
}

const permissionMetadataKey = "permission_state"

// bindPermissionSession restores permission state for st and routes future
// mode/rule changes back into that session's metadata entries.
func (a *App) bindPermissionSession(st session.Store) error {
	a.Perm.SetChangeHandler(nil)
	// Start each session from the configured baseline so switching away from a
	// session cannot leak its remembered rules into the next one.
	a.Perm.RestoreState(permission.State{Mode: a.permissionDefault})

	meta, ok := st.(session.MetadataStore)
	if !ok {
		return nil
	}
	if !a.permissionOverride {
		raw, found, err := meta.Metadata(permissionMetadataKey)
		if err != nil {
			return err
		}
		if found && raw != "" {
			var state permission.State
			if err := json.Unmarshal([]byte(raw), &state); err == nil {
				a.Perm.RestoreState(state)
			}
		}
	}
	a.Perm.SetChangeHandler(func(state permission.State) {
		raw, err := json.Marshal(state)
		if err != nil {
			return
		}
		_ = meta.SetMetadata(permissionMetadataKey, string(raw))
	})
	return nil
}

// SetSession switches the active durable conversation store. The old store is
// closed only after the agent accepts the new store.
func (a *App) SetSession(st session.Store) error {
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot switch session while subagents are active")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot switch session while subagents are active")
	}
	if st == nil {
		return fmt.Errorf("app: session is nil")
	}
	if err := a.Goal.ValidateStore(st); err != nil {
		return err
	}
	old := a.Session
	if err := a.bindPermissionSession(st); err != nil {
		return fmt.Errorf("app: permission state: %w", err)
	}
	if err := a.Agent.SetSessionQuietAdmitted(st); err != nil {
		_ = a.bindPermissionSession(old)
		return err
	}
	if err := a.Goal.SetStore(st); err != nil {
		_ = a.Agent.SetSessionQuietAdmitted(old)
		_ = a.bindPermissionSession(old)
		return err
	}
	if a.Subagents != nil {
		taskStore, ok := st.(session.SubagentTaskStore)
		if !ok {
			_ = a.Goal.SetStore(old)
			_ = a.Agent.SetSessionQuietAdmitted(old)
			_ = a.bindPermissionSession(old)
			return errors.New("app: session does not support subagent topology")
		}
		if err := a.Subagents.SetStoreAdmitted(taskStore); err != nil {
			_ = a.Goal.SetStore(old)
			_ = a.Agent.SetSessionQuietAdmitted(old)
			_ = a.bindPermissionSession(old)
			return err
		}
	}
	a.Agent.ResetTurnIdentityAdmitted()
	a.Session = st
	a.sessionHistory.Set(st)
	g, _ := a.Goal.Get()
	a.Agent.Publish(a.Agent.StateEvent())
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g, Cleared: g == nil}})
	if old != nil && old != st {
		if err := old.Close(); err != nil {
			// The switch is already committed across App, Agent, Goal, and
			// permission state. Surface old-store cleanup as an event instead of
			// falsely reporting that the new session failed to bind.
			a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: "close previous session: " + err.Error()})
		}
	}
	return nil
}

func (a *App) SelectBranch(branchID string) error {
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot switch branch while subagents are active")
	}
	return a.Agent.SelectBranchAdmitted(branchID)
}
func (a *App) ForkBranch(fromEntryID string) (protocol.SessionBranch, error) {
	return a.ForkBranchWithOptions(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}

func (a *App) ForkBranchWithOptions(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.SessionBranch{}, errors.New("app: cannot fork branch while subagents are active")
	}
	return a.Agent.ForkWithOptionsAdmitted(opts)
}

func (a *App) RenameSession(title string) error {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot rename session while subagents are active")
	}
	return a.Agent.RenameSessionAdmitted(title)
}

func (a *App) RenameBranch(branchID, name string) (protocol.SessionBranch, error) {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.SessionBranch{}, errors.New("app: cannot rename branch while subagents are active")
	}
	return a.Agent.RenameBranchAdmitted(branchID, name)
}

func (a *App) DeleteBranch(branchID string) error {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot delete branch while subagents are active")
	}
	return a.Agent.DeleteBranchAdmitted(branchID)
}

func (a *App) GoalState() (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	return a.Goal.Get()
}

// GoalContinuationDeferred reports whether automatic continuation is durably
// suppressed for the active branch.
func (a *App) GoalContinuationDeferred() (bool, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	return a.Goal.Deferred()
}

func (a *App) requireGoalCapabilities() error {
	for _, name := range []string{"get_goal", "create_goal", "update_goal"} {
		if _, ok := a.Registry.Get(name); !ok {
			return fmt.Errorf("goal: required capability %s is disabled by tools allowlist", name)
		}
	}
	return nil
}

// ReadyGoal is called only after a surface installs event subscriptions and interaction handlers.
func (a *App) ReadyGoal() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	g, err := a.Goal.Get()
	if err != nil {
		return err
	}
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g, Cleared: g == nil}})
	if g == nil || g.Status != protocol.GoalActive || a.Agent.Mode() != protocol.ModeDefault {
		return nil
	}
	deferred, err := a.Goal.Deferred()
	if err != nil {
		return err
	}
	if !deferred {
		if err := a.requireGoalCapabilities(); err != nil {
			return err
		}
		a.Agent.ContinueGoal()
	}
	return nil
}
func (a *App) goalAutoResumeEligible() (bool, error) {
	g, err := a.Goal.Get()
	if err != nil {
		return false, err
	}
	if g == nil || g.Status != protocol.GoalActive || a.Agent.Mode() != protocol.ModeDefault {
		return false, nil
	}
	deferred, err := a.Goal.Deferred()
	return !deferred, err
}

func (a *App) restartGoalAfterFailedMutation(eligible bool) {
	if !eligible {
		return
	}
	g, err := a.Goal.Get()
	if err != nil || g == nil || g.Status != protocol.GoalActive || a.Agent.Mode() != protocol.ModeDefault {
		return
	}
	deferred, err := a.Goal.Deferred()
	if err == nil && !deferred {
		a.Agent.ContinueGoal()
	}
}

func (a *App) CreateGoal(objective string, budget *int64, replace bool) (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.requireGoalCapabilities(); err != nil {
		return nil, err
	}
	restartOnFailure := false
	if replace {
		var err error
		restartOnFailure, err = a.goalAutoResumeEligible()
		if err != nil {
			return nil, err
		}
		if err := a.Agent.StopGoal(context.Background(), false); err != nil {
			return nil, err
		}
	}
	g, err := a.Goal.Create(objective, budget, replace)
	if err != nil {
		a.restartGoalAfterFailedMutation(restartOnFailure)
		return nil, err
	}
	a.Agent.ContinueGoal()
	return g, nil
}
func (a *App) EditGoal(objective string) (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	g, e := a.Goal.Get()
	if e != nil {
		return nil, e
	}
	if g == nil {
		return nil, session.ErrNotFound
	}
	restartOnFailure, err := a.goalAutoResumeEligible()
	if err != nil {
		return nil, err
	}
	if err := a.Agent.StopGoal(context.Background(), false); err != nil {
		return nil, err
	}
	next, err := a.Goal.Edit(g.GoalID, objective)
	if err != nil {
		a.restartGoalAfterFailedMutation(restartOnFailure)
		return nil, err
	}
	if next.Status == protocol.GoalActive {
		a.Agent.ResetGoalAudit()
		a.Agent.ContinueGoal()
	}
	return next, nil
}
func (a *App) setGoalStatusLocked(status protocol.ThreadGoalStatus) (*protocol.ThreadGoal, error) {
	g, err := a.Goal.Get()
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, session.ErrNotFound
	}
	return a.Goal.SetStatus(g.GoalID, status, false)
}

func (a *App) SetGoalStatus(status protocol.ThreadGoalStatus) (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	return a.setGoalStatusLocked(status)
}
func (a *App) PauseGoal() (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.Agent.StopGoal(context.Background(), true); err != nil {
		return nil, err
	}
	return a.setGoalStatusLocked(protocol.GoalPaused)
}
func (a *App) ResumeGoal() (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	g, err := a.Goal.Get()
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, session.ErrNotFound
	}
	if err := a.requireGoalCapabilities(); err != nil {
		return nil, err
	}
	if g.Status == protocol.GoalActive {
		deferred, err := a.Goal.Deferred()
		if err != nil {
			return nil, err
		}
		if !deferred {
			return nil, errors.New("goal: active goal is not deferred")
		}
		if err := a.Goal.Defer(false); err != nil {
			return nil, err
		}
	} else {
		g, err = a.Goal.SetStatus(g.GoalID, protocol.GoalActive, false)
		if err != nil {
			return nil, err
		}
	}
	a.Agent.ResetGoalAudit()
	a.Agent.ContinueGoal()
	return g, nil
}
func (a *App) ClearGoal() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.Agent.StopGoal(context.Background(), true); err != nil {
		return err
	}
	g, err := a.Goal.Get()
	if err != nil {
		return err
	}
	id := ""
	if g != nil {
		id = g.GoalID
	}
	return a.Goal.Clear(id)
}
func (a *App) ContinueGoal() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.requireGoalCapabilities(); err != nil {
		return err
	}
	g, err := a.Goal.Get()
	if err != nil {
		return err
	}
	if g == nil || g.Status != protocol.GoalActive {
		return errors.New("goal: no active goal")
	}
	if err := a.Goal.Defer(false); err != nil {
		return err
	}
	a.Agent.ContinueGoal()
	return nil
}

// ConfigureOpenAICompatible replaces the runtime adapter after an operator
// changes its endpoint. Persistence remains the caller's responsibility so TUI
// configuration can save endpoint and credential as one user action.
func (a *App) ConfigureOpenAICompatible(baseURL string) error {
	pc := a.Cfg.Providers[openaicompat.ProviderID]
	cfg := openaicompat.Config{BaseURL: baseURL, DefaultModel: pc.DefaultModel}
	if a.explicitAPIKey != "" && a.explicitAPIKeyProvider == openaicompat.ProviderID {
		cfg.APIKey = a.explicitAPIKey
	}
	if stored, ok := a.Auth.Get(openaicompat.ProviderID); ok {
		cfg.DiscoveryAPIKey = stored.Key
	}
	compatible, err := openaicompat.New(cfg)
	if err != nil {
		return fmt.Errorf("app: %s: %w", openaicompat.ProviderID, err)
	}
	if !compatible.Configured() {
		return errors.New("app: openai-compatible base URL is required")
	}

	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.ProviderID == openaicompat.ProviderID {
		model := a.Agent.Model()
		if err := a.Agent.SetProviderAndModel(compatible, model); err != nil {
			return err
		}
	}
	a.Providers[openaicompat.ProviderID] = compatible
	a.modelCatalog[openaicompat.ProviderID] = nil
	if a.ProviderID == openaicompat.ProviderID {
		a.Provider = compatible
		a.Models = nil
	}
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.providers[openaicompat.ProviderID] = compatible
		a.runtimeSelection.catalogs[openaicompat.ProviderID] = nil
		a.runtimeSelection.mu.Unlock()
	}
	var all []protocol.Model
	seen := map[string]bool{}
	for _, providerID := range []string{a.ProviderID, "opencode-go", openaicompat.ProviderID, "chatgpt", "fake"} {
		if seen[providerID] {
			continue
		}
		seen[providerID] = true
		all = append(all, a.modelCatalog[providerID]...)
	}
	for providerID, catalog := range a.modelCatalog {
		if !seen[providerID] {
			all = append(all, catalog...)
		}
	}
	a.AllModels = all
	return nil
}

// RefreshProviderModels forces an authenticated catalog refresh when the
// provider supports it and atomically replaces app/picker snapshots.
func (a *App) RefreshProviderModels(ctx context.Context, id string) error {
	a.stateMu.Lock()
	p, ok := a.Providers[id]
	a.stateMu.Unlock()
	if !ok {
		return fmt.Errorf("app: provider %q is not available", id)
	}
	var models []protocol.Model
	var err error
	if refreshable, ok := p.(interface {
		RefreshModels(context.Context) ([]protocol.Model, error)
	}); ok {
		models, err = refreshable.RefreshModels(ctx)
	} else {
		models, err = p.ListModels(ctx)
	}
	if len(models) == 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("app: provider %q returned an empty model catalog", id)
	}
	models = normalizeProviderModels(id, models)
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if current, ok := a.Providers[id]; !ok || current != p {
		return fmt.Errorf("app: provider %q configuration changed during model refresh", id)
	}
	var refreshedActive *protocol.Model
	if a.ProviderID == id {
		current := a.Agent.Model()
		level := a.Agent.Thinking()
		for i := range models {
			if models[i].ID != current.ID {
				continue
			}
			model := models[i]
			if !model.SupportsThinkingLevel(level) {
				return fmt.Errorf("app: refreshed metadata for active model %q is incompatible with current settings: thinking level %q is not supported (supported: %v)", model.ID, level, model.SupportedThinkingLevels())
			}
			if setErr := a.Agent.SetModel(model); setErr != nil {
				return fmt.Errorf("app: apply refreshed metadata for active model %q: %w", model.ID, setErr)
			}
			refreshedActive = &model
			break
		}
		if refreshedActive == nil && modelCatalogAuthoritative(p) {
			for i := range models {
				if !models[i].SupportsThinkingLevel(level) {
					continue
				}
				fallback := models[i]
				if setErr := a.Agent.SetModel(fallback); setErr != nil {
					return fmt.Errorf("app: replace unavailable active model %q with %q: %w", current.ID, fallback.ID, setErr)
				}
				refreshedActive = &fallback
				break
			}
			if refreshedActive == nil {
				return fmt.Errorf("app: active model %q is unavailable for this account and no catalog model supports thinking level %q", current.ID, level)
			}
		}
	}
	a.modelCatalog[id] = append([]protocol.Model(nil), models...)
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.catalogs[id] = append([]protocol.Model(nil), models...)
		a.runtimeSelection.mu.Unlock()
	}
	var all []protocol.Model
	seen := map[string]bool{}
	for _, providerID := range []string{a.ProviderID, "opencode-go", "openai-compatible", "chatgpt", "fake"} {
		if seen[providerID] {
			continue
		}
		seen[providerID] = true
		all = append(all, a.modelCatalog[providerID]...)
	}
	for providerID, catalog := range a.modelCatalog {
		if !seen[providerID] {
			all = append(all, catalog...)
		}
	}
	a.AllModels = all
	if a.ProviderID == id {
		a.Models = append([]protocol.Model(nil), models...)
		if refreshedActive != nil {
			a.Model = *refreshedActive
			if a.runtimeSelection != nil {
				a.runtimeSelection.mu.Lock()
				a.runtimeSelection.model = *refreshedActive
				a.runtimeSelection.mu.Unlock()
			}
		}
	}
	return err
}

func modelCatalogAuthoritative(p provider.Provider) bool {
	authority, ok := p.(interface{ ModelCatalogAuthoritative() bool })
	return ok && authority.ModelCatalogAuthoritative()
}

func normalizeProviderModels(providerID string, models []protocol.Model) []protocol.Model {
	out := make([]protocol.Model, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model.Provider = providerID
		if model.ID == "" {
			continue
		}
		if _, ok := seen[model.ID]; ok {
			continue
		}
		seen[model.ID] = struct{}{}
		out = append(out, model.Clone())
	}
	return out
}

// SetProvider switches the active provider and model for subsequent turns.
func (a *App) SetProvider(id string) error {
	p, ok := a.Providers[id]
	if !ok {
		return fmt.Errorf("app: provider %q is not available", id)
	}
	catalog := a.modelCatalog[id]
	target := a.Model
	if a.Agent != nil {
		target = a.Agent.Model()
	}
	valid := target.Provider == id
	if valid {
		valid = false
		for _, m := range catalog {
			if m.ID == target.ID {
				valid = true
				target = m
				break
			}
		}
	}
	if !valid {
		target = protocol.Model{Provider: id, SupportsTools: true}
		if dm, ok := p.(interface{ DefaultModel() protocol.Model }); ok {
			d := dm.DefaultModel()
			for _, m := range catalog {
				if m.ID == d.ID {
					target = m
					break
				}
			}
		}
		if target.ID == "" && len(catalog) > 0 {
			target = catalog[0]
		}
		if target.ID == "" {
			target.ID = "default"
		}
	}
	target.Provider = id
	if err := a.Agent.SetProviderAndModel(p, target); err != nil {
		return err
	}
	a.ProviderID, a.Provider = id, p
	a.Models = append([]protocol.Model(nil), catalog...)
	a.Model = target
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.provider = id
		a.runtimeSelection.model = target
		a.runtimeSelection.mu.Unlock()
	}
	return nil
}

// SetModel updates the active model and its app mirror. Unknown models remain
// permitted for providers that accept custom model identifiers.
func (a *App) SetModel(m protocol.Model) error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("app: model id is required")
	}
	if m.Provider == "" {
		m.Provider = a.ProviderID
	}
	m = m.Clone()
	if m.Provider != a.ProviderID {
		return fmt.Errorf("app: model provider %q does not match active provider %q", m.Provider, a.ProviderID)
	}
	if err := a.Agent.SetModel(m); err != nil {
		return err
	}
	a.Model = m
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.provider = a.ProviderID
		a.runtimeSelection.model = m
		a.runtimeSelection.mu.Unlock()
	}
	return nil
}

// SetProviderModelThinking updates the active provider, model, and effort as
// one admitted Agent transaction and refreshes the App/runtime mirrors.
func (a *App) SetProviderModelThinking(providerID string, model protocol.Model, level protocol.ThinkingLevel) error {
	p, ok := a.Providers[providerID]
	if !ok {
		return fmt.Errorf("app: provider %q is not available", providerID)
	}
	if model.Provider == "" {
		model.Provider = providerID
	}
	if model.Provider != providerID {
		return fmt.Errorf("app: model provider %q does not match selected provider %q", model.Provider, providerID)
	}
	model = model.Clone()
	if err := a.Agent.SetProviderModelThinking(p, model, level); err != nil {
		return err
	}
	a.ProviderID, a.Provider = providerID, p
	a.Models = append([]protocol.Model(nil), a.modelCatalog[providerID]...)
	a.Model = model
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.provider = providerID
		a.runtimeSelection.model = model
		a.runtimeSelection.mu.Unlock()
	}
	return nil
}

// SetPermissionDefault updates both the active session mode and the baseline
// restored for subsequently opened sessions. Config persistence remains the
// caller's responsibility so it can save first and avoid partial updates.
func (a *App) SetPermissionDefault(mode permission.Mode) error {
	if mode != permission.ModeAsk && mode != permission.ModeAllow && mode != permission.ModeDeny {
		return fmt.Errorf("app: invalid permission mode %q", mode)
	}
	a.permissionDefault = mode
	a.Perm.SetMode(mode)
	return nil
}

// CWD returns the app working directory.
func (a *App) CWD() string { return a.cwd }

func getwd() (string, error) { return os.Getwd() }

func mergePluginSpecs(global, project, explicit []publicplugin.PluginSpec) ([]publicplugin.PluginSpec, error) {
	merged := make(map[string]publicplugin.PluginSpec, len(global)+len(project)+len(explicit))
	order := make([]string, 0, len(merged))
	mergeLayer := func(scope string, specs []publicplugin.PluginSpec, allowDuplicates bool) error {
		seen := make(map[string]bool, len(specs))
		for _, spec := range specs {
			if err := publicplugin.ValidateSpec(spec); err != nil {
				return fmt.Errorf("%s plugin %q: %w", scope, spec.ID, err)
			}
			if seen[spec.ID] && !allowDuplicates {
				return fmt.Errorf("%s contains duplicate plugin id %q", scope, spec.ID)
			}
			seen[spec.ID] = true
			if _, exists := merged[spec.ID]; !exists {
				order = append(order, spec.ID)
			}
			merged[spec.ID] = spec
		}
		return nil
	}
	if err := mergeLayer("global configuration", global, false); err != nil {
		return nil, err
	}
	if err := mergeLayer("project configuration", project, false); err != nil {
		return nil, err
	}
	if err := mergeLayer("explicit options", explicit, true); err != nil {
		return nil, err
	}
	out := make([]publicplugin.PluginSpec, 0, len(order))
	for _, id := range order {
		out = append(out, merged[id])
	}
	return out, nil
}

func mergeDisabledPluginSpecs(global, project, explicit []publicplugin.PluginSpec) []publicplugin.PluginSpec {
	merged := make(map[string]publicplugin.PluginSpec, len(global)+len(project)+len(explicit))
	order := make([]string, 0, len(merged))
	for _, specs := range [][]publicplugin.PluginSpec{global, project, explicit} {
		for _, spec := range specs {
			if _, exists := merged[spec.ID]; !exists {
				order = append(order, spec.ID)
			}
			merged[spec.ID] = spec
		}
	}
	out := make([]publicplugin.PluginSpec, 0, len(order))
	for _, id := range order {
		out = append(out, merged[id])
	}
	return out
}

func mergeMCPServers(global, project map[string]publicmcp.ServerSpec, explicit []publicmcp.ServerSpec) []publicmcp.ServerSpec {
	merged := make(map[string]publicmcp.ServerSpec, len(global)+len(project)+len(explicit))
	for id, spec := range global {
		if spec.ID == "" {
			spec.ID = id
		}
		merged[spec.ID] = spec
	}
	for id, spec := range project {
		if spec.ID == "" {
			spec.ID = id
		}
		merged[spec.ID] = spec
	}
	for _, spec := range explicit {
		if spec.ID != "" {
			merged[spec.ID] = spec
		}
	}
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]publicmcp.ServerSpec, 0, len(ids))
	for _, id := range ids {
		out = append(out, merged[id])
	}
	return out
}

// Close releases plugin and router resources before the session store.
func (a *App) Close() error {
	var errs []error
	if a.Subagents != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.Subagents.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	if a.Agent != nil {
		a.Agent.Close()
	}
	if a.userInput != nil {
		a.userInput.Close()
	}
	if a.MCPManager != nil {
		if err := a.MCPManager.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.PluginManager != nil {
		if err := a.PluginManager.Close(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}
	if a.Router != nil {
		if err := a.Router.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.Skills != nil {
		if err := a.Skills.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.artifacts != nil {
		if err := a.artifacts.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.Session != nil {
		if err := a.Session.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if a.toolGuard != nil {
		if err := a.toolGuard.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// toolHost adapts the tools.ToolHost contract to app state.
type toolHost struct {
	cwd             string
	roots           []string
	perm            permission.Service
	reg             *tools.SimpleRegistry
	userInput       *userinput.Broker
	emitUserInput   func(protocol.UserInputRequest)
	inEventCallback func() bool
}

func (h *toolHost) CWD() string                             { return h.cwd }
func (h *toolHost) Roots() []string                         { return h.roots }
func (h *toolHost) Permission() permission.Service          { return h.perm }
func (h *toolHost) Environ() []string                       { return nil }
func (h *toolHost) EmitProgress(ev tools.ToolProgressEvent) {}
func (h *toolHost) RequestUserInput(ctx context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
	if h.userInput == nil {
		return protocol.UserInputResponse{}, userinput.ErrUnavailable
	}
	if h.inEventCallback != nil && h.inEventCallback() && !h.userInput.HasHandler() {
		return protocol.UserInputResponse{}, userinput.ErrUnavailable
	}
	return h.userInput.Ask(ctx, req, h.emitUserInput)
}

// EnableUserInputReplies enables manual TUI/RPC resolution of ask_user calls.
func (a *App) EnableUserInputReplies() {
	if a != nil && a.userInput != nil {
		a.userInput.EnableManual()
	}
}

// CloseUserInput releases a pending request and prevents future interactive
// waits. RPC uses this when its input stream reaches EOF.
func (a *App) CloseUserInput() {
	if a != nil && a.userInput != nil {
		a.userInput.Close()
	}
}

// RequestUserInput submits a host interaction through the app broker. It is
// primarily useful to non-tool hosts and keeps event publication consistent.
func (a *App) RequestUserInput(ctx context.Context, request protocol.UserInputRequest) (protocol.UserInputResponse, error) {
	if a == nil || a.userInput == nil || a.Agent == nil {
		return protocol.UserInputResponse{}, userinput.ErrUnavailable
	}
	reentrantEventCallback := a.Agent.InEventCallback()
	if reentrantEventCallback && !a.userInput.HasHandler() {
		return protocol.UserInputResponse{}, userinput.ErrUnavailable
	}
	response, err := a.userInput.Ask(ctx, request, a.Agent.EmitUserInputRequest)
	// RequestUserInput historically guarantees that observers see the request
	// before the interaction returns. Skip the barrier only for a request made
	// reentrantly by that same ordered event dispatcher.
	if !reentrantEventCallback {
		if drainErr := a.Agent.DrainEvents(ctx); err == nil && drainErr != nil {
			err = drainErr
		}
	}
	return response, err
}

// ReplyUserInput resolves the current ask_user call.
func (a *App) ReplyUserInput(response protocol.UserInputResponse) error {
	if a == nil || a.userInput == nil {
		return userinput.ErrUnavailable
	}
	return a.userInput.Reply(response)
}

// RejectUserInput declines the current ask_user call.
// ReadySubagents is called after a surface subscribes. Restored work is never
// restarted automatically; the call publishes only topology snapshots.
func (a *App) ReadySubagents() error {
	if a.Subagents == nil {
		return nil
	}
	return a.Subagents.Ready(context.Background())
}
func (a *App) SpawnSubagent(ctx context.Context, req protocol.SpawnSubagentRequest) (protocol.SubagentState, error) {
	if a.Subagents == nil {
		return protocol.SubagentState{}, errors.New("app: subagents disabled")
	}
	return a.Subagents.Spawn(ctx, a.Subagents.RootCaller(), req)
}
func (a *App) SendSubagentMessage(ctx context.Context, target, message string) error {
	if a.Subagents == nil {
		return errors.New("app: subagents disabled")
	}
	return a.Subagents.SendMessage(ctx, a.Subagents.RootCaller(), target, message)
}
func (a *App) FollowupSubagent(ctx context.Context, target, message string) error {
	if a.Subagents == nil {
		return errors.New("app: subagents disabled")
	}
	return a.Subagents.Followup(ctx, a.Subagents.RootCaller(), target, message)
}
func (a *App) WaitSubagents(ctx context.Context, timeout time.Duration) (protocol.WaitSubagentsResult, error) {
	if a.Subagents == nil {
		return protocol.WaitSubagentsResult{}, errors.New("app: subagents disabled")
	}
	return a.Subagents.Wait(ctx, a.Subagents.RootCaller(), timeout)
}
func (a *App) WaitSubagentsUntilAll(ctx context.Context, timeout time.Duration) (protocol.WaitSubagentsResult, error) {
	if a.Subagents == nil {
		return protocol.WaitSubagentsResult{}, errors.New("app: subagents disabled")
	}
	return a.Subagents.WaitUntilAll(ctx, a.Subagents.RootCaller(), timeout)
}
func (a *App) WaitSubagentsIdle(ctx context.Context) error {
	if a.Subagents == nil {
		return nil
	}
	return a.Subagents.WaitAll(ctx)
}
func (a *App) InterruptSubagent(ctx context.Context, target string) (protocol.AgentStatus, error) {
	if a.Subagents == nil {
		return protocol.AgentNotFound, errors.New("app: subagents disabled")
	}
	return a.Subagents.Interrupt(ctx, a.Subagents.RootCaller(), target)
}
func (a *App) ListSubagents(ctx context.Context, prefix string) (protocol.SubagentList, error) {
	if a.Subagents == nil {
		return protocol.SubagentList{}, nil
	}
	return a.Subagents.List(ctx, a.Subagents.RootCaller(), prefix)
}
func (a *App) Subagent(ctx context.Context, target string) (protocol.SubagentState, error) {
	if a.Subagents == nil {
		return protocol.SubagentState{Status: protocol.AgentNotFound}, errors.New("app: subagents disabled")
	}
	return a.Subagents.Get(ctx, target)
}
func (a *App) SubagentMessages(ctx context.Context, target string) ([]protocol.Message, error) {
	if a.Subagents == nil {
		return nil, errors.New("app: subagents disabled")
	}
	return a.Subagents.Messages(ctx, target)
}

// ActiveModelsSnapshot returns the active provider, current model, and a
// defensive catalog copy from the same live-selection snapshot.
func (a *App) ActiveModelsSnapshot() (string, protocol.Model, []protocol.Model) {
	if a == nil {
		return "", protocol.Model{}, nil
	}
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.RLock()
		defer a.runtimeSelection.mu.RUnlock()
		providerID := a.runtimeSelection.provider
		catalog := a.runtimeSelection.catalogs[providerID]
		out := make([]protocol.Model, len(catalog))
		for i, model := range catalog {
			out[i] = model.Clone()
		}
		return providerID, a.runtimeSelection.model.Clone(), out
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	out := make([]protocol.Model, len(a.Models))
	for i, model := range a.Models {
		out[i] = model.Clone()
	}
	return a.ProviderID, a.Model.Clone(), out
}

// ModelsSnapshot returns a defensive copy of the active provider catalog.
func (a *App) ModelsSnapshot() []protocol.Model {
	_, _, models := a.ActiveModelsSnapshot()
	return models
}

// SubagentModels returns exact provider/model pairs currently available to children.
func (a *App) SubagentModels() []protocol.Model {
	if a == nil || a.runtimeSelection == nil {
		return nil
	}
	return a.runtimeSelection.availableModels()
}

func (a *App) SubagentUsage() (protocol.Usage, error) {
	if a.Subagents == nil {
		return protocol.Usage{}, nil
	}
	return a.Subagents.Usage()
}

func (a *App) RejectUserInput(requestID string) error {
	if a == nil || a.userInput == nil {
		return userinput.ErrUnavailable
	}
	return a.userInput.Reject(requestID)
}
