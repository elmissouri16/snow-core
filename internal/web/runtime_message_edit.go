package web

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"strings"
	"time"
	"unicode/utf8"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type runtimeMessageEditState struct {
	preparation *protocol.RPCMessageEditPrepared
	action      messageRevisionAction
	pending     bool
	events      []clientrpc.Event
	bytes       int
	// An edit has authoritative turn identity before any event. Never allow
	// acceptEvent's monotonic discovery to substitute another turn for it.
	committedTurn string
}

func validMessageEditText(text string) bool {
	return len(text) <= protocol.RPCMessageEditMaxTextBytes && strings.TrimSpace(text) != "" && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

// Only the specifically admitted optimistic user owns the accepted root turn.
// Identical text, positions, adjacent assistants and display IDs confer no
// durable identity. The read-only RPC subsequently resolves this exact turn.
func (r *liveRuntime) bindPromptUserLocked(e protocol.AgentEvent) {
	if r.pendingUserID == "" || e.TurnID == "" || !runtimeOption(e.TurnID) || e.TurnID != r.turnID || e.RootEpoch == 0 || e.TurnSequence == 0 {
		return
	}
	for i := range r.snapshot.Messages {
		message := &r.snapshot.Messages[i]
		if message.ID == r.pendingUserID && message.Role == "user" && message.SourceID == "" && message.SourceTurnID == "" {
			if len(message.Images) > 0 {
				// Agent emits initial run stats after the durable turn marker but
				// before appending the user. Its subsequent session update follows
				// that append. Earlier updates (e.g. cleared skills) cannot grant
				// image authority, and an early GET must not race persistence.
				if e.Type == protocol.EvRunStatsUpdated {
					message.imageTurnMarker = true
					return
				}
				if !message.imageTurnMarker || e.Type != protocol.EvSessionUpdated {
					return
				}
			}
			message.SourceTurnID = e.TurnID
			message.CanEdit = !message.Truncated && len(message.Images) == 0
			r.pendingUserID = ""
			r.publishLocked()
			return
		}
	}
}

func (m *RuntimeManager) PrepareMessageEdit(ctx context.Context, projectID, instanceID, messageID string) (RuntimeMessageEditPreparation, error) {
	prepared, err := m.prepareMessageRevision(ctx, projectID, instanceID, messageID, messageRevisionEdit)
	if err != nil {
		return RuntimeMessageEditPreparation{}, err
	}
	return RuntimeMessageEditPreparation{ProjectID: projectID, SessionID: prepared.SessionID, InstanceID: instanceID, MessageID: messageID, EditToken: prepared.EditToken, Text: prepared.Text}, nil
}

func (m *RuntimeManager) CommitMessageEdit(ctx context.Context, projectID, instanceID, editToken, text string) (RuntimeSnapshot, error) {
	if editToken == "" || !runtimeOption(editToken) || !validMessageEditText(text) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	return m.commitMessageRevision(ctx, projectID, instanceID, editToken, text, messageRevisionEdit)
}

// commitMessageRevision owns the shared nonqueued gate, action-bound token,
// RPC admission, bounded event replay and same-session projection transaction.
func (m *RuntimeManager) commitMessageRevision(ctx context.Context, projectID, instanceID, editToken, text string, action messageRevisionAction) (RuntimeSnapshot, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeSnapshot{}, err
	}
	r.mu.Lock()
	prepared := r.messageEdit.preparation
	if r.goalBlocksHistoryLocked() || prepared == nil || r.messageEdit.action != action || prepared.EditToken != editToken || prepared.SessionID != r.snapshot.SessionID || r.snapshot.CancelRequested {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	r.messageEdit.preparation = nil // Single-use even if dispatch fails.
	r.mu.Unlock()
	if err := r.savePromptIntent(); err != nil {
		return RuntimeSnapshot{}, err
	}
	r.mu.Lock()
	if r.ctx.Err() != nil || r.snapshot.Status != "idle" {
		r.mu.Unlock()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	r.messageEdit.pending = true
	r.snapshot.Status = "switching"
	r.publishLocked()
	r.mu.Unlock()
	var commitParams any = protocol.RPCMessageEditCommitParams{SessionID: prepared.SessionID, EditToken: editToken, Text: text}
	command := "message_edit_commit"
	if action == messageRevisionRegenerate {
		command = "message_regenerate_commit"
		commitParams = protocol.RPCMessageRegenerateCommitParams{SessionID: prepared.SessionID, EditToken: editToken}
		text = prepared.Text // Verified server source, never a browser draft.
	}
	params, _ := json.Marshal(commitParams)
	callCtx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	response, err := r.worker.Client.Call(callCtx, protocol.RPCRequest{Type: command, Params: params})
	if err != nil {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	var committed protocol.RPCMessageEditCommitted
	data, decodeErr := json.Marshal(response.Data)
	if !response.Success {
		if response.ErrorCode != protocol.RPCMessageEditRejectedErrorCode {
			r.fail()
			return RuntimeSnapshot{}, ErrRuntimeUnavailable
		}
		// A typed rejection proves no commit; transport/decoding ambiguity does not.
		r.eventMu.Lock()
		r.mu.Lock()
		r.messageEdit.pending = false
		r.messageEdit.events = nil
		r.messageEdit.bytes = 0
		if r.ctx.Err() == nil && r.snapshot.Status == "switching" {
			r.snapshot.Status = "idle"
			r.snapshot.Recovery.State = RecoveryRejected
			r.publishLocked()
		}
		r.mu.Unlock()
		r.eventMu.Unlock()
		r.persistRecovery()
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	if decodeErr != nil || json.Unmarshal(data, &committed) != nil || !validMessageEditCommit(committed, *prepared, text) {
		r.fail()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return r.publishMessageEdit(response.ID, committed)
}

func validMessageEditCommit(c protocol.RPCMessageEditCommitted, p protocol.RPCMessageEditPrepared, text string) bool {
	if c.SessionID != p.SessionID || c.SourceBranchID != p.SourceBranchID || c.SourceTipID != p.SourceTipID || c.EntryID != p.EntryID || c.BranchID == "" || c.BranchID == c.SourceBranchID || c.TurnID == "" || c.UserEntryID == "" || len(c.History.Messages) == 0 {
		return false
	}
	last := c.History.Messages[len(c.History.Messages)-1]
	return last.ID == c.UserEntryID && last.Role == protocol.RoleUser && len(last.Content) == 1 && last.Content[0].Type == protocol.BlockText && last.Content[0].Text == text
}

func (r *liveRuntime) publishMessageEdit(requestID string, committed protocol.RPCMessageEditCommitted) (RuntimeSnapshot, error) {
	// Keep wire-order events behind projection + replay as one transaction.
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.mu.Lock()
	r.instanceID = rand.Text()
	r.snapshot.InstanceID = r.instanceID
	r.resetRunControlsForReplacementLocked()
	r.publishLocked() // Retire previous instance subscriptions even on late EOF.
	terminal := r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing"
	events := r.messageEdit.events
	r.messageEdit = runtimeMessageEditState{committedTurn: committed.TurnID}
	r.goal = runtimeGoalState{}
	r.snapshot.Goal = nil
	r.snapshot.Messages = nil
	r.snapshot.Activities = nil
	r.snapshot.ActivitiesTruncated = false
	r.clearPermissionLocked()
	r.snapshot.Input = nil
	if !terminal {
		r.snapshot.Error = ""
	}
	r.activityKeys = nil
	r.activityPrompt++
	r.activityCanceled = false
	r.assistant, r.plan = -1, -1
	r.pendingUserID = ""
	r.pendingRegenerateReplyID = ""
	r.queue = runtimeQueueState{}
	r.snapshot.Queue = nil
	r.assistantHasPlan = false
	r.turnID = committed.TurnID
	// Root epoch and sequence are learned only from this exact committed turn.
	r.turnSequence = 0
	r.promptID, r.earlyCompletion = requestID, ""
	r.busy = true
	if !terminal {
		r.snapshot.CancelToken = rand.Text()
		r.snapshot.CancelRequested = false
		r.snapshot.Status = "running"
	}
	r.snapshot.Recovery.State = RecoveryAdmitted
	r.snapshot.Recovery.UpdatedAt = time.Now().UTC()
	r.snapshot.Telemetry = &RuntimeTelemetry{}
	r.usageBase = RuntimeTelemetry{}
	r.projectHistory(committed.History)
	r.publishLocked()
	r.mu.Unlock()
	if terminal {
		r.persistRecovery()
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	for _, event := range events {
		r.consumeEvent(event)
	}
	r.persistRecovery()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.snapshot.Status == "failed" || r.snapshot.Status == "closing" {
		return RuntimeSnapshot{}, ErrRuntimeUnavailable
	}
	return r.snapshot.clone(), nil
}

var _ RuntimeMessageEditBackend = (*RuntimeManager)(nil)
