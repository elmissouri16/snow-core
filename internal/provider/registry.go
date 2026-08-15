package provider

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/snow-core/snow/internal/auth"
)

// Module binds one built-in transport to its authentication driver. Provider
// construction remains provider-specific, while registration and runtime
// wrapping are uniform.
type Module struct {
	ID        string
	Order     int
	Transport Transport
	Auth      auth.Driver
}

// Registry is the authoritative deterministic inventory of provider modules.
type Registry struct {
	mu      sync.RWMutex
	modules map[string]Module
}

func NewRegistry() *Registry { return &Registry{modules: make(map[string]Module)} }

func (r *Registry) Register(module Module) error {
	if module.ID == "" {
		return errors.New("provider: module id is required")
	}
	if module.Transport == nil {
		return fmt.Errorf("provider: module %q has no transport", module.ID)
	}
	if module.Auth == nil {
		return fmt.Errorf("provider: module %q has no auth driver", module.ID)
	}
	if module.Transport.ID() != module.ID || module.Auth.Descriptor().ProviderID != module.ID {
		return fmt.Errorf("provider: module %q component ids do not match", module.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.modules[module.ID]; exists {
		return fmt.Errorf("provider: module %q already registered", module.ID)
	}
	r.modules[module.ID] = module
	return nil
}

// Replace atomically swaps an already registered module, preserving its order
// when the replacement leaves Order zero.
func (r *Registry) Replace(module Module) error {
	if module.ID == "" || module.Transport == nil || module.Auth == nil {
		return errors.New("provider: replacement module is incomplete")
	}
	if module.Transport.ID() != module.ID || module.Auth.Descriptor().ProviderID != module.ID {
		return fmt.Errorf("provider: module %q component ids do not match", module.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.modules[module.ID]
	if !exists {
		return fmt.Errorf("provider: module %q is not registered", module.ID)
	}
	if module.Order == 0 {
		module.Order = current.Order
	}
	r.modules[module.ID] = module
	return nil
}

func (r *Registry) Module(id string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	module, ok := r.modules[id]
	return module, ok
}

func (r *Registry) Modules() []Module {
	r.mu.RLock()
	out := make([]Module, 0, len(r.modules))
	for _, module := range r.modules {
		out = append(out, module)
	}
	r.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order == out[j].Order {
			return out[i].ID < out[j].ID
		}
		return out[i].Order < out[j].Order
	})
	return out
}

// Build registers all module auth drivers and returns credential-free runtimes.
func (r *Registry) Build(service *auth.Service) (map[string]Provider, error) {
	if service == nil {
		return nil, errors.New("provider: auth service is required")
	}
	modules := r.Modules()
	out := make(map[string]Provider, len(modules))
	for _, module := range modules {
		if !service.Registered(module.ID) {
			if err := service.Register(module.Auth); err != nil {
				return nil, err
			}
		}
		authenticated, err := NewAuthenticated(module.Transport, service)
		if err != nil {
			return nil, err
		}
		out[module.ID] = authenticated
	}
	return out, nil
}
