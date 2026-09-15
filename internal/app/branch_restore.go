package app

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var ErrBranchRestoreRejected = errors.New("branch restore rejected without changing the active version")
var ErrBranchRestoreUnknown = session.ErrBranchRestoreUnknown

const branchRestoreTTL = 2 * time.Minute
const maxBranchRestoreTokens = 64

type branchRestoreAuthorization struct {
	mode    protocol.CollaborationMode
	binding protocol.RPCBranchRestorePrepareParams
	expires time.Time
}

func (a *App) PrepareBranchRestore(ctx context.Context, p protocol.RPCBranchRestorePrepareParams) (result protocol.RPCBranchRestorePrepared, retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(ErrBranchRestoreRejected, retErr)
		}
	}()
	if err := session.ValidateBranchRestoreBinding(p); err != nil {
		return result, err
	}
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	if err := a.branchRestoreReadyAdmitted(); err != nil {
		return result, err
	}
	store, err := a.versionStoreAdmitted(p.SessionID)
	if err != nil {
		return result, err
	}
	snapshot, err := store.BranchVersion(ctx, p.TargetBranchID)
	if err != nil {
		return result, err
	}
	if !snapshot.Matches(p) || snapshot.NonterminalGoal {
		return result, session.ErrBranchVersionStale
	}
	now := time.Now()
	for token, auth := range a.branchRestores {
		if !now.Before(auth.expires) {
			delete(a.branchRestores, token)
		}
	}
	if len(a.branchRestores) >= maxBranchRestoreTokens {
		return result, errors.New("branch restore preparation pool is full")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if a.branchRestores == nil {
		a.branchRestores = make(map[string]branchRestoreAuthorization)
	}
	token := rand.Text()
	expires := now.Add(branchRestoreTTL)
	a.branchRestores[token] = branchRestoreAuthorization{binding: p, expires: expires, mode: snapshot.Mode}
	return protocol.RPCBranchRestorePrepared{RPCBranchRestorePrepareParams: p, RestoreToken: token, ExpiresAt: expires.UnixMilli()}, nil
}

// CommitBranchRestore is serial and stays idle. prepareAck must only prepare a
// bounded public response (without writing it); its returned closure publishes
// that response after exact selection. Neither callback may reenter admission.
// This permits rejecting oversized history before any durable mutation.
func (a *App) CommitBranchRestore(ctx context.Context, p protocol.RPCBranchRestoreCommitParams, prepareAck func(protocol.RPCBranchRestoreCommitted, []protocol.Message) (func() error, error)) (retErr error) {
	var notification *protocol.PluginSessionChanged
	defer a.publishPluginSessionChange(&notification)
	defer func() {
		if retErr != nil && !errors.Is(retErr, ErrBranchRestoreUnknown) {
			retErr = errors.Join(ErrBranchRestoreRejected, retErr)
		}
	}()
	if prepareAck == nil || !session.ValidVersionIdentity(p.SessionID, false) || !session.ValidVersionIdentity(p.RestoreToken, false) {
		return errors.New("branch restore requires a bounded single-use token and acknowledgement")
	}
	unlockPlugins, err := a.lockPluginSession()
	if err != nil {
		return err
	}
	defer unlockPlugins()
	unlock, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	auth, ok := a.branchRestores[p.RestoreToken]
	delete(a.branchRestores, p.RestoreToken)
	if !ok || !time.Now().Before(auth.expires) || auth.binding.SessionID != p.SessionID {
		return session.ErrBranchVersionStale
	}
	if err := a.branchRestoreReadyAdmitted(); err != nil {
		return err
	}
	store, err := a.versionStoreAdmitted(p.SessionID)
	if err != nil {
		return err
	}
	snapshot, err := store.BranchVersion(ctx, auth.binding.TargetBranchID)
	if err != nil {
		return err
	}
	if !snapshot.Matches(auth.binding) || snapshot.NonterminalGoal || snapshot.Mode != auth.mode {
		return session.ErrBranchVersionStale
	}
	change := a.pluginTransitionRequest("branch_restore", a.Session)
	change.OldBranchID = auth.binding.SourceBranchID
	change.NewBranchID = auth.binding.TargetBranchID
	if err := a.beforePluginSessionChange(ctx, change); err != nil {
		return err
	}
	if err := a.branchRestoreReadyAdmitted(); err != nil {
		return err
	}
	// Revalidate after policy hooks, not merely at preparation time.
	snapshot, err = store.BranchVersion(ctx, auth.binding.TargetBranchID)
	if err != nil {
		return err
	}
	if !snapshot.Matches(auth.binding) || snapshot.NonterminalGoal || snapshot.Mode != auth.mode {
		return session.ErrBranchVersionStale
	}
	result := protocol.RPCBranchRestoreCommitted{Mode: snapshot.Mode, SessionID: p.SessionID, BranchID: auth.binding.TargetBranchID, TipID: auth.binding.TargetTipID}
	ack, err := prepareAck(result, snapshot.Messages())
	if err != nil {
		return err
	}
	if ack == nil {
		return errors.New("branch restore acknowledgement is missing")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.Agent.RestoreBranchVersionAdmitted(ctx, auth.binding, snapshot.Mode); err != nil {
		if errors.Is(err, ErrBranchRestoreUnknown) {
			probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			actual, probeErr := store.ProbeBranchVersion(probeCtx)
			cancel()
			if probeErr == nil && actual == (session.BranchVersionIdentity{SessionID: p.SessionID, BranchID: auth.binding.TargetBranchID, TipID: auth.binding.TargetTipID}) {
				a.pluginSessionChanged()
				notification = a.pluginTransitionNotification(change)
				notification.NewBranchID = auth.binding.TargetBranchID
			}
		}
		return err
	}
	a.pluginSessionChanged()
	// Publish the transition notification after releasing admission/plugin locks,
	// matching the existing lifecycle policy and avoiding callback deadlocks.
	notification = a.pluginTransitionNotification(change)
	notification.NewBranchID = auth.binding.TargetBranchID
	if err := ack(); err != nil {
		return errors.Join(ErrBranchRestoreUnknown, err)
	}
	return nil
}

func (a *App) branchRestoreReadyAdmitted() error {
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: branch restore rejects active subagents")
	}
	return a.Agent.BranchRestoreReadyAdmitted()
}
