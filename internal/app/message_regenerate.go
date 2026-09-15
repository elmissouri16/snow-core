package app

import (
	"context"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (a *App) PrepareMessageRegenerate(ctx context.Context, params protocol.RPCMessageRegeneratePrepareParams) (protocol.RPCMessageRegeneratePrepared, error) {
	var result protocol.RPCMessageRegeneratePrepared
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err := a.messageEditReadyAdmitted(); err != nil {
		return result, err
	}
	source, err := session.ResolveMessageRegenerate(ctx, a.Session, params)
	if err != nil {
		return result, err
	}
	result.RPCMessageEditPrepared, err = a.authorizeMessageEdit(source.MessageEditSource, messageActionRegenerate, source.ReplyEntryID)
	if err != nil {
		return result, err
	}
	result.ReplyEntryID = source.ReplyEntryID
	return result, nil
}

// CommitMessageRegenerate runs the shared historical replacement transaction
// with the original server-held text. It regenerates the whole owning turn,
// including tool work, without appending a duplicate visible user prompt.
func (a *App) CommitMessageRegenerate(ctx context.Context, params protocol.RPCMessageRegenerateCommitParams, admitted func(protocol.RPCMessageEditCommitted, []protocol.Message) error) error {
	return a.commitMessageRevision(ctx, protocol.RPCMessageEditCommitParams{SessionID: params.SessionID, EditToken: params.EditToken}, messageActionRegenerate, admitted)
}
