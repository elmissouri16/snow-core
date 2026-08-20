package provider

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Authenticated is the sole credential boundary presented to an Agent. It
// resolves provider-scoped material for every request so login/logout and
// environment changes take effect without rebuilding an agent.
type Authenticated struct {
	transport Transport
	auth      *auth.Service
}

func NewAuthenticated(transport Transport, service *auth.Service) (*Authenticated, error) {
	if transport == nil {
		return nil, errors.New("provider: nil transport")
	}
	if service == nil {
		return nil, errors.New("provider: nil auth service")
	}
	if binder, ok := transport.(interface {
		SetAuthRefresh(func(context.Context, auth.Credential) (auth.Credential, error))
	}); ok {
		providerID := transport.ID()
		binder.SetAuthRefresh(func(ctx context.Context, rejected auth.Credential) (auth.Credential, error) {
			return service.RefreshRejected(ctx, providerID, rejected)
		})
	}
	return &Authenticated{transport: transport, auth: service}, nil
}

func (p *Authenticated) ID() string { return p.transport.ID() }

// Transport exposes the raw adapter for provider-manager configuration hooks.
// Agents and user surfaces must not use it for inference.
func (p *Authenticated) Transport() Transport { return p.transport }

// CredentialCatalogTransport receives the same resolved credential as Chat.
// It removes the need for adapter-level discovery-key fallbacks.
type CredentialCatalogTransport interface {
	ListModelsWithCredential(context.Context, auth.Credential) ([]protocol.Model, error)
}

func (p *Authenticated) ListModels(ctx context.Context) ([]protocol.Model, error) {
	catalog, ok := p.transport.(CredentialCatalogTransport)
	if !ok {
		return p.transport.ListModels(ctx)
	}
	credential, err := p.auth.Resolve(ctx, p.ID())
	if err != nil {
		// Required-auth providers may still expose a safe static/offline catalog
		// before login.
		return p.transport.ListModels(ctx)
	}
	return catalog.ListModelsWithCredential(ctx, credential)
}

func (p *Authenticated) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	credential, err := p.auth.Resolve(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	return p.transport.Chat(ctx, credential, request)
}

// Optional provider metadata is forwarded across the wrapper.
func (p *Authenticated) DefaultModel() protocol.Model {
	if value, ok := p.transport.(interface{ DefaultModel() protocol.Model }); ok {
		return value.DefaultModel()
	}
	return protocol.Model{}
}

func (p *Authenticated) RefreshModels(ctx context.Context) ([]protocol.Model, error) {
	if value, ok := p.transport.(interface {
		RefreshModelsWithCredential(context.Context, auth.Credential) ([]protocol.Model, error)
	}); ok {
		credential, err := p.auth.Resolve(ctx, p.ID())
		if err == nil {
			return value.RefreshModelsWithCredential(ctx, credential)
		}
	}
	if value, ok := p.transport.(interface {
		RefreshModels(context.Context) ([]protocol.Model, error)
	}); ok {
		return value.RefreshModels(ctx)
	}
	return p.ListModels(ctx)
}

func (p *Authenticated) ModelCatalogAuthoritative() bool {
	if value, ok := p.transport.(interface{ ModelCatalogAuthoritative() bool }); ok {
		return value.ModelCatalogAuthoritative()
	}
	return false
}
