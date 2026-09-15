package web

import (
	"context"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *RuntimeManager) SessionDeleteSupported(projectID, instanceID string) bool {
	r, err := m.runtime(projectID, instanceID)
	if err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.worker != nil && r.snapshot.Status != "opening" && r.snapshot.Status != "closing" && r.snapshot.Status != "failed" && r.supports("session_management")
}

func (m *RuntimeManager) DeleteSession(ctx context.Context, projectID, instanceID, sessionID string) error {
	if !runtimeIdentifier(sessionID) {
		return ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	if !r.supports("session_management") {
		return ErrRuntimeInvalid
	}
	if err = r.idle(); err != nil {
		return err
	}
	r.mu.Lock()
	active := r.snapshot.SessionID
	blocked := r.snapshot.CancelRequested || r.goal.pending || r.goal.active || r.snapshot.Permission != nil || r.snapshot.Input != nil
	r.mu.Unlock()
	if blocked {
		return ErrRuntimeBusy
	}
	if sessionID == active {
		return ErrRuntimeInvalid
	}
	// Revalidate authoritative membership under the same nonqueued control gate.
	// Neither this read nor deletion performs Choices/model discovery/activation.
	var list protocol.RPCSessionList
	if err = r.call(protocol.RPCRequest{Type: "sessions_list"}, nil, &list); err != nil {
		return ErrRuntimeInvalid
	}
	found := false
	for _, item := range list.Sessions {
		if item.SessionID == sessionID {
			if item.Active {
				return ErrRuntimeInvalid
			}
			found = true
		}
	}
	if !found {
		return ErrRuntimeInvalid
	}
	if err = m.revalidate(r, projectID, instanceID); err != nil {
		return err
	}
	if err = r.idle(); err != nil {
		return err
	}
	var result protocol.RPCSessionDeleteResult
	if err = r.call(protocol.RPCRequest{Type: "session_delete"}, protocol.RPCSessionDeleteParams{SessionID: sessionID}, &result); err != nil {
		return ErrSessionDeleteUncertain
	}
	if !result.Deleted || result.SessionID != sessionID {
		r.fail()
		return ErrSessionDeleteUncertain
	}
	// Invalidate only this known item. No post-mutation discovery/read or retry.
	r.choices.Sessions = slices.DeleteFunc(r.choices.Sessions, func(s RuntimeSessionChoice) bool { return s.SessionID == sessionID })
	return nil
}
