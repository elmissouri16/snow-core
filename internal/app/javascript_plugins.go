package app

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/pkg/plugin"
)

func resolveJavaScriptPlugins(ctx context.Context, startup startupConfig, opts Options) ([]javascript.Declaration, error) {
	explicit := maps.Clone(opts.JavaScriptPlugins)
	if explicit == nil {
		explicit = map[string]plugin.JavaScriptSpec{}
	}
	for id, spec := range explicit {
		spec.Config = slices.Clone(spec.Config)
		explicit[id] = spec
	}
	for _, path := range opts.JavaScriptPaths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(startup.absCWD, path)
		}
		p, err := javascript.ReadPackage(ctx, path, "", nil)
		if err != nil {
			return nil, err
		}
		if _, exists := explicit[p.Manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate explicit JavaScript plugin %s", p.Manifest.ID)
		}
		explicit[p.Manifest.ID] = plugin.JavaScriptSpec{Path: path}
	}
	return javascript.Resolve(startup.cfg.JavaScriptPlugins, startup.projectJavaScriptPlugins, explicit, filepath.Dir(startup.configPath), startup.projectInputRoot, startup.absCWD)
}

func loadJavaScriptPlugins(ctx context.Context, manager *internalplugin.Manager, declarations []javascript.Declaration, startup startupConfig) error {
	for _, d := range declarations {
		if d.Disabled {
			continue
		}
		p, err := javascript.ReadPackage(ctx, d.Path, d.Root, d.Spec.Config)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", d.ID, err)
		}
		if p.Manifest.ID != d.ID {
			return fmt.Errorf("plugin %s: manifest id is %s", d.ID, p.Manifest.ID)
		}
		runtime := javascript.New(p, javascript.Options{
			MaxOutputBytes: startup.cfg.ToolOutputLimit(), MaxProgressBytes: startup.cfg.ToolOutputLimit(),
			Diagnostic: func(status, message string) { manager.RecordDiagnostic(d.ID, status, message) },
		})
		if err := manager.LoadJavaScript(runtime, p.Fingerprint); err != nil {
			return err
		}
		manager.SetJavaScriptScope(d.ID, d.Scope)
	}
	return nil
}
