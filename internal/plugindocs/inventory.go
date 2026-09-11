package plugindocs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
)

// Plugin deliberately has no arbitrary JSON, config, defaults, state, dynamic
// view contents, or filesystem reader. Paths are informational, not authority.
// Runtime details describe the loaded snapshot, not unexecuted files on disk.
type Plugin struct {
	ID              string    `json:"id"`
	Path            string    `json:"path,omitempty"`
	LoadedPath      string    `json:"loaded_path,omitempty"`
	Scope           string    `json:"scope,omitempty"`
	Registered      bool      `json:"registered"`
	Enabled         bool      `json:"enabled"`
	Loaded          bool      `json:"loaded"`
	RestartRequired bool      `json:"restart_required"`
	Name            string    `json:"name,omitempty"`
	Version         string    `json:"version,omitempty"`
	APIVersion      int       `json:"api_version,omitzero"`
	Capabilities    []string  `json:"capabilities,omitempty"`
	HostTools       []string  `json:"host_tools,omitempty"`
	Tools           []string  `json:"tools,omitempty"`
	Commands        []Command `json:"commands,omitempty"`
	Views           []View    `json:"views,omitempty"`
	Themes          []string  `json:"themes,omitempty"`
	Settings        []Setting `json:"settings,omitempty"`
}

type Command struct {
	ID           string `json:"id"`
	Description  string `json:"description,omitempty"`
	ArgumentHint string `json:"argument_hint,omitempty"`
	Alias        string `json:"alias,omitempty"`
}

type View struct {
	ID        string `json:"id"`
	Placement string `json:"placement"`
}

type Setting struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (t *Tool) plugins(ctx context.Context, args arguments, r *response) error {
	if t.opts.Inventory == nil {
		return errors.New("current-runtime plugin inventory is unavailable; offline references remain available")
	}
	plugins, err := t.opts.Inventory(ctx)
	if err != nil {
		return err
	}
	plugins = slices.Clone(plugins)
	slices.SortFunc(plugins, func(a, b Plugin) int { return cmp.Compare(a.ID, b.ID) })
	if args.PluginID != "" {
		i := slices.IndexFunc(plugins, func(p Plugin) bool { return p.ID == args.PluginID })
		if i < 0 {
			return fmt.Errorf("JavaScript plugin %q is not registered in allowed scopes or loaded in this runtime", args.PluginID)
		}
		plugins = plugins[i : i+1]
	}
	r.Total = len(plugins)
	start := min(args.Offset-1, len(plugins))
	for _, p := range plugins[start : start+min(args.Limit, len(plugins)-start)] {
		if err := ctx.Err(); err != nil {
			return err
		}
		if args.PluginID == "" {
			p.Capabilities, p.HostTools, p.Tools = nil, nil, nil
			p.Commands, p.Views, p.Themes, p.Settings = nil, nil, nil, nil
		}
		r.Plugins = append(r.Plugins, p)
		if !t.fits(r) {
			r.Plugins = r.Plugins[:len(r.Plugins)-1]
			if len(r.Plugins) == 0 {
				return errors.New("plugin metadata exceeds the configured output limit; increase the limit or inspect source with normal file tools")
			}
			break
		}
	}
	r.next(len(r.Plugins))
	return nil
}
