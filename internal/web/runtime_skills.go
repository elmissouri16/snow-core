package web

import (
	"context"
	"net/http"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeSkillsBackend is optional. Opt-in applies only to an explicitly opened
// worker, never to remembered project trust or a saved session's tool policy.
type RuntimeSkillsBackend interface {
	OpenWithSkills(ctx context.Context, project Project, sessionID, provider, model string, skills bool) (RuntimeSnapshot, error)
	Skills(ctx context.Context, projectID, instanceID string) (RuntimeSkills, error)
}

// RuntimeSkills is metadata only. Enabled describes runtime opt-in; each entry's
// Enabled is the authoritative worker catalog policy, not activation state.
type RuntimeSkills struct {
	ProjectID  string         `json:"project_id"`
	InstanceID string         `json:"instance_id"`
	SessionID  string         `json:"session_id"`
	Enabled    bool           `json:"enabled"`
	Skills     []RuntimeSkill `json:"skills"`
	Limited    bool           `json:"limited"`
}

type RuntimeSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	DisabledBy  string `json:"disabled_by"`
}

const (
	runtimeSkillsLimit           = 100
	runtimeSkillDescriptionBytes = 1024
)

// OpenWithSkills preserves the restricted worker profile except for the three
// skill lifecycle tools when explicitly selected. CLI extension trust and all
// tool permission gates remain owned by the worker.
func (m *RuntimeManager) OpenWithSkills(ctx context.Context, project Project, sessionID, provider, model string, skills bool) (RuntimeSnapshot, error) {
	return m.open(ctx, project, sessionID, provider, model, skills)
}

// Skills never discovers on the manager or starts a worker. The same nonqueued
// control gate fences session switches, prompt admission and retired instances.
func (m *RuntimeManager) Skills(ctx context.Context, projectID, instanceID string) (RuntimeSkills, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSkills{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeSkills{}, err
	}
	r.mu.Lock()
	idle := r.skillsIdleLocked()
	enabled := r.skillsEnabled
	sessionID := r.snapshot.SessionID
	r.mu.Unlock()
	if !idle {
		return RuntimeSkills{}, ErrRuntimeBusy
	}
	result := RuntimeSkills{ProjectID: projectID, InstanceID: instanceID, SessionID: sessionID, Enabled: enabled, Skills: []RuntimeSkill{}}
	if !enabled {
		return result, nil
	}
	var catalog protocol.RPCSkillsList
	if err := r.call(protocol.RPCRequest{Type: "skills"}, nil, &catalog); err != nil {
		return RuntimeSkills{}, err
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeSkills{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.skillsIdleLocked() {
		return RuntimeSkills{}, ErrRuntimeBusy
	}
	result = projectRuntimeSkills(catalog)
	result.ProjectID, result.InstanceID, result.SessionID = projectID, instanceID, r.snapshot.SessionID
	return result, nil
}

func (r *liveRuntime) skillsIdleLocked() bool {
	return r.permissionPolicyIdleLocked() && !r.snapshot.CancelRequested && !r.goalBlocksHistoryLocked()
}

func projectRuntimeSkills(catalog protocol.RPCSkillsList) RuntimeSkills {
	result := RuntimeSkills{Enabled: true, Skills: []RuntimeSkill{}, Limited: len(catalog.Skills) > runtimeSkillsLimit}
	for _, skill := range catalog.Skills[:min(len(catalog.Skills), runtimeSkillsLimit)] {
		if !runtimeIdentifier(skill.Name) {
			result.Limited = true
			continue
		}
		description := runtimeText(skill.Description, runtimeSkillDescriptionBytes)
		if description != skill.Description {
			result.Limited = true
		}
		disabledBy := ""
		if !skill.Enabled {
			disabledBy = publicSkillDisabledBy(skill.DisabledBy)
		}
		result.Skills = append(result.Skills, RuntimeSkill{Name: skill.Name, Description: description, Enabled: skill.Enabled, DisabledBy: disabledBy})
	}
	return result
}

// Reasons are policy labels, not an escape hatch for paths/configuration or
// arbitrary worker diagnostics. Unknown future reasons retain disabled status.
func publicSkillDisabledBy(reason string) string {
	switch reason {
	case "skills disabled by configuration", "skills disabled by runtime policy",
		"disabled by named skill policy", "disabled by global skills policy",
		"disabled by project skills policy", "disabled by global named skill policy",
		"disabled by project named skill policy", "excluded by Agent Skills catalog byte limit",
		"activate_skill disabled by explicit tool allowlist":
		return reason
	default:
		return "Disabled by skill policy"
	}
}

// The parent resolves the registered project. Recheck browser authority here so
// this helper also fails closed when invoked without the outer HTTP dispatcher.
func (s *shell) runtimeSkillsAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	if r.Method != http.MethodPost {
		http.Error(w, "Skill catalog requires POST", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	if r.PathValue("action") != "skills" || len(r.URL.Query()) != 0 || len(r.PostForm) != 2 || len(r.PostForm["csrf"]) != 1 || len(r.PostForm["instance_id"]) != 1 || !runtimeIdentifier(r.PostForm.Get("instance_id")) {
		http.Error(w, "Invalid skill catalog fields; reload this page", http.StatusBadRequest)
		return
	}
	backend, ok := s.runtimes.(RuntimeSkillsBackend)
	if !ok {
		http.Error(w, "Agent Skills are unavailable", http.StatusServiceUnavailable)
		return
	}
	instance := r.PostForm.Get("instance_id")
	snapshot, live := s.runtimes.Snapshot(project.ID)
	if !live || snapshot.ProjectID != project.ID || snapshot.InstanceID != instance || !runtimeIdentifier(snapshot.SessionID) || snapshot.Status != "idle" {
		http.Error(w, "Reload the current conversation", http.StatusConflict)
		return
	}
	result, err := backend.Skills(ctx, project.ID, instance)
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	current, live := s.runtimes.Snapshot(project.ID)
	if !live || current.ProjectID != project.ID || current.InstanceID != instance || current.SessionID != snapshot.SessionID || current.Status != "idle" {
		http.Error(w, "Reload the current conversation", http.StatusConflict)
		return
	}
	// Browser identity comes only from the stable current snapshot, never from
	// arbitrary identity fields returned by an optional catalog backend.
	result.ProjectID, result.InstanceID, result.SessionID = current.ProjectID, current.InstanceID, current.SessionID
	s.runtimeJSON(w, result)
}

var _ RuntimeSkillsBackend = (*RuntimeManager)(nil)
