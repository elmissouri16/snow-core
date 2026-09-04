package responsesapi

import (
	"context"
	"net/http"

	providerpkg "github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func newTestStream(ctx context.Context, response *http.Response, providerID string, secrets ...string) protocol.EventStream {
	return NewStreamWithIdleTimeout(ctx, response, providerID, providerpkg.DefaultStreamIdleTimeout, secrets...)
}
