package web

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var ErrSessionDeleteUncertain = errors.New("Deletion could not be confirmed. Reload this workspace before taking further action.")

// CatalogSessionDeleteBackend is an explicit runtime-free mutation capability;
// reads do not imply deletion authority and never activate this operation.
type CatalogSessionDeleteBackend interface {
	DeleteSession(context.Context, Project, string) error
}

type RuntimeSessionDeleteBackend interface {
	SessionDeleteSupported(string, string) bool
	DeleteSession(context.Context, string, string, string) error
}

type SessionDeleteResult struct {
	ProjectID  string `json:"project_id"`
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	Deleted    bool   `json:"deleted"`
}

func (s *shell) deleteSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	if _, ok := s.authorizeFormLimit(w, r, 4096); !ok {
		return
	}
	projectID, sessionID := r.PathValue("project"), r.PathValue("session")
	instance := r.PostForm.Get("instance_id")
	valid := r.URL.RawQuery == "" && validProjectID(projectID) && runtimeIdentifier(sessionID) && (instance == "" || runtimeIdentifier(instance)) && r.PostForm.Get("confirm") == "delete"
	for _, key := range []string{"csrf", "confirm", "instance_id"} {
		if len(r.PostForm[key]) != 1 {
			valid = false
		}
	}
	for key, values := range r.PostForm {
		if (key != "csrf" && key != "confirm" && key != "instance_id") || len(values) != 1 {
			valid = false
		}
	}
	if !valid {
		http.Error(w, "Invalid session deletion request", http.StatusBadRequest)
		return
	}
	if s.registry == nil {
		http.NotFound(w, r)
		return
	}
	if !s.projectControl.TryLock() {
		http.Error(w, "Workspace controls are busy. No deletion was started.", http.StatusConflict)
		return
	}
	defer s.projectControl.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	release, err := s.sidebarReads.preempt(ctx, projectID)
	if err != nil {
		http.Error(w, "Workspace controls are busy. No deletion was started.", http.StatusConflict)
		return
	}
	defer release()
	project, err := s.registry.Lookup(ctx, projectID)
	if err != nil || !project.Available {
		http.Error(w, "Workspace unavailable. No deletion was started.", http.StatusConflict)
		return
	}
	var snapshot RuntimeSnapshot
	live := false
	if s.runtimes != nil {
		snapshot, live = s.runtimes.Snapshot(projectID)
	}
	if live && (snapshot.ProjectID != projectID || snapshot.InstanceID != instance) || !live && instance != "" {
		http.Error(w, "Session owner changed. Reload this workspace; no deletion was started.", http.StatusConflict)
		return
	}
	if live {
		backend, ok := s.runtimes.(RuntimeSessionDeleteBackend)
		if !ok || !backend.SessionDeleteSupported(projectID, instance) {
			http.Error(w, "Session deletion is unavailable", http.StatusConflict)
			return
		}
		if snapshot.SessionID == sessionID {
			http.Error(w, "The active session cannot be deleted. Switch sessions or close this workspace first.", http.StatusConflict)
			return
		}
		err = backend.DeleteSession(ctx, projectID, instance, sessionID)
	} else {
		backend, ok := s.catalog.(CatalogSessionDeleteBackend)
		if !ok {
			http.Error(w, "Session deletion is unavailable", http.StatusConflict)
			return
		}
		// The child is joined before releasing ownership admission, even after the
		// browser disconnects. Activation cannot overlap this admitted operation.
		mutationCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		err = backend.DeleteSession(mutationCtx, project, sessionID)
		stop()
	}
	if err != nil {
		if errors.Is(err, ErrRuntimeBusy) || errors.Is(err, ErrRuntimeInvalid) || errors.Is(err, ErrProjectInvalid) {
			http.Error(w, "Session deletion was not admitted. Reload this workspace and check its current owner.", http.StatusConflict)
		} else {
			http.Error(w, ErrSessionDeleteUncertain.Error(), http.StatusServiceUnavailable)
		}
		return
	}
	// A successful worker response is authoritative only for the original owner
	// and folder identity. Never acknowledge a replacement as the deleted owner.
	verifyCtx, verifyCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer verifyCancel()
	current, err := s.registry.Lookup(verifyCtx, projectID)
	if err != nil || !current.Available || current.Path != project.Path || current.device != project.device || current.inode != project.inode {
		http.Error(w, ErrSessionDeleteUncertain.Error(), http.StatusServiceUnavailable)
		return
	}
	if s.runtimes != nil {
		after, owned := s.runtimes.Snapshot(projectID)
		if owned != live || owned && (after.ProjectID != projectID || after.InstanceID != instance) {
			http.Error(w, ErrSessionDeleteUncertain.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	s.runtimeJSON(w, SessionDeleteResult{ProjectID: projectID, SessionID: sessionID, InstanceID: instance, Deleted: true})
}
