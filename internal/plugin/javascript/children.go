package javascript

import (
	"errors"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/plugin"
)

// ChildToolUses reports only tools explicitly opted into the isolated profile.
func (r *Runtime) ChildToolUses(name string) ([]string, bool) {
	prefix := "plugin_" + r.pkg.Manifest.ID + "_"
	if !strings.HasPrefix(name, prefix) {
		return nil, false
	}
	for _, def := range r.childDefinitions {
		if prefix+def.Name == name {
			return slices.Clone(def.Uses), true
		}
	}
	return nil, false
}
func (r *Runtime) ChildPlugin(names []string) (plugin.Plugin, error) {
	local := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := r.ChildToolUses(name); !ok {
			return nil, errors.New("tool is not available to children")
		}
		local = append(local, strings.TrimPrefix(name, "plugin_"+r.pkg.Manifest.ID+"_"))
	}
	opts := r.opts
	r.mu.Lock()
	opts.Diagnostic = r.diagnosticFn
	r.mu.Unlock()
	opts.ChildTools = local
	return New(r.pkg, opts), nil
}
