package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (e *SessionDeleteCleanupError) Error() string {
	return "session deleted; cleanup warning: " + e.Err.Error()
}

func (e *SessionDeleteCleanupError) Unwrap() error { return e.Err }

var errCatalogConfigurationChanged = errors.New("app: provider configuration changed during catalog load")

func (s *liveRuntimeSelection) childSelection(ctx context.Context, providerID, modelID string) (provider.Provider, protocol.Model, error) {
	requestedProvider, requestedModel := providerID, modelID
	for {
		s.mu.RLock()
		providerID = requestedProvider
		if providerID == "" {
			providerID = s.provider
		}
		p, ok := s.providers[providerID]
		active := s.model.Clone()
		s.mu.RUnlock()
		if !ok {
			return nil, protocol.Model{}, fmt.Errorf("app: subagent provider %q is unavailable", providerID)
		}
		models, listErr := s.ensureCatalog(ctx, providerID, false)

		// Discovery can race with compatible-profile replacement or an active
		// provider switch. Pair a catalog only with the exact transport snapshot
		// that initiated it; otherwise restart against the current selection.
		s.mu.RLock()
		currentProviderID := requestedProvider
		if currentProviderID == "" {
			currentProviderID = s.provider
		}
		currentProvider := s.providers[currentProviderID]
		currentModels, catalogLoaded := s.catalogs[currentProviderID]
		currentListErr := s.catalogErrors[currentProviderID]
		currentModels = cloneModels(currentModels)
		s.mu.RUnlock()
		if currentProviderID != providerID || currentProvider != p {
			continue
		}
		if catalogLoaded {
			models, listErr = currentModels, currentListErr
		}

		modelID = requestedModel
		if modelID == "" && active.Provider == providerID {
			modelID = active.ID
		}
		if modelID == "" {
			if defaults, ok := p.(interface{ DefaultModel() protocol.Model }); ok {
				modelID = defaults.DefaultModel().ID
			}
			if modelID == "" && len(models) > 0 {
				modelID = models[0].ID
			}
		}
		for _, candidate := range models {
			if candidate.ID == modelID {
				return p, candidate, nil
			}
		}
		// The active model may be an explicit custom ID intentionally preserved
		// when discovery is unavailable. Children inheriting that exact selection
		// must not require it to appear in the remote catalog.
		if active.Provider == providerID && active.ID == modelID {
			return p, active, nil
		}
		if listErr != nil {
			return nil, protocol.Model{}, fmt.Errorf("app: discover subagent models for provider %s: %w", providerID, listErr)
		}
		return nil, protocol.Model{}, fmt.Errorf("app: subagent model %q is unavailable for provider %s", modelID, providerID)
	}
}

func (s *liveRuntimeSelection) ensureCatalog(ctx context.Context, providerID string, force bool) ([]protocol.Model, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		s.mu.Lock()
		p, ok := s.providers[providerID]
		if !ok {
			s.mu.Unlock()
			return nil, fmt.Errorf("app: provider %q is unavailable", providerID)
		}
		if !force {
			if models, loaded := s.catalogs[providerID]; loaded {
				err := s.catalogErrors[providerID]
				out := cloneModels(models)
				s.mu.Unlock()
				return out, err
			}
		}
		if load := s.catalogLoads[providerID]; load != nil {
			done := load.done
			s.mu.Unlock()
			select {
			case <-done:
				// A forced refresh waits for an ordinary lazy load and then performs
				// its own refresh instead of accepting the just-cached result.
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if s.catalogLoads == nil {
			s.catalogLoads = make(map[string]*catalogLoad)
		}
		generation := s.catalogGeneration[providerID]
		load := &catalogLoad{done: make(chan struct{}), generation: generation}
		s.catalogLoads[providerID] = load
		s.mu.Unlock()

		var models []protocol.Model
		var err error
		if force {
			if refreshable, ok := p.(interface {
				RefreshModels(context.Context) ([]protocol.Model, error)
			}); ok {
				models, err = refreshable.RefreshModels(ctx)
			} else {
				models, err = p.ListModels(ctx)
			}
		} else {
			models, err = p.ListModels(ctx)
		}
		models = normalizeProviderModels(providerID, models)

		s.mu.Lock()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if s.catalogLoads[providerID] == load {
				delete(s.catalogLoads, providerID)
			}
			close(load.done)
			s.mu.Unlock()
			return nil, err
		}
		if s.catalogGeneration[providerID] != load.generation {
			if s.catalogLoads[providerID] == load {
				delete(s.catalogLoads, providerID)
			}
			close(load.done)
			s.mu.Unlock()
			if force {
				return nil, errCatalogConfigurationChanged
			}
			continue
		}
		if force && len(models) == 0 {
			if s.catalogLoads[providerID] == load {
				delete(s.catalogLoads, providerID)
			}
			close(load.done)
			s.mu.Unlock()
			return nil, err
		}
		s.catalogs[providerID] = cloneModels(models)
		if s.catalogErrors == nil {
			s.catalogErrors = make(map[string]error)
		}
		s.catalogErrors[providerID] = err
		if s.catalogLoads[providerID] == load {
			delete(s.catalogLoads, providerID)
		}
		close(load.done)
		s.mu.Unlock()
		return cloneModels(models), err
	}
}

