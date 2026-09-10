package snowsdk

import (
	"context"
	"encoding/json"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PluginStatuses includes disabled registrations and distinguishes saved state
// from the currently loaded catalog. It does not execute plugin code.
func (s *Session) PluginStatuses() ([]protocol.PluginStatus, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	return a.PluginStatuses()
}

// SetPluginEnabled saves one registered JavaScript plugin's enabled state in
// its effective global/project scope. Restart the session to apply the change.
// Explicit Options.JavaScriptPlugins entries must be changed in launch options.
func (s *Session) SetPluginEnabled(ctx context.Context, id string, enabled bool) (protocol.PluginStatus, error) {
	a, err := s.activeApp()
	if err != nil {
		return protocol.PluginStatus{}, err
	}
	return a.SetPluginEnabled(ctx, id, enabled)
}

func (s *Session) Plugins() ([]protocol.PluginInfo, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	return a.PluginInfos(), nil
}
func (s *Session) PluginCommands() ([]protocol.PluginCommand, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	return a.PluginCommands(), nil
}
func (s *Session) PluginViews() ([]protocol.PluginView, error) {
	a, err := s.activeApp()
	if err != nil {
		return nil, err
	}
	return a.PluginViews(), nil
}
func (s *Session) RunPluginCommand(ctx context.Context, id, input string) (plugin.ToolResult, error) {
	a, err := s.activeApp()
	if err != nil {
		return plugin.ToolResult{}, err
	}
	return a.RunPluginCommand(ctx, id, input)
}
func (s *Session) CancelPluginCommand(id string) (bool, error) {
	a, err := s.activeApp()
	if err != nil {
		return false, err
	}
	return a.CancelPluginCommand(id), nil
}

// AttachPluginUI installs a host-owned presentation bridge. Permission replies
// remain on the separate trusted permission broker.
func (s *Session) AttachPluginUI(handler func(context.Context, protocol.PluginUIEvent) (json.RawMessage, error)) error {
	a, err := s.activeApp()
	if err != nil {
		return err
	}
	a.AttachPluginUI(app.PluginUIHandler(handler))
	return nil
}
