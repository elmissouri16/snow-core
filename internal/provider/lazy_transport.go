package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// LazyTransport defers construction of an inactive provider adapter until its
// catalog, authentication flow, or chat transport is first used. Construction
// is generation-local and runs at most once; static configuration failures are
// therefore returned consistently to every caller.
// TransportInitializationError identifies a deferred adapter constructor
// failure so selection code can reject an unusable provider even when ordinary
// catalog discovery failures permit explicit custom model IDs.
type TransportInitializationError struct {
	Provider string
	Err      error
}

func (e *TransportInitializationError) Error() string {
	if e == nil {
		return "provider: initialize transport"
	}
	if e.Err == nil {
		return fmt.Sprintf("provider: initialize %s", e.Provider)
	}
	return fmt.Sprintf("provider: initialize %s: %v", e.Provider, e.Err)
}

func (e *TransportInitializationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsTransportInitializationError reports whether err contains a deferred
// adapter-construction failure.
func IsTransportInitializationError(err error) bool {
	_, ok := errors.AsType[*TransportInitializationError](err)
	return ok
}

type lazyTransportState struct{ transport Transport }

type LazyTransport struct {
	id    string
	build func() (Transport, error)
	once  sync.Once
	state atomic.Pointer[lazyTransportState]

	mu          sync.Mutex
	err         error
	authRefresh func(context.Context, auth.Credential) (auth.Credential, error)
}

// NewLazyTransport creates a transport placeholder without invoking build.
func NewLazyTransport(id string, build func() (Transport, error)) (*LazyTransport, error) {
	if id == "" {
		return nil, errors.New("provider: lazy transport id is required")
	}
	if build == nil {
		return nil, fmt.Errorf("provider: lazy transport %q has no constructor", id)
	}
	return &LazyTransport{id: id, build: build}, nil
}

func (p *LazyTransport) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

// Materialize returns the shared concrete adapter, constructing it once.
func (p *LazyTransport) Materialize() (Transport, error) {
	if p == nil {
		return nil, errors.New("provider: nil lazy transport")
	}
	if state := p.state.Load(); state != nil {
		return state.transport, nil
	}
	p.once.Do(func() {
		build := p.build
		p.build = nil
		transport, err := build()
		if err != nil {
			p.err = &TransportInitializationError{Provider: p.id, Err: err}
			return
		}
		if transport == nil {
			p.err = &TransportInitializationError{Provider: p.id, Err: errors.New("constructor returned nil")}
			return
		}
		if transport.ID() != p.id {
			p.err = &TransportInitializationError{Provider: p.id, Err: fmt.Errorf("constructor returned provider %q", transport.ID())}
			return
		}
		p.mu.Lock()
		if binder, ok := transport.(interface {
			SetAuthRefresh(func(context.Context, auth.Credential) (auth.Credential, error))
		}); ok && p.authRefresh != nil {
			binder.SetAuthRefresh(p.authRefresh)
		}
		p.state.Store(&lazyTransportState{transport: transport})
		p.mu.Unlock()
	})
	if p.err != nil {
		return nil, p.err
	}
	state := p.state.Load()
	if state == nil || state.transport == nil {
		return nil, fmt.Errorf("provider: initialize %s: transport unavailable", p.id)
	}
	return state.transport, nil
}

// Materialized reports whether construction completed successfully. It is
// intended for startup diagnostics and tests; callers should use Materialize
// when they require the concrete adapter.
func (p *LazyTransport) Materialized() bool {
	if p == nil {
		return false
	}
	return p.state.Load() != nil
}

// SetAuthRefresh records the authenticated runtime callback without forcing
// adapter construction, then forwards it if construction already occurred.
func (p *LazyTransport) SetAuthRefresh(refresh func(context.Context, auth.Credential) (auth.Credential, error)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.authRefresh = refresh
	if state := p.state.Load(); state != nil {
		if binder, ok := state.transport.(interface {
			SetAuthRefresh(func(context.Context, auth.Credential) (auth.Credential, error))
		}); ok {
			binder.SetAuthRefresh(refresh)
		}
	}
	p.mu.Unlock()
}

func (p *LazyTransport) ListModels(ctx context.Context) ([]protocol.Model, error) {
	transport, err := p.Materialize()
	if err != nil {
		return nil, err
	}
	return transport.ListModels(ctx)
}

func (p *LazyTransport) ListModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	transport, err := p.Materialize()
	if err != nil {
		return nil, err
	}
	if catalog, ok := transport.(CredentialCatalogTransport); ok {
		return catalog.ListModelsWithCredential(ctx, credential)
	}
	return transport.ListModels(ctx)
}

func (p *LazyTransport) RefreshModels(ctx context.Context) ([]protocol.Model, error) {
	transport, err := p.Materialize()
	if err != nil {
		return nil, err
	}
	if refresher, ok := transport.(interface {
		RefreshModels(context.Context) ([]protocol.Model, error)
	}); ok {
		return refresher.RefreshModels(ctx)
	}
	return transport.ListModels(ctx)
}

func (p *LazyTransport) RefreshModelsWithCredential(ctx context.Context, credential auth.Credential) ([]protocol.Model, error) {
	transport, err := p.Materialize()
	if err != nil {
		return nil, err
	}
	if refresher, ok := transport.(interface {
		RefreshModelsWithCredential(context.Context, auth.Credential) ([]protocol.Model, error)
	}); ok {
		return refresher.RefreshModelsWithCredential(ctx, credential)
	}
	if refresher, ok := transport.(interface {
		RefreshModels(context.Context) ([]protocol.Model, error)
	}); ok {
		return refresher.RefreshModels(ctx)
	}
	if catalog, ok := transport.(CredentialCatalogTransport); ok {
		return catalog.ListModelsWithCredential(ctx, credential)
	}
	return transport.ListModels(ctx)
}

func (p *LazyTransport) Chat(ctx context.Context, credential auth.Credential, request protocol.ChatRequest) (protocol.EventStream, error) {
	transport, err := p.Materialize()
	if err != nil {
		return nil, err
	}
	return transport.Chat(ctx, credential, request)
}

func (p *LazyTransport) DefaultModel() protocol.Model {
	transport, err := p.Materialize()
	if err != nil {
		return protocol.Model{}
	}
	if defaults, ok := transport.(interface{ DefaultModel() protocol.Model }); ok {
		return defaults.DefaultModel()
	}
	return protocol.Model{}
}

func (p *LazyTransport) ModelCatalogAuthoritative() bool {
	transport, err := p.Materialize()
	if err != nil {
		return false
	}
	value, ok := transport.(interface{ ModelCatalogAuthoritative() bool })
	return ok && value.ModelCatalogAuthoritative()
}

func (p *LazyTransport) RejectUnknownModels() bool {
	transport, err := p.Materialize()
	if err != nil {
		return false
	}
	value, ok := transport.(interface{ RejectUnknownModels() bool })
	return ok && value.RejectUnknownModels()
}
