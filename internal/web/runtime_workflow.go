package web

import (
	"context"
	"crypto/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// revalidate runs after taking the nonqueued control gate. A pointer looked up
// before a session rotation or worker replacement confers no authority.
func (m *RuntimeManager) revalidate(r *liveRuntime, projectID, instanceID string) error {
	current, err := m.runtime(projectID, instanceID)
	if err != nil {
		return err
	}
	if current != r {
		return ErrRuntimeInvalid
	}
	return nil
}

func (r *liveRuntime) idle() error {
	r.mu.Lock()
	busy := r.busy || r.transitioning
	r.mu.Unlock()
	if busy {
		return ErrRuntimeBusy
	}
	project := r.project
	project.checkIdentity()
	if !project.Available {
		return ErrProjectInvalid
	}
	return nil
}

func (r *liveRuntime) applyInfo(info protocol.RPCSessionInfo) {
	r.snapshot.Provider = runtimeText(info.Provider, 256)
	r.snapshot.Model = runtimeText(info.Model, 256)
	r.snapshot.SessionName = runtimeText(info.Name, 256)
	r.snapshot.Mode = runtimeMode(info.CollaborationMode)
	r.snapshot.PermissionMode = info.PermissionMode
	r.snapshot.Thinking = string(protocol.NormalizeThinkingLevel(info.Thinking))
}

func (r *liveRuntime) verifiedInfo(sessionID string) (protocol.RPCSessionInfo, error) {
	var info protocol.RPCSessionInfo
	if err := r.call(protocol.RPCRequest{Type: "session_info"}, nil, &info); err != nil {
		// This is an authoritative binding check, including after admitted
		// mutations. A rejected verification is not proof of unchanged state.
		r.fail()
		return info, ErrRuntimeUnavailable
	}
	project := r.project
	project.checkIdentity()
	if !project.Available || info.SessionID != sessionID || info.Path == "" || info.CWD != project.Path || !runtimePermissionMode(info.PermissionMode) || info.Provider == "" || info.Model == "" {
		r.fail()
		return info, ErrRuntimeUnavailable
	}
	return info, nil
}

func (m *RuntimeManager) SetMode(ctx context.Context, projectID, instanceID, mode string) error {
	if mode != "default" && mode != "plan" {
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
	if err := r.call(protocol.RPCRequest{Type: "set_mode", Mode: mode}, nil, nil); err != nil {
		return err
	}
	r.mu.Lock()
	sessionID := r.snapshot.SessionID
	r.mu.Unlock()
	info, err := r.verifiedInfo(sessionID)
	if err != nil {
		return err
	}
	if runtimeMode(info.CollaborationMode) != mode {
		r.fail()
		return ErrRuntimeUnavailable
	}
	r.mu.Lock()
	r.applyInfo(info)
	r.publishLocked()
	r.mu.Unlock()
	return r.refreshTelemetry()
}

func (m *RuntimeManager) Rename(ctx context.Context, projectID, instanceID, name string) error {
	if strings.TrimSpace(name) != name || name == "" || len(name) > 256 || !utf8.ValidString(name) || runtimeText(name, 256) != name {
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
	sessionID := r.snapshot.SessionID
	r.mu.Unlock()
	var result protocol.RPCSessionRenameResult
	if err := r.call(protocol.RPCRequest{Type: "session_rename"}, struct {
		SessionID string `json:"session_id"`
		Name      string `json:"name"`
	}{sessionID, name}, &result); err != nil {
		return err
	}
	if result.SessionID != sessionID {
		r.fail()
		return ErrRuntimeUnavailable
	}
	info, err := r.verifiedInfo(sessionID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.applyInfo(info)
	r.publishLocked()
	r.mu.Unlock()
	return nil
}

// Switch retains the same RPC process and its explicitly chosen provider/model.
// Session restore must not silently substitute another model. The worker's
// session_open verifies immutable ID membership in its pinned current-CWD index.
func (m *RuntimeManager) Switch(ctx context.Context, projectID, instanceID, targetSessionID string, confirmStop bool) (RuntimeSnapshot, error) {
	if targetSessionID != "" && !runtimeIdentifier(targetSessionID) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	project := r.project
	project.checkIdentity()
	if !project.Available {
		return RuntimeSnapshot{}, ErrProjectInvalid
	}
	r.mu.Lock()
	busy := r.busy
	old := r.snapshot.clone()
	r.mu.Unlock()
	if busy && !confirmStop {
		return RuntimeSnapshot{}, ErrRuntimeBusy
	}
	if targetSessionID == old.SessionID {
		return old, nil
	}
	if old.Queue != nil && len(old.Queue.Items) != 0 {
		return RuntimeSnapshot{}, ErrRuntimeQueueReview
	}
	if targetSessionID != "" {
		if err := r.refreshSessions(); err != nil {
			return RuntimeSnapshot{}, err
		}
		found := false
		for _, item := range r.choices.Sessions {
			if item.SessionID == targetSessionID {
				found = true
				break
			}
		}
		if !found {
			return RuntimeSnapshot{}, ErrRuntimeInvalid
		}
	}
	if busy {
		if err := r.call(protocol.RPCRequest{Type: "abort"}, nil, nil); err != nil {
			return RuntimeSnapshot{}, err
		}
		if err := r.waitIdle(); err != nil {
			return RuntimeSnapshot{}, err
		}
	}
	r.mu.Lock()
	// Stop may have moved inputs into review while this control waited. Reject
	// before retiring authority or changing any core binding.
	if r.snapshot.Queue != nil && len(r.snapshot.Queue.Items) != 0 {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeQueueReview
	}
	r.retiredEpoch = max(r.retiredEpoch, r.rootEpoch)
	r.transitioning = true
	r.snapshot.Status = "switching"
	r.publishLocked()
	r.mu.Unlock()
	command := "session_create"
	var params any
	if targetSessionID != "" {
		command = "session_open"
		params = struct {
			SessionID string `json:"session_id"`
		}{targetSessionID}
	}
	var session protocol.RPCSessionSummary
	if err := r.call(protocol.RPCRequest{Type: command}, params, &session); err != nil {
		// An RPC command can commit its binding before a subsequent response
		// projection fails. Neither rejection nor transport failure proves the
		// old session is still bound; never reopen admission on that assumption.
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	// Rotate immediately after commit, even if later verification fails.
	r.mu.Lock()
	r.instanceID = rand.Text()
	r.snapshot.InstanceID = r.instanceID
	r.resetRunControlsForReplacementLocked()
	r.publishLocked()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeClosed
	}
	r.snapshot = RuntimeSnapshot{ProjectID: projectID, InstanceID: r.instanceID, SessionID: session.SessionID, Status: "switching", Mode: "default", Thinking: "off", Telemetry: &RuntimeTelemetry{}, Revision: r.snapshot.Revision}
	r.busy = false
	r.promptID = ""
	r.earlyCompletion = ""
	r.assistant = -1
	r.plan = -1
	r.activityKeys = nil
	r.activityPrompt++
	r.activityCanceled = false
	r.turnID = ""
	r.pendingUserID = ""
	r.pendingRegenerateReplyID = ""
	r.queue = runtimeQueueState{}
	r.snapshot.Queue = nil
	r.assistantHasPlan = false
	r.messageEdit = runtimeMessageEditState{}
	r.goal = runtimeGoalState{}
	r.turnSequence = 0
	r.usageBase = RuntimeTelemetry{}
	r.choices = RuntimeChoices{}
	r.publishLocked()
	r.mu.Unlock()
	if err := r.call(protocol.RPCRequest{Type: "abort"}, nil, nil); err != nil {
		return RuntimeSnapshot{}, err
	}
	if !runtimeIdentifier(session.SessionID) || (targetSessionID != "" && session.SessionID != targetSessionID) || session.SessionID == old.SessionID {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	info, err := r.verifiedInfo(session.SessionID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	if info.Provider != old.Provider || info.Model != old.Model {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	if err := r.loadHistory(); err != nil {
		r.fail()
		return RuntimeSnapshot{}, err
	}
	if err := r.refreshTelemetry(); err != nil {
		return RuntimeSnapshot{}, err
	}
	if err := r.refreshGoal(); err != nil {
		return RuntimeSnapshot{}, err
	}
	if err := r.bindRecovery(session.SessionID); err != nil {
		r.fail()
		return RuntimeSnapshot{}, err
	}
	return r.publishSwitch(info)
}

// publishSwitch is the final transition boundary, after all RPC refreshes.
// EOF and manager shutdown must remain terminal even when they race a successful
// last response. Keep the lifetime check and projection under the same lock.
func (r *liveRuntime) publishSwitch(info protocol.RPCSessionInfo) (RuntimeSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return RuntimeSnapshot{}, ErrRuntimeClosed
	}
	r.applyInfo(info)
	r.transitioning = false
	r.snapshot.Status = "idle"
	r.publishLocked()
	return r.snapshot.clone(), nil
}

func (r *liveRuntime) waitIdle() error {
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		busy := r.busy
		r.mu.Unlock()
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			r.fail()
			return ErrRuntimeUnavailable
		case <-ticker.C:
		}
	}
}

func runtimeMode(mode protocol.CollaborationMode) string {
	if mode == protocol.ModePlan {
		return "plan"
	}
	return "default"
}
