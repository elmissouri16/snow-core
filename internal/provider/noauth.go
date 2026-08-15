package provider

import (
	"context"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

// NoAuthTransport adapts a credential-free provider to the raw transport
// contract. It is primarily used by deterministic local/test providers.
type NoAuthTransport struct{ Provider Provider }

func (p NoAuthTransport) ID() string { return p.Provider.ID() }
func (p NoAuthTransport) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.Provider.ListModels(ctx)
}
func (p NoAuthTransport) Resolve(_ context.Context, credential auth.Credential) (auth.Credential, error) {
	return credential, nil
}
func (p NoAuthTransport) Chat(ctx context.Context, _ auth.Credential, request protocol.ChatRequest) (protocol.EventStream, error) {
	return p.Provider.Chat(ctx, request)
}
