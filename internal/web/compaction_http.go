package web

import (
	"context"
	"net/http"
	"strconv"
)

type RuntimeCompactionBackend interface {
	StartCompaction(context.Context, string, string, RuntimeCompactionInput) (RuntimeSnapshot, error)
}

// Called after browser authentication, CSRF verification and registered-project
// lookup. No prompt, mode, tool, command, summary, or provider override is accepted.
func (s *shell) runtimeCompactionAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimeCompactionBackend)
	if !ok {
		http.Error(w, "Manual compaction is unavailable", http.StatusServiceUnavailable)
		return
	}
	allowed := map[string]bool{"csrf": true, "instance_id": true, "session_id": true, "branch_id": true, "expected_tip_id": true, "expected_revision": true}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "Invalid compaction authority", http.StatusConflict)
			return
		}
	}
	for key := range r.URL.Query() {
		if allowed[key] {
			http.Error(w, "Invalid compaction authority", http.StatusConflict)
			return
		}
	}
	for _, key := range []string{"instance_id", "session_id", "branch_id", "expected_tip_id", "expected_revision"} {
		if len(r.PostForm[key]) != 1 || !runtimeOption(r.PostForm.Get(key)) {
			http.Error(w, "Reload the current conversation", http.StatusConflict)
			return
		}
	}
	instance, session, branch := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id"), r.PostForm.Get("branch_id")
	raw := r.PostForm.Get("expected_revision")
	revision, err := strconv.ParseUint(raw, 10, 64)
	if instance == "" || session == "" || branch == "" || err != nil || revision == 0 || strconv.FormatUint(revision, 10) != raw {
		http.Error(w, "Reload the reviewed conversation", http.StatusConflict)
		return
	}
	snapshot, err := backend.StartCompaction(ctx, project.ID, instance, RuntimeCompactionInput{SessionID: session, BranchID: branch, ExpectedTipID: r.PostForm.Get("expected_tip_id"), ExpectedRevision: revision})
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, snapshot)
}

var _ RuntimeCompactionBackend = (*RuntimeManager)(nil)
