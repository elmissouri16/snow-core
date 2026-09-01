package rpc

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isSessionManagementCommand(command string) bool {
	switch command {
	case "sessions_list", "session_create", "session_open", "session_delete", "session_rename":
		return true
	default:
		return false
	}
}

func (s *Server) handleSessionManagementCommand(req Request) error {
	switch req.Type {
	case "sessions_list":
		infos, err := s.app.ListSessions()
		if err != nil {
			return err
		}
		activeID, _, err := s.app.Agent.SessionIdentity()
		if err != nil {
			return err
		}
		sessions := make([]protocol.RPCSessionSummary, 0, len(infos))
		for _, info := range infos {
			sessions = append(sessions, rpcSessionSummary(info, info.ID == activeID))
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCSessionList{Sessions: sessions}})
	case "session_create":
		info, err := s.app.CreateSession()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: rpcSessionSummary(info, true)})
	case "session_open":
		sessionID, err := sessionIDParam(req)
		if err != nil {
			return err
		}
		info, err := s.app.OpenSession(sessionID)
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: rpcSessionSummary(info, true)})
	case "session_delete":
		sessionID, err := sessionIDParam(req)
		if err != nil {
			return err
		}
		if err := s.app.DeleteSessionByID(sessionID); err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCSessionDeleteResult{SessionID: sessionID, Deleted: true}})
	case "session_rename":
		var params struct {
			SessionID string `json:"session_id"`
			Name      string `json:"name"`
		}
		if len(req.Params) == 0 {
			return errors.New("session_rename requires params")
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("session_rename params: %w", err)
		}
		if params.Name == "" {
			return errors.New("session_rename requires name")
		}
		if params.SessionID == "" {
			if err := s.app.RenameSession(params.Name); err != nil {
				return err
			}
			var err error
			params.SessionID, _, err = s.app.Agent.SessionIdentity()
			if err != nil {
				return err
			}
		} else if err := s.app.RenameSessionByID(params.SessionID, params.Name); err != nil {
			return err
		}
		name, err := renamedSessionTitle(s, params.SessionID)
		if err != nil {
			return err
		}
		result := protocol.RPCSessionRenameResult{SessionID: params.SessionID, Name: name}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
	default:
		return fmt.Errorf("unknown session management command %q", req.Type)
	}
}

func sessionIDParam(req Request) (string, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if len(req.Params) == 0 {
		return "", fmt.Errorf("%s requires params", req.Type)
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return "", fmt.Errorf("%s params: %w", req.Type, err)
	}
	if params.SessionID == "" {
		return "", fmt.Errorf("%s requires session_id", req.Type)
	}
	return params.SessionID, nil
}

func renamedSessionTitle(s *Server, sessionID string) (string, error) {
	activeID, _, err := s.app.Agent.SessionIdentity()
	if err != nil {
		return "", err
	}
	if activeID == sessionID {
		return s.app.Agent.SessionTitle()
	}
	infos, err := s.app.ListSessions()
	if err != nil {
		return "", err
	}
	for _, info := range infos {
		if info.ID == sessionID {
			return info.Name, nil
		}
	}
	return "", session.ErrNotFound
}

func rpcSessionSummary(info session.SessionInfo, active bool) protocol.RPCSessionSummary {
	return protocol.RPCSessionSummary{
		SessionID: info.ID, Name: info.Name, CreatedAt: info.CreatedAt,
		UpdatedAt: info.UpdatedAt, Messages: info.Messages,
		MessagesCapped: info.MessagesCapped, Active: active,
	}
}
