package javascript

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

type Declaration struct {
	ID       string                `json:"id"`
	Scope    string                `json:"scope"`
	Spec     plugin.JavaScriptSpec `json:"-"`
	Path     string                `json:"path"`
	Disabled bool                  `json:"disabled"`
	Root     string                `json:"-"`
}

// Resolve merges declarations without reading any package. Project must have
// already passed trust resolution. Disabled entries shadow lower scopes.
func Resolve(global, project, explicit map[string]plugin.JavaScriptSpec, globalBase, projectRoot, cwd string) ([]Declaration, error) {
	effective := map[string]Declaration{}
	for _, scope := range []struct {
		name, base, root string
		specs            map[string]plugin.JavaScriptSpec
	}{
		{"global", globalBase, "", global}, {"project", projectRoot, projectRoot, project}, {"explicit", cwd, "", explicit},
	} {
		for id, spec := range scope.specs {
			if err := plugin.ValidateIdentifier("plugin id", id); err != nil {
				return nil, err
			}
			if spec.Path == "" && !spec.Disabled {
				return nil, fmt.Errorf("plugin %s: path is required", id)
			}
			path := spec.Path
			if path != "" && !filepath.IsAbs(path) {
				path = filepath.Join(scope.base, path)
			}
			if path != "" {
				var err error
				path, err = filepath.Abs(path)
				if err != nil {
					return nil, err
				}
			}
			effective[id] = Declaration{ID: id, Scope: scope.name, Spec: spec, Path: path, Disabled: spec.Disabled, Root: scope.root}
		}
	}
	out := make([]Declaration, 0, len(effective))
	enabled := 0
	for _, id := range slices.Sorted(maps.Keys(effective)) {
		d := effective[id]
		if !d.Disabled {
			enabled++
		}
		out = append(out, d)
	}
	if enabled > MaxPlugins {
		return nil, errors.New("at most 32 JavaScript plugins may be enabled")
	}
	return out, nil
}
