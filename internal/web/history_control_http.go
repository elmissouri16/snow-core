package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeHistoryControlBackend is optional and accepts only explicit, bound
// history operations. It grants neither a generic RPC tunnel nor file undo.
type RuntimeHistoryControlBackend interface {
	HistoryBranchFork(context.Context, string, string, RuntimeHistoryControlRequest) (RuntimeSnapshot, error)
	HistorySessionFork(context.Context, string, string, RuntimeHistoryControlRequest) (RuntimeHistoryControlResult, error)
	HistoryBranchRename(context.Context, string, string, RuntimeHistoryControlRequest) (RuntimeHistoryControlResult, error)
}

type RuntimeHistoryControlRequest struct {
	protocol.RPCHistoryControlBinding
	ExpectedRevision uint64 `json:"expected_revision"`
	Name             string `json:"name"`
	OldName          string `json:"old_name"`
}

// Metadata-only operations retain the current instance. ChildSessionID is an
// inventory hint, never replacement authority or an instruction to open it.
type RuntimeHistoryControlResult struct {
	ProjectID      string `json:"project_id"`
	InstanceID     string `json:"instance_id"`
	SessionID      string `json:"session_id"`
	Revision       uint64 `json:"revision"`
	BranchID       string `json:"branch_id"`
	TipID          string `json:"tip_id"`
	Name           string `json:"name"`
	ChildSessionID string `json:"child_session_id,omitempty"`
}

func historyControlAction(action string) bool {
	return action == "history-branch-fork" || action == "history-session-fork" || action == "history-branch-rename"
}

func (s *shell) runtimeHistoryControlAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	if r.Method != http.MethodPost {
		http.Error(w, "History controls require POST", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorizeFormLimit(w, r, 16<<10); !ok {
		return
	}
	backend, ok := s.runtimes.(RuntimeHistoryControlBackend)
	if !ok {
		http.Error(w, "History controls are unavailable", http.StatusServiceUnavailable)
		return
	}
	action := r.PathValue("action")
	if !historyControlAction(action) || len(r.URL.Query()) != 0 {
		http.Error(w, "Invalid history control", http.StatusBadRequest)
		return
	}
	required := []string{"csrf", "instance_id", "session_id", "expected_revision", "current_branch_id", "current_tip_id", "branch_id", "tip_id", "name"}
	if action == "history-branch-rename" {
		required = append(required, "old_name")
	}
	allowed := make(map[string]bool, len(required))
	for _, key := range required {
		if len(r.PostForm[key]) != 1 {
			http.Error(w, "Reload the selected version", http.StatusConflict)
			return
		}
		allowed[key] = true
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "Invalid history control fields", http.StatusBadRequest)
			return
		}
	}
	revision, err := strconv.ParseUint(r.PostForm.Get("expected_revision"), 10, 64)
	instance := r.PostForm.Get("instance_id")
	p := RuntimeHistoryControlRequest{SessionID: r.PostForm.Get("session_id"), SourceBranchID: r.PostForm.Get("current_branch_id"), SourceTipID: r.PostForm.Get("current_tip_id"), TargetBranchID: r.PostForm.Get("branch_id"), TargetTipID: r.PostForm.Get("tip_id"), ExpectedRevision: revision, Name: r.PostForm.Get("name"), OldName: r.PostForm.Get("old_name")}
	if err != nil || instance == "" || !runtimeOption(instance) || !validHistoryControlRequest(p, action) {
		http.Error(w, "Reload the selected version and check its name", http.StatusConflict)
		return
	}
	var result any
	switch action {
	case "history-branch-fork":
		result, err = backend.HistoryBranchFork(ctx, project.ID, instance, p)
	case "history-session-fork":
		result, err = backend.HistorySessionFork(ctx, project.ID, instance, p)
	case "history-branch-rename":
		result, err = backend.HistoryBranchRename(ctx, project.ID, instance, p)
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, result)
}
