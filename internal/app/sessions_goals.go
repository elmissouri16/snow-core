package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/worktree"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// bindPermissionSession restores permission state for st and routes future
// mode/rule changes back into that session's metadata entries.
func (a *App) bindPermissionSession(st session.Store) error {
	a.Perm.SetChangeHandler(nil)
	// Start each session from the configured baseline so switching away from a
	// session cannot leak its remembered rules into the next one.
	a.Perm.RestoreState(permission.State{Mode: a.permissionDefault})

	meta, ok := st.(session.MetadataStore)
	if !ok {
		return nil
	}
	if !a.permissionOverride {
		raw, found, err := meta.Metadata(permissionMetadataKey)
		if err != nil {
			return err
		}
		if found && raw != "" {
			var state permission.State
			if err := json.Unmarshal([]byte(raw), &state); err == nil {
				a.Perm.RestoreState(state)
			}
		}
	}
	a.Perm.SetChangeHandler(func(state permission.State) {
		raw, err := json.Marshal(state)
		if err != nil {
			return
		}
		_ = meta.SetMetadata(permissionMetadataKey, string(raw))
	})
	return nil
}

// SetSession switches the active durable conversation store. The old store is
// closed only after the agent accepts the new store.
func (a *App) SetSession(st session.Store) error {
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot switch session while subagents are active")
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot switch session while subagents are active")
	}
	if st == nil {
		return fmt.Errorf("app: session is nil")
	}
	if err := a.Goal.ValidateStore(st); err != nil {
		return err
	}
	old := a.Session
	if err := a.bindPermissionSession(st); err != nil {
		return fmt.Errorf("app: permission state: %w", err)
	}
	if err := a.Agent.SetSessionQuietAdmitted(st); err != nil {
		_ = a.bindPermissionSession(old)
		return err
	}
	if err := a.Goal.SetStore(st); err != nil {
		_ = a.Agent.SetSessionQuietAdmitted(old)
		_ = a.bindPermissionSession(old)
		return err
	}
	if a.Subagents != nil {
		taskStore, ok := st.(session.SubagentTaskStore)
		if !ok {
			_ = a.Goal.SetStore(old)
			_ = a.Agent.SetSessionQuietAdmitted(old)
			_ = a.bindPermissionSession(old)
			return errors.New("app: session does not support subagent topology")
		}
		if err := a.Subagents.SetStoreAdmitted(taskStore); err != nil {
			_ = a.Goal.SetStore(old)
			_ = a.Agent.SetSessionQuietAdmitted(old)
			_ = a.bindPermissionSession(old)
			return err
		}
	}
	if a.ProcessManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), managedprocess.DefaultShutdownTimeout)
		err := a.ProcessManager.RebindSession(ctx, st.ID())
		cancel()
		if err != nil {
			rollbackErrs := make([]error, 0, 4)
			if a.Subagents != nil {
				if oldTasks, ok := old.(session.SubagentTaskStore); ok {
					if rollbackErr := a.Subagents.SetStoreAdmitted(oldTasks); rollbackErr != nil {
						rollbackErrs = append(rollbackErrs, fmt.Errorf("restore subagent store: %w", rollbackErr))
					}
				}
			}
			if rollbackErr := a.Goal.SetStore(old); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore goal store: %w", rollbackErr))
			}
			if rollbackErr := a.Agent.SetSessionQuietAdmitted(old); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore agent session: %w", rollbackErr))
			}
			if rollbackErr := a.bindPermissionSession(old); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore permission session: %w", rollbackErr))
			}
			return errors.Join(err, errors.Join(rollbackErrs...))
		}
	}
	a.Agent.ResetTurnIdentityAdmitted()
	a.Session = st
	a.sessionHistory.Set(st)
	g, _ := a.Goal.Get()
	a.Agent.Publish(a.Agent.StateEvent())
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g, Cleared: g == nil}})
	if old != nil && old != st {
		if err := old.Close(); err != nil {
			// The switch is already committed across App, Agent, Goal, and
			// permission state. Surface old-store cleanup as an event instead of
			// falsely reporting that the new session failed to bind.
			a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvError, Message: "close previous session: " + err.Error()})
		}
	}
	return nil
}

func (a *App) SelectBranch(branchID string) error {
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot switch branch while subagents are active")
	}
	return a.Agent.SelectBranchAdmitted(branchID)
}

func (a *App) ForkBranch(fromEntryID string) (protocol.SessionBranch, error) {
	return a.ForkBranchWithOptions(protocol.BranchForkOptions{FromEntryID: fromEntryID})
}

