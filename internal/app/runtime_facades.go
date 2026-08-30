package app

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/config"
	internalmcp "github.com/elmissouri16/snow-core/internal/mcp"
	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/userinput"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (a *App) beginProviderTransition(operation string) (func(), error) {
	if !a.providerTransitionMu.TryLock() {
		return nil, fmt.Errorf("app: cannot %s while another provider transition is in progress", operation)
	}
	return a.providerTransitionMu.Unlock, nil
}

func (a *App) checkProviderTransitionAvailable(operation string) error {
	unlock, err := a.beginProviderTransition(operation)
	if err != nil {
		return err
	}
	unlock()
	return nil
}

func auxiliaryConfigFingerprint(globalDir, projectRoot string, projectAllowed bool) string {
	h := sha256.New()
	paths := []string{filepath.Join(globalDir, "keybindings.yaml"), filepath.Join(globalDir, "themes")}
	if projectAllowed {
		paths = append(paths, filepath.Join(projectRoot, ".snow", "keybindings.yaml"), filepath.Join(projectRoot, ".snow", "themes"))
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			fmt.Fprintf(h, "%s\x00missing\x00", path)
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00", path, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			fmt.Fprintf(h, "error:%v\x00", err)
			continue
		}
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
		count := 0
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") {
				continue
			}
			entryInfo, err := entry.Info()
			if err != nil || !entryInfo.Mode().IsRegular() {
				continue
			}
			fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00", entry.Name(), entryInfo.Mode(), entryInfo.Size(), entryInfo.ModTime().UnixNano())
			count++
			if count >= config.ThemeFileLimit+1 {
				break
			}
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ConfigDiagnostics returns an independent snapshot of non-fatal auxiliary
// configuration warnings, including lazily loaded theme and keybinding files.
func (a *App) ConfigDiagnostics() []protocol.ConfigDiagnostic {
	key := auxiliaryConfigFingerprint(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed) + "\x00" + a.Cfg.TUI.Theme
	a.diagnosticsMu.Lock()
	defer a.diagnosticsMu.Unlock()
	if key == a.diagnosticsCacheKey && a.diagnosticsCache != nil {
		return slices.Clone(a.diagnosticsCache)
	}
	all := slices.Clone(a.Diagnostics)
	themes, themeDiagnostics := config.LoadThemes(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	_, keyDiagnostics := config.LoadKeybindings(config.GlobalDir(), a.ProjectInputRoot, a.ProjectAllowed)
	selected := a.Cfg.TUI.Theme
	if !config.IsBuiltInTUITheme(selected) {
		if _, ok := themes[selected]; !ok {
			themeDiagnostics = append(themeDiagnostics, config.Diagnostic{Path: "tui.theme", Message: "selected custom theme is missing or invalid: " + selected})
		}
	}
	all = append(all, themeDiagnostics...)
	all = append(all, keyDiagnostics...)
	out := make([]protocol.ConfigDiagnostic, 0, len(all))
	for _, diagnostic := range all {
		out = append(out, protocol.ConfigDiagnostic{Path: diagnostic.Path, Message: diagnostic.Message})
	}
	a.diagnosticsCacheKey = key
	a.diagnosticsCache = slices.Clone(out)
	return out
}

// RefreshProviderModels forces an authenticated catalog refresh when the
// provider supports it and atomically replaces app/picker snapshots.
func (a *App) RefreshProviderModels(ctx context.Context, id string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.checkProviderTransitionAvailable("refresh provider models"); err != nil {
		return err
	}
	a.stateMu.Lock()
	p, ok := a.Providers[id]
	a.stateMu.Unlock()
	if !ok {
		return fmt.Errorf("app: provider %q is not available", id)
	}

	useRuntimeCatalog := false
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.RLock()
		useRuntimeCatalog = a.runtimeSelection.providers[id] == p
		a.runtimeSelection.mu.RUnlock()
	}
	var models []protocol.Model
	var err error
	if useRuntimeCatalog {
		models, err = a.runtimeSelection.ensureCatalog(ctx, id, true)
		if errors.Is(err, errCatalogConfigurationChanged) {
			return fmt.Errorf("app: provider %q configuration changed during model refresh", id)
		}
	} else if refreshable, ok := p.(interface {
		RefreshModels(context.Context) ([]protocol.Model, error)
	}); ok {
		models, err = refreshable.RefreshModels(ctx)
	} else {
		models, err = p.ListModels(ctx)
	}
	models = normalizeProviderModels(id, models)
	if len(models) == 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("app: provider %q returned an empty model catalog", id)
	}

	unlockTransition, transitionErr := a.beginProviderTransition("refresh provider models")
	if transitionErr != nil {
		return transitionErr
	}
	defer unlockTransition()
	a.stateMu.Lock()
	currentProvider, stillConfigured := a.Providers[id]
	active := a.ProviderID == id
	a.stateMu.Unlock()
	if !stillConfigured || currentProvider != p {
		return fmt.Errorf("app: provider %q configuration changed during model refresh", id)
	}

	var refreshedActive *protocol.Model
	if active {
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

	a.stateMu.Lock()
	a.modelCatalog[id] = cloneModels(models)
	if a.runtimeSelection != nil && !useRuntimeCatalog {
		a.runtimeSelection.mu.Lock()
		if a.runtimeSelection.providers[id] == p {
			a.runtimeSelection.catalogs[id] = cloneModels(models)
			a.runtimeSelection.catalogErrors[id] = err
		}
		a.runtimeSelection.mu.Unlock()
	}
	a.rebuildAllModelsLocked()
	if active {
		a.Models = cloneModels(models)
		if refreshedActive != nil {
			a.Model = refreshedActive.Clone()
			if a.runtimeSelection != nil {
				a.runtimeSelection.mu.Lock()
				a.runtimeSelection.model = refreshedActive.Clone()
				a.runtimeSelection.mu.Unlock()
			}
		}
	}
	a.stateMu.Unlock()
	return err
}

