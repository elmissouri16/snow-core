package subagent

import (
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var errPlanRequiresReadOnlyChild = errors.New("subagents: Plan mode permits only children with resolved read-only capabilities")

var planReadOnlyChildTools = map[string]bool{
	"read":                true,
	"grep":                true,
	"glob":                true,
	"artifact_read":       true,
	"artifact_grep":       true,
	"activate_skill":      true,
	"deactivate_skill":    true,
	"read_skill_resource": true,
}

// planRoleReadOnly evaluates the effective role allowlist rather than trusting
// its display name or AllowMutation flag. An empty allowlist inherits the
// shell-capable child surface, so it is not safe in Plan mode.
func planRoleReadOnly(role Role, recursiveAuthority bool) bool {
	if recursiveAuthority || len(role.Tools) == 0 {
		return false
	}
	for _, name := range role.Tools {
		if !planReadOnlyChildTools[name] {
			return false
		}
	}
	return true
}

func (m *Manager) planSafeTarget(r *runtime) error {
	state := r.snapshot()
	m.mu.RLock()
	role, ok := m.limits.Roles[state.Agent.Role]
	recursiveAuthority := m.limits.Recursive && state.Agent.Depth < m.limits.MaxDepth
	m.mu.RUnlock()
	if !ok || !planRoleReadOnly(role, recursiveAuthority) {
		return errPlanRequiresReadOnlyChild
	}
	r.mu.Lock()
	fingerprint := r.record.RoleFingerprint
	r.mu.Unlock()
	if fingerprint != "" && fingerprint != roleFingerprint(role) {
		return errors.New("subagents: persisted child authority changed and is unavailable in Plan mode")
	}
	return nil
}

func (m *Manager) requirePlanSafeTarget(caller Caller, r *runtime) error {
	if caller.Path != protocol.RootAgentPath || m.root.Mode() != protocol.ModePlan {
		return nil
	}
	return m.planSafeTarget(r)
}

// ValidatePlanTransition prevents an authoritative switch to Plan mode while
// mutation-capable child work is already queued, running, or finalizing.
func (m *Manager) ValidatePlanTransition() error {
	m.mu.RLock()
	if len(m.reserved) != 0 {
		m.mu.RUnlock()
		return errors.New("subagents: cannot enter Plan mode while child admission is in progress")
	}
	runtimes := make([]*runtime, 0, len(m.byID))
	for _, r := range m.byID {
		runtimes = append(runtimes, r)
	}
	m.mu.RUnlock()
	for _, r := range runtimes {
		r.mu.Lock()
		active := runtimeHasActiveWorkLocked(r)
		r.mu.Unlock()
		if active {
			if err := m.planSafeTarget(r); err != nil {
				return errors.New("subagents: cannot enter Plan mode while mutation-capable child work is active")
			}
		}
	}
	return nil
}