func (a *App) ForkBranchWithOptions(opts protocol.BranchForkOptions) (protocol.SessionBranch, error) {
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.SessionBranch{}, errors.New("app: cannot fork branch while subagents are active")
	}
	return a.Agent.ForkWithOptionsAdmitted(opts)
}

// ForkSession creates an independent durable session in the current workspace.
// It leaves the active App/session unchanged; callers may explicitly open or
// switch to the returned path after this operation succeeds.
func (a *App) ForkSession(ctx context.Context, opts protocol.SessionForkOptions) (protocol.SessionForkResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.SessionForkResult{}, errors.New("app: cannot fork session while subagents are active")
	}
	source, err := a.Agent.IdleSessionAdmitted("fork session")
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return protocol.SessionForkResult{}, err
	}
	index := session.NewFileIndex(session.DefaultSessionsRoot())
	child, result, err := index.CreateFork(a.cwd, source, opts)
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	if err := copyForkArtifacts(ctx, a.artifacts, child, result); err != nil {
		_ = child.Close()
		removeSessionFiles(result.SessionPath)
		return protocol.SessionForkResult{}, err
	}
	if err := child.Close(); err != nil {
		removeSessionFiles(result.SessionPath)
		return protocol.SessionForkResult{}, fmt.Errorf("app: close forked session: %w", err)
	}
	return result, nil
}

// ForkWorktree creates a clean Git worktree plus an independent durable
// session rooted there. It is detached: the current App keeps its immutable
// trust and tool-root bindings.
func (a *App) ForkWorktree(ctx context.Context, opts protocol.SessionWorktreeForkOptions) (protocol.SessionForkResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.SessionForkResult{}, errors.New("app: cannot fork worktree while subagents are active")
	}
	source, err := a.Agent.IdleSessionAdmitted("fork worktree")
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	created, err := worktree.Create(ctx, worktree.Request{
		SourceDir: a.cwd,
		TargetDir: opts.WorktreePath,
		Branch:    opts.GitBranch,
		Name:      opts.Name,
	})
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	forkOpts := protocol.SessionForkOptions{
		SourceBranchID:  opts.SourceBranchID,
		FromEntryID:     opts.FromEntryID,
		Name:            opts.Name,
		DestinationPath: opts.DestinationPath,
	}
	forkOpts.DestinationPath, err = worktree.ResolveSessionPath(created.TargetDir, forkOpts.DestinationPath)
	if err != nil {
		return protocol.SessionForkResult{}, errors.Join(err, worktree.Remove(context.Background(), created))
	}
	index := session.NewFileIndex(session.DefaultSessionsRoot())
	child, result, err := index.CreateFork(created.TargetDir, source, forkOpts)
	if err != nil {
		return protocol.SessionForkResult{}, errors.Join(err, worktree.Remove(context.Background(), created))
	}
	if err := copyForkArtifacts(ctx, a.artifacts, child, result); err != nil {
		_ = child.Close()
		removeSessionFiles(result.SessionPath)
		return protocol.SessionForkResult{}, errors.Join(err, worktree.Remove(context.Background(), created))
	}
	if err := child.Close(); err != nil {
		removeSessionFiles(result.SessionPath)
		return protocol.SessionForkResult{}, errors.Join(fmt.Errorf("app: close worktree session: %w", err), worktree.Remove(context.Background(), created))
	}
	result.Worktree = &protocol.WorktreeInfo{Path: created.TargetDir, Branch: created.Branch, Commit: created.Commit}
	return result, nil
}

func copyForkArtifacts(ctx context.Context, store artifact.Store, child session.Store, result protocol.SessionForkResult) error {
	ids, err := session.ForkArtifactIDs(child)
	if err != nil {
		return fmt.Errorf("app: inspect fork artifacts: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	ownedIDs, err := store.ListIDs(ctx, result.SourceSessionID)
	if err != nil {
		return fmt.Errorf("app: enumerate source artifacts: %w", err)
	}
	owned := make(map[string]bool, len(ownedIDs))
	for _, id := range ownedIDs {
		owned[id] = true
	}
	verified := make([]string, 0, min(len(ids), len(ownedIDs)))
	for _, id := range ids {
		if owned[id] {
			verified = append(verified, id)
		}
	}
	if len(verified) == 0 {
		return nil
	}
	if len(verified) > maxForkArtifactCopies {
		// Never create a physically exact fork with silently partial retrieval.
		// Enforce the cap only after intersecting untrusted text markers with the
		// source session's structurally enumerated private artifacts.
		return fmt.Errorf("app: fork owns %d referenced artifacts; maximum is %d", len(verified), maxForkArtifactCopies)
	}
	copier, ok := store.(artifact.Copier)
	if !ok {
		return errors.New("app: artifact store cannot copy session artifacts")
	}
	for _, id := range verified {
		if err := copier.CopyText(ctx, result.SourceSessionID, result.SessionID, id); err != nil {
			return fmt.Errorf("app: copy fork artifact %s: %w", id, err)
		}
	}
	return nil
}

func removeSessionFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}

func (a *App) RenameSession(title string) error {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot rename session while subagents are active")
	}
	return a.Agent.RenameSessionAdmitted(title)
}

