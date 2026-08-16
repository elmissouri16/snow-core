package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snow-core/snow/internal/sandbox"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/supervisor"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/internal/worktree"
	"github.com/snow-core/snow/pkg/protocol"
)

const maxWorkspaceSessions = 8

// WorktreeWorkspace is the internal observer/supervisor view of one linked Git
// worktree and its exact-CWD Snow sessions. It intentionally carries no process
// liveness claim; the supervisor overlays only handles that it owns.
type WorktreeWorkspace struct {
	ID             string
	Path           string
	Name           string
	Branch         string
	Head           string
	Current        bool
	Detached       bool
	Locked         bool
	LockReason     string
	Prunable       bool
	PrunableReason string
	Dirty          bool
	GitError       string
	SessionError   string
	SessionCount   int
	Sessions       []session.SessionInfo
}

// WorktreeWorkspaces discovers linked worktrees and exact-CWD sessions without
// opening remote sessions through a mutable store or inspecting their trust and
// sandbox state. The current workspace is returned alone for non-Git projects.
func (a *App) WorktreeWorkspaces(ctx context.Context) ([]WorktreeWorkspace, error) {
	if a == nil {
		return nil, errors.New("app: nil app")
	}
	inventory, err := worktree.List(ctx, a.cwd)
	if err != nil {
		if !errors.Is(err, worktree.ErrNotRepository) {
			return nil, err
		}
		path := canonicalWorkspacePath(a.cwd)
		workspace := WorktreeWorkspace{
			ID: "workspace-current", Path: path, Name: filepath.Base(path), Current: true,
			GitError: "linked worktrees require a Git repository",
		}
		a.populateWorkspaceSessions(&workspace)
		return []WorktreeWorkspace{workspace}, nil
	}

	workspaces := make([]WorktreeWorkspace, 0, len(inventory.Worktrees))
	for _, linked := range inventory.Worktrees {
		workspace := WorktreeWorkspace{
			ID: linked.ID, Path: linked.Path, Branch: linked.Branch, Head: linked.Head,
			Current: linked.Current, Detached: linked.Detached, Locked: linked.Locked,
			LockReason: linked.LockReason, Prunable: linked.Prunable,
			PrunableReason: linked.PrunableReason, Dirty: linked.Dirty, GitError: linked.StatusError,
		}
		workspace.Name = workspace.Branch
		if workspace.Name == "" {
			workspace.Name = filepath.Base(workspace.Path)
		}
		a.populateWorkspaceSessions(&workspace)
		workspaces = append(workspaces, workspace)
	}
	disambiguateWorkspaceNames(workspaces)
	sort.SliceStable(workspaces, func(i, j int) bool {
		if workspaces[i].Current != workspaces[j].Current {
			return workspaces[i].Current
		}
		if workspaces[i].Branch != workspaces[j].Branch {
			return workspaces[i].Branch < workspaces[j].Branch
		}
		return workspaces[i].Path < workspaces[j].Path
	})
	return workspaces, nil
}

func (a *App) populateWorkspaceSessions(workspace *WorktreeWorkspace) {
	if workspace == nil {
		return
	}
	infos, err := session.NewFileIndex(session.DefaultSessionsRoot()).List(workspace.Path)
	if err != nil {
		workspace.SessionError = err.Error()
		return
	}
	// An explicitly located current session may live outside the default index.
	// Include that already-open identity without opening any remote store.
	if workspace.Current && a.Session != nil && a.Session.Path() != "" {
		found := false
		for _, info := range infos {
			if sameWorkspacePath(info.Path, a.Session.Path()) {
				found = true
				break
			}
		}
		if !found && sameWorkspacePath(a.Session.Header().CWD, workspace.Path) {
			header := a.Session.Header()
			infos = append(infos, session.SessionInfo{
				Path: a.Session.Path(), ID: a.Session.ID(), CWD: header.CWD, Name: header.Name,
				CreatedAt: header.CreatedAt,
			})
		}
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].UpdatedAt > infos[j].UpdatedAt })
	workspace.SessionCount = len(infos)
	if len(infos) > maxWorkspaceSessions {
		infos = infos[:maxWorkspaceSessions]
	}
	workspace.Sessions = append([]session.SessionInfo(nil), infos...)
}

