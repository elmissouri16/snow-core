package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

// EditPluginSetting uses the existing interaction broker and saves to the
// declaration's configuration scope. New values take effect on restart.
func (a *App) EditPluginSetting(ctx context.Context, id, name string) error {
	for _, info := range a.PluginInfos() {
		if info.ID != id {
			continue
		}
		for _, setting := range info.Settings {
			if setting.Name != name {
				continue
			}
			raw, _ := jsonv2.Marshal(map[string]any{"title": "Configure " + info.Name, "fields": []any{setting}})
			host := &appExtensionHost{app: a, services: a.extensions, info: info, agent: a.Agent, store: a.Session}
			answer, err := host.askPluginInput(ctx, "ui.form", raw)
			if err != nil {
				return err
			}
			var values map[string]any
			if err := jsonv2.Unmarshal(answer, &values); err != nil {
				return err
			}
			return a.UpdatePluginSettings(id, values)
		}
	}
	return errors.New("unknown plugin setting")
}
func (a *App) UpdatePluginSettings(id string, values map[string]any) error {
	for _, info := range a.PluginInfos() {
		if info.ID != id {
			continue
		}
		known := map[string]bool{}
		for _, setting := range info.Settings {
			known[setting.Name] = true
		}
		for key := range values {
			if !known[key] {
				return fmt.Errorf("unknown setting %s", key)
			}
		}
		merged := map[string]any{}
		if err := jsonv2.Unmarshal(info.Config, &merged); err != nil {
			return err
		}
		for key, value := range values {
			merged[key] = value
		}
		if err := javascript.ValidateSettings(info.Settings, merged); err != nil {
			return err
		}
		// Only edit an existing effective declaration; explicit path flags have no
		// persistent configuration to update.
		paths := []struct {
			path   string
			global bool
		}{{filepath.Join(a.ProjectInputRoot, ".snow", "config.json"), false}, {a.ConfigPath, true}}
		if info.Scope == "explicit" {
			return errors.New("settings for an explicit plugin path cannot be persisted; use its registered declaration")
		}
		for _, candidate := range paths {
			if candidate.global && info.Scope == "project" || !candidate.global && info.Scope != "project" {
				continue
			}
			if !candidate.global && !a.ProjectAllowed {
				continue
			}
			var specs map[string]plugin.JavaScriptSpec
			if candidate.global {
				cfg, err := config.Load(candidate.path)
				if err != nil {
					continue
				}
				specs = cfg.JavaScriptPlugins
			} else {
				ext, err := config.LoadProjectExtensions(candidate.path)
				if err != nil {
					continue
				}
				specs = ext.JavaScriptPlugins
			}
			if _, ok := specs[id]; !ok {
				continue
			}
			return config.UpdateJavaScriptPlugins(candidate.path, candidate.global, func(specs map[string]plugin.JavaScriptSpec) error {
				spec, ok := specs[id]
				if !ok {
					return errors.New("plugin registration changed")
				}
				current := map[string]any{}
				if len(spec.Config) > 0 {
					if err := jsonv2.Unmarshal(spec.Config, &current); err != nil {
						return err
					}
				}
				for key, value := range values {
					current[key] = value
				}
				raw, err := jsonv2.Marshal(current)
				if err != nil {
					return err
				}
				spec.Config = json.RawMessage(raw)
				specs[id] = spec
				return nil
			})
		}
		return errors.New("register this plugin with snow plugin add before saving settings")
	}
	return errors.New("unknown plugin")
}
