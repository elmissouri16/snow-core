package web

import (
	"context"
	"net/http"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimePermissionPolicyBackend is optional and distinct from per-request
// permission answers. Its instance binds the current project, worker and session.
// Confirmation authorizes only an explicit idle policy change, never a tool reply.
type RuntimePermissionPolicyBackend interface {
	SetPermissionMode(ctx context.Context, projectID, instanceID, mode string, confirmAllow bool) error
}

func runtimePermissionMode(mode string) bool {
	return mode == "ask" || mode == "deny" || mode == "allow"
}

// permissionPolicyIdleLocked requires verified idle authority, not merely a
// missing busy flag. Retained uncertain recovery evidence confers no authority.
func (r *liveRuntime) permissionPolicyIdleLocked() bool {
	if r.ctx.Err() != nil || r.busy || r.transitioning || r.snapshot.Status != "idle" ||
		r.snapshot.Permission != nil || r.snapshot.Input != nil ||
		!runtimeIdentifier(r.snapshot.SessionID) || !runtimePermissionMode(r.snapshot.PermissionMode) {
		return false
	}
	switch r.snapshot.Recovery.State {
	case RecoveryBound, RecoveryCompleted, RecoveryFailed, RecoveryCanceled, RecoveryRejected:
		return true
	default:
		return false
	}
}

// SetPermissionMode admits exactly one bounded mutation and authoritative read.
// Admitted work uses worker lifetime, so browser disconnect never cancels or
// retries it. An unverified outcome closes admission rather than guessing policy.
func (m *RuntimeManager) SetPermissionMode(ctx context.Context, projectID, instanceID, mode string, confirmAllow bool) error {
	if !runtimePermissionMode(mode) || confirmAllow != (mode == "allow") {
		return ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return err
	}
	r.mu.Lock()
	idle := r.permissionPolicyIdleLocked()
	sessionID := r.snapshot.SessionID
	r.mu.Unlock()
	if !idle {
		return ErrRuntimeBusy
	}
	if err := r.call(protocol.RPCRequest{Type: "permission_mode_set"}, protocol.RPCPermissionMode{Mode: mode}, nil); err != nil {
		// Even rejection may follow a committed change whose response failed.
		r.fail()
		return ErrRuntimeUnavailable
	}
	info, err := r.verifiedInfo(sessionID)
	if err != nil {
		return err
	}
	if info.PermissionMode != mode {
		r.fail()
		return ErrRuntimeUnavailable
	}
	return r.publishPermissionPolicy(info)
}

// Keep terminal lifetime checks and publication under one lock: EOF racing the
// final successful RPC must not revive controls or acknowledge a live policy.
func (r *liveRuntime) publishPermissionPolicy(info protocol.RPCSessionInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.permissionPolicyIdleLocked() || info.SessionID != r.snapshot.SessionID {
		return ErrRuntimeUnavailable
	}
	r.applyInfo(info)
	r.publishLocked()
	return nil
}

// runtimeAction already authenticated the browser, checked same-origin/CSRF and
// resolved the project. This typed endpoint cannot tunnel arbitrary RPC fields.
func (s *shell) runtimePermissionPolicyAction(ctx context.Context, w http.ResponseWriter, r *http.Request, project Project) {
	backend, ok := s.runtimes.(RuntimePermissionPolicyBackend)
	if !ok {
		http.Error(w, "Permission policy controls are unavailable", http.StatusServiceUnavailable)
		return
	}
	instance, mode, confirm := r.PostForm.Get("instance_id"), r.PostForm.Get("mode"), r.PostForm.Get("confirm_allow")
	if !runtimeOption(instance) || instance == "" || !runtimePermissionMode(mode) ||
		(confirm != "" && confirm != "allow") || (confirm == "allow") != (mode == "allow") ||
		len(r.PostForm["instance_id"]) != 1 || len(r.PostForm["mode"]) != 1 || len(r.PostForm["confirm_allow"]) > 1 {
		http.Error(w, runtimePublicError(ErrRuntimeInvalid), http.StatusConflict)
		return
	}
	if err := backend.SetPermissionMode(ctx, project.ID, instance, mode, confirm == "allow"); err != nil {
		http.Error(w, runtimePublicError(err), http.StatusConflict)
		return
	}
	snapshot, ok := s.runtimes.Snapshot(project.ID)
	if !ok || snapshot.InstanceID != instance || snapshot.ProjectID != project.ID || snapshot.Status != "idle" || snapshot.PermissionMode != mode {
		http.Error(w, "The runtime changed; review the current conversation", http.StatusConflict)
		return
	}
	s.runtimeJSON(w, snapshot)
}
