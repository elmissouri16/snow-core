package web

import (
	"context"
	"net/http"
	"strconv"
)

// RuntimeQueueNextBackend is an explicit live-only follow-up capability. Normal
// Prompt remains a nonqueued admission; reads and reconnects never replay work.
type RuntimeQueueNextBackend interface {
	EnqueueFollowUp(context.Context, string, string, string, string, uint64, string) (RuntimeSnapshot, error)
	UpdateQueuedFollowUp(context.Context, string, string, string, string, uint64, string, string) (RuntimeSnapshot, error)
	RemoveQueuedFollowUp(context.Context, string, string, string, string, uint64, string) (RuntimeSnapshot, error)
}

// RuntimeQueue is a bounded reviewable projection, not a manager scheduler.
// Held/uncertain text never becomes pending again without a separate explicit
// submission. Token is a queue-only capability; it is never a CancelToken.
type RuntimeQueue struct {
	Token      string             `json:"token"`
	Revision   uint64             `json:"revision"`
	CanEnqueue bool               `json:"can_enqueue"`
	Items      []RuntimeQueueItem `json:"items"`
}

type RuntimeQueueItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state"` // pending, starting (locked), held, uncertain
}

func (s *shell) runtimeQueueAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeQueueNextBackend)
	if !ok {
		http.Error(w, "Queue next is unavailable", http.StatusServiceUnavailable)
		return
	}
	// Every authority field has exactly one source. Query values and duplicate
	// fields cannot choose a different session/token/revision than the UI checked.
	for _, key := range []string{"instance_id", "session_id", "queue_token", "queue_revision"} {
		if len(r.PostForm[key]) != 1 {
			http.Error(w, "Reload the current pending queue before changing it", http.StatusConflict)
			return
		}
	}
	instance, session, token := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id"), r.PostForm.Get("queue_token")
	rawRevision := r.PostForm.Get("queue_revision")
	revision, err := strconv.ParseUint(rawRevision, 10, 64)
	if err != nil || strconv.FormatUint(revision, 10) != rawRevision || instance == "" || !runtimeOption(instance) || session == "" || !runtimeIdentifier(session) || token == "" || !runtimeOption(token) {
		http.Error(w, "Reload the current pending queue before changing it", http.StatusConflict)
		return
	}
	var snapshot RuntimeSnapshot
	switch r.PathValue("action") {
	case "queue-enqueue":
		if len(r.PostForm["text"]) != 1 || !validMessageEditText(r.PostForm.Get("text")) {
			err = ErrRuntimeInvalid
			break
		}
		snapshot, err = backend.EnqueueFollowUp(ctx, project.ID, instance, session, token, revision, r.PostForm.Get("text"))
	case "queue-update", "queue-remove":
		id := r.PostForm.Get("item_id")
		if len(r.PostForm["item_id"]) != 1 || id == "" || !runtimeOption(id) {
			err = ErrRuntimeInvalid
			break
		}
		if r.PathValue("action") == "queue-update" {
			if len(r.PostForm["text"]) != 1 || !validMessageEditText(r.PostForm.Get("text")) {
				err = ErrRuntimeInvalid
				break
			}
			snapshot, err = backend.UpdateQueuedFollowUp(ctx, project.ID, instance, session, token, revision, id, r.PostForm.Get("text"))
		} else {
			if r.PostForm.Has("text") {
				err = ErrRuntimeInvalid
				break
			}
			snapshot, err = backend.RemoveQueuedFollowUp(ctx, project.ID, instance, session, token, revision, id)
		}
	default:
		err = ErrRuntimeInvalid
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, snapshot)
}
