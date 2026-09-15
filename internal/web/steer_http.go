package web

import (
	"context"
	"net/http"
	"strconv"
)

// RuntimeSteerBackend exposes only explicit literal steering of the reviewed
// current root. It is not a scheduler or a synonym for Queue next.
type RuntimeSteerBackend interface {
	SteerCurrentRun(context.Context, string, string, string, string, uint64, string, string) (RuntimeSnapshot, error)
}

func (s *shell) runtimeSteerAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeSteerBackend)
	if !ok {
		http.Error(w, "Steer current run is unavailable", http.StatusServiceUnavailable)
		return
	}
	for _, key := range []string{"instance_id", "session_id", "live_steer_token", "steer_revision", "request_id", "text"} {
		if len(r.PostForm[key]) != 1 {
			http.Error(w, "Review the current run before steering it", http.StatusConflict)
			return
		}
	}
	instance, session, token, request := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id"), r.PostForm.Get("live_steer_token"), r.PostForm.Get("request_id")
	raw := r.PostForm.Get("steer_revision")
	revision, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || revision == 0 || strconv.FormatUint(revision, 10) != raw || instance == "" || !runtimeOption(instance) || session == "" || !runtimeIdentifier(session) || token == "" || !runtimeOption(token) || !validSteerRequestID(request) || !validMessageEditText(r.PostForm.Get("text")) {
		http.Error(w, "Review the current run and enter nonblank text of at most 64 KiB", http.StatusConflict)
		return
	}
	snapshot, err := backend.SteerCurrentRun(ctx, project.ID, instance, session, token, revision, request, r.PostForm.Get("text"))
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, snapshot)
}
