package app

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var ErrHistoryControlRejected = errors.New("history control rejected before mutation")
var ErrHistoryControlUnknown = agent.ErrHistoryControlUnknown

func historyControlError(err error) error {
	if err != nil && !errors.Is(err, ErrHistoryControlUnknown) {
		return errors.Join(ErrHistoryControlRejected, err)
	}
	return err
}

func validateHistoryControlName(name string) error {
	if name == "" || name != strings.TrimSpace(name) || len(name) > protocol.RPCHistoryControlMaxNameBytes || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("history control requires a nonempty bounded display name without surrounding whitespace or control characters")
	}
	return nil
}

func validateHistoryControlBranchName(name string) error {
	if err := validateHistoryControlName(name); err != nil {
		return err
	}
	if utf8.RuneCountInString(name) > 64 {
		return errors.New("history control branch name exceeds 64 runes")
	}
	return nil
}

func validateHistoryControlBinding(p protocol.RPCHistoryControlBinding) error {
	for _, id := range []string{p.SessionID, p.SourceBranchID, p.TargetBranchID} {
		if !session.ValidVersionIdentity(id, false) {
			return session.ErrBranchVersionStale
		}
	}
	if !session.ValidVersionIdentity(p.SourceTipID, true) || !session.ValidVersionIdentity(p.TargetTipID, true) {
		return session.ErrBranchVersionStale
	}
	return nil
}

// lockHistoryControl follows SetSession's plugin -> state -> admission order.
// No public admission-taking method may be called before releasing this lock.
func (a *App) lockHistoryControl(ctx context.Context) (func(), error) {
	unlockPlugins, err := a.lockPluginSession()
	if err != nil {
		return nil, err
	}
	a.stateMu.Lock()
	unlockAdmission, err := a.Agent.LockAdmissionContext(ctx)
	if err != nil {
		a.stateMu.Unlock()
		unlockPlugins()
		return nil, err
	}
	return func() { unlockAdmission(); a.stateMu.Unlock(); unlockPlugins() }, nil
}

func (a *App) historyControlSnapshotAdmitted(ctx context.Context, p protocol.RPCHistoryControlBinding) (session.BranchVersionSnapshot, error) {
	var empty session.BranchVersionSnapshot
	if err := validateHistoryControlBinding(p); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := a.branchRestoreReadyAdmitted(); err != nil {
		return empty, err
	}
	store, err := a.versionStoreAdmitted(p.SessionID)
	if err != nil {
		return empty, err
	}
	snapshot, err := store.BranchVersion(ctx, p.TargetBranchID)
	if err != nil {
		return empty, err
	}
	if snapshot.Active != (session.BranchVersionIdentity{SessionID: p.SessionID, BranchID: p.SourceBranchID, TipID: p.SourceTipID}) || snapshot.Branch.ID != p.TargetBranchID || snapshot.Branch.TipID != p.TargetTipID || snapshot.NonterminalGoal {
		return empty, session.ErrBranchVersionStale
	}
	return snapshot, nil
}

func historyControlBranch(b protocol.SessionBranch) protocol.RPCBranchVersion {
	return protocol.RPCBranchVersion{ID: b.ID, Name: b.Name, ParentID: b.ParentID, ForkedFromID: b.ForkedFromID, TipID: b.TipID, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Active: b.Active}
}

// ManagedBranchFork activates only the selected saved history. It neither
// admits input nor invokes providers/tools. Returned history identity is safe
// to reload through the existing bounded Versions pages after acknowledgement.
func (a *App) ManagedBranchFork(ctx context.Context, p protocol.RPCManagedBranchForkParams) (result protocol.RPCManagedBranchForkResult, retErr error) {
	defer func() { retErr = historyControlError(retErr) }()
	if err := validateHistoryControlBranchName(p.Name); err != nil {
		return result, err
	}
	var notification *protocol.PluginSessionChanged
	defer a.publishPluginSessionChange(&notification)
	unlock, err := a.lockHistoryControl(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	snapshot, err := a.historyControlSnapshotAdmitted(ctx, p.RPCHistoryControlBinding)
	if err != nil {
		return result, err
	}
	if err := session.ValidateForkBoundary(snapshot.Entries); err != nil {
		return result, err
	}
	change := a.pluginTransitionRequest("fork_branch", a.Session)
	change.NewBranchID, change.FromEntryID = "", p.TargetTipID
	if err := a.beforePluginSessionChange(ctx, change); err != nil {
		return result, err
	}
	// Hooks may inspect state; recheck every read-only gate and binding before
	// mutation, not just the initially requested source identity.
	snapshot, err = a.historyControlSnapshotAdmitted(ctx, p.RPCHistoryControlBinding)
	if err != nil {
		return result, err
	}
	if err := session.ValidateForkBoundary(snapshot.Entries); err != nil {
		return result, err
	}
	branch, err := a.Agent.ManagedForkBranchAdmitted(ctx, p, snapshot.Mode)
	if err != nil {
		return result, err
	}
	a.pluginSessionChanged()
	notification = a.pluginTransitionNotification(change)
	notification.NewBranchID = branch.ID
	result = protocol.RPCManagedBranchForkResult{SessionID: p.SessionID, BranchID: branch.ID, TipID: branch.TipID, Branch: historyControlBranch(branch), Mode: snapshot.Mode, ReasoningEffort: a.Agent.BranchRestoreThinking(snapshot.Mode), RootEpoch: a.Agent.RootEpoch()}
	return result, nil
}

// ManagedBranchRename changes only exact-target display metadata, never the
// active branch, its event epoch, input queues, or provider-facing history.
func (a *App) ManagedBranchRename(ctx context.Context, p protocol.RPCManagedBranchRenameParams) (result protocol.RPCManagedBranchRenameResult, retErr error) {
	defer func() { retErr = historyControlError(retErr) }()
	if err := validateHistoryControlBranchName(p.Name); err != nil {
		return result, err
	}
	if err := validateHistoryControlBranchName(p.OldName); err != nil {
		return result, err
	}
	unlock, err := a.lockHistoryControl(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	snapshot, err := a.historyControlSnapshotAdmitted(ctx, p.RPCHistoryControlBinding)
	if err != nil {
		return result, err
	}
	if snapshot.Branch.Name != p.OldName {
		return result, session.ErrBranchVersionStale
	}
	manager, ok := a.Session.(session.HistoryControlStore)
	if !ok {
		return result, errors.New("app: managed branch rename unsupported")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	branch, err := manager.RenameHistoryBranch(ctx, p)
	if err != nil {
		return result, err
	}
	return protocol.RPCManagedBranchRenameResult{SessionID: p.SessionID, BranchID: p.SourceBranchID, TipID: p.SourceTipID, Branch: historyControlBranch(branch), RootEpoch: a.Agent.RootEpoch()}, nil
}
