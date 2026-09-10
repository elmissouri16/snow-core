package app

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type expiringCatalogProvider struct {
	provider.Provider
	stale            atomic.Bool
	revision         atomic.Uint64
	models           []protocol.Model
	lists, refreshes int
}

func (p *expiringCatalogProvider) ModelCatalogStale() bool         { return p.stale.Load() }
func (p *expiringCatalogProvider) ModelCatalogRevision() uint64    { return p.revision.Load() }
func (p *expiringCatalogProvider) ModelCatalogAuthoritative() bool { return true }
func (p *expiringCatalogProvider) ListModels(context.Context) ([]protocol.Model, error) {
	p.lists++
	p.stale.Store(false)
	return cloneModels(p.models), nil
}
func (p *expiringCatalogProvider) RefreshModels(ctx context.Context) ([]protocol.Model, error) {
	p.refreshes++
	p.revision.Add(1)
	return p.ListModels(ctx)
}

func TestPickerCatalogRefreshesExpiryRevisionAndForcedEmptyCatalog(t *testing.T) {
	old := protocol.Model{Provider: "fake", ID: "old"}
	p := &expiringCatalogProvider{Provider: fake.NewWithModels([]protocol.Model{old}), models: []protocol.Model{old}}
	a := &App{
		ProviderID: "fake", Model: old,
		runtimeSelection: &liveRuntimeSelection{
			provider: "fake", model: old,
			providers: map[string]provider.Provider{"fake": p},
			catalogs:  map[string][]protocol.Model{"fake": {old}},
		},
	}
	models, err := a.LoadProviderCatalogs(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "old" || p.lists != 0 {
		t.Fatalf("fresh snapshot=%+v lists=%d err=%v", models, p.lists, err)
	}
	p.models = []protocol.Model{{Provider: "fake", ID: "new"}}
	p.stale.Store(true)
	models, err = a.LoadProviderCatalogs(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "new" || p.lists != 1 ||
		len(a.AllModels) != 1 || a.AllModels[0].ID != "new" || a.Models[0].ID != "new" {
		t.Fatalf("expired snapshot=%+v lists=%d err=%v", models, p.lists, err)
	}
	// A direct adapter refresh (for example before Chat) publishes a new
	// revision while the provider cache itself is still fresh.
	p.models = []protocol.Model{{Provider: "fake", ID: "newer"}}
	p.revision.Add(1)
	models, err = a.LoadProviderCatalogs(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "newer" || p.lists != 2 {
		t.Fatalf("revision snapshot=%+v lists=%d err=%v", models, p.lists, err)
	}
	p.models = nil
	models, err = a.RefreshProviderCatalogs(t.Context())
	if err != nil || len(models) != 0 || len(a.AllModels) != 0 || len(a.Models) != 0 || p.refreshes != 1 {
		t.Fatalf("empty snapshot=%+v refreshes=%d err=%v", models, p.refreshes, err)
	}
	if a.Model.ID != "old" || a.runtimeSelection.model.ID != "old" {
		t.Fatal("browsing catalogs changed the active model")
	}
}
