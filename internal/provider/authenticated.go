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

// CredentialCatalogTransport receives the same resolved credential as Chat.
// It removes the need for adapter-level discovery-key fallbacks.
type CredentialCatalogTransport interface {
	ListModelsWithCredential(context.Context, auth.Credential) ([]protocol.Model, error)
}

func (p *Authenticated) ListModels(ctx context.Context) ([]protocol.Model, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	credential, available, err := p.catalogCredential(ctx)
	if err != nil || !available {
		return nil, err
	}
	if catalog, ok := p.transport.(CredentialCatalogTransport); ok {
		return catalog.ListModelsWithCredential(ctx, credential)
	}
	return p.transport.ListModels(ctx)
}

// catalogCredential keeps required-auth provider inventories hidden until a
// usable credential resolves. Optional-auth providers continue with an empty
// credential so anonymous catalogs remain available.
func (p *Authenticated) catalogCredential(ctx context.Context) (auth.Credential, bool, error) {
	credential, err := p.auth.Resolve(ctx, p.ID())
	if err == nil {
		return credential, true, nil
	}
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return auth.Credential{}, false, ctxErr
		}
	}
	descriptor, descriptorErr := p.auth.Descriptor(p.ID())
	if descriptorErr != nil {
		return auth.Credential{}, false, descriptorErr
	}
	if descriptor.Required {
		return auth.Credential{}, false, nil
	}
	return auth.Credential{Provider: p.ID()}, true, nil
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
	if ctx == nil {
		ctx = context.Background()
	}
	credential, available, err := p.catalogCredential(ctx)
	if err != nil || !available {
		return nil, err
	}
	if value, ok := p.transport.(interface {
		RefreshModelsWithCredential(context.Context, auth.Credential) ([]protocol.Model, error)
	}); ok {
		return value.RefreshModelsWithCredential(ctx, credential)
	}
	if value, ok := p.transport.(interface {
		RefreshModels(context.Context) ([]protocol.Model, error)
	}); ok {
		return value.RefreshModels(ctx)
	}
	if catalog, ok := p.transport.(CredentialCatalogTransport); ok {
		return catalog.ListModelsWithCredential(ctx, credential)
	}
	return p.transport.ListModels(ctx)
}

func (p *Authenticated) ModelCatalogAuthoritative() bool {
	if value, ok := p.transport.(interface{ ModelCatalogAuthoritative() bool }); ok {
		return value.ModelCatalogAuthoritative()
	}
	return false
}

func (p *Authenticated) RejectUnknownModels() bool {
	if value, ok := p.transport.(interface{ RejectUnknownModels() bool }); ok {
		return value.RejectUnknownModels()
	}
	return false
}
