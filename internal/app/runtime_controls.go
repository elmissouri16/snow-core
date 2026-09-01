package app

import (
	_ "embed"
	"errors"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ProjectTrustState is the effective preflight decision for the canonical
// project path. Loaded reports the immutable startup decision; a differing
// effective decision requires a process restart before project-local inputs
// can be loaded or unloaded.
type ProjectTrustState struct {
	Path            string
	Level           trust.Level
	Prompt          bool
	Loaded          bool
	RestartRequired bool
}

// PermissionMode returns the active session's permission policy.
func (a *App) PermissionMode() (permission.Mode, error) {
	if a == nil || a.Perm == nil {
		return "", errors.New("app: permission service unavailable")
	}
	return a.Perm.Mode(), nil
}

// ProjectTrust resolves the persisted trust decision with the same global
// preflight policy used during startup. It never loads project-local inputs.
func (a *App) ProjectTrust() (ProjectTrustState, error) {
	if a == nil || a.Trust == nil {
		return ProjectTrustState{}, errors.New("app: trust store unavailable")
	}
	resolution, err := trust.Resolve(a.CWD(), a.PersistedCfg.DefaultProjectTrust, a.Trust)
	if err != nil {
		return ProjectTrustState{}, err
	}
	loaded := a.ProjectAllowed
	allowed := !resolution.Prompt && resolution.Level == trust.LevelAllow
	return ProjectTrustState{
		Path:            resolution.Path,
		Level:           resolution.Level,
		Prompt:          resolution.Prompt,
		Loaded:          loaded,
		RestartRequired: allowed != loaded || resolution.Path != a.ProjectInputRoot,
	}, nil
}

// SetProjectTrust persists a canonical project decision for the next launch.
// Runtime project-local resources remain exactly as loaded at startup.
func (a *App) SetProjectTrust(level trust.Level) (ProjectTrustState, error) {
	if a == nil || a.Trust == nil {
		return ProjectTrustState{}, errors.New("app: trust store unavailable")
	}
	if level != trust.LevelAsk && level != trust.LevelAllow && level != trust.LevelDeny {
		return ProjectTrustState{}, errors.New("app: invalid project trust level")
	}
	path, err := trust.CanonicalPath(a.CWD())
	if err != nil {
		return ProjectTrustState{}, err
	}
	if err := a.Trust.Set(path, level); err != nil {
		return ProjectTrustState{}, err
	}
	return a.ProjectTrust()
}

//go:embed project_init_prompt.md
var projectInitPrompt string

// PrepareProjectInit returns the core-owned initialization prompt at a safe
// turn boundary. Surfaces pass the returned prompt through their normal prompt
// lifecycle so persistence, streaming, cancellation, and completion stay
// unified with every other agent turn.
func (a *App) PrepareProjectInit() (string, error) {
	if a == nil || a.Agent == nil {
		return "", errors.New("init: agent unavailable")
	}
	if a.Agent.IsRunning() {
		return "", errors.New("init: wait for the current turn to finish")
	}
	if a.Agent.Mode() == protocol.ModePlan {
		return "", errors.New("init: switch to Default mode first")
	}
	prompt := strings.TrimSpace(projectInitPrompt)
	if prompt == "" {
		return "", errors.New("init: prompt is unavailable")
	}
	return prompt, nil
}
