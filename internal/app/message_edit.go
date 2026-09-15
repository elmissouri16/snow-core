package app

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ErrMessageEditOutcomeUnknown means input was persisted or rollback failed.
// Surfaces must invalidate authority and refresh, never present a no-change
// rejection or automatically retry this token.
var ErrMessageEditOutcomeUnknown = errors.New("message edit outcome requires authoritative refresh")

const messageEditTTL = 2 * time.Minute
const maxMessageEditTokens = 64

type messageEditAction string

const (
	messageActionEdit       messageEditAction = "edit"
	messageActionRegenerate messageEditAction = "regenerate"
)

type messageEditAuthorization struct {
	action       messageEditAction
	replyEntryID string
	source       session.MessageEditSource
	expires      time.Time
}

// PrepareMessageEdit reads an exact active-path user without changing its
// session, branch, goals, permissions or persisted history.
func (a *App) PrepareMessageEdit(ctx context.Context, params protocol.RPCMessageEditPrepareParams) (protocol.RPCMessageEditPrepared, error) {
	var result protocol.RPCMessageEditPrepared
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err := a.messageEditReadyAdmitted(); err != nil {
		return result, err
	}
	source, err := session.ResolveMessageEdit(ctx, a.Session, params)
	if err != nil {
		return result, err
	}
	return a.authorizeMessageEdit(source, messageActionEdit, "")
}

func (a *App) authorizeMessageEdit(source session.MessageEditSource, action messageEditAction, replyEntryID string) (protocol.RPCMessageEditPrepared, error) {
	var result protocol.RPCMessageEditPrepared
	now := time.Now()
	for token, authorization := range a.messageEdits {
		if !now.Before(authorization.expires) {
			delete(a.messageEdits, token)
		}
	}
	if len(a.messageEdits) >= maxMessageEditTokens {
		return result, errors.New("message edit preparation pool is full; wait for expiry")
	}
	if a.messageEdits == nil {
		a.messageEdits = make(map[string]messageEditAuthorization)
	}
	token := rand.Text()
	expires := now.Add(messageEditTTL)
	a.messageEdits[token] = messageEditAuthorization{source: source, expires: expires, action: action, replyEntryID: replyEntryID}
	return protocol.RPCMessageEditPrepared{EditToken: token, SessionID: source.SessionID, SourceBranchID: source.BranchID, SourceTipID: source.TipID, EntryID: source.EntryID, TurnID: source.TurnID, Text: source.Text, ExpiresAt: expires.UnixMilli()}, nil
}

func (a *App) messageEditReadyAdmitted() error {
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: message edit rejects active subagents")
	}
	return a.Agent.MessageEditReadyAdmitted()
}

// CommitMessageEdit atomically validates, forks and admits replacement input in
// this same session. The callback sees durable replacement history before model
// work and must not reenter admission-taking App/Agent methods. The raw history
// argument is internal only: RPC must apply its public allowlist before sending.
// Pre-input failures restore the old branch; provider/cancellation/write failures
// after durable input retain the new branch. A token is never replayable.
func (a *App) CommitMessageEdit(ctx context.Context, params protocol.RPCMessageEditCommitParams, admitted func(protocol.RPCMessageEditCommitted, []protocol.Message) error) (retErr error) {
	if err := session.ValidateMessageEditText(params.Text); err != nil {
		return err
	}
	return a.commitMessageRevision(ctx, params, messageActionEdit, admitted)
}

