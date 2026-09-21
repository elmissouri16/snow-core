package app

import (
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider"
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
