package rpc

import (
	"bufio"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// CatalogMain is the opt-in, runtime-free stdio catalog entry point. sessionDir
// is the sessions root, not a project directory. It never constructs an app,
// reads config/auth, starts plugins/providers, or activates a session.
func CatalogMain(ctx context.Context, cwd, sessionDir, version string) error {
	return ServeCatalog(ctx, newProcessInputReader(os.Stdin), newProcessOutputWriter(os.Stdout), cwd, sessionDir, version)
}

// ServeCatalog serves only inactive project session inventory and plain display
// history. It uses the same bounded/interruptible transport contracts as Serve.
// The input is closed on return; the output remains owned by the caller.
func ServeCatalog(ctx context.Context, in io.Reader, out io.Writer, cwd, sessionDir, version string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	srv, err := catalogTransport(in, out)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, srv.interruptInput)
	defer stop()
	defer srv.interruptInput()
	if err := srv.write(protocol.NewRPCCatalogReady(version)); err != nil {
		return err
	}
	catalog := session.NewCatalog(sessionDir, cwd)
	scanner := bufio.NewScanner(srv.in)
	scanner.Buffer(make([]byte, 0, 64*1024), protocol.RPCMaxInputBytes+1)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > protocol.RPCMaxInputBytes {
			return bufio.ErrTooLong
		}
		var request protocol.RPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := srv.write(Response{Type: "response", Command: "invalid", Error: "invalid JSON", ErrorCode: "invalid"}); err != nil {
				return err
			}
			continue
		}
		// Large request IDs or command names cannot turn a small page into a
		// frame-sized reflected payload or an error-data exfiltration channel.
		if len(request.ID) > 1024 || len(request.Type) > 128 {
			if err := srv.write(Response{Type: "response", Command: "invalid", Error: "request identifier too long", ErrorCode: "invalid"}); err != nil {
				return err
			}
			continue
		}
		response := catalogResponse(ctx, catalog, request)
		response, err := boundCatalogResponse(response)
		if err != nil {
			return err
		}
		if err := srv.write(response); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return scanner.Err()
}

func catalogResponse(ctx context.Context, catalog *session.Catalog, request protocol.RPCRequest) Response {
	response := Response{ID: request.ID, Type: "response", Command: request.Type}
	var err error
	switch request.Type {
	case "catalog_sessions":
		var params protocol.RPCCatalogSessionsParams
		if len(request.Params) > 0 {
			err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		}
		if err == nil {
			response.Data, err = catalog.Sessions(ctx, params.Offset, params.Limit)
		}
	case "catalog_image":
		var params protocol.RPCCatalogImageParams
		err = validateImageParams(request.Params, false)
		if err == nil {
			err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		}
		if err == nil {
			response.Data, err = catalog.Image(ctx, params)
		}
	case "catalog_messages":
		var params protocol.RPCCatalogMessagesParams
		if len(request.Params) > 0 {
			err = json.Unmarshal(request.Params, &params, json.RejectUnknownMembers(true))
		}
		if err == nil {
			response.Data, err = catalog.Messages(ctx, params.SessionID, params.Offset, params.Limit, params.IncludeTools)
		}
	default:
		response.Error = "command is unsupported in runtime-free catalog mode"
		response.ErrorCode = "unsupported"
		return response
	}
	if err != nil {
		response.Data = nil
		response.Error = "catalog request failed"
		response.ErrorCode = "invalid"
		if errors.Is(err, session.ErrNotFound) {
			response.ErrorCode = "not_found"
			response.Error = "catalog session not found or unavailable"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			response.ErrorCode = "canceled"
			response.Error = "catalog request canceled"
		}
		return response
	}
	response.Success = true
	return response
}
