package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/skills"
)

// SkillStatus separates persisted policy from the immutable running catalog.
// Changing policy never edits skill files or deactivates branch-active skills.
type SkillStatus struct {
	Skill           skills.Skill
	Enabled         bool
	PolicyScope     string
	RestartRequired bool
}

// SkillStatuses reports discovered skills with their effective saved policy.
// Only the canonical project root authorized at startup may supply project policy.
func (a *App) SkillStatuses() ([]SkillStatus, error) {
	if a == nil || a.Skills == nil {
		return nil, nil
	}
	global, project, err := a.skillPolicies()
	if err != nil {
		return nil, err
	}
	var statuses []SkillStatus
	for _, skill := range a.Skills.Inventory() {
		statuses = append(statuses, a.skillStatus(skill, global, project))
	}
	return statuses, nil
}

func (a *App) skillPolicies() (config.SkillsConfig, config.ProjectSkillsConfig, error) {
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return config.SkillsConfig{}, config.ProjectSkillsConfig{}, err
	}
	var project config.ProjectSkillsConfig
	if a.ProjectAllowed {
		ext, err := config.LoadProjectExtensions(filepath.Join(a.ProjectInputRoot, ".snow", "config.json"))
		if err != nil {
			return config.SkillsConfig{}, project, err
		}
		project = ext.Skills
	}
	return cfg.Skills, project, nil
}

func (a *App) skillStatus(skill skills.Skill, global config.SkillsConfig, project config.ProjectSkillsConfig) SkillStatus {
	enabled := !global.Disabled
	if override, ok := global.Overrides[skill.Name]; ok {
		enabled = override
	}
	scope := "global"
	if a.ProjectAllowed && skill.Scope == "project" {
		scope = "project"
	}
	// A project default resets global named overrides, matching startup.
	if project.Disabled != nil {
		enabled, scope = !*project.Disabled, "project"
	}
	if override, ok := project.Overrides[skill.Name]; ok {
		enabled, scope = override, "project"
	}
	return SkillStatus{Skill: skill, Enabled: enabled, PolicyScope: scope, RestartRequired: enabled != skill.Enabled}
}

// SetSkillEnabled persists a named override in its effective policy scope,
// using the same atomic section writers as the CLI. Restart applies the policy
// to discovery, tools and completion; the running root/child catalogs stay intact.
func (a *App) SetSkillEnabled(ctx context.Context, name string, enabled bool) (SkillStatus, error) {
	if a == nil || a.Skills == nil {
		return SkillStatus{}, errors.New("skills are unavailable")
	}
	if err := ctx.Err(); err != nil {
		return SkillStatus{}, err
	}
	skill, ok := a.Skills.Lookup(name)
	if !ok {
		return SkillStatus{}, fmt.Errorf("skill %q is not discovered", name)
	}
	a.settingsMutationMu.Lock()
	defer a.settingsMutationMu.Unlock()
	global, project, err := a.skillPolicies()
	if err != nil {
		return SkillStatus{}, err
	}
	status := a.skillStatus(skill, global, project)
	if status.Enabled == enabled {
		return status, nil
	}
	if status.PolicyScope == "project" {
		err = config.UpdateProjectSkills(filepath.Join(a.ProjectInputRoot, ".snow", "config.json"), func(latest *config.ProjectSkillsConfig) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			latest.Overrides[name] = enabled
			return nil
		})
	} else {
		err = config.UpdateSkills(a.ConfigPath, func(latest *config.SkillsConfig) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			latest.Overrides[name] = enabled
			return nil
		})
	}
	if err != nil {
		return SkillStatus{}, err
	}
	status.Enabled, status.RestartRequired = enabled, enabled != skill.Enabled
	return status, nil
}