func modelCatalogAuthoritative(p provider.Provider) bool {
	authority, ok := p.(interface{ ModelCatalogAuthoritative() bool })
	return ok && authority.ModelCatalogAuthoritative()
}

func rejectsUnknownModels(p provider.Provider) bool {
	strict, ok := p.(interface{ RejectUnknownModels() bool })
	return ok && strict.RejectUnknownModels()
}

func configuredStreamIdleTimeout(milliseconds int) time.Duration {
	if milliseconds < 0 {
		return -1
	}
	if milliseconds == 0 {
		return 0
	}
	return time.Duration(milliseconds) * time.Millisecond
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
	return a.SetProviderContext(context.Background(), id)
}

// SetProviderContext is SetProvider with cancellation for lazy catalog
// discovery. The compatibility SetProvider method uses a background context.
func (a *App) SetProviderContext(ctx context.Context, id string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.checkProviderTransitionAvailable("set provider"); err != nil {
		return err
	}
	for {
		var loadErr error
		if a.runtimeSelection != nil {
			_, loadErr = a.runtimeSelection.ensureCatalog(ctx, id, false)
		}
		unlockTransition, err := a.beginProviderTransition("set provider")
		if err != nil {
			return err
		}

		a.stateMu.Lock()
		p, ok := a.Providers[id]
		catalog, loaded := a.modelCatalog[id]
		if a.runtimeSelection != nil {
			a.runtimeSelection.mu.RLock()
			runtimeProvider, runtimeOK := a.runtimeSelection.providers[id]
			if runtimeOK && runtimeProvider == p {
				catalog, loaded = a.runtimeSelection.catalogs[id]
				loadErr = a.runtimeSelection.catalogErrors[id]
				if loaded {
					a.modelCatalog[id] = cloneModels(catalog)
				}
			}
			a.runtimeSelection.mu.RUnlock()
		}
		catalog = cloneModels(catalog)
		target := a.Model
		a.stateMu.Unlock()
		if !ok {
			unlockTransition()
			return fmt.Errorf("app: provider %q is not available", id)
		}
		if a.runtimeSelection != nil && !loaded {
			unlockTransition()
			if loadErr != nil {
				return fmt.Errorf("app: discover models for provider %s: %w", id, loadErr)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}
		if provider.IsTransportInitializationError(loadErr) {
			unlockTransition()
			return fmt.Errorf("app: initialize provider %s: %w", id, loadErr)
		}
		if rejectsUnknownModels(p) && len(catalog) == 0 {
			unlockTransition()
			if loadErr != nil {
				return fmt.Errorf("app: provider %s has no maintained models currently available: %w", id, loadErr)
			}
			return fmt.Errorf("app: provider %s has no maintained models currently available", id)
		}
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
				unlockTransition()
				if loadErr != nil {
					return fmt.Errorf("app: discover models for provider %s: %w", id, loadErr)
				}
				return fmt.Errorf("app: provider %s has no default or discovered model", id)
			}
		}
		target.Provider = id
		if err := a.Agent.SetProviderAndModel(p, target); err != nil {
			unlockTransition()
			return err
		}
		a.stateMu.Lock()
		a.ProviderID, a.Provider = id, p
		a.Models = cloneModels(catalog)
		a.Model = target
		if a.runtimeSelection != nil {
			a.runtimeSelection.mu.Lock()
			a.runtimeSelection.provider = id
			a.runtimeSelection.model = target
			a.runtimeSelection.mu.Unlock()
		}
		a.stateMu.Unlock()
		unlockTransition()
		return nil
	}
}

