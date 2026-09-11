package app

import (
	"context"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/internal/plugindocs"
	"github.com/elmissouri16/snow-core/internal/tools"
)

// pluginDocsInventory reuses the management surface's trust-scoped declarations
// and the live loaded catalog. It never opens a registered package or executes
// JavaScript. Whitelist fields rather than serializing PluginInfo: that public
// UI type also contains configuration, setting defaults, and dynamic contents.
func (a *App) pluginDocsInventory(ctx context.Context) ([]plugindocs.Plugin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.pluginMutationMu.Lock()
	defer a.pluginMutationMu.Unlock()
	statuses, err := a.PluginStatuses()
	if err != nil {
		// Configuration parse errors may include operator-controlled values.
		return nil, errors.New("plugin registration inventory unavailable; inspect configuration through normal permissioned tools")
	}
	byID := make(map[string]int, len(statuses))
	out := make([]plugindocs.Plugin, 0, len(statuses))
	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		byID[status.ID] = len(out)
		out = append(out, plugindocs.Plugin{
			ID: status.ID, Path: status.Path, Scope: status.Scope,
			Registered: true, Enabled: status.Enabled, Loaded: status.Loaded,
			RestartRequired: status.RestartRequired,
		})
	}
	for _, info := range a.PluginInfos() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index, ok := byID[info.ID]
		if !ok {
			// Removed registrations can remain loaded until restart. Go plugins
			// have no JavaScript API version and are outside this inventory.
			if info.APIVersion == 0 {
				continue
			}
			index = len(out)
			byID[info.ID] = index
			out = append(out, plugindocs.Plugin{ID: info.ID, Scope: info.Scope, RestartRequired: true})
		}
		p := &out[index]
		p.Loaded, p.LoadedPath = true, info.Path
		p.Name, p.Version, p.APIVersion = info.Name, info.Version, info.APIVersion
		p.Capabilities, p.HostTools = slices.Clone(info.Capabilities), slices.Clone(info.HostTools)
		for _, command := range info.Commands {
			p.Commands = append(p.Commands, plugindocs.Command{ID: command.ID, Description: command.Description, ArgumentHint: command.ArgumentHint, Alias: command.Alias})
		}
		for _, view := range info.Views {
			p.Views = append(p.Views, plugindocs.View{ID: view.ID, Placement: view.Placement})
		}
		for _, theme := range info.Themes {
			p.Themes = append(p.Themes, theme.ID)
		}
		for _, setting := range info.Settings {
			p.Settings = append(p.Settings, plugindocs.Setting{Name: setting.Name, Type: setting.Type})
		}
		if p.Path != "" && p.Path != p.LoadedPath {
			p.RestartRequired = true
		}
	}
	for _, desc := range tools.SelectMetadata(a.Registry, func(d tools.DescriptorMetadata) bool { return d.Source == tools.SourceJSPlugin }) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if desc.Source != tools.SourceJSPlugin {
			continue
		}
		// Original registry ownership is plugin:<id>; descriptor lookup avoids
		// treating a tool-name prefix as plugin identity.
		full, ok := a.Registry.Descriptor(desc.Name)
		if !ok {
			continue
		}
		if index, ok := byID[full.PluginID]; ok {
			out[index].Tools = append(out[index].Tools, desc.Name)
		}
	}
	return out, nil
}
