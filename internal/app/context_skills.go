package app

import (
	"errors"

	"github.com/elmissouri16/snow-core/internal/agent"
)

// ContextReport returns the root agent's count-only provider-context report.
func (a *App) ContextReport() (agent.ContextReport, error) {
	if a == nil || a.Agent == nil {
		return agent.ContextReport{}, errors.New("app: agent is unavailable")
	}
	return a.Agent.ContextReport()
}

// ClearActiveSkills durably deactivates every branch-active Agent Skill. It
// does not delete discovered skill files or mutate global configuration.
func (a *App) ClearActiveSkills() (int, error) {
	if a == nil || a.Agent == nil {
		return 0, errors.New("app: agent is unavailable")
	}
	return a.Agent.ClearActiveSkills()
}