// DeleteSession permanently removes an inactive session belonging to the
// app's working directory. The active database remains owned by the running
// app and must be switched away from before it can be deleted.
func (a *App) DeleteSession(path, expectedID string) error {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	activeID, activePath, running, err := a.Agent.SessionIdentityAdmitted()
	if err != nil {
		return err
	}
	if running {
		return errors.New("app: cannot delete a session while a turn is running")
	}
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot delete a session while subagents are active")
	}
	activeAlias := false
	if path != "" && activePath != "" {
		selectedInfo, selectedErr := os.Stat(path)
		activeInfo, activeErr := os.Stat(activePath)
		activeAlias = selectedErr == nil && activeErr == nil && os.SameFile(selectedInfo, activeInfo)
	}
	if expectedID == activeID || activeAlias {
		return errors.New("app: cannot delete the active session; resume or create another session first")
	}
	index := session.NewFileIndex(session.DefaultSessionsRoot())
	path, err = indexedSessionPath(index, a.cwd, path, expectedID)
	if err != nil {
		return err
	}
	ownedIDs, err := index.DeleteWithIDs(a.cwd, path, expectedID)
	if err != nil && len(ownedIDs) == 0 {
		return err
	}
	var cleanupErrs []error
	if err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}
	if deleter, ok := a.artifacts.(artifact.SessionDeleter); ok {
		for _, id := range ownedIDs {
			if err := deleter.DeleteSession(context.Background(), id); err != nil {
				cleanupErrs = append(cleanupErrs, err)
			}
		}
	}
	for _, id := range ownedIDs {
		if err := goalpkg.DeleteSessionData(config.GlobalDir(), id); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	if cleanupErr := errors.Join(cleanupErrs...); cleanupErr != nil {
		return &SessionDeleteCleanupError{Err: cleanupErr}
	}
	return nil
}

func indexedSessionPath(index *session.FileIndex, cwd, path, expectedID string) (string, error) {
	requested, err := filepath.Abs(path)
	if err != nil {
		return "", session.ErrNotFound
	}
	infos, err := index.List(cwd)
	if err != nil {
		return "", err
	}
	for _, info := range infos {
		listed, listedErr := filepath.Abs(info.Path)
		if listedErr == nil && filepath.Clean(requested) == filepath.Clean(listed) && info.ID == expectedID {
			return listed, nil
		}
	}
	return "", session.ErrNotFound
}

func (a *App) RenameBranch(branchID, name string) (protocol.SessionBranch, error) {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return protocol.SessionBranch{}, errors.New("app: cannot rename branch while subagents are active")
	}
	return a.Agent.RenameBranchAdmitted(branchID, name)
}

func (a *App) DeleteBranch(branchID string) error {
	unlock := a.Agent.LockAdmission()
	defer unlock()
	if a.Subagents != nil && a.Subagents.HasActive() {
		return errors.New("app: cannot delete branch while subagents are active")
	}
	return a.Agent.DeleteBranchAdmitted(branchID)
}

func (a *App) GoalState() (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	return a.Goal.Get()
}

// GoalContinuationDeferred reports whether automatic continuation is durably
// suppressed for the active branch.
func (a *App) GoalContinuationDeferred() (bool, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	return a.Goal.Deferred()
}

func (a *App) requireGoalCapabilities() error {
	for _, name := range []string{"get_goal", "create_goal", "update_goal"} {
		if _, ok := a.Registry.Get(name); !ok {
			return fmt.Errorf("goal: required capability %s is disabled by tools allowlist", name)
		}
	}
	return nil
}

