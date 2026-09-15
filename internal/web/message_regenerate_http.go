package web

import (
	"context"
	"net/http"
)

// RuntimeMessageRegenerateBackend authorizes rerunning an exact completed
// assistant reply. It never accepts browser-selected user text or RPC commands.
type RuntimeMessageRegenerateBackend interface {
	PrepareMessageRegenerate(context.Context, string, string, string) (RuntimeMessageRegeneratePreparation, error)
	CommitMessageRegenerate(context.Context, string, string, string) (RuntimeSnapshot, error)
}

type RuntimeMessageRegeneratePreparation struct {
	ProjectID  string `json:"project_id"`
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	MessageID  string `json:"message_id"`
	EditToken  string `json:"edit_token"`
}

func (s *shell) runtimeMessageRegenerateAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeMessageRegenerateBackend)
	if !ok {
		http.Error(w, "Reply regeneration is unavailable", http.StatusServiceUnavailable)
		return
	}
	instance := r.PostForm.Get("instance_id")
	if instance == "" || !runtimeOption(instance) {
		http.Error(w, "Reload the current conversation before regenerating", http.StatusConflict)
		return
	}
	var err error
	switch r.PathValue("action") {
	case "message-regenerate-prepare":
		messageID := r.PostForm.Get("message_id")
		if messageID == "" || !runtimeOption(messageID) {
			err = ErrRuntimeInvalid
			break
		}
		var prepared RuntimeMessageRegeneratePreparation
		prepared, err = backend.PrepareMessageRegenerate(ctx, project.ID, instance, messageID)
		if err == nil {
			s.runtimeJSON(w, prepared)
			return
		}
	case "message-regenerate-commit":
		token := r.PostForm.Get("edit_token")
		// Even an empty browser text field is not part of this capability. The
		// original user belongs to the core's verified source, never a browser draft.
		if token == "" || !runtimeOption(token) || r.PostForm.Get("confirm") != "regenerate" || r.PostForm.Has("text") {
			err = ErrRuntimeInvalid
			break
		}
		var snapshot RuntimeSnapshot
		snapshot, err = backend.CommitMessageRegenerate(ctx, project.ID, instance, token)
		if err == nil {
			s.runtimeJSON(w, snapshot)
			return
		}
	}
	http.Error(w, runtimePublicError(err), http.StatusConflict)
}
