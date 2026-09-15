package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const maxConcurrentImageReads = 4

// Image reads may wait for admission or bounded history. Never block the RPC
// reader: abort and interaction replies must remain dispatchable. Serve cancels
// the inherited context and joins this bounded worker via promptWG on shutdown.
func (s *Server) handleMessageImage(ctx context.Context, req Request) error {
	if len(req.ID) > 1024 {
		return errors.New("message_image request identifier too long")
	}
	if err := validateImageParams(req.Params, true); err != nil {
		return err
	}
	var params protocol.RPCMessageImageParams
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("message_image requires valid params")
	}
	select {
	case s.imageReadSlots <- struct{}{}:
	default:
		return errors.New("too many outstanding message_image reads")
	}
	s.promptWG.Go(func() {
		defer func() { <-s.imageReadSlots }()
		if err := s.readMessageImage(ctx, req, params); err != nil {
			_ = s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: err.Error(), ErrorCode: rpcErrorCode(err)})
		}
	})
	return nil
}

func (s *Server) readMessageImage(ctx context.Context, req Request, params protocol.RPCMessageImageParams) error {
	image, err := s.app.MessageImage(ctx, params)
	if err != nil {
		return err
	}
	response := Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: image}
	// The exception is limited to this one explicit byte-bearing command.
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded)+1 > protocol.RPCMessageImageMaxOutputBytes {
		return errors.New("message_image exceeds output bound")
	}
	s.write(response)
	return nil
}
