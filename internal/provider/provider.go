// Package provider defines the provider abstraction and the credential
// resolution contract. Adapters live in subpackages (opencodego, chatgpt, fake).
package provider

import (
	"context"
	"fmt"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

// Provider is an LLM backend adapter.
// UsageLimitedError marks quota/rate-limit failures that should pause goal
// continuation without treating the objective as intrinsically blocked.
type UsageLimitedError interface {
	error
	UsageLimited() bool
}
type LimitError struct {
	Provider string
	Status   int
	Message  string
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: usage limited (HTTP %d): %s", e.Provider, e.Status, e.Message)
}
func (*LimitError) UsageLimited() bool { return true }

// Provider is an LLM backend adapter.
type Provider interface {
	// ID returns the stable provider identifier, e.g. "opencode-go".
	ID() string

	// ListModels returns the model catalog for this provider.
	ListModels(ctx context.Context) ([]protocol.Model, error)

	// Resolve ensures credentials are valid (including OAuth refresh) and
	// returns the exact credential Chat must use for this request.
	Resolve(ctx context.Context, creds auth.Credential) (auth.Credential, error)

	// Chat starts a streaming chat. Callers must Close the returned stream.
	Chat(ctx context.Context, creds auth.Credential, req protocol.ChatRequest) (protocol.EventStream, error)
}
