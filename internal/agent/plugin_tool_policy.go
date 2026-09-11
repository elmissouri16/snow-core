package agent

import (
	"errors"
	"github.com/elmissouri16/snow-core/internal/tools"
)

// PluginToolPolicy is additive to PluginHooks. A snapshot is immutable and may
// only restrict tools; permissions and native modes are still authoritative.
type PluginToolPolicy interface {
	ToolPolicy() func(string) error
}

func (a *Agent) pluginToolPolicy() func(string) error {
	if hooks, ok := a.opts.PluginHooks.(PluginToolPolicy); ok {
		return hooks.ToolPolicy()
	}
	return func(string) error { return nil }
}

func (h *progressHost) ToolAvailable(name string) bool { return h.agent.ToolAvailable(name) }

// PluginWorkflowBusyAdmitted is a read-only idle check for branch metadata
// updates. The caller holds admission; no automatic goal is stopped here.
func (a *Agent) PluginWorkflowBusyAdmitted() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return errors.New("plugin workflow: agent closed")
	}
	if a.running || a.autoRunning || a.autoPending {
		return errors.New("plugin workflow: root turn or automatic worker active")
	}
	return nil
}

// ToolAvailable reports provider eligibility without performing a permission
// request. Actual dispatch always repeats admission and authorization.
func (a *Agent) ToolAvailable(name string) bool {
	metadata, ok := tools.Metadata(a.opts.Registry, name)
	return ok && a.requestToolPolicy()(metadata) && tools.CanExposeMetadata(a.opts.Permission, metadata)
}
