package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func pluginBranchID(store session.Store) string {
	if store == nil {
		return ""
	}
	if active, ok := store.(session.ActiveBranchStore); ok {
		return active.ActiveBranchID()
	}
	return ""
}

func (a *App) pluginTransitionRequest(operation string, next session.Store) plugin.SessionChange {
	return plugin.SessionChange{Operation: operation, OldSessionID: a.Session.ID(), NewSessionID: next.ID(), OldBranchID: pluginBranchID(a.Session), NewBranchID: pluginBranchID(next)}
}

// beforePluginSessionChange runs under admission and the exclusive plugin
// session guard. Pure hooks see old-branch workflow state and cannot perform
// host operations while permission, goal, process and store bindings are old.
func (a *App) beforePluginSessionChange(ctx context.Context, change plugin.SessionChange) error {
	if a.PluginManager == nil || !a.PluginManager.HasHook("before_session_change", false) {
		return nil
	}
	_, _, err := a.PluginManager.RunHooksWithWorkflow(ctx, plugin.HookRequest{Phase: "before_session_change", SessionChange: &change}, a.loadPluginWorkflow)
	if err != nil {
		return fmt.Errorf("app: session change hook: %w", err)
	}
	return ctx.Err()
}

func (a *App) pluginTransitionNotification(change plugin.SessionChange) *protocol.PluginSessionChanged {
	generation := uint64(0)
	if a.extensions != nil {
		generation = a.extensions.generation.Load()
	}
	return &protocol.PluginSessionChanged{OldSessionID: change.OldSessionID, NewSessionID: a.Session.ID(), OldBranchID: change.OldBranchID, NewBranchID: pluginBranchID(a.Session), Reason: change.Operation, Generation: generation}
}

// Defer this before acquiring transition locks. Publication then occurs only
// after every lock is released and the new generation's hosts are bound.
func (a *App) publishPluginSessionChange(change **protocol.PluginSessionChanged) {
	if *change != nil {
		a.Agent.Publish(protocol.AgentEvent{Type: protocol.EvPluginSessionChanged, PluginSessionChanged: *change})
	}
}

func (a *App) validatePluginSessionTarget(store session.Store) error {
	if state, ok := store.(session.ThreadStateStore); ok {
		mode, err := state.CollaborationMode()
		if err != nil {
			return err
		}
		if _, err := protocol.ParseCollaborationMode(string(mode)); err != nil {
			return err
		}
	}
	if a.Subagents != nil {
		if _, ok := store.(session.SubagentTaskStore); !ok {
			return errors.New("app: session does not support subagent topology")
		}
	}
	return nil
}

func (a *App) validatePluginBranchTarget(branchID string) error {
	branches, ok := a.Session.(session.BranchStore)
	if !ok {
		return errors.New("app: session does not support durable branches")
	}
	listed, err := branches.Branches()
	if err != nil {
		return err
	}
	for _, branch := range listed {
		if branch.ID == branchID {
			return nil
		}
	}
	return session.ErrNotFound
}

// Validate the entire fork target before invoking a veto hook (and therefore
// before the agent stops an automatic goal). Existing fork stores remain the
// final authority and repeat their validation at mutation time.
func (a *App) validatePluginForkTarget(ctx context.Context, opts protocol.BranchForkOptions) (string, error) {
	branches, ok := a.Session.(session.BranchStore)
	if !ok {
		return "", errors.New("app: session does not support durable branches")
	}
	listed, err := branches.Branches()
	if err != nil {
		return "", err
	}
	sourceID := opts.SourceBranchID
	if sourceID == "" {
		sourceID = pluginBranchID(a.Session)
	}
	sourceTip := ""
	name := strings.TrimSpace(opts.Name)
	if name != "" {
		if len([]rune(name)) > 64 {
			return "", errors.New("app: branch name exceeds 64 runes")
		}
		for _, r := range name {
			if r < 0x20 || r == 0x7f {
				return "", errors.New("app: branch name contains control characters")
			}
		}
	}
	for _, branch := range listed {
		if name != "" && strings.EqualFold(name, branch.Name) {
			return "", errors.New("app: branch name already exists")
		}
		if branch.ID == sourceID || (sourceID == "" && branch.Active) {
			sourceTip = branch.TipID
		}
	}
	if sourceTip == "" {
		return "", session.ErrNotFound
	}
	from := opts.FromEntryID
	if from == "" {
		return sourceTip, nil
	}
	if from == sourceTip {
		return from, nil
	}
	if lookup, ok := a.Session.(session.BranchEntryLookup); ok {
		// Exact entry lookup also supports an explicitly selected inactive source
		// branch. Bound traversal and check cancellation between individual reads.
		seen := make(map[string]bool)
		for id := sourceTip; id != ""; {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if id == from {
				return from, nil
			}
			if seen[id] || len(seen) >= 100000 {
				return "", errors.New("app: fork ancestry exceeds validation bound or contains a cycle")
			}
			seen[id] = true
			entries, err := lookup.BranchEntriesByID([]string{id})
			if err != nil {
				return "", err
			}
			if len(entries) != 1 {
				return "", session.ErrNotFound
			}
			id = entries[0].ParentID
		}
		return "", errors.New("app: fork entry is not on source branch")
	}
	if sourceID == pluginBranchID(a.Session) {
		if entries, ok := a.Session.(session.BranchEntryStore); ok {
			path, err := entries.BranchEntries()
			if err != nil {
				return "", err
			}
			for _, entry := range path {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				if entry.ID == from {
					return from, nil
				}
			}
			return "", errors.New("app: fork entry is not on source branch")
		}
	}
	return "", errors.New("app: store cannot validate plugin-gated fork ancestry")
}

func pluginSessionChangeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// HookEnvironment is an immutable invocation snapshot for pure callbacks. The
// JavaScript runtime must prefer this optional method over Environment for hooks:
// the latter deliberately acquires sessionMu to admit effectful callbacks, while
// lifecycle gates already hold its exclusive side. Hosts are immutable and get
// replaced (never rebound in place) whenever the generation changes.
func (h *appExtensionHost) HookEnvironment() plugin.Environment {
	kind := "root"
	if h.child {
		kind = "child"
	}
	h.services.mu.Lock()
	ui := h.services.ui != nil && !h.child
	h.services.mu.Unlock()
	return plugin.Environment{SessionID: h.store.ID(), CWD: h.app.CWD(), Kind: kind, UI: ui, Generation: h.generation}
}