// ReadyGoal is called only after a surface installs event subscriptions and interaction handlers.
func (a *App) ReadyGoal() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	g, err := a.Goal.Get()
	if err != nil {
		return err
	}
	a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, ThreadGoal: &protocol.ThreadGoalUpdate{Goal: g, Cleared: g == nil}})
	if g == nil || g.Status != protocol.GoalActive || a.Agent.Mode() != protocol.ModeDefault {
		return nil
	}
	deferred, err := a.Goal.Deferred()
	if err != nil {
		return err
	}
	if !deferred {
		if err := a.requireGoalCapabilities(); err != nil {
			return err
		}
		a.Agent.ContinueGoal()
	}
	return nil
}

func (a *App) goalAutoResumeEligible() (bool, error) {
	g, err := a.Goal.Get()
	if err != nil {
		return false, err
	}
	if g == nil || g.Status != protocol.GoalActive || a.Agent.Mode() != protocol.ModeDefault {
		return false, nil
	}
	deferred, err := a.Goal.Deferred()
	return !deferred, err
}

func (a *App) restartGoalAfterFailedMutation(eligible bool) {
	if !eligible {
		return
	}
	g, err := a.Goal.Get()
	if err != nil || g == nil || g.Status != protocol.GoalActive || a.Agent.Mode() != protocol.ModeDefault {
		return
	}
	deferred, err := a.Goal.Deferred()
	if err == nil && !deferred {
		a.Agent.ContinueGoal()
	}
}

func (a *App) CreateGoal(objective string, budget *int64, replace bool) (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.requireGoalCapabilities(); err != nil {
		return nil, err
	}
	restartOnFailure := false
	if replace {
		var err error
		restartOnFailure, err = a.goalAutoResumeEligible()
		if err != nil {
			return nil, err
		}
		if err := a.Agent.StopGoal(context.Background(), false); err != nil {
			return nil, err
		}
	}
	g, err := a.Goal.Create(objective, budget, replace)
	if err != nil {
		a.restartGoalAfterFailedMutation(restartOnFailure)
		return nil, err
	}
	a.Agent.ContinueGoal()
	return g, nil
}

func (a *App) EditGoal(objective string) (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	g, e := a.Goal.Get()
	if e != nil {
		return nil, e
	}
	if g == nil {
		return nil, session.ErrNotFound
	}
	restartOnFailure, err := a.goalAutoResumeEligible()
	if err != nil {
		return nil, err
	}
	if err := a.Agent.StopGoal(context.Background(), false); err != nil {
		return nil, err
	}
	next, err := a.Goal.Edit(g.GoalID, objective)
	if err != nil {
		a.restartGoalAfterFailedMutation(restartOnFailure)
		return nil, err
	}
	if next.Status == protocol.GoalActive {
		a.Agent.ResetGoalAudit()
		a.Agent.ContinueGoal()
	}
	return next, nil
}

func (a *App) setGoalStatusLocked(status protocol.ThreadGoalStatus) (*protocol.ThreadGoal, error) {
	g, err := a.Goal.Get()
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, session.ErrNotFound
	}
	return a.Goal.SetStatus(g.GoalID, status, false)
}

func (a *App) SetGoalStatus(status protocol.ThreadGoalStatus) (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	return a.setGoalStatusLocked(status)
}

func (a *App) PauseGoal() (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.Agent.StopGoal(context.Background(), true); err != nil {
		return nil, err
	}
	return a.setGoalStatusLocked(protocol.GoalPaused)
}

func (a *App) ResumeGoal() (*protocol.ThreadGoal, error) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	g, err := a.Goal.Get()
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, session.ErrNotFound
	}
	if err := a.requireGoalCapabilities(); err != nil {
		return nil, err
	}
	if g.Status == protocol.GoalActive {
		deferred, err := a.Goal.Deferred()
		if err != nil {
			return nil, err
		}
		if !deferred {
			return nil, errors.New("goal: active goal is not deferred")
		}
		if err := a.Goal.Defer(false); err != nil {
			return nil, err
		}
	} else {
		g, err = a.Goal.SetStatus(g.GoalID, protocol.GoalActive, false)
		if err != nil {
			return nil, err
		}
	}
	a.Agent.ResetGoalAudit()
	a.Agent.ContinueGoal()
	return g, nil
}

func (a *App) ClearGoal() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.Agent.StopGoal(context.Background(), true); err != nil {
		return err
	}
	g, err := a.Goal.Get()
	if err != nil {
		return err
	}
	id := ""
	if g != nil {
		id = g.GoalID
	}
	return a.Goal.Clear(id)
}

