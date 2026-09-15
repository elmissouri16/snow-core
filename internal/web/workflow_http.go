package web

import (
	"context"
	"net/http"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeWorkflowBackend adds a small explicit control surface to the existing
// worker. Browser input never names arbitrary RPC commands or execution flags.
// All calls bind to the current activation/session identity, including reads
// which can perform provider discovery or refresh session metadata.
type RuntimeWorkflowBackend interface {
	Choices(context.Context, string, string) (RuntimeChoices, error)
	SetModel(context.Context, string, string, string, string) error
	SetMode(context.Context, string, string, string) error
	Rename(context.Context, string, string, string) error
	Switch(context.Context, string, string, string, bool) (RuntimeSnapshot, error)
}

func workflowAction(action string) bool {
	switch action {
	case "choices", "model", "mode", "rename", "switch":
		return true
	default:
		return false
	}
}

// The caller has already authenticated the browser, checked CSRF and resolved
// the registered project. This remains a typed extension, not an RPC tunnel.
func (s *shell) runtimeWorkflowAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeWorkflowBackend)
	if !ok {
		http.Error(w, "Conversation controls are unavailable", http.StatusServiceUnavailable)
		return
	}
	instance := r.PostForm.Get("instance_id")
	if !runtimeOption(instance) || instance == "" {
		http.Error(w, "Reload the current conversation before using its controls", http.StatusConflict)
		return
	}
	var err error
	switch r.PathValue("action") {
	case "choices":
		var choices RuntimeChoices
		choices, err = backend.Choices(ctx, project.ID, instance)
		if err == nil {
			s.runtimeJSON(w, choices)
			return
		}
	case "model":
		provider, model := r.PostForm.Get("provider"), r.PostForm.Get("model")
		if !runtimeOption(provider) || provider == "" || !runtimeOption(model) || model == "" {
			err = ErrRuntimeInvalid
			break
		}
		err = backend.SetModel(ctx, project.ID, instance, provider, model)
	case "mode":
		mode := r.PostForm.Get("mode")
		if mode != string(protocol.ModeDefault) && mode != string(protocol.ModePlan) {
			err = ErrRuntimeInvalid
			break
		}
		err = backend.SetMode(ctx, project.ID, instance, mode)
	case "rename":
		err = backend.Rename(ctx, project.ID, instance, r.PostForm.Get("name"))
	case "switch":
		confirm := r.PostForm.Get("confirm_stop")
		if confirm != "" && confirm != "stop" {
			err = ErrRuntimeInvalid
			break
		}
		releaseReads, stopErr := s.sidebarReads.preempt(ctx, project.ID)
		if stopErr != nil {
			err = stopErr
			break
		}
		defer releaseReads()
		var snapshot RuntimeSnapshot
		snapshot, err = backend.Switch(ctx, project.ID, instance, r.PostForm.Get("session_id"), confirm == "stop")
		if err == nil {
			s.runtimeJSON(w, snapshot)
			return
		}
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	snapshot, ok := s.runtimes.Snapshot(project.ID)
	if !ok {
		http.Error(w, "The runtime closed; review the project before continuing", http.StatusConflict)
		return
	}
	// Do not acknowledge a model/mode/rename mutation against a replacement
	// session if another control moved it before this display read.
	if snapshot.InstanceID != instance {
		http.Error(w, "The session changed; review the current conversation", http.StatusConflict)
		return
	}
	s.runtimeJSON(w, snapshot)
}
