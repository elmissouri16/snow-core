package rpc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PermissionReplyPayload is the additive wire params for permission_reply.
type PermissionReplyPayload struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
}

func validPermissionDecision(d string) bool {
	switch d {
	case "allow", "allow_session", "allow_always", "deny":
		return true
	default:
		return false
	}
}

func (s *Server) handlePermissionReply(req Request) error {
	var payload PermissionReplyPayload
	if len(req.Params) == 0 {
		return errors.New("permission_reply requires params")
	}
	if err := json.Unmarshal(req.Params, &payload); err != nil {
		return fmt.Errorf("permission_reply params: %w", err)
	}
	if payload.RequestID == "" {
		return errors.New("permission_reply requires request_id")
	}
	if !validPermissionDecision(payload.Decision) {
		return fmt.Errorf("permission_reply invalid decision %q", payload.Decision)
	}
	if err := s.app.ReplyPermission(protocol.PermissionResponse{
		RequestID: payload.RequestID,
		Decision:  protocol.PermissionDecision(payload.Decision),
	}); err != nil {
		return err
	}
	s.write(Response{ID: req.ID, Type: "response", Command: "permission_reply", Success: true})
	return nil
}

func (s *Server) handlePermissionReject(req Request) error {
	var payload struct {
		RequestID string `json:"request_id"`
	}
	if len(req.Params) == 0 {
		return errors.New("permission_reject requires params")
	}
	if err := json.Unmarshal(req.Params, &payload); err != nil {
		return fmt.Errorf("permission_reject params: %w", err)
	}
	if payload.RequestID == "" {
		return errors.New("permission_reject requires request_id")
	}
	if err := s.app.RejectPermission(payload.RequestID); err != nil {
		return err
	}
	s.write(Response{ID: req.ID, Type: "response", Command: "permission_reject", Success: true})
	return nil
}
