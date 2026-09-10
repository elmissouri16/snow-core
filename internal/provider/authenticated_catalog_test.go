package provider

import (
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
)

type expiringCatalogTransport struct {
	staticCatalogTransport
	stale    bool
	revision uint64
}

func (t *expiringCatalogTransport) ModelCatalogStale() bool      { return t.stale }
func (t *expiringCatalogTransport) ModelCatalogRevision() uint64 { return t.revision }

func TestAuthenticatedForwardsCatalogExpiryAndRevision(t *testing.T) {
	transport := &expiringCatalogTransport{staticCatalogTransport: staticCatalogTransport{id: "optional"}}
	p, err := NewAuthenticated(transport, auth.NewService(auth.NewMemoryStore()))
	if err != nil {
		t.Fatal(err)
	}
	if p.ModelCatalogStale() || p.ModelCatalogRevision() != 0 {
		t.Fatal("fresh transport marked stale")
	}
	transport.stale, transport.revision = true, 2
	if !p.ModelCatalogStale() || p.ModelCatalogRevision() != 2 {
		t.Fatal("transport catalog change not forwarded")
	}
}
