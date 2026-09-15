package rpc

import (
	"context"
	"encoding/json/v2"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/sessiondelete"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// SessionDeleteControlService is composed explicitly; absent it, control workers
// cannot mutate session storage. No runtime/configuration loading is needed.
type SessionDeleteControlService interface {
	DeleteSession(context.Context, string, string) error
}

type localSessionDeleteControl struct{ root, home string }

func (d localSessionDeleteControl) DeleteSession(ctx context.Context, cwd, id string) error {
	return sessiondelete.ByID(ctx, d.root, cwd, id, d.home, artifact.ExistingSessionDeleter{Path: filepath.Join(d.home, "artifacts")})
}
func defaultSessionDeleteControl() SessionDeleteControlService {
	return localSessionDeleteControl{root: session.DefaultSessionsRoot(), home: config.GlobalDir()}
}
func controlSessionDeleteResponse(ctx context.Context, pin controlProjectPin, service SessionDeleteControlService, request controlRequest) Response {
	var p protocol.RPCSessionDeleteParams
	if json.Unmarshal(request.Params, &p, json.RejectUnknownMembers(true)) != nil || !sessiondelete.ValidID(p.SessionID) || !pin.matches("project", pin.cwd) || ctx.Err() != nil {
		return controlFailure(request.ID, request.Type, "invalid")
	}
	if err := service.DeleteSession(ctx, pin.cwd, p.SessionID); err != nil {
		// Even rollback errors can leave staged files. Never claim unchanged or retry.
		return controlFailure(request.ID, request.Type, "unavailable")
	}
	return Response{ID: request.ID, Type: "response", Command: request.Type, Success: true, Data: protocol.RPCSessionDeleteResult{SessionID: p.SessionID, Deleted: true}}
}
