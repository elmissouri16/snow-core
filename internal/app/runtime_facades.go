package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	internalmcp "github.com/snow-core/snow/internal/mcp"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/userinput"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
	publicsandbox "github.com/snow-core/snow/pkg/sandbox"
)

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
			a.stateMu.Unlock()
			setErr := a.Agent.SetModel(model)
			a.stateMu.Lock()
			if setErr != nil {
				return fmt.Errorf("app: apply refreshed metadata for active model %q: %w", model.ID, setErr)
			}
			if currentProvider, ok := a.Providers[id]; !ok || currentProvider != p {
				return fmt.Errorf("app: provider %q configuration changed during model refresh", id)
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
				a.stateMu.Unlock()
				setErr := a.Agent.SetModel(fallback)
				a.stateMu.Lock()
				if setErr != nil {
					return fmt.Errorf("app: replace unavailable active model %q with %q: %w", current.ID, fallback.ID, setErr)
				}
				if currentProvider, ok := a.Providers[id]; !ok || currentProvider != p {
					return fmt.Errorf("app: provider %q configuration changed during model refresh", id)
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
	a.rebuildAllModelsLocked()
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

// SandboxStatus returns a secret-free snapshot of the Bash execution boundary.
func (a *App) SandboxStatus() publicsandbox.Status {
	status := publicsandbox.Status{Backend: "host"}
	if a == nil || a.Sandbox == nil {
		return status
	}
	record, configured, err := a.Sandbox.Record()
	if err != nil {
		return status
	}
	status.Configured = configured
	status.Active = configured && a.SandboxEnabled && a.Sandbox.Active()
	if !configured {
		return status
	}
	status.Backend = "smolvm"
	status.Machine = record.Machine
	status.Profile = record.Profile
	status.GuestCWD = record.GuestCWD
	status.ReadOnly = record.ReadOnly
	status.Network = record.Network
	status.CPUs = record.CPUs
	status.MemoryMiB = record.MemoryMiB
	status.StorageGiB = record.StorageGiB
	status.OverlayGiB = record.OverlayGiB
	return status
}

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
	declarations := mergeMCPDeclarations(global, project, explicit, "")
	out := make([]publicmcp.ServerSpec, 0, len(declarations))
	for _, declaration := range declarations {
		out = append(out, declaration.Spec)
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
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
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
