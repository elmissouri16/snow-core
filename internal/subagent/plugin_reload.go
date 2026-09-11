package subagent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// CheckPluginReloadAdmitted checks every branch, including dormant children.
// The caller holds root admission, preventing new child admission until commit.
func (m *Manager) CheckPluginReloadAdmitted(id string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return errors.New("plugin reload: subagent manager closed")
	}
	if len(m.reserved) != 0 {
		return errors.New("plugin reload: child admission active")
	}
	for _, r := range m.byID {
		r.mu.Lock()
		active := runtimeHasActiveWorkLocked(r)
		r.mu.Unlock()
		if active {
			return errors.New("plugin reload: child work active")
		}
		state := r.snapshot()
		if state.Status == protocol.AgentClosed {
			continue
		}
		for name := range state.PluginTools {
			if strings.HasPrefix(name, "plugin_"+id+"_") {
				return fmt.Errorf("plugin reload: close child %s retaining %s first", state.Agent.Path, id)
			}
		}
	}
	return nil
}
