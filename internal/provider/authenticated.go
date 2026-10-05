package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Authenticated is the sole credential boundary presented to an Agent. It
// resolves provider-scoped material for every request so login/logout and
// environment changes take effect without rebuilding an agent.
type Authenticated struct {
	transport Transport
	auth      *auth.Service

	accountMu sync.RWMutex
	account   string
	guard     RequestGuard
	// Named or switched accounts must prove entitlement to the requested model.
	checkModels bool
	catalog     []protocol.Model
	catalogFor  string
	limits      map[string]accountLimit
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
	return p.modelsWithCredential(ctx, credential, false)
}

// catalogCredential keeps required-auth provider inventories hidden until a
// usable credential resolves. Optional-auth providers continue with an empty
// credential so anonymous catalogs remain available.
func (p *Authenticated) catalogCredential(ctx context.Context) (auth.Credential, bool, error) {
	credential, err := p.auth.ResolveAccount(ctx, p.ID(), p.Account())
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
	credential, err := p.auth.ResolveAccount(ctx, p.ID(), p.Account())
	if err != nil {
		return nil, err
	}
	p.accountMu.RLock()
	guard := p.guard
	checkModels := p.checkModels
	catalog := slices.Clone(p.catalog)
	catalogFor := p.catalogFor
	p.accountMu.RUnlock()
	if guard != nil {
		if err := guard(ctx, credential, request); err != nil {
			return nil, err
		}
	}
	if checkModels {
		identity := auth.AccountIdentity(credential)
		if catalogFor == "" || catalogFor != identity {
			var err error
			catalog, err = p.modelsWithCredential(ctx, credential, false)
			if err != nil {
				return nil, fmt.Errorf("provider: inspect selected account's model catalog: %w", err)
			}
		}
		index := slices.IndexFunc(catalog, func(model protocol.Model) bool { return model.ID == request.Model.ID })
		if index < 0 {
			return nil, fmt.Errorf("provider: model %q is unavailable to ChatGPT account %q; choose an available model with /model", request.Model.ID, credential.Account)
		}
		selected := catalog[index]
		if request.Model.ContextWindow != selected.ContextWindow {
			return nil, fmt.Errorf("provider: context window for model %q changed on ChatGPT account %q; reselect the model with /model before continuing so context and compaction budgets are revalidated", request.Model.ID, credential.Account)
		}
		if !selected.SupportsThinkingLevel(request.Thinking) {
			return nil, fmt.Errorf("provider: reasoning effort %q is unavailable for model %q on ChatGPT account %q; choose an available effort explicitly", request.Thinking, request.Model.ID, credential.Account)
		}
		summary := protocol.NormalizeReasoningSummary(request.ReasoningSummary)
		if selected.SupportsReasoningSummary != nil && !*selected.SupportsReasoningSummary && summary != protocol.ReasoningSummaryOff && summary != protocol.ReasoningSummaryAuto {
			return nil, fmt.Errorf("provider: reasoning summary %q is unavailable for model %q on ChatGPT account %q", summary, request.Model.ID, credential.Account)
		}
		verbosity := protocol.NormalizeTextVerbosity(request.TextVerbosity)
		if !selected.SupportsVerbosity && verbosity != protocol.TextVerbosityLow {
			return nil, fmt.Errorf("provider: text verbosity %q is unavailable for model %q on ChatGPT account %q", verbosity, request.Model.ID, credential.Account)
		}
		// Refresh capabilities for the same requested model, never substitute
		// another model or silently repair an explicit reasoning choice.
		request.Model = selected
	}
	stream, err := p.transport.Chat(ctx, credential, request)
	if p.ID() != "chatgpt" {
		return stream, err
	}
	p.observeAccountLimit(credential, err)
	if err != nil || stream == nil {
		return stream, err
	}
	return &accountObservingStream{EventStream: stream, provider: p, credential: credential}, nil
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
	return p.modelsWithCredential(ctx, credential, true)
}

// ModelCatalogStale forwards adapter-owned cache expiry to app snapshots.
func (p *Authenticated) ModelCatalogStale() bool {
	if value, ok := p.transport.(interface{ ModelCatalogStale() bool }); ok {
		return value.ModelCatalogStale()
	}
	return false
}

func (p *Authenticated) ModelCatalogRevision() uint64 {
	if value, ok := p.transport.(interface{ ModelCatalogRevision() uint64 }); ok {
		return value.ModelCatalogRevision()
	}
	return 0
}

func (p *Authenticated) ModelCatalogAuthoritative() bool {
	if p.ID() == "chatgpt" && p.Account() != "default" {
		status, err := p.auth.StatusAccount(context.Background(), p.ID(), p.Account())
		return err == nil && status.Configured()
	}
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