func (a *App) ContinueGoal() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	unlockAdmission := a.Agent.LockAdmission()
	defer unlockAdmission()
	if err := a.requireGoalCapabilities(); err != nil {
		return err
	}
	g, err := a.Goal.Get()
	if err != nil {
		return err
	}
	if g == nil || g.Status != protocol.GoalActive {
		return errors.New("goal: no active goal")
	}
	if err := a.Goal.Defer(false); err != nil {
		return err
	}
	a.Agent.ContinueGoal()
	return nil
}

// ConfigureOpenAICompatible replaces the runtime adapter after an operator
// changes its endpoint. Persistence remains the caller's responsibility so TUI
// configuration can save endpoint and credential as one user action.
func (a *App) ConfigureOpenAICompatible(baseURL string) error {
	return a.ConfigureOpenAICompatibleProfile(openaicompat.ProviderID, baseURL)
}

// ConfigureOpenAICompatibleProfile creates or replaces one named compatible
// endpoint. The profile ID is also its config key, auth-store key, model
// provider ID, CLI selector, and TUI label.
func (a *App) ConfigureOpenAICompatibleProfile(profileID, baseURL string) error {
	if err := config.ValidateProviderProfileID(profileID); err != nil {
		return err
	}
	unlockTransition, err := a.beginProviderTransition("configure provider")
	if err != nil {
		return err
	}
	defer unlockTransition()
	environment := []string(nil)
	if profileID == openaicompat.ProviderID {
		environment = []string{openaicompat.EnvAPIKey}
	}
	if !a.AuthService.Registered(profileID) {
		if err := a.AuthService.Register(auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: profileID, DisplayName: profileID, Required: false, Environment: environment})); err != nil {
			return err
		}
	}
	pc := a.Cfg.Providers[profileID]
	cfg := openaicompat.Config{ProviderID: profileID, BaseURL: baseURL, DefaultModel: pc.DefaultModel, StreamIdleTimeout: configuredStreamIdleTimeout(pc.StreamIdleTimeoutMS), DisableEnvAPIKey: true}
	compatible, err := openaicompat.New(cfg)
	if err != nil {
		return fmt.Errorf("app: %s: %w", profileID, err)
	}
	if !compatible.Configured() {
		return fmt.Errorf("app: OpenAI-compatible profile %q requires a base URL", profileID)
	}
	authenticated, err := provider.NewAuthenticated(compatible, a.AuthService)
	if err != nil {
		return err
	}
	driver := auth.NewAPIKeyDriver(auth.APIKeyOptions{ProviderID: profileID, DisplayName: profileID, Required: false, Environment: environment})
	module := provider.Module{ID: profileID, Order: 20 + len(a.ProviderModules.Modules()), Transport: compatible, Auth: driver}
	if _, ok := a.ProviderModules.Module(profileID); ok {
		module.Order = 0 // Registry.Replace preserves the existing order.
		if err := a.ProviderModules.Replace(module); err != nil {
			return err
		}
	} else if err := a.ProviderModules.Register(module); err != nil {
		return err
	}

	a.stateMu.Lock()
	active := a.ProviderID == profileID
	a.stateMu.Unlock()
	if active {
		model := a.Agent.Model()
		if err := a.Agent.SetProviderAndModel(authenticated, model); err != nil {
			return err
		}
	}
	a.stateMu.Lock()
	a.Providers[profileID] = authenticated
	delete(a.modelCatalog, profileID)
	if active {
		a.Provider = authenticated
		a.Models = nil
	}
	if a.runtimeSelection != nil {
		a.runtimeSelection.mu.Lock()
		a.runtimeSelection.providers[profileID] = authenticated
		if a.runtimeSelection.catalogGeneration == nil {
			a.runtimeSelection.catalogGeneration = make(map[string]uint64)
		}
		a.runtimeSelection.catalogGeneration[profileID]++
		delete(a.runtimeSelection.catalogs, profileID)
		delete(a.runtimeSelection.catalogErrors, profileID)
		a.runtimeSelection.mu.Unlock()
	}
	a.rebuildAllModelsLocked()
	a.stateMu.Unlock()
	return nil
}

func (a *App) rebuildAllModelsLocked() {
	var all []protocol.Model
	seen := map[string]bool{}
	ids := []string{a.ProviderID}
	for _, module := range a.ProviderModules.Modules() {
		ids = append(ids, module.ID)
	}
	for _, providerID := range ids {
		if seen[providerID] {
			continue
		}
		seen[providerID] = true
		all = append(all, a.modelCatalog[providerID]...)
	}
	a.AllModels = all
}
