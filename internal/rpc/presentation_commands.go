package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isPresentationCommand(command string) bool {
	switch command {
	case "themes_list", "keybindings_get", "keybindings_update":
		return true
	default:
		return false
	}
}

func (s *Server) handlePresentationCommand(_ context.Context, req Request) error {
	if settingsRequestHasUnsupportedFields(req) {
		return fmt.Errorf("%s accepts no top-level fields other than id, type, and params", req.Type)
	}
	switch req.Type {
	case "themes_list":
		if len(req.Params) != 0 {
			return errors.New("themes_list does not accept params")
		}
		catalog, err := s.app.RPCThemes()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: catalog})
	case "keybindings_get":
		if len(req.Params) != 0 {
			return errors.New("keybindings_get does not accept params")
		}
		bindings, err := s.app.RPCKeybindings()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: bindings})
	case "keybindings_update":
		if len(req.Params) == 0 {
			return errors.New("keybindings_update requires params")
		}
		var params protocol.RPCKeybindingsUpdateParams
		if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("keybindings_update params: %w", err)
		}
		bindings, err := s.app.UpdateRPCKeybindings(params)
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: bindings})
	default:
		return fmt.Errorf("unknown presentation command %q", req.Type)
	}
}
