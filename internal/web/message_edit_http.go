package web

import (
	"context"
	"net/http"
)

// RuntimeMessageEditBackend is optional; editing never falls back to resending a
// prompt or to an arbitrary RPC tunnel. Prepare resolves a local display ID.
type RuntimeMessageEditBackend interface {
	PrepareMessageEdit(context.Context, string, string, string) (RuntimeMessageEditPreparation, error)
	CommitMessageEdit(context.Context, string, string, string, string) (RuntimeSnapshot, error)
}

// RuntimeMessageEditPreparation contains only the full public source text and
// opaque, instance-bound authorization. Durable IDs remain worker-side.
type RuntimeMessageEditPreparation struct {
	ProjectID  string `json:"project_id"`
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	MessageID  string `json:"message_id"`
	EditToken  string `json:"edit_token"`
	Text       string `json:"text"`
}

func (s *shell) runtimeMessageEditAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeMessageEditBackend)
	if !ok {
		http.Error(w, "Message editing is unavailable", http.StatusServiceUnavailable)
		return
	}
	instance := r.PostForm.Get("instance_id")
	if instance == "" || !runtimeOption(instance) {
		http.Error(w, "Reload the current conversation before editing", http.StatusConflict)
		return
	}
	var err error
	switch r.PathValue("action") {
	case "message-edit-prepare":
		if id := r.PostForm.Get("message_id"); id == "" || !runtimeOption(id) {
			err = ErrRuntimeInvalid
			break
		}
		var prepared RuntimeMessageEditPreparation
		prepared, err = backend.PrepareMessageEdit(ctx, project.ID, instance, r.PostForm.Get("message_id"))
		if err == nil {
			s.runtimeJSON(w, prepared)
			return
		}
	case "message-edit-commit":
		if token := r.PostForm.Get("edit_token"); token == "" || !runtimeOption(token) || !validMessageEditText(r.PostForm.Get("text")) {
			err = ErrRuntimeInvalid
			break
		}
		var snapshot RuntimeSnapshot
		snapshot, err = backend.CommitMessageEdit(ctx, project.ID, instance, r.PostForm.Get("edit_token"), r.PostForm.Get("text"))
		if err == nil {
			s.runtimeJSON(w, snapshot)
			return
		}
	}
	http.Error(w, runtimePublicError(err), http.StatusConflict)
}
