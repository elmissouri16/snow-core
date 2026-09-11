package rpc

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
)

// Reload readiness may await a UI broker. Keep the RPC reader available for
// broker replies and use the existing bounded outstanding-operation slots.
func (s *Server) handlePluginReload(ctx context.Context, req Request) error {
	var params struct {
		ID string `json:"id"`
	}
	if err := jsonv2.Unmarshal(req.Params, &params, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if params.ID == "" {
		return errors.New("plugin id is required")
	}
	select {
	case s.waitSlots <- struct{}{}:
	default:
		return errors.New("too many outstanding RPC operations")
	}
	s.promptWG.Go(func() {
		defer func() { <-s.waitSlots }()
		result, err := s.app.ReloadPlugin(ctx, params.ID)
		response := Response{ID: req.ID, Type: "response", Command: req.Type, Success: err == nil}
		if err != nil {
			response.Error = err.Error()
			response.ErrorCode = rpcErrorCode(err)
		} else {
			response.Data = result
		}
		s.write(response)
	})
	return nil
}
