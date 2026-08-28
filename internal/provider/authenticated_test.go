package provider

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type refreshDriver struct{ count int }

func (*refreshDriver) Descriptor() auth.Descriptor {
	return auth.Descriptor{ProviderID: "oauth-runtime", Required: true, Kinds: []auth.CredentialType{auth.CredentialOAuth}}
}
func (*refreshDriver) Inspect(auth.Credential) (auth.Status, error) {
	return auth.Status{ProviderID: "oauth-runtime", State: auth.StateConfigured}, nil
}
func (*refreshDriver) Login(context.Context, auth.LoginRequest, auth.Interaction) (auth.Credential, error) {
	return auth.Credential{}, errors.New("unused")
}
func (*refreshDriver) Validate(c auth.Credential) error {
	if c.Access == "" {
		return errors.New("missing access")
	}
	return nil
}
func (*refreshDriver) NeedsRefresh(auth.Credential, time.Time) bool { return false }
func (d *refreshDriver) Refresh(_ context.Context, c auth.Credential, _ auth.RefreshReason) (auth.Credential, error) {
	d.count++
	c.Access = "fresh"
	return c, nil
}

type rejectingTransport struct {
	mu      sync.Mutex
	refresh func(context.Context, auth.Credential) (auth.Credential, error)
	seen    string
}

type staticCatalogTransport struct {
	id           string
	listCalls    int
	refreshCalls int
}

func (t *staticCatalogTransport) ID() string { return t.id }
func (t *staticCatalogTransport) ListModels(context.Context) ([]protocol.Model, error) {
	t.listCalls++
	return []protocol.Model{{Provider: t.id, ID: "offline"}}, nil
}
func (t *staticCatalogTransport) RefreshModels(context.Context) ([]protocol.Model, error) {
	t.refreshCalls++
	return []protocol.Model{{Provider: t.id, ID: "refreshed"}}, nil
}
func (*staticCatalogTransport) Chat(context.Context, auth.Credential, protocol.ChatRequest) (protocol.EventStream, error) {
	return registryStream{}, nil
}

func (*rejectingTransport) ID() string                                           { return "oauth-runtime" }
func (*rejectingTransport) ListModels(context.Context) ([]protocol.Model, error) { return nil, nil }
func (t *rejectingTransport) SetAuthRefresh(refresh func(context.Context, auth.Credential) (auth.Credential, error)) {
	t.refresh = refresh
}
func (t *rejectingTransport) Chat(ctx context.Context, credential auth.Credential, _ protocol.ChatRequest) (protocol.EventStream, error) {
	fresh, err := t.refresh(ctx, credential)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.seen = fresh.Access
	t.mu.Unlock()
	return registryStream{}, nil
}

func TestAuthenticatedHidesRequiredCatalogWithoutCredential(t *testing.T) {
	service := auth.NewService(auth.NewMemoryStore())
	driver := auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: "required", Required: true})
	if err := service.Register(driver); err != nil {
		t.Fatal(err)
	}
	transport := &staticCatalogTransport{id: "required"}
	runtime, err := NewAuthenticated(transport, service)
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 || transport.listCalls != 0 {
		t.Fatalf("models=%+v list calls=%d, want hidden catalog", models, transport.listCalls)
	}
	models, err = runtime.RefreshModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 || transport.refreshCalls != 0 {
		t.Fatalf("refreshed models=%+v refresh calls=%d, want hidden catalog", models, transport.refreshCalls)
	}
}

func TestAuthenticatedKeepsOptionalAnonymousCatalog(t *testing.T) {
	service := auth.NewService(auth.NewMemoryStore())
	driver := auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: "optional", Required: false})
	if err := service.Register(driver); err != nil {
		t.Fatal(err)
	}
	transport := &staticCatalogTransport{id: "optional"}
	runtime, err := NewAuthenticated(transport, service)
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "offline" || transport.listCalls != 1 {
		t.Fatalf("models=%+v list calls=%d, want anonymous catalog", models, transport.listCalls)
	}
}

func TestAuthenticatedBindsRejectedRefreshToService(t *testing.T) {
	store := auth.NewMemoryStore()
	if err := store.Put("oauth-runtime", auth.Credential{Type: auth.CredentialOAuth, Access: "old", Refresh: "refresh"}); err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(store)
	driver := &refreshDriver{}
	if err := service.Register(driver); err != nil {
		t.Fatal(err)
	}
	transport := &rejectingTransport{}
	runtime, err := NewAuthenticated(transport, service)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := runtime.Chat(context.Background(), protocol.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if driver.count != 1 || transport.seen != "fresh" {
		t.Fatalf("refreshes=%d seen=%q", driver.count, transport.seen)
	}
}
