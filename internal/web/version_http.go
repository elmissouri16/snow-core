package web

import (
	"context"
	"net/http"
	"time"
)

// RuntimeVersionsBackend is an explicit same-session history capability. List
// and preview are bounded reads; only a prepared restore may change selection.
// Browser input never names an RPC command or supplies execution/tool flags.
type RuntimeVersionsBackend interface {
	ListVersions(context.Context, string, string, string, string) (RuntimeVersionsPage, error)
	PreviewVersion(context.Context, string, string, string, string, string, string) (RuntimeVersionPreview, error)
	PrepareVersionRestore(context.Context, string, string, string, string, string, string, string) (RuntimeVersionRestorePreparation, error)
	CommitVersionRestore(context.Context, string, string, string, string) (RuntimeSnapshot, error)
}

type RuntimeVersion struct {
	BranchID string `json:"branch_id"`
	TipID    string `json:"tip_id"`
	Name     string `json:"name"`
	Current  bool   `json:"current"`
}

type RuntimeVersionsPage struct {
	ProjectID       string           `json:"project_id"`
	InstanceID      string           `json:"instance_id"`
	SessionID       string           `json:"session_id"`
	Revision        uint64           `json:"revision"`
	CurrentBranchID string           `json:"current_branch_id"`
	CurrentTipID    string           `json:"current_tip_id"`
	Versions        []RuntimeVersion `json:"versions"`
	NextCursor      string           `json:"next_cursor"`
	HasMore         bool             `json:"has_more"`
}

type RuntimeVersionPreview struct {
	ProjectID             string           `json:"project_id"`
	InstanceID            string           `json:"instance_id"`
	SessionID             string           `json:"session_id"`
	Revision              uint64           `json:"revision"`
	BranchID              string           `json:"branch_id"`
	TipID                 string           `json:"tip_id"`
	Messages              []RuntimeMessage `json:"messages"`
	NextCursor            string           `json:"next_cursor"`
	HasMore               bool             `json:"has_more"`
	HistoryTruncated      bool             `json:"history_truncated"`
	HistoryToolsTruncated bool             `json:"history_tools_truncated"`
}

type RuntimeVersionRestorePreparation struct {
	ProjectID       string    `json:"project_id"`
	InstanceID      string    `json:"instance_id"`
	SessionID       string    `json:"session_id"`
	CurrentBranchID string    `json:"current_branch_id"`
	CurrentTipID    string    `json:"current_tip_id"`
	BranchID        string    `json:"branch_id"`
	TipID           string    `json:"tip_id"`
	RestoreToken    string    `json:"restore_token"`
	ExpiresAt       time.Time `json:"expires_at"`
}

func versionAction(action string) bool {
	return action == "versions-list" || action == "version-preview" || action == "version-restore-prepare" || action == "version-restore-commit"
}

// runtimeVersionAction is safe as a handler helper even when the caller has not
// parsed/authorized the form. The production runtimeAction also validates the
// project through the pinned registry before dispatching here.
func (s *shell) runtimeVersionAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	if r.Method != http.MethodPost {
		http.Error(w, "Version actions require POST", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorizeFormLimit(w, r, 16<<10); !ok {
		return
	}
	backend, ok := s.runtimes.(RuntimeVersionsBackend)
	if !ok {
		http.Error(w, "Conversation versions are unavailable", http.StatusServiceUnavailable)
		return
	}
	action := r.PathValue("action")
	required := []string{"csrf", "instance_id", "session_id"}
	optional := []string{}
	switch action {
	case "versions-list":
		optional = append(optional, "cursor")
	case "version-preview":
		required = append(required, "branch_id", "tip_id")
		optional = append(optional, "cursor")
	case "version-restore-prepare":
		required = append(required, "current_branch_id", "current_tip_id", "branch_id", "tip_id")
	case "version-restore-commit":
		required = append(required, "restore_token")
	default:
		http.Error(w, "Unknown version action", http.StatusBadRequest)
		return
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		if len(r.PostForm[key]) != 1 {
			http.Error(w, "Reload the selected conversation version", http.StatusConflict)
			return
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "Invalid version action fields", http.StatusBadRequest)
			return
		}
	}
	// Neither a query parameter nor an alternate source may override the form.
	if len(r.URL.Query()) != 0 {
		http.Error(w, "Version authority must be in the form", http.StatusBadRequest)
		return
	}
	instance, session := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id")
	if instance == "" || !runtimeOption(instance) || !runtimeIdentifier(session) {
		http.Error(w, "Reload the current conversation", http.StatusConflict)
		return
	}
	for _, key := range []string{"branch_id", "current_branch_id"} {
		if r.PostForm.Has(key) && !validVersionIdentity(r.PostForm.Get(key), false) {
			http.Error(w, "Invalid version identity", http.StatusConflict)
			return
		}
	}
	for _, key := range []string{"tip_id", "current_tip_id"} {
		if value := r.PostForm.Get(key); value != "" && !validVersionIdentity(value, true) {
			http.Error(w, "Invalid version tip", http.StatusConflict)
			return
		}
	}
	if r.PostForm.Has("restore_token") && (r.PostForm.Get("restore_token") == "" || !runtimeOption(r.PostForm.Get("restore_token"))) {
		http.Error(w, "Invalid version restore capability", http.StatusConflict)
		return
	}
	if cursor := r.PostForm.Get("cursor"); !validVersionCursor(cursor) {
		http.Error(w, "Invalid version cursor", http.StatusConflict)
		return
	}
	var result any
	var err error
	switch action {
	case "versions-list":
		result, err = backend.ListVersions(ctx, project.ID, instance, session, r.PostForm.Get("cursor"))
	case "version-preview":
		result, err = backend.PreviewVersion(ctx, project.ID, instance, session, r.PostForm.Get("branch_id"), r.PostForm.Get("tip_id"), r.PostForm.Get("cursor"))
	case "version-restore-prepare":
		result, err = backend.PrepareVersionRestore(ctx, project.ID, instance, session, r.PostForm.Get("current_branch_id"), r.PostForm.Get("current_tip_id"), r.PostForm.Get("branch_id"), r.PostForm.Get("tip_id"))
	case "version-restore-commit":
		result, err = backend.CommitVersionRestore(ctx, project.ID, instance, session, r.PostForm.Get("restore_token"))
	}
	if err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	s.runtimeJSON(w, result)
}
