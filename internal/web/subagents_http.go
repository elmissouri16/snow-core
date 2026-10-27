package web

import (
	"context"
	"net/http"
)

// Parent runtimeAction must authenticate, enforce POST/same-origin/CSRF, bound
// ParseForm and resolve the registered project before dispatching here.
func (s *shell) runtimeSubagentsAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeSubagentsBackend)
	if !ok {
		http.Error(w, "Subagent status is unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(r.URL.Query()) != 0 || len(r.PostForm) != 3 || len(r.PostForm["csrf"]) != 1 || len(r.PostForm["instance_id"]) != 1 || len(r.PostForm["session_id"]) != 1 {
		http.Error(w, "Invalid subagent status fields; reload this page", http.StatusBadRequest)
		return
	}
	instance, session := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id")
	if !runtimeIdentifier(instance) || !runtimeIdentifier(session) {
		http.Error(w, "Reload the current conversation", http.StatusConflict)
		return
	}
	result, err := backend.Subagents(ctx, project.ID, instance)
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	snapshot, live := s.runtimes.Snapshot(project.ID)
	if !live || result.ProjectID != project.ID || result.InstanceID != instance || result.SessionID != session || snapshot.InstanceID != instance || snapshot.SessionID != session {
		http.Error(w, "Reload the current conversation", http.StatusConflict)
		return
	}
	s.runtimeJSON(w, result)
}
