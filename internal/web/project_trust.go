package web

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Activation always requires a fresh explicit POST. Remembered trust only
// replaces the repeated warning; it never grants tool permissions or starts work.
func (s *shell) authorizeProjectActivation(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) bool {
	for key, values := range r.PostForm {
		switch key {
		case "csrf", "confirm", "session_id", "provider", "model", "remember_trust", "enable_skills":
			if len(values) == 1 {
				continue
			}
		}
		http.Error(w, "Invalid activation fields; reload this page", http.StatusBadRequest)
		return false
	}
	if skills := r.PostForm.Get("enable_skills"); skills != "" && skills != "runtime" {
		http.Error(w, "Invalid runtime skills choice", http.StatusBadRequest)
		return false
	}
	remember := r.PostForm.Get("remember_trust")
	switch r.PostForm.Get("confirm") {
	case "activate":
		if remember != "" && remember != "project" {
			http.Error(w, "Invalid project trust choice", http.StatusBadRequest)
			return false
		}
		if remember == "project" {
			if err := s.registry.RememberProjectTrust(ctx, project); err != nil {
				http.Error(w, "Could not remember project trust. No runtime was started", http.StatusConflict)
				return false
			}
		}
		return true // Existing explicit one-time activation remains supported.
	case "trusted":
		if remember == "" && project.Trusted {
			return true
		}
		http.Error(w, "Project trust changed. Reload and review before starting", http.StatusConflict)
		return false
	default:
		http.Error(w, "Explicit runtime activation is required", http.StatusBadRequest)
		return false
	}
}

func (s *shell) revokeProjectTrust(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	if len(r.PostForm) != 2 || len(r.PostForm["csrf"]) != 1 || len(r.PostForm["confirm"]) != 1 || r.PostForm.Get("confirm") != "revoke" {
		http.Error(w, "Explicit project trust revocation is required", http.StatusBadRequest)
		return
	}
	if s.registry == nil {
		http.Error(w, "Project registry unavailable", http.StatusServiceUnavailable)
		return
	}
	if !s.projectControl.TryLock() {
		http.Error(w, "Project activation or removal is busy", http.StatusConflict)
		return
	}
	defer s.projectControl.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	id := r.PathValue("project")
	if err := s.registry.RevokeProjectTrust(ctx, id); err != nil {
		http.Error(w, "Could not forget project trust. Reload and review", http.StatusConflict)
		return
	}
	// Revocation only affects future starts. Never stop a worker or rewrite its
	// saved/current permission policy as a side effect of forgetting consent.
	http.Redirect(w, r, "/?view=projects&project="+url.QueryEscape(id), http.StatusSeeOther)
}
