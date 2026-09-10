package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// PluginStatuses lists effective registrations, including disabled packages,
// without reading or executing their JavaScript. Project inputs are restricted
// to the canonical project root authorized at startup. Explicit inputs retain
// their startup precedence and cannot be persisted by these controls.
func (a *App) PluginStatuses() ([]protocol.PluginStatus, error) {
	declarations, err := a.currentPluginDeclarations()
	if err != nil {
		return nil, err
	}
	out := make([]protocol.PluginStatus, 0, len(declarations))
	for _, d := range declarations {
		out = append(out, a.pluginStatus(d))
	}
	return out, nil
}

func (a *App) currentPluginDeclarations() ([]javascript.Declaration, error) {
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return nil, err
	}
	var project map[string]plugin.JavaScriptSpec
	if a.ProjectAllowed {
		ext, err := config.LoadProjectExtensions(filepath.Join(a.ProjectInputRoot, ".snow", "config.json"))
		if err != nil {
			return nil, err
		}
		project = ext.JavaScriptPlugins
	}
	explicit := map[string]plugin.JavaScriptSpec{}
	for _, d := range a.pluginDeclarations {
		if d.Scope == "explicit" {
			explicit[d.ID] = d.Spec
		}
	}
	return javascript.Resolve(cfg.JavaScriptPlugins, project, explicit, filepath.Dir(a.ConfigPath), a.ProjectInputRoot, a.CWD())
}

func (a *App) pluginStatus(d javascript.Declaration) protocol.PluginStatus {
	loaded := false
	for _, info := range a.PluginInfos() {
		if info.ID == d.ID {
			loaded = true
			break
		}
	}
	return protocol.PluginStatus{
		ID: d.ID, Path: d.Path, Scope: d.Scope, Enabled: !d.Disabled,
		Loaded: loaded, CanToggle: d.Scope != "explicit",
		RestartRequired: loaded == d.Disabled,
	}
}

// SetPluginEnabled saves an individual registration in its effective scope.
// It never changes a running plugin, command, hook, tool, dialog, or child.
// The returned status explicitly reports whether a restart is needed.
func (a *App) SetPluginEnabled(ctx context.Context, id string, enabled bool) (protocol.PluginStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.pluginMutationMu.Lock()
	defer a.pluginMutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return protocol.PluginStatus{}, err
	}
	if err := plugin.ValidateIdentifier("plugin id", id); err != nil {
		return protocol.PluginStatus{}, err
	}
	declarations, err := a.currentPluginDeclarations()
	if err != nil {
		return protocol.PluginStatus{}, err
	}
	for _, d := range declarations {
		if d.ID != id {
			continue
		}
		if d.Scope == "explicit" {
			return protocol.PluginStatus{}, errors.New("explicit JavaScript plugins are controlled by launch options; change JavaScriptPlugins or register with snow plugin add and launch without --js-plugin")
		}
		if d.Disabled == !enabled {
			return a.pluginStatus(d), nil
		}
		if enabled {
			count := 0
			for _, other := range declarations {
				if !other.Disabled {
					count++
				}
			}
			if count >= javascript.MaxPlugins {
				return protocol.PluginStatus{}, errors.New("at most 32 JavaScript plugins may be enabled")
			}
			p, err := javascript.ReadPackage(ctx, d.Path, d.Root, d.Spec.Config)
			if err != nil {
				return protocol.PluginStatus{}, fmt.Errorf("plugin %s: %w", id, err)
			}
			if p.Manifest.ID != id {
				return protocol.PluginStatus{}, fmt.Errorf("plugin %s: manifest id is %s", id, p.Manifest.ID)
			}
		}
		path, global := a.ConfigPath, d.Scope == "global"
		if !global {
			path = filepath.Join(a.ProjectInputRoot, ".snow", "config.json")
		}
		err := config.UpdateJavaScriptPlugins(path, global, func(specs map[string]plugin.JavaScriptSpec) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, ok := specs[id]
			if !ok || current.Path != d.Spec.Path || !bytes.Equal(current.Config, d.Spec.Config) {
				return errors.New("plugin registration changed; refresh and try again")
			}
			current.Disabled = !enabled
			specs[id] = current
			return nil
		})
		if err != nil {
			return protocol.PluginStatus{}, err
		}
		d.Disabled = !enabled
		return a.pluginStatus(d), nil
	}
	return protocol.PluginStatus{}, fmt.Errorf("JavaScript plugin %q not found in allowed configuration scopes", id)
}
