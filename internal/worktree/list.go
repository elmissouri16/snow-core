package worktree

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	maxLinkedWorktrees = 64
	dirtyConcurrency   = 4
)

var ErrInventoryTooLarge = errors.New("worktree: linked-worktree inventory exceeds limits")

// Linked describes one entry returned by Git's stable porcelain worktree
// inventory. Path and repository identities are canonical whenever they exist.
type Linked struct {
	ID             string
	Path           string
	Head           string
	Branch         string
	BranchRef      string
	Current        bool
	Detached       bool
	Bare           bool
	Locked         bool
	LockReason     string
	Prunable       bool
	PrunableReason string
	Dirty          bool
	StatusError    string
}

// Inventory identifies one repository and its linked worktrees.
type Inventory struct {
	CommonDir   string
	CurrentRoot string
	Worktrees   []Linked
}

// List returns the linked Git worktrees for source without mutating repository
// state. It uses only direct Git arguments and stable NUL porcelain output.
func List(ctx context.Context, source string) (Inventory, error) {
	return list(ctx, gitRunner{}, source)
}

func list(ctx context.Context, git runner, source string) (Inventory, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return Inventory{}, fmt.Errorf("worktree: resolve source: %w", err)
	}
	rootOutput, err := git.Run(ctx, absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Inventory{}, err
		}
		return Inventory{}, fmt.Errorf("%w: %v", ErrNotRepository, err)
	}
	if outputTruncated(rootOutput) {
		return Inventory{}, ErrInventoryTooLarge
	}
	currentRoot, err := canonicalExisting(strings.TrimSpace(string(rootOutput)))
	if err != nil {
		return Inventory{}, fmt.Errorf("worktree: canonical current root: %w", err)
	}
	commonOutput, err := git.Run(ctx, currentRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return Inventory{}, fmt.Errorf("worktree: resolve common directory: %w", err)
	}
	if outputTruncated(commonOutput) {
		return Inventory{}, ErrInventoryTooLarge
	}
	commonDir := strings.TrimSpace(string(commonOutput))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(currentRoot, commonDir)
	}
	commonDir, err = canonicalExisting(commonDir)
	if err != nil {
		return Inventory{}, fmt.Errorf("worktree: canonical common directory: %w", err)
	}

	output, err := git.Run(ctx, currentRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Inventory{}, err
	}
	if outputTruncated(output) {
		return Inventory{}, ErrInventoryTooLarge
	}
	entries, err := parsePorcelainZ(output)
	if err != nil {
		return Inventory{}, err
	}
	if len(entries) > maxLinkedWorktrees {
		return Inventory{}, fmt.Errorf("%w: got %d, maximum %d", ErrInventoryTooLarge, len(entries), maxLinkedWorktrees)
	}

	seen := make(map[string]bool, len(entries))
	worktrees := make([]Linked, 0, len(entries))
	for _, entry := range entries {
		path := entry.Path
		if existing, canonicalErr := canonicalExisting(path); canonicalErr == nil {
			path = existing
		} else if absolutePath, absoluteErr := filepath.Abs(path); absoluteErr == nil {
			path = filepath.Clean(absolutePath)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		entry.Path = path
		entry.Current = samePath(path, currentRoot)
		entry.ID = linkedID(commonDir, path)
		worktrees = append(worktrees, entry)
	}
	populateDirty(ctx, git, worktrees)
	sort.SliceStable(worktrees, func(i, j int) bool {
		if worktrees[i].Current != worktrees[j].Current {
			return worktrees[i].Current
		}
		if worktrees[i].Branch != worktrees[j].Branch {
			return worktrees[i].Branch < worktrees[j].Branch
		}
		return worktrees[i].Path < worktrees[j].Path
	})
	return Inventory{CommonDir: commonDir, CurrentRoot: currentRoot, Worktrees: worktrees}, nil
}

func parsePorcelainZ(output []byte) ([]Linked, error) {
	var entries []Linked
	var current *Linked
	flush := func() error {
		if current == nil {
			return nil
		}
		if strings.TrimSpace(current.Path) == "" {
			return errors.New("worktree: malformed porcelain record without path")
		}
		entries = append(entries, *current)
		current = nil
		return nil
	}
	for _, raw := range strings.Split(string(output), "\x00") {
		if raw == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		key, value, _ := strings.Cut(raw, " ")
		switch key {
		case "worktree":
			if current != nil {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			current = &Linked{Path: value}
		case "HEAD":
			if current == nil {
				return nil, errors.New("worktree: malformed porcelain field before worktree")
			}
			current.Head = value
		case "branch":
			if current == nil {
				return nil, errors.New("worktree: malformed porcelain field before worktree")
			}
			current.BranchRef = value
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			if current == nil {
				return nil, errors.New("worktree: malformed porcelain field before worktree")
			}
			current.Detached = true
		case "bare":
			if current == nil {
				return nil, errors.New("worktree: malformed porcelain field before worktree")
			}
			current.Bare = true
		case "locked":
			if current == nil {
				return nil, errors.New("worktree: malformed porcelain field before worktree")
			}
			current.Locked = true
			current.LockReason = value
		case "prunable":
			if current == nil {
				return nil, errors.New("worktree: malformed porcelain field before worktree")
			}
			current.Prunable = true
			current.PrunableReason = value
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return entries, nil
}

func populateDirty(ctx context.Context, git runner, worktrees []Linked) {
	sem := make(chan struct{}, dirtyConcurrency)
	var wg sync.WaitGroup
	for i := range worktrees {
		if worktrees[i].Bare || worktrees[i].Prunable {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				worktrees[index].StatusError = ctx.Err().Error()
				return
			}
			output, err := git.Run(ctx, worktrees[index].Path, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
			if err != nil {
				worktrees[index].StatusError = err.Error()
				return
			}
			if outputTruncated(output) {
				worktrees[index].StatusError = ErrInventoryTooLarge.Error()
				return
			}
			worktrees[index].Dirty = len(output) > 0
		}(i)
	}
	wg.Wait()
}

func canonicalExisting(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func samePath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func linkedID(commonDir, path string) string {
	sum := sha256.Sum256([]byte(commonDir + "\x00" + path))
	return fmt.Sprintf("workspace-%x", sum[:12])
}

func outputTruncated(output []byte) bool {
	return strings.HasSuffix(string(output), gitTruncatedSentinel)
}
