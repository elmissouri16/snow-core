package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isSettingsCommand(command string) bool {
	switch command {
	case "settings_get", "settings_update":
		return true
	default:
		return false
	}
}

func (s *Server) handleSettingsCommand(ctx context.Context, req Request) error {
	if settingsRequestHasUnsupportedFields(req) {
		return fmt.Errorf("%s accepts no top-level fields other than id, type, and params", req.Type)
	}
	switch req.Type {
	case "settings_get":
		if len(req.Params) != 0 {
			return errors.New("settings_get does not accept params")
		}
		settings, err := s.app.RPCSettings()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: rpcSettings(settings)})
	case "settings_update":
		if len(req.Params) == 0 {
			return errors.New("settings_update requires params")
		}
		var params protocol.RPCSettingsUpdateParams
		if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("settings_update params: %w", err)
		}
		settings, err := s.app.UpdateRPCSettingsContext(ctx, app.SettingsUpdate{
			Provider:               params.Provider,
			Model:                  params.Model,
			Thinking:               params.Thinking,
			ReasoningSummary:       params.ReasoningSummary,
			TextVerbosity:          params.TextVerbosity,
			Theme:                  params.Theme,
			DebugEnabled:           params.DebugEnabled,
			SubagentsEnabled:       params.SubagentsEnabled,
			SubagentsMaxConcurrent: params.SubagentsMaxConcurrent,
			SkillsEnabled:          params.SkillsEnabled,
			UpdateCheckOnStartup:   params.UpdateCheckOnStartup,
		})
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: rpcSettings(settings)})
	default:
		return fmt.Errorf("unknown settings command %q", req.Type)
	}
}

func rpcSettings(settings app.SettingsSnapshot) protocol.RPCSettings {
	return protocol.RPCSettings{
		Provider: settings.Provider, Model: settings.Model, Thinking: settings.Thinking,
		ReasoningSummary: settings.ReasoningSummary, TextVerbosity: settings.TextVerbosity, Theme: settings.Theme,
		PermissionMode: settings.PermissionMode, DebugEnabled: settings.DebugEnabled,
		SubagentsEnabled: settings.SubagentsEnabled, SubagentsMaxConcurrent: settings.SubagentConcurrent,
		SubagentsMaxAgents: settings.SubagentAgentLimit, SkillsEnabled: settings.SkillsEnabled,
		SubagentsRestartRequired: settings.SubagentsRestartRequired,
		SkillsRestartRequired:    settings.SkillsRestartRequired, RestartRequired: settings.RestartRequired,
		UpdateCheckOnStartup: settings.UpdateCheckOnStartup,
	}
}

func settingsRequestHasUnsupportedFields(req Request) bool {
	return req.Message != "" || len(req.Content) != 0 || req.Model != "" || req.Thinking != "" ||
		req.ReasoningSummary != "" || req.TextVerbosity != "" || req.Mode != "" ||
		req.Provider != "" || req.Method != "" || req.Secret != ""
}