// SetModel updates the active model and its app mirror. Unknown models remain
// permitted for providers that accept custom model identifiers.
func (a *App) SetModel(m protocol.Model) error {
	unlockTransition, err := a.beginProviderTransition("set model")
	if err != nil {
		return err
	}
	defer unlockTransition()
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("app: model id is required")
	}
	a.stateMu.Lock()
	providerID := a.ProviderID
	activeProvider := a.Provider
	catalog := cloneModels(a.modelCatalog[providerID])
	a.stateMu.Unlock()
	if m.Provider == "" {
		m.Provider = providerID
	}
	m = m.Clone()
	if m.Provider != providerID {
		return fmt.Errorf("app: model provider %q does not match active provider %q", m.Provider, providerID)
	}
	if rejectsUnknownModels(activeProvider) {
		found := false
		for _, candidate := range catalog {
			if candidate.ID == m.ID {
				m = candidate.Clone()
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("app: model %q is not available for provider %s", m.ID, providerID)
		}
	}
	if err := a.Agent.SetModel(m); err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.ProviderID != providerID || a.Provider != activeProvider {
		return errors.New("app: active provider changed while setting model")
	}
	a.Model = m
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.provider = providerID
		a.runtimeSelection.model = m
		a.runtimeSelection.mu.Unlock()
	}
	return nil
}

// SetProviderModelThinking updates the active provider, model, and effort as
// one admitted Agent transaction and refreshes the App/runtime mirrors.
func (a *App) SetProviderModelThinking(providerID string, model protocol.Model, level protocol.ThinkingLevel) error {
	return a.SetProviderModelThinkingContext(context.Background(), providerID, model, level)
}

// SetProviderModelThinkingContext is the cancellation-aware variant used when
// an inactive provider catalog may need lazy discovery.
func (a *App) SetProviderModelThinkingContext(ctx context.Context, providerID string, model protocol.Model, level protocol.ThinkingLevel) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.checkProviderTransitionAvailable("set provider model"); err != nil {
		return err
	}
	for {
		var loadErr error
		if a.runtimeSelection != nil {
			_, loadErr = a.runtimeSelection.ensureCatalog(ctx, providerID, false)
		}
		unlockTransition, err := a.beginProviderTransition("set provider model")
		if err != nil {
			return err
		}
		a.stateMu.Lock()
		p, ok := a.Providers[providerID]
		catalog, loaded := a.modelCatalog[providerID]
		if a.runtimeSelection != nil {
			a.runtimeSelection.mu.RLock()
			runtimeProvider, runtimeOK := a.runtimeSelection.providers[providerID]
			if runtimeOK && runtimeProvider == p {
				catalog, loaded = a.runtimeSelection.catalogs[providerID]
				loadErr = a.runtimeSelection.catalogErrors[providerID]
				if loaded {
					a.modelCatalog[providerID] = cloneModels(catalog)
				}
			}
			a.runtimeSelection.mu.RUnlock()
		}
		catalog = cloneModels(catalog)
		a.stateMu.Unlock()
		if !ok {
			unlockTransition()
			return fmt.Errorf("app: provider %q is not available", providerID)
		}
		if a.runtimeSelection != nil && !loaded {
			unlockTransition()
			if loadErr != nil {
				return fmt.Errorf("app: discover models for provider %s: %w", providerID, loadErr)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}
		if provider.IsTransportInitializationError(loadErr) {
			unlockTransition()
			return fmt.Errorf("app: initialize provider %s: %w", providerID, loadErr)
		}
		candidate := model.Clone()
		if candidate.Provider == "" {
			candidate.Provider = providerID
		}
		if candidate.Provider != providerID {
			unlockTransition()
			return fmt.Errorf("app: model provider %q does not match selected provider %q", candidate.Provider, providerID)
		}
		if rejectsUnknownModels(p) {
			found := false
			for _, available := range catalog {
				if available.ID == candidate.ID {
					candidate = available.Clone()
					found = true
					break
				}
			}
			if !found {
				unlockTransition()
				if loadErr != nil {
					return fmt.Errorf("app: model %q is not available for provider %s: %w", candidate.ID, providerID, loadErr)
				}
				return fmt.Errorf("app: model %q is not available for provider %s", candidate.ID, providerID)
			}
		}
		if err := a.Agent.SetProviderModelThinking(p, candidate, level); err != nil {
			unlockTransition()
			return err
		}
		a.stateMu.Lock()
		a.ProviderID, a.Provider = providerID, p
		a.Models = cloneModels(catalog)
		a.Model = candidate
		if a.runtimeSelection != nil {
			a.runtimeSelection.mu.Lock()
			a.runtimeSelection.provider = providerID
			a.runtimeSelection.model = candidate
			a.runtimeSelection.mu.Unlock()
		}
		a.stateMu.Unlock()
		unlockTransition()
		return nil
	}
}

