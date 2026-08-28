package app

import (
	"context"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/opencodezen"
)

var benchmarkStartupProvider startupProvider

func TestInitializeProviderDefersInactiveAdapters(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultProvider = "opencode-go"
	store := auth.NewMemoryStore()
	service := auth.NewService(store)
	startup, err := initializeProvider(Options{}, cfg, store, service)
	if err != nil {
		t.Fatal(err)
	}

	for _, module := range startup.modules.Modules() {
		lazy, deferred := module.Transport.(*provider.LazyTransport)
		if module.ID == startup.id {
			if deferred {
				t.Fatalf("active provider %q was wrapped lazily", module.ID)
			}
			continue
		}
		if !deferred {
			t.Errorf("inactive provider %q transport type = %T; want *provider.LazyTransport", module.ID, module.Transport)
			continue
		}
		if lazy.Materialized() {
			t.Errorf("inactive provider %q was materialized during startup", module.ID)
		}
	}

	zenModule, ok := startup.modules.Module(opencodezen.ProviderID)
	if !ok {
		t.Fatal("OpenCode Zen module is missing")
	}
	zenLazy, ok := zenModule.Transport.(*provider.LazyTransport)
	if !ok {
		t.Fatalf("OpenCode Zen transport type = %T", zenModule.Transport)
	}
	strict, ok := startup.providers[opencodezen.ProviderID].(interface{ RejectUnknownModels() bool })
	if !ok || !strict.RejectUnknownModels() {
		t.Fatal("lazy OpenCode Zen runtime did not preserve strict catalog metadata")
	}
	if !zenLazy.Materialized() {
		t.Fatal("using inactive provider metadata did not materialize its adapter")
	}
}

func TestInactiveProviderConstructionErrorIsDeferredUntilUse(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultProvider = "opencode-go"
	zenConfig := cfg.Providers[opencodezen.ProviderID]
	zenConfig.BaseURL = "://invalid"
	cfg.Providers[opencodezen.ProviderID] = zenConfig
	store := auth.NewMemoryStore()
	service := auth.NewService(store)
	startup, err := initializeProvider(Options{}, cfg, store, service)
	if err != nil {
		t.Fatalf("inactive provider prevented startup: %v", err)
	}
	zenConfig.BaseURL = "https://later.example"
	cfg.Providers[opencodezen.ProviderID] = zenConfig
	_, err = startup.providers[opencodezen.ProviderID].ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("inactive provider use error = %v; want base URL validation", err)
	}

	zenConfig.BaseURL = "://invalid"
	cfg.Providers[opencodezen.ProviderID] = zenConfig
	cfg.DefaultProvider = opencodezen.ProviderID
	activeStore := auth.NewMemoryStore()
	_, err = initializeProvider(Options{}, cfg, activeStore, auth.NewService(activeStore))
	if err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("active provider startup error = %v; want base URL validation", err)
	}
}

func BenchmarkInitializeProvider(b *testing.B) {
	cfg := config.Default()
	b.ReportAllocs()
	for b.Loop() {
		store := auth.NewMemoryStore()
		service := auth.NewService(store)
		startup, err := initializeProvider(Options{}, cfg, store, service)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStartupProvider = startup
	}
}
