package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/buildinfo"
	"github.com/elmissouri16/snow-core/internal/config"
	ctxpkg "github.com/elmissouri16/snow-core/internal/context"
	"github.com/elmissouri16/snow-core/internal/diagnostics"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	internalmcp "github.com/elmissouri16/snow-core/internal/mcp"
	"github.com/elmissouri16/snow-core/internal/permission"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/tools/builtin"
	toolrouter "github.com/elmissouri16/snow-core/internal/tools/router"
	"github.com/elmissouri16/snow-core/internal/userinput"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// New assembles the app.
func New(ctx context.Context, opts Options) (result *App, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	buildVersion := opts.BuildVersion
	if buildVersion == "" {
		buildVersion = buildinfo.Version
	}
	startup, err := initializeStartup(ctx, opts)
	if err != nil {
		return nil, err
	}
	absCWD := startup.absCWD
	configPath := startup.configPath
	persistedCfg := startup.persistedCfg
	cfg := startup.cfg
	if opts.Retry != nil {
		if err := opts.Retry.Validate(); err != nil {
			return nil, err
		}
		cfg.Retry = *opts.Retry
	}
	if opts.Debug != nil {
		cfg.Debug.Enabled = *opts.Debug
	}
	if opts.DebugDumpPath != "" {
		cfg.Debug.Enabled = true
	}
	permMode := startup.permMode
	thinking := startup.thinking
	reasoningSummary := startup.reasoningSummary
	textVerbosity := startup.textVerbosity
	collaborationMode := startup.collaborationMode
	planThinking := startup.planThinking
	authPath := startup.authPath
	authStore := startup.authStore
	tr := startup.trust
	authService := startup.authService
	projectPlugins := startup.projectPlugins
	projectMCPServers := startup.projectMCPServers
	projectSkills := startup.projectSkills
	projectSystemPrompt := startup.projectSystemPrompt
	searchPolicy := startup.searchPolicy
	configDiagnostics := startup.configDiagnostics
	projectAllowed := startup.projectAllowed
	projectInputRoot := startup.projectInputRoot

	// Tools. Pin the canonical root once so later launch-path replacement cannot
	// retarget the file capability. Managed processes are app-owned and bound to
	// the session after its store is opened, but their tools must be registered
	// before the explicit allowlist and deferred router are assembled.
	reg := tools.NewRegistry()
	processManager := managedprocess.NewManager(managedprocess.Options{
		CWD: absCWD, MaxRunning: cfg.Processes.MaxRunning, MaxRecords: cfg.Processes.MaxRecords,
		RetainedOutputBytes: cfg.Processes.RetainedOutputBytes, MaxLogReadBytes: cfg.ToolOutputLimit(),
	})
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
	if err := builtin.RegisterBuiltins(reg, toolOpts); err != nil {
		return nil, fmt.Errorf("app: built-in tools: %w", err)
	}
	if err := builtin.RegisterProcessTools(reg, processManager); err != nil {
		return nil, fmt.Errorf("app: managed process tools: %w", err)
	}

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
			// Resource and deactivation tools are meaningful only with tier-one
			// disclosure and activation, and otherwise leak names-only enums.
			if (descriptor.Schema.Name == "read_skill_resource" || descriptor.Schema.Name == "deactivate_skill") && !activationAllowed {
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
	providerStartup, err := initializeProvider(opts, cfg, authStore, authService)
	if err != nil {
		return nil, err
	}
	providerID := providerStartup.id
	providerModules := providerStartup.modules
	providers := providerStartup.providers
	prov := providerStartup.selected

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
	if err := processManager.BindSession(st.ID()); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("app: bind managed processes: %w", err)
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
		processCloseCtx, processCancel := context.WithTimeout(context.Background(), managedprocess.DefaultShutdownTimeout)
		cleanupErrs = append(cleanupErrs, processManager.Close(processCloseCtx))
		processCancel()
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
	permBroker := permission.NewBroker(perm)
	if opts.PermissionHandler != nil {
		permBroker.SetHandler(opts.PermissionHandler)
	}

	// Only the active provider catalog is readiness-critical. Picker-only and
	// ad-hoc subagent catalogs are loaded on demand through liveRuntimeSelection.
	modelCatalog := make(map[string][]protocol.Model, len(providers))
	modelCatalogErrors := make(map[string]error, len(providers))
	activeModels, activeListErr := providers[providerID].ListModels(ctx)
	modelCatalog[providerID] = normalizeProviderModels(providerID, activeModels)
	modelCatalogErrors[providerID] = activeListErr
	models := modelCatalog[providerID]
	if rejectsUnknownModels(prov) && len(models) == 0 {
		if listErr := modelCatalogErrors[providerID]; listErr != nil {
			return nil, fmt.Errorf("app: provider %s model discovery failed: %w", providerID, listErr)
		}
		return nil, fmt.Errorf("app: provider %s has no maintained models currently available", providerID)
	}
	activeProviderConfig := cfg.Providers[providerID]
	if config.IsOpenAICompatibleProfile(providerID, activeProviderConfig) && cfg.DefaultModel == "" && len(models) == 0 {
		if listErr := modelCatalogErrors[providerID]; listErr != nil {
			return nil, fmt.Errorf("app: openai-compatible model discovery failed; pass --model or configure default_model: %w", listErr)
		}
		return nil, errors.New("app: openai-compatible model discovery returned no models; pass --model or configure default_model")
	}
	var allModels []protocol.Model
	seenProviders := make(map[string]bool)
	orderedProviderIDs := []string{providerID}
	for _, module := range providerModules.Modules() {
		orderedProviderIDs = append(orderedProviderIDs, module.ID)
	}
	for _, id := range orderedProviderIDs {
		if seenProviders[id] {
			continue
		}
		if catalog, ok := modelCatalog[id]; ok {
			allModels = append(allModels, catalog...)
			seenProviders[id] = true
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
	// Strict catalogs reject explicit/configured unknown IDs so they cannot
	// bypass provider policy (for example, Zen's free-only boundary).
	if model.ID != "" && !configuredFound && rejectsUnknownModels(prov) {
		return nil, fmt.Errorf("app: model %q is not available for provider %s", model.ID, providerID)
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

	// Backend catalogs can withdraw an effort that an older interactive
	// selection persisted. Repair only that remembered project tuple; explicit
	// CLI/SDK and global fallback values remain strict startup errors.
	if !model.SupportsThinkingLevel(thinking) && startup.projectSelectionApplied && opts.Thinking == "" {
		selection, ok := persistedCfg.ProjectSelections[absCWD]
		if ok && selection.Provider == providerID && selection.Model == model.ID && selection.Thinking == string(thinking) {
			previous := thinking
			candidate, updateErr := config.Update(configPath, func(latest *config.Config) error {
				updated, err := config.WithProjectSelection(*latest, absCWD, config.ProjectSelection{
					Provider: providerID,
					Model:    model.ID,
					Thinking: string(protocol.ThinkingOff),
				})
				if err != nil {
					return err
				}
				*latest = updated
				return nil
			})
			if updateErr != nil {
				return nil, fmt.Errorf("app: repair unsupported project thinking selection: %w", updateErr)
			}
			persistedCfg = candidate
			cfg.ProjectSelections = candidate.ProjectSelections
			cfg.Thinking = string(protocol.ThinkingOff)
			thinking = protocol.ThinkingOff
			configDiagnostics = append(configDiagnostics, config.Diagnostic{
				Path:    configPath,
				Message: fmt.Sprintf("reset project thinking from %q to off because model %q no longer advertises it", previous, model.ID),
			})
		}
	}

	runtimeProviders := make(map[string]provider.Provider, len(providers))
	for id, candidate := range providers {
		runtimeProviders[id] = candidate
	}
	runtimeCatalogs := make(map[string][]protocol.Model, len(modelCatalog))
	for id, catalog := range modelCatalog {
		runtimeCatalogs[id] = cloneModels(catalog)
	}
	runtimeCatalogErrors := make(map[string]error, len(modelCatalogErrors))
	for id, catalogErr := range modelCatalogErrors {
		runtimeCatalogErrors[id] = catalogErr
	}
	runtimeSelection := &liveRuntimeSelection{
		provider: providerID, model: model, providers: runtimeProviders, catalogs: runtimeCatalogs,
		catalogErrors: runtimeCatalogErrors, catalogLoads: make(map[string]*catalogLoad),
		catalogGeneration: make(map[string]uint64),
	}

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
		CWD: extensionCWD, SessionID: st.ID(), HostVersion: buildVersion, HostCapabilities: []string{"tools", "events"},
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
	}

	// MCP servers are independent of Snow's plugin protocol. The official Go
	// SDK performs protocol negotiation and lifecycle handling; negotiated
	// tools/resources/prompts are adapted into the same permissioned registry.
	mcpDeclarations := mergeMCPDeclarations(cfg.MCPServers, projectMCPServers, opts.MCPServers, projectInputRoot)
	mcpSpecs := make([]publicmcp.ServerSpec, 0, len(mcpDeclarations))
	for _, declaration := range mcpDeclarations {
		mcpSpecs = append(mcpSpecs, declaration.Spec)
	}
	var mcpStatuses []publicmcp.Status
	if !opts.NoMCP {
		mcpManager = internalmcp.NewManager(reg, internalmcp.Options{
			CWD: projectInputRoot, Roots: []string{projectInputRoot}, HostName: "snow", HostVersion: buildVersion, MaxOutputBytes: cfg.ToolOutputLimit(),
			CacheRoot: startup.globalDir,
		})
		mcpManager.Initialize(ctx, mcpDeclarations)
		mcpStatuses = mcpManager.Statuses()
	} else {
		for _, spec := range mcpSpecs {
			mcpStatuses = append(mcpStatuses, publicmcp.Status{ID: spec.ID, Transport: spec.EffectiveTransport(), Message: "disabled by --no-mcp"})
		}
	}

	// Collaboration tools are direct and registered before deferred-router
	// indexing so the model always receives the complete control set together.
	if cfg.Subagents.Enabled {
		if err := runtimeSelection.preloadCatalogs(ctx, requiredSubagentProviders(cfg.Subagents, providerID)); err != nil {
			return nil, fmt.Errorf("app: discover configured subagent providers: %w", err)
		}

		validateChildSelection := func(label, providerOverride, modelID string) error {
			if providerOverride == "" && modelID == "" {
				return nil
			}
			childProvider := providerOverride
			if childProvider == "" {
				childProvider = providerID
			}
			if _, _, err := runtimeSelection.childSelection(ctx, childProvider, modelID); err != nil {
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
		subManager.SetModelSelection(func(ctx context.Context, providerID, modelID string) (protocol.Model, error) {
			_, selected, err := runtimeSelection.childSelection(ctx, providerID, modelID)
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
	routeMetadata := tools.SelectMetadata(reg, nil)
	needsRouter := false
	for _, desc := range routeMetadata {
		if desc.Deferred {
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
		router = toolrouter.NewMetadata(routeMetadata)
		if err := reg.Register(builtin.NewSearchTools(router, reg)); err != nil {
			return nil, fmt.Errorf("app: register search_tools: %w", err)
		}
	}
	if mcpManager != nil && router != nil {
		catalogChanged := func(candidate []tools.ToolDescriptor) error {
			if refreshable, ok := router.(interface {
				RefreshMetadata([]tools.DescriptorMetadata) error
			}); ok {
				metadata := make([]tools.DescriptorMetadata, 0, len(candidate))
				for _, desc := range candidate {
					metadata = append(metadata, tools.MetadataFromDescriptor(desc))
				}
				return refreshable.RefreshMetadata(metadata)
			}
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
	projectBasePrompt := assembly.Render()
	rootSystemPrompt := projectBasePrompt
	if mcpManager != nil {
		if catalog := mcpManager.CatalogPrompt(); catalog != "" {
			rootSystemPrompt += "\n\n" + catalog
		}
	}
	systemPrompt := rootSystemPrompt
	skillNames := skillNamesForRegistry(skillCatalog, reg)
	if catalog := skillPromptForRegistry(skillCatalog, reg); catalog != "" {
		systemPrompt += "\n\n" + catalog
	}

	initialMode := collaborationMode
	if opts.CollaborationMode == "" && opts.SessionPath != "" {
		initialMode = "" // restore the persisted active-branch mode
	}
	ag, err = agent.New(agent.Options{
		Provider:   prov,
		Registry:   reg,
		Session:    st,
		Permission: perm,
		ToolHost:   host,
		Router:     router,
		DeferredBundles: []agent.DeferredBundle{{
			Members: builtin.ManagedProcessToolNames(), Sticky: processManager.HasRecords,
		}},
		ToolGuidance:              runtimeToolGuidance(),
		FixedContextBudgetPercent: cfg.FixedContextBudgetPercent,
		SystemPrompt:              systemPrompt,
		Model:                     model,
		Thinking:                  thinking,
		ReasoningSummary:          reasoningSummary,
		TextVerbosity:             textVerbosity,
		CollaborationMode:         initialMode,
		PlanThinking:              planThinking,
		Goal:                      goalController,
		SkillNames:                skillNames,
		Artifacts:                 artifactStore,
		Retry:                     agentRetryOptions(cfg.Retry),
		Compaction: agent.CompactionOptions{RetainTokens: cfg.Compaction.RetainTokens, MinRetainedTurns: cfg.Compaction.MinRetainedTurns,
			SummaryMaxTokens: cfg.Compaction.SummaryMaxTokens, Fallback: cfg.Compaction.Fallback, Guidance: cfg.Compaction.Guidance,
			AutoThresholdPercent: cfg.Compaction.AutoThresholdPercent, ToolHistoryBudgetPercent: cfg.Compaction.ToolHistoryBudgetPercent,
			ToolResultInlineBytes: cfg.Compaction.ToolResultInlineBytes, HistoricalToolResultThreshold: cfg.Compaction.HistoricalToolResultThreshold},
	})
	if err != nil {
		return nil, fmt.Errorf("app: agent: %w", err)
	}
	host.emitUserInput = ag.EmitUserInputRequest
	host.inEventCallback = ag.InEventCallback
	goalController.SetEmitter(func(ev protocol.AgentEvent) { ag.Publish(ev) })
	// Wire the trusted-host permission broker to the agent event stream and,
	// only in ask mode, make it the permission asker. Deny and allow modes
	// never consult the broker, and ask mode still blocks only when a handler
	// or manual replies are enabled (see Broker.Ask).
	permBroker.SetPublisher(func(ev protocol.AgentEvent) { ag.Publish(ev) })
	if permMode == permission.ModeAsk {
		perm.SetAsker(permBroker)
	}

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
					batch := make([]session.Entry, 0, len(messages))
					for i := range messages {
						message := &messages[i]
						message.ParentID = parent
						batch = append(batch, session.Entry{Type: session.EntryMessage, ID: message.ID, ParentID: parent, Message: message})
						parent = message.ID
					}
					if err := opened.AppendBatch(batch); err != nil {
						_ = opened.Close()
						return nil, err
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
			childProvider, childModel, err := runtimeSelection.childSelection(childCtx, spec.State.Provider, spec.State.Model)
			if err != nil {
				_ = childStore.Close()
				return nil, err
			}
			childSkillNames := skillNamesForRegistry(skillCatalog, childReg)
			childSystem := projectBasePrompt
			if catalog := skillPromptForRegistry(skillCatalog, childReg); catalog != "" {
				childSystem += "\n\n" + catalog
			}
			childSystem += "\n\n<subagent>\nYou are " + string(spec.State.Agent.Path) + ", an independent child agent. Complete the assigned task and return a concise final answer to your parent. The filesystem is shared with peers and is not a sandbox; do not overwrite peer work.\n"
			if spec.Role.System != "" {
				childSystem += spec.Role.System + "\n"
			}
			childSystem += "</subagent>"
			child, err := agent.New(agent.Options{Provider: childProvider, Registry: childReg, Session: childStore, Permission: childPerm, ToolHost: childHost,
				SystemPrompt: childSystem, ToolGuidance: runtimeToolGuidance(), FixedContextBudgetPercent: cfg.FixedContextBudgetPercent,
				Model: childModel, Thinking: spec.State.Thinking, ReasoningSummary: reasoningSummary,
				TextVerbosity: textVerbosity, CollaborationMode: protocol.ModeDefault, Identity: spec.State.Agent.Clone(),
				SkillNames: childSkillNames, Artifacts: artifactStore, Retry: agentRetryOptions(cfg.Retry), Compaction: agent.CompactionOptions{RetainTokens: cfg.Compaction.RetainTokens, MinRetainedTurns: cfg.Compaction.MinRetainedTurns,
					SummaryMaxTokens: cfg.Compaction.SummaryMaxTokens, Fallback: cfg.Compaction.Fallback, Guidance: cfg.Compaction.Guidance,
					AutoThresholdPercent: cfg.Compaction.AutoThresholdPercent, ToolHistoryBudgetPercent: cfg.Compaction.ToolHistoryBudgetPercent,
					ToolResultInlineBytes: cfg.Compaction.ToolResultInlineBytes, HistoricalToolResultThreshold: cfg.Compaction.HistoricalToolResultThreshold}})
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
		Cfg:                cfg,
		PersistedCfg:       persistedCfg,
		ConfigPath:         configPath,
		AuthPath:           authPath,
		BuildVersion:       buildVersion,
		Auth:               authStore,
		AuthService:        authService,
		Registry:           reg,
		Router:             router,
		Provider:           prov,
		ProviderID:         providerID,
		Providers:          providers,
		ProviderModules:    providerModules,
		Models:             append([]protocol.Model(nil), models...),
		AllModels:          allModels,
		modelCatalog:       modelCatalog,
		runtimeSelection:   runtimeSelection,
		Model:              model,
		Perm:               perm,
		Session:            st,
		Agent:              ag,
		Goal:               goalController,
		Trust:              tr,
		PluginManager:      manager,
		ProcessManager:     processManager,
		MCPManager:         mcpManager,
		MCPStatuses:        append([]publicmcp.Status(nil), mcpStatuses...),
		Skills:             skillCatalog,
		Subagents:          subManager,
		Diagnostics:        append([]config.Diagnostic(nil), configDiagnostics...),
		diagnosticSecrets:  collectDiagnosticSecrets(opts, cfg, authStore, authService),
		DebugDumpPath:      opts.DebugDumpPath,
		SearchPolicy:       searchPolicy,
		ProjectAllowed:     projectAllowed,
		ProjectInputRoot:   projectInputRoot,
		permissionBaseline: permMode,
		permissionOverride: opts.Permission != "",
		PermBroker:         permBroker,
		cwd:                absCWD,
		userInput:          inputBroker,
		toolGuard:          toolGuard,
		sessionHistory:     sessionHistory,
		sessionQuery:       sessionQuery,
		artifacts:          artifactStore,
	}
	if skillCatalog != nil {
		a.SkillDiagnostics = skillCatalog.Diagnostics()
	}
	if err := a.bindPermissionSession(st); err != nil {
		return nil, fmt.Errorf("app: permission state: %w", err)
	}
	a.Debugger = diagnostics.New(cfg.Debug.Enabled)
	ag.Subscribe(a.Debugger.Record)
	committed = true
	guardCommitted = true
	return a, nil
}
