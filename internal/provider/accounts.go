package provider

import (
	"context"
	"slices"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RequestGuard validates account-bound context immediately before transport use.
// It runs for every request, including compaction, regardless of execution surface.
type RequestGuard func(context.Context, auth.Credential, protocol.ChatRequest) error

// Account returns the runtime's selected local credential alias.
func (p *Authenticated) Account() string {
	p.accountMu.RLock()
	defer p.accountMu.RUnlock()
	if p.account == "" {
		return "default"
	}
	return p.account
}

// SetAccount changes future resolutions only. Hosts must serialize selection
// with turn admission; an in-flight credential remains bound to its own alias.
func (p *Authenticated) SetAccount(account string) error {
	if _, err := auth.AccountKey(p.ID(), account); err != nil {
		return err
	}
	if account == "" {
		account = "default"
	}
	p.accountMu.Lock()
	defer p.accountMu.Unlock()
	current := p.account
	if current == "" {
		current = "default"
	}
	if current != account {
		p.account = account
		p.checkModels = true
		p.catalog = nil
		p.catalogFor = ""
	}
	return nil
}

// RequireAccountModelValidation resets model proof after an explicit context
// handoff, including a replaced identity under the same default alias. Normal
// implicit-default startup keeps its legacy behavior; a handoff must validate
// the chosen model/settings for its new identity without substituting them.
func (p *Authenticated) RequireAccountModelValidation() {
	p.accountMu.Lock()
	defer p.accountMu.Unlock()
	p.checkModels = true
	p.catalog = nil
	p.catalogFor = ""
}

func (p *Authenticated) SetRequestGuard(guard RequestGuard) {
	p.accountMu.Lock()
	p.guard = guard
	p.accountMu.Unlock()
}

// ForSession gives a child its own selection snapshot and continuity guard.
// Transport and credential persistence stay shared; no agent loop is duplicated.
func (p *Authenticated) ForSession(guard RequestGuard) *Authenticated {
	p.accountMu.RLock()
	defer p.accountMu.RUnlock()
	return &Authenticated{transport: p.transport, auth: p.auth, account: p.account,
		guard: guard, checkModels: p.checkModels, catalog: slices.Clone(p.catalog), catalogFor: p.catalogFor}
}

func (p *Authenticated) modelsWithCredential(ctx context.Context, credential auth.Credential, force bool) ([]protocol.Model, error) {
	var models []protocol.Model
	var err error
	if force {
		if value, ok := p.transport.(interface {
			RefreshModelsWithCredential(context.Context, auth.Credential) ([]protocol.Model, error)
		}); ok {
			models, err = value.RefreshModelsWithCredential(ctx, credential)
		} else if value, ok := p.transport.(interface {
			RefreshModels(context.Context) ([]protocol.Model, error)
		}); ok {
			models, err = value.RefreshModels(ctx)
		} else {
			force = false
		}
	}
	if !force {
		if catalog, ok := p.transport.(CredentialCatalogTransport); ok {
			models, err = catalog.ListModelsWithCredential(ctx, credential)
		} else {
			models, err = p.transport.ListModels(ctx)
		}
	}
	if err == nil {
		p.accountMu.Lock()
		p.catalog = slices.Clone(models)
		p.catalogFor = auth.AccountIdentity(credential)
		p.accountMu.Unlock()
	}
	return models, err
}
