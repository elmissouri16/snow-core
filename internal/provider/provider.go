// Package provider defines the provider abstraction and the credential
// resolution contract. Adapters live in subpackages (opencodego, chatgpt, fake).
package provider

import (
	"context"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

// Provider is an LLM backend adapter.
type Provider interface {
	// ID returns the stable provider identifier, e.g. "opencode-go".
	ID() string

	// ListModels returns the model catalog for this provider.
	ListModels(ctx context.Context) ([]protocol.Model, error)

	// Resolve ensures credentials are valid (e.g. refresh OAuth tokens).
	Resolve(ctx context.Context, creds auth.Credential) error

	// Chat starts a streaming chat. Callers must Close the returned stream.
	Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error)
}
