package rpc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io"

	"github.com/elmissouri16/snow-core/internal/hostcontrol"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// This envelope intentionally does not use RPCRequest: that wider runtime DTO
// silently discards some members before command validation could inspect them.
type controlRequest struct {
	ID     string         `json:"id,omitempty"`
	Type   string         `json:"type"`
	Params jsontext.Value `json:"params,omitempty"`
}

func decodeControlRequest(frame []byte) (controlRequest, error) {
	var request controlRequest
	if len(frame) > protocol.RPCControlMaxInputBytes || !controlObject(frame) {
		return request, hostcontrol.ErrInvalidRequest
	}
	// Validate the original frame, including raw params. jsontext rejects
	// duplicate object names; reject nulls rather than treating them as omitted
	// operations, and bound every string before any host operation runs.
	decoder := jsontext.NewDecoder(bytes.NewReader(frame))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || token.Kind() == 'n' || (token.Kind() == '"' && len(token.String()) > 4096) {
			return request, hostcontrol.ErrInvalidRequest
		}
	}
	if err := json.Unmarshal(frame, &request, json.RejectUnknownMembers(true)); err != nil {
		return controlRequest{}, hostcontrol.ErrInvalidRequest
	}
	if len(request.ID) > 128 || len(request.Type) == 0 || len(request.Type) > 64 || len(request.Params) > 16*1024 {
		return controlRequest{}, hostcontrol.ErrInvalidRequest
	}
	if len(request.Params) != 0 && !controlObject(request.Params) {
		return controlRequest{}, hostcontrol.ErrInvalidRequest
	}
	return request, nil
}

func controlObject(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) >= 2 && data[0] == '{' && data[len(data)-1] == '}'
}

func controlResponse(ctx context.Context, pin controlProjectPin, service ControlService, request controlRequest) Response {
	response := Response{ID: request.ID, Type: "response", Command: request.Type}
	var err error
	switch request.Type {
	case "defaults_get":
		var params protocol.HostDefaultsRequest
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && !pin.matches(params.Scope, params.CWD) {
			err = hostcontrol.ErrInvalidRequest
		}
		if err == nil {
			response.Data, err = service.GetDefaults(ctx, params)
		}
	case "defaults_update":
		var params protocol.HostDefaultsUpdateRequest
		err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		if err == nil && (!pin.matches(params.Scope, params.CWD) || !boundedControlUpdate(params)) {
			err = hostcontrol.ErrInvalidRequest
		}
		if err == nil {
			response.Data, err = service.UpdateDefaults(ctx, params)
		}
	case "provider_status_list":
		var params struct{}
		if len(request.Params) != 0 {
			err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		}
		if err == nil {
			response.Data, err = service.ProviderStatus(ctx)
		}
	default:
		// Canonical unsupported command names retain client correlation. Other
		// arbitrary strings cannot become an error-data reflection channel.
		return controlFailure(request.ID, controlCommandName(request.Type), "unsupported")
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
		}
		return controlFailure(request.ID, request.Type, code)
	}
	response.Success = true
	return response
}

func controlFailure(id, command, code string) Response {
	message := "control request invalid"
	switch code {
	case "unsupported":
		message = "command unsupported in runtime-free control mode"
	case "unavailable":
		message = "control unavailable"
	case "revision_conflict":
		message = "control revision conflict; read current defaults before retrying"
	case "canceled":
		message = "control request canceled"
	}
	return Response{ID: id, Type: "response", Command: command, ErrorCode: code, Error: message}
}

func boundedControlUpdate(request protocol.HostDefaultsUpdateRequest) bool {
	if request.Revision == "" || len(request.Revision) > 256 {
		return false
	}
	if request.Scope == "global" {
		p := request.Global
		return p != nil && request.Project == nil && boundedControlPair(p.ProviderModel) &&
			boundedControlString(p.Thinking) && boundedControlString(p.ReasoningSummary) && boundedControlString(p.TextVerbosity)
	}
	p := request.Project
	return p != nil && request.Global == nil && boundedControlPair(p.ProviderModel) && boundedControlString(p.Thinking)
}

func boundedControlString(operation *protocol.HostStringOperation) bool {
	if operation == nil {
		return true
	}
	return (operation.Op == "reset" && operation.Value == nil) ||
		(operation.Op == "set" && operation.Value != nil && len(*operation.Value) <= 32)
}

func boundedControlPair(operation *protocol.HostProviderModelOperation) bool {
	if operation == nil {
		return true
	}
	return (operation.Op == "reset" && operation.Value == nil) ||
		(operation.Op == "set" && operation.Value != nil && len(operation.Value.Provider) > 0 &&
			len(operation.Value.Provider) <= 128 && len(operation.Value.Model) > 0 && len(operation.Value.Model) <= 256)
}

// Preserve correlation for canonical command names (the RPC client checks it),
// while refusing to reflect arbitrary path-like or control-character payloads.
func controlCommandName(command string) string {
	for _, r := range command {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "invalid"
		}
	}
	return command
}
