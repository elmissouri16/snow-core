package app

import (
	"fmt"
	"sort"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/skills"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/pkg/protocol"
)

func (e *SessionDeleteCleanupError) Error() string {
	return "session deleted; cleanup warning: " + e.Err.Error()
}

func (e *SessionDeleteCleanupError) Unwrap() error { return e.Err }

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