func (s *liveRuntimeSelection) preloadCatalogs(ctx context.Context, providerIDs []string) error {
	type result struct {
		provider string
		err      error
	}
	results := make(chan result, len(providerIDs))
	for _, id := range providerIDs {
		go func(providerID string) {
			_, err := s.ensureCatalog(ctx, providerID, false)
			results <- result{provider: providerID, err: err}
		}(id)
	}
	var errs []error
	for range providerIDs {
		loaded := <-results
		if loaded.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", loaded.provider, loaded.err))
		}
	}
	return errors.Join(errs...)
}

func requiredSubagentProviders(cfg config.SubagentConfig, activeProvider string) []string {
	ids := make(map[string]bool)
	if cfg.DefaultProvider != "" && cfg.DefaultProvider != activeProvider {
		ids[cfg.DefaultProvider] = true
	}
	for _, role := range cfg.Roles {
		providerID := role.Provider
		if providerID == "" {
			providerID = cfg.DefaultProvider
		}
		if providerID != "" && providerID != activeProvider {
			ids[providerID] = true
		}
	}
	out := slices.Sorted(maps.Keys(ids))
	return out
}

func (s *liveRuntimeSelection) availableModels(ctx context.Context) ([]protocol.Model, error) {
	s.mu.RLock()
	providerIDs := make([]string, 0, len(s.providers))
	for id := range s.providers {
		providerIDs = append(providerIDs, id)
	}
	s.mu.RUnlock()
	slices.Sort(providerIDs)
	type result struct {
		id     string
		models []protocol.Model
		err    error
	}
	results := make(chan result, len(providerIDs))
	for _, id := range providerIDs {
		go func(providerID string) {
			models, err := s.ensureCatalog(ctx, providerID, false)
			results <- result{id: providerID, models: models, err: err}
		}(id)
	}
	byProvider := make(map[string][]protocol.Model, len(providerIDs))
	errorsByProvider := make(map[string]error)
	for range providerIDs {
		loaded := <-results
		byProvider[loaded.id] = loaded.models
		if loaded.err != nil {
			errorsByProvider[loaded.id] = loaded.err
		}
	}
	var out []protocol.Model
	var errs []error
	for _, id := range providerIDs {
		out = append(out, cloneModels(byProvider[id])...)
		if err := errorsByProvider[id]; err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
	}
	return out, errors.Join(errs...)
}

func (s *liveRuntimeSelection) cachedModels() []protocol.Model {
	s.mu.RLock()
	providerIDs := slices.Sorted(maps.Keys(s.catalogs))
	var out []protocol.Model
	for _, id := range providerIDs {
		out = append(out, cloneModels(s.catalogs[id])...)
	}
	s.mu.RUnlock()
	return out
}

func cloneModels(models []protocol.Model) []protocol.Model {
	out := make([]protocol.Model, len(models))
	for i := range models {
		out[i] = models[i].Clone()
	}
	return out
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
	reader, readerOK := registry.Descriptor("read_skill_resource")
	deactivator, deactivatorOK := registry.Descriptor("deactivate_skill")
	return catalog.CatalogPromptForToolAvailability(
		readerOK && reader.Owner == "skills",
		deactivatorOK && deactivator.Owner == "skills",
	)
}

// DefaultPaths resolves config/auth paths from the environment.
func DefaultPaths() (configPath, authPath string) {
	c, a, _ := config.DefaultPaths()
	return c, a
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
