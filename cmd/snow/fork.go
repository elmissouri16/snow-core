package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/snow-core/snow/internal/artifact"
	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/worktree"
	"github.com/snow-core/snow/pkg/protocol"
)

func forkCmd() *cobra.Command {
	var opts protocol.SessionForkOptions
	cmd := &cobra.Command{
		Use:   "fork [session-path]",
		Short: "Create an independent session in the same workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, closeStore, err := openForkSource(cmd, args)
			if err != nil {
				return err
			}
			defer closeStore()
			index := session.NewFileIndex(session.DefaultSessionsRoot())
			child, result, err := index.CreateFork(store.Header().CWD, store, opts)
			if err != nil {
				return fmt.Errorf("fork: %w", err)
			}
			if err := copyCLIForkArtifacts(cmd.Context(), child, result); err != nil {
				_ = child.Close()
				removeExactSessionFiles(result.SessionPath)
				return fmt.Errorf("fork: %w", err)
			}
			if err := child.Close(); err != nil {
				return fmt.Errorf("fork: close child: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	addSessionForkFlags(cmd, &opts)
	return cmd
}

func forkWorktreeCmd() *cobra.Command {
	var opts protocol.SessionWorktreeForkOptions
	cmd := &cobra.Command{
		Use:   "fork-worktree [session-path]",
		Short: "Create a clean Git worktree and an independent session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, closeStore, err := openForkSource(cmd, args)
			if err != nil {
				return err
			}
			defer closeStore()
			created, err := worktree.Create(cmd.Context(), worktree.Request{SourceDir: store.Header().CWD, TargetDir: opts.WorktreePath, Branch: opts.GitBranch, Name: opts.Name})
			if err != nil {
				return fmt.Errorf("fork-worktree: %w", err)
			}
			forkOpts := protocol.SessionForkOptions{SourceBranchID: opts.SourceBranchID, FromEntryID: opts.FromEntryID, Name: opts.Name, DestinationPath: opts.DestinationPath}
			forkOpts.DestinationPath, err = worktree.ResolveSessionPath(created.TargetDir, forkOpts.DestinationPath)
			if err != nil {
				return errors.Join(fmt.Errorf("fork-worktree: %w", err), worktree.Remove(context.Background(), created))
			}
			index := session.NewFileIndex(session.DefaultSessionsRoot())
			child, result, err := index.CreateFork(created.TargetDir, store, forkOpts)
			if err != nil {
				return errors.Join(fmt.Errorf("fork-worktree: %w", err), worktree.Remove(context.Background(), created))
			}
			if err := copyCLIForkArtifacts(cmd.Context(), child, result); err != nil {
				_ = child.Close()
				removeExactSessionFiles(result.SessionPath)
				return errors.Join(fmt.Errorf("fork-worktree: %w", err), worktree.Remove(context.Background(), created))
			}
			if err := child.Close(); err != nil {
				removeExactSessionFiles(result.SessionPath)
				return errors.Join(fmt.Errorf("fork-worktree: close child: %w", err), worktree.Remove(context.Background(), created))
			}
			result.Worktree = &protocol.WorktreeInfo{Path: created.TargetDir, Branch: created.Branch, Commit: created.Commit}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	cmd.Flags().StringVar(&opts.FromEntryID, "from-entry", "", "entry id to fork (default: source branch tip)")
	cmd.Flags().StringVar(&opts.SourceBranchID, "source-branch", "", "source branch id (default: active branch)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "new session/worktree name")
	cmd.Flags().StringVar(&opts.DestinationPath, "destination", "", "absolute child SQLite path (must not exist; default uses Snow's private session root)")
	cmd.Flags().StringVar(&opts.WorktreePath, "worktree", "", "worktree path (default: generated sibling path)")
	cmd.Flags().StringVar(&opts.GitBranch, "git-branch", "", "new Git branch (default: generated snow/* name)")
	return cmd
}

func addSessionForkFlags(cmd *cobra.Command, opts *protocol.SessionForkOptions) {
	cmd.Flags().StringVar(&opts.FromEntryID, "from-entry", "", "entry id to fork (default: source branch tip)")
	cmd.Flags().StringVar(&opts.SourceBranchID, "source-branch", "", "source branch id (default: active branch)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "new session display name")
	cmd.Flags().StringVar(&opts.DestinationPath, "destination", "", "child SQLite path (must not exist)")
}

func openForkSource(cmd *cobra.Command, args []string) (session.Store, func(), error) {
	path, _ := cmd.Flags().GetString("session")
	if len(args) == 1 {
		if cmd.Flags().Changed("session") {
			return nil, func() {}, errors.New("session path provided both as an argument and with --session")
		}
		path = args[0]
	}
	if path == "" {
		infos, err := session.NewFileIndex(session.DefaultSessionsRoot()).List(mustCWD())
		if err != nil {
			return nil, func() {}, err
		}
		if len(infos) == 0 {
			return nil, func() {}, errors.New("no saved sessions for this directory")
		}
		path = infos[0].Path
	}
	if err := session.ValidateSQLiteSession(path); err != nil {
		return nil, func() {}, fmt.Errorf("session %q: %w", path, err)
	}
	store, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return store, func() { _ = store.Close() }, nil
}

func copyCLIForkArtifacts(ctx context.Context, child session.Store, result protocol.SessionForkResult) error {
	ids, err := session.ForkArtifactIDs(child)
	if err != nil || len(ids) == 0 {
		return err
	}
	// Config validation caps artifacts at 64 MiB. Opening at that cap lets this
	// provider-free command preserve artifacts created under any valid profile.
	store, err := artifact.NewLocalStore(filepath.Join(config.GlobalDir(), "artifacts"), 64<<20)
	if err != nil {
		return err
	}
	defer store.Close()
	for _, id := range ids {
		if err := store.CopyText(ctx, result.SourceSessionID, result.SessionID, id); err != nil {
			return fmt.Errorf("copy artifact %s: %w", id, err)
		}
	}
	return nil
}

func removeExactSessionFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}
