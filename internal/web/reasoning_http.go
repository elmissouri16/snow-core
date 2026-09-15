package web

import (
	"context"
	"net/http"
	"strconv"
)

func reasoningAction(action string) bool {
	return action == "reasoning-inspect" || action == "reasoning-set"
}

// Parent runtimeAction must authenticate, enforce POST/same-origin/CSRF, bound
// ParseForm and resolve the registered project before dispatching here.
func (s *shell) runtimeReasoningAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeReasoningBackend)
	if !ok {
		http.Error(w, "Reasoning controls are unavailable", http.StatusServiceUnavailable)
		return
	}
	action := r.PathValue("action")
	if !reasoningAction(action) {
		http.Error(w, "Unknown reasoning action", http.StatusBadRequest)
		return
	}
	allowed := map[string]bool{"csrf": true, "instance_id": true, "session_id": true}
	if action == "reasoning-set" {
		for _, key := range []string{"branch_id", "tip_id", "expected_revision", "provider", "model", "mode", "permission_mode", "thinking", "reasoning_summary", "text_verbosity", "scope", "field", "value", "confirm"} {
			allowed[key] = true
		}
	}
	invalid := func() { http.Error(w, "Reload and review the current reasoning settings", http.StatusConflict) }
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
	var result RuntimeReasoning
	var err error
	if action == "reasoning-inspect" {
		result, err = backend.InspectReasoning(ctx, project.ID, instance, session)
	} else {
		for key := range allowed {
			if len(r.PostForm[key]) != 1 {
				invalid()
				return
			}
		}
		get := r.PostForm.Get
		raw := get("expected_revision")
		revision, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || revision == 0 || strconv.FormatUint(revision, 10) != raw || get("confirm") != "session" || get("scope") != "session" || !reasoningField(get("field")) {
			invalid()
			return
		}
		expected := RuntimeReasoning{ProjectID: project.ID, InstanceID: instance, SessionID: session, BranchID: get("branch_id"), TipID: get("tip_id"), Revision: revision, Provider: get("provider"), Model: get("model"), Mode: get("mode"), PermissionMode: get("permission_mode"), Thinking: get("thinking"), ReasoningSummary: get("reasoning_summary"), TextVerbosity: get("text_verbosity")}
		result, err = backend.SetReasoning(ctx, project.ID, instance, RuntimeReasoningInput{Expected: expected, Scope: get("scope"), Field: get("field"), Value: get("value"), Confirm: true})
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	snapshot, live := s.runtimes.Snapshot(project.ID)
	if !live || result.ProjectID != project.ID || result.InstanceID != instance || result.SessionID != session || snapshot.InstanceID != instance || snapshot.SessionID != session || snapshot.Revision != result.Revision || snapshot.Status != "idle" || snapshot.Provider != result.Provider || snapshot.Model != result.Model || snapshot.Mode != result.Mode || snapshot.PermissionMode != result.PermissionMode || snapshot.Thinking != result.Thinking {
		invalid()
		return
	}
	s.runtimeJSON(w, result)
}
