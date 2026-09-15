package app

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// MessageImage reads the worker-owned current branch without changing goals,
// running state or permissions. Admission prevents concurrent session switching.
func (a *App) MessageImage(ctx context.Context, params protocol.RPCMessageImageParams) (protocol.RPCCatalogImage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return protocol.RPCCatalogImage{}, err
	}
	defer unlock()
	id, _, _, err := a.Agent.SessionIdentityAdmitted()
	if err != nil {
		return protocol.RPCCatalogImage{}, err
	}
	if id != params.SessionID || a.Session == nil || a.Session.ID() != id {
		return protocol.RPCCatalogImage{}, errors.New("image requires the current session")
	}
	return session.ReadMessageImage(ctx, a.Session, params)
}
