package rpc

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"strings"

	"github.com/elmissouri16/snow-core/internal/hostcontrol"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const controlAPIKeyCapability = protocol.RPCAPIKeyControlCapability

// APIKeyControlService is optional and write-only for credentials. These calls
// are trusted same-user local stdio/control-RPC operations and must never be
// exposed through the Web Manager HTTP surface.
type APIKeyControlService interface {
	InspectAPIKey(context.Context, protocol.HostAPIKeyInspectRequest) (protocol.HostAPIKeyStatusResponse, error)
	SetAPIKey(context.Context, protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error)
}

func isControlAPIKeyCommand(command string) bool {
	return command == "api_key_inspect" || command == "api_key_set"
}

func controlAPIKeyResponse(ctx context.Context, service APIKeyControlService, request controlRequest) Response {
	response := Response{ID: request.ID, Type: "response", Command: request.Type}
	var err error
	switch request.Type {
	case "api_key_inspect":
		var params protocol.HostAPIKeyInspectRequest
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && !validControlProviderID(params.ProviderID) {
			err = hostcontrol.ErrInvalidRequest
		}
		if err == nil {
			response.Data, err = service.InspectAPIKey(ctx, params)
		}
	case "api_key_set":
		var params protocol.HostAPIKeySetRequest
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && (!validControlProviderID(params.ProviderID) || !validControlAPIKeyRevision(params.ExpectedRevision) ||
			len(params.Secret) == 0 || len(params.Secret) > 4096 || strings.TrimSpace(params.Secret) != params.Secret ||
			!controlMemberPresent(request.Params, "confirm_replace")) {
			err = hostcontrol.ErrInvalidRequest
		}
		if err == nil {
			response.Data, err = service.SetAPIKey(ctx, params)
		}
		// Do not retain the submitted secret in a response, event or diagnostic.
		params.Secret = ""
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		code := "invalid"
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			code = "canceled"
		case errors.Is(err, hostcontrol.ErrRevisionConflict):
			code = "revision_conflict"
		case errors.Is(err, hostcontrol.ErrUnavailable):
			code = "unavailable"
		case errors.Is(err, hostcontrol.ErrReplaceConfirmationRequired):
			code = "replace_confirmation_required"
		}
		failure := controlFailure(request.ID, request.Type, code)
		if code == "replace_confirmation_required" {
			failure.Error = "explicit credential replacement confirmation required"
		}
		return failure
	}
	response.Success = true
	return response
}

func controlMemberPresent(params jsontext.Value, member string) bool {
	var fields map[string]jsontext.Value
	if json.Unmarshal(params, &fields) != nil {
		return false
	}
	_, ok := fields[member]
	return ok
}

func validControlProviderID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && (r == '-' || r == '_' || r == '.')) {
			return false
		}
	}
	return true
}

func validControlAPIKeyRevision(revision string) bool {
	if revision == "missing" {
		return true
	}
	if len(revision) != 64 {
		return false
	}
	for _, r := range revision {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
