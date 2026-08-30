package app

import (
	"context"
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type blockingCatalogProvider struct {
	provider.Provider
	models  []protocol.Model
	started chan struct{}
	release chan struct{}
}

func (p *blockingCatalogProvider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	close(p.started)
	select {
	case <-p.release:
		return cloneModels(p.models), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type emptyRefreshProvider struct {
	provider.Provider
	err error
}

func (p *emptyRefreshProvider) RefreshModels(context.Context) ([]protocol.Model, error) {
	return nil, p.err
}

type initializationFailureProvider struct {
	provider.Provider
	id  string
	err error
}

func (p *initializationFailureProvider) ID() string { return p.id }
func (p *initializationFailureProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, p.err
}

type cancellationThenCatalogProvider struct {
	provider.Provider
	models []protocol.Model
	calls  int
}

func (p *cancellationThenCatalogProvider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	p.calls++
	if p.calls == 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return cloneModels(p.models), nil
}

type countingCatalogProvider struct {
	provider.Provider
	models []protocol.Model
	calls  int
}

func (p *countingCatalogProvider) ListModels(context.Context) ([]protocol.Model, error) {
	p.calls++
	return cloneModels(p.models), nil
}

func TestLiveRuntimeSelectionDiscardsCatalogFromReplacedProvider(t *testing.T) {
	oldBase := fake.NewWithModels([]protocol.Model{{Provider: "shared", ID: "old"}})
	old := &blockingCatalogProvider{
		Provider: oldBase,
		models:   []protocol.Model{{Provider: "shared", ID: "old"}},
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	newProvider := &countingCatalogProvider{
		Provider: fake.NewWithModels([]protocol.Model{{Provider: "shared", ID: "new"}}),
		models:   []protocol.Model{{Provider: "shared", ID: "new"}},
	}
	selection := &liveRuntimeSelection{
		providers: map[string]provider.Provider{"shared": old},
		catalogs:  map[string][]protocol.Model{}, catalogErrors: map[string]error{},
		catalogLoads: map[string]*catalogLoad{}, catalogGeneration: map[string]uint64{},
	}
	type loadResult struct {
		models []protocol.Model
		err    error
	}
	result := make(chan loadResult, 1)
	go func() {
		models, err := selection.ensureCatalog(context.Background(), "shared", false)
		result <- loadResult{models: models, err: err}
	}()
	<-old.started
	selection.mu.Lock()
	selection.providers["shared"] = newProvider
	selection.catalogGeneration["shared"]++
	delete(selection.catalogs, "shared")
	delete(selection.catalogErrors, "shared")
	selection.mu.Unlock()
	close(old.release)
	loaded := <-result
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	if len(loaded.models) != 1 || loaded.models[0].ID != "new" || newProvider.calls != 1 {
		t.Fatalf("loaded=%+v new calls=%d", loaded.models, newProvider.calls)
	}
}

func TestChildSelectionPairsRetriedCatalogWithReplacementProvider(t *testing.T) {
	old := &blockingCatalogProvider{
		Provider: fake.NewWithModels([]protocol.Model{{Provider: "shared", ID: "old"}}),
		models:   []protocol.Model{{Provider: "shared", ID: "old"}},
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	replacement := &countingCatalogProvider{
		Provider: fake.NewWithModels([]protocol.Model{{Provider: "shared", ID: "new"}}),
		models:   []protocol.Model{{Provider: "shared", ID: "new"}},
	}
	selection := &liveRuntimeSelection{
		provider: "shared", model: protocol.Model{Provider: "shared", ID: "new"},
		providers: map[string]provider.Provider{"shared": old}, catalogs: map[string][]protocol.Model{},
		catalogErrors: map[string]error{}, catalogLoads: map[string]*catalogLoad{}, catalogGeneration: map[string]uint64{},
	}
	type result struct {
		provider provider.Provider
		model    protocol.Model
		err      error
	}
	done := make(chan result, 1)
	go func() {
		gotProvider, model, err := selection.childSelection(context.Background(), "shared", "new")
		done <- result{provider: gotProvider, model: model, err: err}
	}()
	<-old.started
	selection.mu.Lock()
	selection.providers["shared"] = replacement
	selection.catalogGeneration["shared"]++
	selection.mu.Unlock()
	close(old.release)
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.provider != replacement || got.model.ID != "new" || replacement.calls != 1 {
		t.Fatalf("provider=%T model=%+v replacement calls=%d", got.provider, got.model, replacement.calls)
	}
}

func TestEmptyForcedRefreshRetainsLastGoodCatalog(t *testing.T) {
	refreshErr := errors.New("refresh unavailable")
	candidate := &emptyRefreshProvider{
		Provider: fake.NewWithModels([]protocol.Model{{Provider: "keep", ID: "good"}}),
		err:      refreshErr,
	}
	selection := &liveRuntimeSelection{
		providers:     map[string]provider.Provider{"keep": candidate},
		catalogs:      map[string][]protocol.Model{"keep": {{Provider: "keep", ID: "good"}}},
		catalogErrors: map[string]error{"keep": nil}, catalogLoads: map[string]*catalogLoad{}, catalogGeneration: map[string]uint64{},
	}
	if models, err := selection.ensureCatalog(context.Background(), "keep", true); !errors.Is(err, refreshErr) || len(models) != 0 {
		t.Fatalf("forced refresh models=%+v error=%v", models, err)
	}
	models, err := selection.ensureCatalog(context.Background(), "keep", false)
	if err != nil || len(models) != 1 || models[0].ID != "good" {
		t.Fatalf("cached catalog after failed refresh=%+v error=%v", models, err)
	}
}

func TestSetProviderModelRejectsDeferredInitializationFailure(t *testing.T) {
	a, err := New(context.Background(), Options{
		Provider: "fake", Model: "current", NoSession: true, Permission: "deny", CWD: t.TempDir(),
		NoPlugins: true, NoMCP: true, NoSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	initialModel := a.Agent.Model()
	initializationErr := &provider.TransportInitializationError{Provider: "broken", Err: errors.New("invalid endpoint")}
	broken := &initializationFailureProvider{Provider: fake.NewWithModels(nil), id: "broken", err: initializationErr}
	a.stateMu.Lock()
	a.Providers["broken"] = broken
	a.stateMu.Unlock()
	a.runtimeSelection.mu.Lock()
	a.runtimeSelection.providers["broken"] = broken
	a.runtimeSelection.mu.Unlock()

	err = a.SetProviderModelThinkingContext(context.Background(), "broken", protocol.Model{Provider: "broken", ID: "unvalidated"}, protocol.ThinkingOff)
	if !provider.IsTransportInitializationError(err) {
		t.Fatalf("SetProviderModelThinkingContext() error = %v; want transport initialization error", err)
	}
	currentModel := a.Agent.Model()
	if a.ProviderID != "fake" || currentModel.Provider != initialModel.Provider || currentModel.ID != initialModel.ID {
		t.Fatalf("failed transition mutated provider/model: provider=%q model=%+v", a.ProviderID, currentModel)
	}
}

func TestCanceledCatalogLoadIsNotCached(t *testing.T) {
	candidate := &cancellationThenCatalogProvider{
		Provider: fake.NewWithModels([]protocol.Model{{Provider: "retry", ID: "model"}}),
		models:   []protocol.Model{{Provider: "retry", ID: "model"}},
	}
	selection := &liveRuntimeSelection{
		providers: map[string]provider.Provider{"retry": candidate}, catalogs: map[string][]protocol.Model{},
		catalogErrors: map[string]error{}, catalogLoads: map[string]*catalogLoad{}, catalogGeneration: map[string]uint64{},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := selection.ensureCatalog(ctx, "retry", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("first load error=%v, want canceled", err)
	}
	models, err := selection.ensureCatalog(context.Background(), "retry", false)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.calls != 2 || len(models) != 1 || models[0].ID != "model" {
		t.Fatalf("calls=%d models=%+v", candidate.calls, models)
	}
}

func TestLiveRuntimeSelectionLoadsInactiveCatalogOnceOnDemand(t *testing.T) {
	firstBase := fake.NewWithModels([]protocol.Model{{Provider: "first", ID: "root"}})
	secondBase := fake.NewWithModels([]protocol.Model{{Provider: "second", ID: "child"}})
	first := &countingCatalogProvider{Provider: firstBase, models: []protocol.Model{{Provider: "first", ID: "root"}}}
	second := &countingCatalogProvider{Provider: secondBase, models: []protocol.Model{{Provider: "second", ID: "child"}}}
	selection := &liveRuntimeSelection{
		provider: "first", model: protocol.Model{Provider: "first", ID: "root"},
		providers:     map[string]provider.Provider{"first": first, "second": second},
		catalogs:      map[string][]protocol.Model{"first": {{Provider: "first", ID: "root"}}},
		catalogErrors: map[string]error{}, catalogLoads: map[string]*catalogLoad{},
	}
	if got := selection.cachedModels(); len(got) != 1 || second.calls != 0 {
		t.Fatalf("startup cache=%+v inactive calls=%d", got, second.calls)
	}
	models, err := selection.availableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || first.calls != 0 || second.calls != 1 {
		t.Fatalf("models=%+v calls first=%d second=%d", models, first.calls, second.calls)
	}
	if _, err := selection.availableModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.calls != 1 {
		t.Fatalf("cached inactive catalog reloaded %d times", second.calls)
	}
}

func TestLiveRuntimeSelectionCrossProviderDefaultAndCatalog(t *testing.T) {
	first := fake.NewWithModels([]protocol.Model{{Provider: "first", ID: "root"}})
	second := fake.NewWithModels([]protocol.Model{{Provider: "second", ID: "child"}})
	selection := &liveRuntimeSelection{
		provider: "first", model: protocol.Model{Provider: "first", ID: "root"},
		providers: map[string]provider.Provider{"first": first, "second": second},
		catalogs:  map[string][]protocol.Model{"first": {{Provider: "first", ID: "root"}}, "second": {{Provider: "second", ID: "child"}}},
	}
	gotProvider, gotModel, err := selection.childSelection(context.Background(), "second", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotProvider != second || gotModel.Provider != "second" || gotModel.ID != "child" {
		t.Fatalf("cross-provider default = %T %+v", gotProvider, gotModel)
	}
	models, err := selection.availableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Provider != "first" || models[1].Provider != "second" {
		t.Fatalf("available models = %+v", models)
	}
	models[0].ID = "mutated"
	if selection.catalogs["first"][0].ID != "root" {
		t.Fatal("availableModels returned aliased metadata")
	}
}

func TestLiveRuntimeSelectionFollowsProviderSwitchAndCatalogRefresh(t *testing.T) {
	first := fake.NewWithModels([]protocol.Model{{Provider: "first", ID: "old"}})
	second := fake.NewWithModels([]protocol.Model{{Provider: "second", ID: "other"}})
	selection := &liveRuntimeSelection{
		provider: "first",
		model:    protocol.Model{Provider: "first", ID: "old"},
		providers: map[string]provider.Provider{
			"first": first, "second": second,
		},
		catalogs: map[string][]protocol.Model{
			"first":  {{Provider: "first", ID: "old"}},
			"second": {{Provider: "second", ID: "other"}},
		},
	}
	selection.mu.Lock()
	selection.provider = "second"
	selection.model = protocol.Model{Provider: "second", ID: "other"}
	selection.mu.Unlock()
	gotProvider, gotModel, err := selection.childSelection(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotProvider != second || gotModel.ID != "other" {
		t.Fatalf("switch selection provider=%T model=%+v", gotProvider, gotModel)
	}

	selection.mu.Lock()
	selection.catalogs["second"] = []protocol.Model{{Provider: "second", ID: "other", SupportsVision: true}}
	selection.mu.Unlock()
	_, refreshed, err := selection.childSelection(context.Background(), "second", "other")
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed.SupportsVision {
		t.Fatalf("child used startup catalog capture: %+v", refreshed)
	}

	selection.mu.Lock()
	selection.model = protocol.Model{Provider: "second", ID: "explicit-custom", SupportsTools: true}
	selection.mu.Unlock()
	_, custom, err := selection.childSelection(context.Background(), "", "")
	if err != nil {
		t.Fatalf("inherit explicit custom model: %v", err)
	}
	if custom.ID != "explicit-custom" || !custom.SupportsTools {
		t.Fatalf("custom model = %+v", custom)
	}
}