func (a *App) commitMessageRevision(ctx context.Context, params protocol.RPCMessageEditCommitParams, action messageEditAction, admitted func(protocol.RPCMessageEditCommitted, []protocol.Message) error) (retErr error) {
	if admitted == nil {
		return errors.New("message edit requires an admission callback")
	}
	unlockPlugins, err := a.lockPluginSession()
	if err != nil {
		return err
	}
	unlockAdmission, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		unlockPlugins()
		return err
	}
	var notification *protocol.PluginSessionChanged
	held := true
	release := func() {
		if held {
			held = false
			unlockAdmission()
			unlockPlugins()
			if notification != nil {
				a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvPluginSessionChanged, PluginSessionChanged: notification})
				notification = nil
			}
		}
	}
	defer release()
	authorization, found := a.messageEdits[params.EditToken]
	delete(a.messageEdits, params.EditToken)
	if !found || authorization.action != action || !time.Now().Before(authorization.expires) || params.SessionID != authorization.source.SessionID {
		return errors.New("message edit token is unknown, expired or invalid for this session")
	}
	if err := a.messageEditReadyAdmitted(); err != nil {
		return err
	}
	var source session.MessageEditSource
	if action == messageActionRegenerate {
		var resolved session.MessageRegenerateSource
		resolved, err = session.ResolveMessageRegenerate(ctx, a.Session, protocol.RPCMessageRegeneratePrepareParams{SessionID: params.SessionID, EntryID: authorization.replyEntryID})
		source = resolved.MessageEditSource
		if err == nil && resolved.ReplyEntryID != authorization.replyEntryID {
			return errors.New("regeneration reply changed; prepare again")
		}
		params.Text = authorization.source.Text
	} else {
		source, err = session.ResolveMessageEdit(ctx, a.Session, protocol.RPCMessageEditPrepareParams{SessionID: params.SessionID, EntryID: authorization.source.EntryID})
	}
	if err != nil {
		return err
	}
	if source != authorization.source {
		return errors.New("message edit source branch or tip changed; prepare again")
	}
	var created protocol.SessionBranch
	persisted := false
	// This runs before release on pre-turn failures, while admission still excludes
	// prompts and all session controls. No old entry is deleted or overwritten.
	defer func() {
		if created.ID != "" && !persisted {
			if rollbackErr := a.Agent.RollbackMessageEditAdmitted(created.ID, source.BranchID); rollbackErr != nil {
				retErr = errors.Join(ErrMessageEditOutcomeUnknown, retErr, rollbackErr)
			}
		}
	}()
	change := a.pluginTransitionRequest("fork_branch", a.Session)
	change.NewBranchID = ""
	change.FromEntryID = source.BoundaryID
	transition := func() error {
		if err := a.beforePluginSessionChange(ctx, change); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		created, err = a.Agent.ForkWithOptionsAdmitted(protocol.BranchForkOptions{SourceBranchID: source.BranchID, FromEntryID: source.BoundaryID})
		if err != nil && (errors.Is(err, agent.ErrBranchRollback) || pluginBranchID(a.Session) != source.BranchID) {
			return errors.Join(ErrMessageEditOutcomeUnknown, err)
		}
		return err
	}
	acknowledge := func(turnID, entryID string) error {
		persisted = true
		a.pluginSessionChanged()
		notification = a.pluginTransitionNotification(change)
		messages, err := a.Session.Messages()
		if err != nil {
			return errors.Join(ErrMessageEditOutcomeUnknown, err)
		}
		err = admitted(protocol.RPCMessageEditCommitted{SessionID: source.SessionID, SourceBranchID: source.BranchID, SourceTipID: source.TipID, EntryID: source.EntryID, BranchID: created.ID, TurnID: turnID, UserEntryID: entryID}, messages)
		if err != nil {
			return errors.Join(ErrMessageEditOutcomeUnknown, err)
		}
		return nil
	}
	// Append errors are not proof of absence: SQLite may commit the user and
	// then fail refreshing its title. Only an exact durable-ID absence permits
	// rollback; an unavailable probe is ambiguous and conservatively retains it.
	inputFailed := func(entryID string, appendErr error) error {
		probe, ok := a.Session.(session.MessageEditEntryPresenceStore)
		found := false
		var probeErr error
		if ok {
			probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			found, probeErr = probe.MessageEditEntryExists(probeCtx, entryID)
			cancel()
		}
		if !ok || probeErr != nil || found {
			persisted = true
			a.pluginSessionChanged()
			notification = a.pluginTransitionNotification(change)
			return errors.Join(ErrMessageEditOutcomeUnknown, appendErr, probeErr)
		}
		return appendErr
	}
	return a.Agent.PromptEditAdmitted(ctx, params.Text, transition, acknowledge, inputFailed, release)
}
