package provider

import (
	"context"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// NoAuthTransport adapts a credential-free provider to the raw transport
// contract. It is primarily used by deterministic local/test providers.
type NoAuthTransport struct{ Provider Provider }

func (p NoAuthTransport) ID() string { return p.Provider.ID() }
func (p NoAuthTransport) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.Provider.ListModels(ctx)
}
func (p NoAuthTransport) Chat(ctx context.Context, _ auth.Credential, request protocol.ChatRequest) (protocol.EventStream, error) {
	return p.Provider.Chat(ctx, request)
}
