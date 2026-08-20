package app

import (
	"testing"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestLiveRuntimeSelectionCrossProviderDefaultAndCatalog(t *testing.T) {
	first := fake.NewWithModels([]protocol.Model{{Provider: "first", ID: "root"}})
	second := fake.NewWithModels([]protocol.Model{{Provider: "second", ID: "child"}})
	selection := &liveRuntimeSelection{
		provider: "first", model: protocol.Model{Provider: "first", ID: "root"},
		providers: map[string]provider.Provider{"first": first, "second": second},
		catalogs:  map[string][]protocol.Model{"first": {{Provider: "first", ID: "root"}}, "second": {{Provider: "second", ID: "child"}}},
	}
	gotProvider, gotModel, err := selection.childSelection("second", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotProvider != second || gotModel.Provider != "second" || gotModel.ID != "child" {
		t.Fatalf("cross-provider default = %T %+v", gotProvider, gotModel)
	}
	models := selection.availableModels()
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
	gotProvider, gotModel, err := selection.childSelection("", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotProvider != second || gotModel.ID != "other" {
		t.Fatalf("switch selection provider=%T model=%+v", gotProvider, gotModel)
	}

	selection.mu.Lock()
	selection.catalogs["second"] = []protocol.Model{{Provider: "second", ID: "other", SupportsVision: true}}
	selection.mu.Unlock()
	_, refreshed, err := selection.childSelection("second", "other")
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed.SupportsVision {
		t.Fatalf("child used startup catalog capture: %+v", refreshed)
	}

	selection.mu.Lock()
	selection.model = protocol.Model{Provider: "second", ID: "explicit-custom", SupportsTools: true}
	selection.mu.Unlock()
	_, custom, err := selection.childSelection("", "")
	if err != nil {
		t.Fatalf("inherit explicit custom model: %v", err)
	}
	if custom.ID != "explicit-custom" || !custom.SupportsTools {
		t.Fatalf("custom model = %+v", custom)
	}
}
