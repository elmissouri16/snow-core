package rpc

import (
	json "encoding/json/v2"
	"errors"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *Server) handleManagedSteer(req Request) (retErr error) {
	defer func() {
		if retErr != nil {
			if errors.Is(retErr, agent.ErrManagedSteerStale) {
				retErr = agent.ErrManagedSteerStale
			} else {
				retErr = agent.ErrManagedSteerRejected
			}
		}
	}()
	if err := validateManagedRuntimeParams(req, []string{"session_id", "turn_id", "root_epoch", "request_id", "text"}, protocol.RPCManagedSteerMaxTextBytes+8192); err != nil {
		return err
	}
	var params protocol.RPCManagedSteerParams
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	result, err := s.app.ManagedSteer(params)
	if err != nil {
		return err
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
}