// SetPermissionMode updates the active session mode. The permission service's
// change handler persists it with that session; the launch baseline used by a
// newly opened session remains unchanged.
func (a *App) SetPermissionMode(mode permission.Mode) error {
	if mode != permission.ModeAsk && mode != permission.ModeAllow && mode != permission.ModeDeny {
		return fmt.Errorf("app: invalid permission mode %q", mode)
	}
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

func mergeMCPDeclarations(global, project map[string]publicmcp.ServerSpec, explicit []publicmcp.ServerSpec, projectIdentity string) []internalmcp.Declaration {
	merged := make(map[string]internalmcp.Declaration, len(global)+len(project)+len(explicit))
	add := func(id, scope string, spec publicmcp.ServerSpec) {
		if spec.ID == "" {
			spec.ID = id
		}
		if spec.ID != "" {
			merged[spec.ID] = internalmcp.Declaration{Spec: spec, Scope: scope, ProjectIdentity: projectIdentity}
		}
	}
	for id, spec := range global {
		add(id, "global", spec)
	}
	for id, spec := range project {
		add(id, "project", spec)
	}
	for _, spec := range explicit {
		add(spec.ID, "explicit", spec)
	}
	ids := slices.Sorted(maps.Keys(merged))
	out := make([]internalmcp.Declaration, 0, len(ids))
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
	if a.DebugDumpPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if a.Agent != nil {
			if err := a.Agent.AbortContext(ctx); err != nil {
				errs = append(errs, fmt.Errorf("diagnostics: stop active turn before final dump: %w", err))
			}
			if err := a.Agent.WaitIdle(ctx); err != nil {
				errs = append(errs, fmt.Errorf("diagnostics: wait for final dump boundary: %w", err))
			}
		}
		if _, err := a.CreateDebugDump(ctx, a.DebugDumpPath); err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	if a.Agent != nil {
		a.Agent.Close()
	}
	if a.Debugger != nil {
		a.Debugger.Close()
	}
	if a.ProcessManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := a.ProcessManager.Close(ctx); err != nil {
			errs = append(errs, err)
		}
		cancel()
	}
	if a.userInput != nil {
		a.userInput.Close()
	}
	if a.PermBroker != nil {
		a.PermBroker.Close()
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
	if a.sessionQuery != nil {
		if err := a.sessionQuery.Close(); err != nil {
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

func (h *toolHost) CWD() string { return h.cwd }

func (h *toolHost) Roots() []string { return h.roots }

func (h *toolHost) Permission() permission.Service { return h.perm }

func (h *toolHost) Environ() []string { return nil }

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

// ClosePermissionBroker releases any pending permission request and prevents
// future interactive waits. RPC uses this when its input stream reaches EOF.
func (a *App) ClosePermissionBroker() {
	if a != nil && a.PermBroker != nil {
		a.PermBroker.Close()
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

func (a *App) CloseSubagent(ctx context.Context, target string) (protocol.AgentStatus, error) {
	if a.Subagents == nil {
		return protocol.AgentNotFound, errors.New("app: subagents disabled")
	}
	return a.Subagents.CloseAgent(ctx, a.Subagents.RootCaller(), target)
}

func (a *App) ResumeSubagent(ctx context.Context, target string) (protocol.SubagentState, error) {
	if a.Subagents == nil {
		return protocol.SubagentState{}, errors.New("app: subagents disabled")
	}
	return a.Subagents.ResumeAgent(ctx, a.Subagents.RootCaller(), target)
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

// ManagedProcessState and ManagedProcessLogs are app-facade snapshots used by
// first-party surfaces. They intentionally do not create a public RPC or SDK
// process-control contract.
type ManagedProcessState = managedprocess.State
type ManagedProcessLogs = managedprocess.LogsResult

func (a *App) ListManagedProcesses(ctx context.Context) ([]ManagedProcessState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.ProcessManager == nil {
		return nil, errors.New("app: managed processes unavailable")
	}
	return a.ProcessManager.List(), nil
}

func (a *App) ManagedProcessLogs(ctx context.Context, processID string, cursor *int64, maxBytes int) (ManagedProcessLogs, error) {
	if a.ProcessManager == nil {
		return ManagedProcessLogs{}, errors.New("app: managed processes unavailable")
	}
	return a.ProcessManager.Logs(ctx, managedprocess.LogsRequest{ProcessID: processID, Cursor: cursor, MaxBytes: maxBytes})
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

// EnablePermissionReplies enables manual RPC/SDK resolution of ask-mode
// permission requests. It does not change the permission mode.
func (a *App) EnablePermissionReplies() {
	if a != nil && a.PermBroker != nil {
		a.PermBroker.EnableManual()
	}
}

// ReplyPermission resolves the pending ask-mode permission request.
func (a *App) ReplyPermission(response protocol.PermissionResponse) error {
	if a == nil || a.PermBroker == nil {
		return errors.New("app: permission broker unavailable")
	}
	return a.PermBroker.Reply(response.RequestID, permission.Decision(response.Decision))
}

// RejectPermission declines the pending ask-mode permission request.
func (a *App) RejectPermission(requestID string) error {
	if a == nil || a.PermBroker == nil {
		return errors.New("app: permission broker unavailable")
	}
	return a.PermBroker.Reject(requestID)
}

// SkillInventoryPublic returns the full skill catalog and its discovery
// diagnostics as protocol-level secret-free entries for remote surfaces.
func (a *App) SkillInventoryPublic() (protocol.RPCSkillsList, error) {
	out := protocol.RPCSkillsList{}
	if a == nil || a.Skills == nil {
		out.Skills = []protocol.RPCSkill{}
		return out, nil
	}
	skills := a.Skills.Inventory()
	out.Skills = make([]protocol.RPCSkill, 0, len(skills))
	for _, skill := range skills {
		metadata := make(map[string]string, len(skill.Metadata))
		maps.Copy(metadata, skill.Metadata)
		out.Skills = append(out.Skills, protocol.RPCSkill{
			Name: skill.Name, Description: skill.Description, License: skill.License,
			Compatibility: skill.Compatibility, Metadata: metadata, AllowedTools: skill.AllowedTools,
			Location: skill.Location, Scope: skill.Scope, Source: skill.Source,
			Enabled: skill.Enabled, DisabledBy: skill.DisabledBy,
		})
	}
	if len(out.Skills) == 0 {
		out.Skills = []protocol.RPCSkill{}
	}
	diagnostics := a.Skills.Diagnostics()
	if len(a.SkillDiagnostics) > 0 {
		diagnostics = append(diagnostics, a.SkillDiagnostics...)
	}
	out.Diagnostics = make([]protocol.RPCSkillDiagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		out.Diagnostics = append(out.Diagnostics, protocol.RPCSkillDiagnostic{Path: d.Path, Skill: d.Skill, Level: d.Level, Message: d.Message})
	}
	return out, nil
}
