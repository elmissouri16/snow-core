package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

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

// LoadProviderCatalogs resolves every currently available provider catalog on
// demand and refreshes the combined picker snapshot. Partial results are kept
// when one inactive provider cannot be listed.
func (a *App) LoadProviderCatalogs(ctx context.Context) ([]protocol.Model, error) {
	return a.loadProviderCatalogs(ctx, false)
}

// RefreshProviderCatalogs bypasses adapter caches where supported and publishes
// the refreshed picker snapshot without changing the selected model.
func (a *App) RefreshProviderCatalogs(ctx context.Context) ([]protocol.Model, error) {
	return a.loadProviderCatalogs(ctx, true)
}

func (a *App) loadProviderCatalogs(ctx context.Context, force bool) ([]protocol.Model, error) {
	if a == nil || a.runtimeSelection == nil {
		return nil, errors.New("app: provider catalogs unavailable")
	}
	_, loadErr := a.runtimeSelection.loadAvailableModels(ctx, force)
	// Publish one generation-consistent snapshot. Profile reconfiguration uses
	// the same stateMu -> runtimeSelection.mu lock order, so an obsolete lazy
	// load cannot restore the replaced provider's catalog in App mirrors.
	a.stateMu.Lock()
	a.runtimeSelection.mu.RLock()
	providerIDs := make([]string, 0, len(a.runtimeSelection.catalogs))
	catalogs := make(map[string][]protocol.Model, len(a.runtimeSelection.catalogs))
	for id, catalog := range a.runtimeSelection.catalogs {
		providerIDs = append(providerIDs, id)
		catalogs[id] = cloneModels(catalog)
	}
	a.runtimeSelection.mu.RUnlock()
	slices.Sort(providerIDs)
	var models []protocol.Model
	for _, id := range providerIDs {
		models = append(models, cloneModels(catalogs[id])...)
	}
	a.modelCatalog = catalogs
	a.AllModels = cloneModels(models)
	if active, ok := catalogs[a.ProviderID]; ok {
		a.Models = cloneModels(active)
	}
	a.stateMu.Unlock()
	return cloneModels(models), loadErr
}

// ResolveProviderModel returns one exact catalog-backed provider/model pair.
// It loads only the requested provider and does not change the active selection.
func (a *App) ResolveProviderModel(ctx context.Context, providerID, modelID string) (protocol.Model, error) {
	if a == nil || a.runtimeSelection == nil {
		return protocol.Model{}, errors.New("app: provider catalogs unavailable")
	}
	models, loadErr := a.runtimeSelection.ensureCatalog(ctx, providerID, false)
	for _, model := range models {
		if model.ID == modelID {
			return model.Clone(), nil
		}
	}
	if loadErr != nil {
		return protocol.Model{}, fmt.Errorf("app: discover models for provider %s: %w", providerID, loadErr)
	}
	return protocol.Model{}, fmt.Errorf("app: model %q is unavailable for provider %s", modelID, providerID)
}

// SubagentModels returns exact provider/model pairs currently available to children.
func (a *App) SubagentModels() []protocol.Model {
	if a == nil || a.runtimeSelection == nil {
		return nil
	}
	return a.runtimeSelection.cachedModels()
}
