package web

import (
	"context"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type messageRevisionAction string

const (
	messageRevisionEdit       messageRevisionAction = "edit"
	messageRevisionRegenerate messageRevisionAction = "regenerate"
)

// Both history controls resolve a single local row, verify its exact durable
// source over read-only RPC, and install one action-bound preparation. No view
// mutation, text matching or adjacent user inference occurs here.
func (m *RuntimeManager) prepareMessageRevision(ctx context.Context, projectID, instanceID, messageID string, action messageRevisionAction) (protocol.RPCMessageEditPrepared, error) {
	if messageID == "" || !runtimeOption(messageID) {
		return protocol.RPCMessageEditPrepared{}, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return protocol.RPCMessageEditPrepared{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return protocol.RPCMessageEditPrepared{}, err
	}
	r.mu.Lock()
	index := slices.IndexFunc(r.snapshot.Messages, func(message RuntimeMessage) bool { return message.ID == messageID })
	eligible := index >= 0 && (action == messageRevisionEdit && r.snapshot.Messages[index].CanEdit || action == messageRevisionRegenerate && r.snapshot.Messages[index].CanRegenerate)
	if r.goalBlocksHistoryLocked() || !eligible || r.snapshot.CancelRequested {
		r.mu.Unlock()
		return protocol.RPCMessageEditPrepared{}, ErrRuntimeInvalid
	}
	source := r.snapshot.Messages[index]
	sessionID := r.snapshot.SessionID
	r.messageEdit.preparation = nil
	r.mu.Unlock()
	capability := "message_edit"
	if action == messageRevisionRegenerate {
		capability = "message_regenerate"
	}
	if !r.supports(capability) {
		return protocol.RPCMessageEditPrepared{}, ErrRuntimeInvalid
	}
	entryID, turnID := source.SourceID, ""
	if entryID == "" {
		// Input-span IDs are not root turn IDs. Queued replies must carry the exact
		// durable assistant EntryID before they can use either history capability.
		if source.SourceSpanID != "" && source.SourceSpanID != source.SourceTurnID {
			return protocol.RPCMessageEditPrepared{}, ErrRuntimeInvalid
		}
		turnID = source.SourceTurnID
	}
	if entryID == "" && turnID == "" {
		return protocol.RPCMessageEditPrepared{}, ErrRuntimeInvalid
	}
	var prepared protocol.RPCMessageEditPrepared
	var selectedEntryID string
	if action == messageRevisionRegenerate {
		var reply protocol.RPCMessageRegeneratePrepared
		params := protocol.RPCMessageRegeneratePrepareParams{SessionID: sessionID, EntryID: entryID, TurnID: turnID}
		if err := r.call(protocol.RPCRequest{Type: "message_regenerate_prepare"}, params, &reply); err != nil {
			return protocol.RPCMessageEditPrepared{}, err
		}
		prepared, selectedEntryID = reply.RPCMessageEditPrepared, reply.ReplyEntryID
	} else {
		params := protocol.RPCMessageEditPrepareParams{SessionID: sessionID, EntryID: entryID, TurnID: turnID}
		if err := r.call(protocol.RPCRequest{Type: "message_edit_prepare"}, params, &prepared); err != nil {
			return protocol.RPCMessageEditPrepared{}, err
		}
		selectedEntryID = prepared.EntryID
	}
	if prepared.SessionID != sessionID || prepared.EntryID == "" || selectedEntryID == "" || prepared.EditToken == "" || !runtimeOption(prepared.EditToken) || prepared.SourceBranchID == "" || prepared.SourceTipID == "" || !validMessageEditText(prepared.Text) || entryID != "" && selectedEntryID != entryID || turnID != "" && prepared.TurnID != turnID {
		r.fail()
		return protocol.RPCMessageEditPrepared{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.goalBlocksHistoryLocked() || r.ctx.Err() != nil || r.snapshot.Status != "idle" || r.snapshot.CancelRequested {
		return protocol.RPCMessageEditPrepared{}, ErrRuntimeUnavailable
	}
	r.messageEdit.preparation = &prepared
	r.messageEdit.action = action
	return prepared, nil
}
