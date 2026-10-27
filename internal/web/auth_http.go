package web

import (
	"context"
	"net/http"
)

func authAction(action string) bool {
	switch action {
	case "auth-inspect", "auth-login-start", "auth-login-status", "auth-login-cancel", "auth-logout":
		return true
	default:
		return false
	}
}

// Parent runtimeAction must authenticate, enforce POST/same-origin/CSRF, bound
// ParseForm and resolve the registered project before dispatching here.
func (s *shell) runtimeAuthAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeAuthBackend)
	if !ok {
		http.Error(w, "Provider sign-in is unavailable", http.StatusServiceUnavailable)
		return
	}
	action := r.PathValue("action")
	allowed := map[string]bool{"csrf": true, "instance_id": true, "session_id": true}
	switch action {
	case "auth-login-start":
		allowed["provider"], allowed["method"], allowed["secret"] = true, true, true
	case "auth-login-status", "auth-login-cancel":
		allowed["job_id"] = true
	case "auth-logout":
		allowed["provider"] = true
	}
	invalid := func() { http.Error(w, "Reload and review the current sign-in state", http.StatusConflict) }
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			invalid()
			return
		}
	}
	if len(r.URL.Query()) != 0 {
		invalid()
		return
	}
	instance, session := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id")
	if !runtimeIdentifier(instance) || !runtimeIdentifier(session) {
		invalid()
		return
	}
	// An API key travels in the clear on the trusted-LAN origin. Only the direct
	// loopback origin may relay one; browser OAuth and device codes carry no
	// secret through the manager and remain available on both origins.
	if r.PostForm.Get("secret") != "" && s.requestBoundary(r).network.profile != "local" {
		http.Error(w, "API keys can be entered only from the local URL on the Snow host, or with snow login in a terminal", http.StatusForbidden)
		return
	}
	var identity struct {
		project, instance, session string
	}
	var result any
	var err error
	get := r.PostForm.Get
	switch action {
	case "auth-inspect":
		var inventory RuntimeAuth
		inventory, err = backend.AuthProviders(ctx, project.ID, instance)
		identity.project, identity.instance, identity.session = inventory.ProjectID, inventory.InstanceID, inventory.SessionID
		result = inventory
	case "auth-login-start":
		var job RuntimeAuthJob
		job, err = backend.AuthLoginStart(ctx, project.ID, instance, RuntimeAuthLoginInput{ProviderID: get("provider"), Method: get("method"), Secret: get("secret")})
		identity.project, identity.instance, identity.session = job.ProjectID, job.InstanceID, job.SessionID
		result = job
	case "auth-login-status", "auth-login-cancel":
		var job RuntimeAuthJob
		if action == "auth-login-status" {
			job, err = backend.AuthLoginStatus(ctx, project.ID, instance, get("job_id"))
		} else {
			job, err = backend.AuthLoginCancel(ctx, project.ID, instance, get("job_id"))
		}
		identity.project, identity.instance, identity.session = job.ProjectID, job.InstanceID, job.SessionID
		result = job
	case "auth-logout":
		var inventory RuntimeAuth
		inventory, err = backend.AuthLogout(ctx, project.ID, instance, get("provider"))
		identity.project, identity.instance, identity.session = inventory.ProjectID, inventory.InstanceID, inventory.SessionID
		result = inventory
	default:
		http.Error(w, "Unknown sign-in action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	snapshot, live := s.runtimes.Snapshot(project.ID)
	if !live || identity.project != project.ID || identity.instance != instance || identity.session != session || snapshot.InstanceID != instance || snapshot.SessionID != session {
		invalid()
		return
	}
	s.runtimeJSON(w, result)
}
