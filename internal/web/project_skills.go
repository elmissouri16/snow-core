package web

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

func (s *shell) saveProjectSkills(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	choice := r.PostForm.Get("enable_skills")
	if len(r.PostForm) != 2 || len(r.PostForm["csrf"]) != 1 || len(r.PostForm["enable_skills"]) != 1 || (choice != "" && choice != "runtime") {
		http.Error(w, "Invalid skill preference", http.StatusBadRequest)
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
	project, err := s.registry.Lookup(ctx, r.PathValue("project"))
	if err != nil {
		http.Error(w, "Project unavailable. Reload and review", http.StatusConflict)
		return
	}
	if err := s.registry.SetProjectSkills(ctx, project, choice == "runtime"); err != nil {
		http.Error(w, "Could not save skill preference. Reload and review", http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/?view=projects&project="+url.QueryEscape(project.ID), http.StatusSeeOther)
}
