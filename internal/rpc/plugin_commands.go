package rpc

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *Server) handlePluginCommand(ctx context.Context, req Request) error {
	if req.Type == "plugin_statuses" || req.Type == "plugin_enable" || req.Type == "plugin_disable" {
		return s.handlePluginManagement(ctx, req)
	}
	var params struct {
		Command string `json:"command"`
		Input   string `json:"input"`
	}
	if len(req.Params) > 0 {
		if err := jsonv2.Unmarshal(req.Params, &params, jsonv2.RejectUnknownMembers(true)); err != nil {
			return err
		}
	}
	respond := func(data any) {
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: data})
	}
	switch req.Type {
	case "plugins_list":
		respond(s.app.PluginInfos())
	case "plugin_commands":
		respond(s.app.PluginCommands())
	case "plugin_views":
		respond(s.app.PluginViews())
	case "plugin_command_cancel":
		respond(map[string]bool{"canceled": s.app.CancelPluginCommand(params.Command)})
	case "plugin_command_run":
		if params.Command == "" {
			return errors.New("command is required")
		}
		select {
		case s.waitSlots <- struct{}{}:
		default:
			return errors.New("too many outstanding RPC operations")
		}
		s.promptWG.Go(func() {
			defer func() { <-s.waitSlots }()
			result, err := s.app.RunPluginCommand(ctx, params.Command, params.Input)
			response := Response{ID: req.ID, Type: "response", Command: req.Type, Success: err == nil}
			if err != nil {
				response.Error = err.Error()
				response.ErrorCode = rpcErrorCode(err)
			} else {
				response.Data = struct {
					Content []protocol.ContentBlock `json:"content"`
					IsError bool                    `json:"is_error"`
				}{result.Content, result.IsError}
			}
			s.write(response)
		})
	}
	return nil
}

func (s *Server) handlePluginManagement(ctx context.Context, req Request) error {
	var data any
	if req.Type == "plugin_statuses" {
		var params struct{}
		if len(req.Params) > 0 {
			if err := jsonv2.Unmarshal(req.Params, &params, jsonv2.RejectUnknownMembers(true)); err != nil {
				return err
			}
		}
		statuses, err := s.app.PluginStatuses()
		if err != nil {
			return err
		}
		data = statuses
	} else {
		var params struct {
			ID string `json:"id"`
		}
		if err := jsonv2.Unmarshal(req.Params, &params, jsonv2.RejectUnknownMembers(true)); err != nil {
			return err
		}
		if params.ID == "" {
			return errors.New("plugin id is required")
		}
		status, err := s.app.SetPluginEnabled(ctx, params.ID, req.Type == "plugin_enable")
		if err != nil {
			return err
		}
		data = status
	}
	s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: data})
	return nil
}