func disambiguateWorkspaceNames(workspaces []WorktreeWorkspace) {
	counts := make(map[string]int, len(workspaces))
	for _, workspace := range workspaces {
		counts[workspace.Name]++
	}
	for i := range workspaces {
		if counts[workspaces[i].Name] < 2 {
			continue
		}
		parent := filepath.Base(filepath.Dir(workspaces[i].Path))
		if parent == "" || parent == "." || parent == string(filepath.Separator) {
			parent = workspaces[i].Path
		}
		workspaces[i].Name += " · " + parent
	}
}

func canonicalWorkspacePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}

func sameWorkspacePath(left, right string) bool {
	return strings.EqualFold(canonicalWorkspacePath(left), canonicalWorkspacePath(right))
}

// WorktreeWorkerPreflight is the side-effect-free launch confirmation shown by
// the TUI. SandboxAssociated is exact-path operator state, not a claim that an
// unrelated process is alive.
type WorktreeWorkerPreflight struct {
	Path              string
	TrustLevel        trust.Level
	TrustPrompt       bool
	ProjectInputs     bool
	SandboxAssociated bool
	SandboxStopped    bool
	RequireSandbox    bool
	Shell             string
	Provider          string
	Model             string
	Thinking          protocol.ThinkingLevel
	PermissionMode    string
}

// PreflightWorktreeWorker resolves trust and exact-path sandbox policy without
// loading project input, starting a VM, opening a session, or spawning a worker.
func (a *App) PreflightWorktreeWorker(path string) (WorktreeWorkerPreflight, error) {
	if a == nil {
		return WorktreeWorkerPreflight{}, errors.New("app: nil app")
	}
	preflight, err := InspectProjectTrust(Options{CWD: path, ConfigPath: a.ConfigPath})
	if err != nil {
		return WorktreeWorkerPreflight{}, err
	}
	providerID, activeModel, _ := a.ActiveModelsSnapshot()
	result := WorktreeWorkerPreflight{
		Path: preflight.Resolution.Path, TrustLevel: preflight.Resolution.Level,
		TrustPrompt:    preflight.Resolution.Prompt,
		ProjectInputs:  !preflight.Resolution.Prompt && preflight.Resolution.Level == trust.LevelAllow,
		RequireSandbox: a.requireSandbox, Shell: "host", Provider: providerID,
		Model: activeModel.ID, Thinking: a.Agent.Thinking(), PermissionMode: "ask",
	}
	if !a.disableSandbox && a.sandboxStatePath != "" {
		record, ok, recordErr := sandbox.NewStore(a.sandboxStatePath).Get(result.Path)
		if recordErr != nil {
			return WorktreeWorkerPreflight{}, recordErr
		}
		result.SandboxAssociated = ok
		if ok {
			result.SandboxStopped = record.Stopped
			if !record.Stopped {
				result.Shell = "vm"
			}
		}
	}
	return result, nil
}

// SetWorktreeTrust persists an exact destination decision. Callers must obtain
// explicit human confirmation before invoking it.
func (a *App) SetWorktreeTrust(path string, level trust.Level) error {
	if a == nil || a.Trust == nil {
		return errors.New("app: trust store unavailable")
	}
	return a.Trust.Set(path, level)
}

// WorktreeDiffSummary returns the bounded, read-only status/diff-stat view used
// by the supervisor panel.
func (a *App) WorktreeDiffSummary(ctx context.Context, path string) (string, error) {
	return worktree.ReadOnlySummary(ctx, path)
}

// StartWorktreeWorker launches an already-confirmed exact worktree/session.
func (a *App) StartWorktreeWorker(ctx context.Context, workspace WorktreeWorkspace, selected session.SessionInfo) (supervisor.WorkerState, error) {
	if a == nil || a.Supervisor == nil {
		return supervisor.WorkerState{}, errors.New("app: worktree supervisor unavailable")
	}
	if !sameWorkspacePath(selected.CWD, workspace.Path) {
		return supervisor.WorkerState{}, errors.New("app: session CWD does not match worktree")
	}
	providerID, model, _ := a.ActiveModelsSnapshot()
	return a.Supervisor.Start(ctx, supervisor.StartRequest{
		WorkspaceID: workspace.ID, SessionID: selected.ID, SessionPath: selected.Path,
		WorktreePath: workspace.Path, Branch: workspace.Branch,
		Provider: providerID, Model: model.ID, Thinking: a.Agent.Thinking(),
		ConfigPath: a.ConfigPath, AuthPath: a.AuthPath,
		RequireSandbox: a.requireSandbox, DisableSandbox: a.disableSandbox,
	})
}
