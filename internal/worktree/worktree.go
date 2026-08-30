// Package worktree creates isolated Git worktrees for detached session forks.
// It invokes Git directly without a shell and never reuses an existing path or
// branch.
package worktree

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotRepository     = errors.New("worktree: source is not a Git repository")
	ErrDirty             = errors.New("worktree: source has uncommitted changes")
	ErrDestinationExists = errors.New("worktree: destination already exists")
	ErrUnsafeDestination = errors.New("worktree: unsafe destination")
)

const (
	defaultTimeout = 30 * time.Second
	maxGitOutput   = 32 * 1024
)

// Request describes one new Git worktree. Empty target and branch values are
// generated from Name with a collision-resistant suffix. Dirty source trees are
// rejected because Git does not transfer uncommitted state to a new worktree.
type Request struct {
	SourceDir string
	TargetDir string
	Branch    string
	Base      string
	Name      string
}

// Result identifies a worktree created by Create.
type Result struct {
	SourceRoot string
	TargetDir  string
	Branch     string
	Commit     string
}

type runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type gitRunner struct{}

type boundedGitOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (w *boundedGitOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := maxGitOutput - w.buf.Len()
	if remaining > 0 {
		_, _ = w.buf.Write(p[:min(remaining, len(p))])
	}
	if len(p) > remaining {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedGitOutput) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	output := slices.Clone(w.buf.Bytes())
	if w.truncated {
		output = append(output, []byte("\n… output truncated")...)
	}
	return output
}

func (gitRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.WaitDelay = 2 * time.Second
	var captured boundedGitOutput
	cmd.Stdout, cmd.Stderr = &captured, &captured
	err := cmd.Run()
	output := captured.Bytes()
	if err != nil {
		if ctx.Err() != nil {
			return output, ctx.Err()
		}
		return output, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// Create validates the source and atomically delegates worktree registration to
// Git. It never creates or replaces an existing destination path.
func Create(ctx context.Context, req Request) (Result, error) {
	return create(ctx, gitRunner{}, req)
}

func create(ctx context.Context, git runner, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	source, err := filepath.Abs(req.SourceDir)
	if err != nil {
		return Result{}, fmt.Errorf("worktree: resolve source: %w", err)
	}
	rootOut, err := git.Run(ctx, source, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("%w: %v", ErrNotRepository, err)
	}
	sourceRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(rootOut)))
	if err != nil {
		return Result{}, fmt.Errorf("worktree: canonical source: %w", err)
	}
	bare, err := git.Run(ctx, sourceRoot, "rev-parse", "--is-bare-repository")
	if err != nil || strings.TrimSpace(string(bare)) != "false" {
		return Result{}, ErrNotRepository
	}
	status, err := git.Run(ctx, sourceRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(status)) != "" {
		return Result{}, ErrDirty
	}
	base := strings.TrimSpace(req.Base)
	if base == "" {
		base = "HEAD"
	}
	commitOut, err := git.Run(ctx, sourceRoot, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return Result{}, fmt.Errorf("worktree: resolve base %q: %w", base, err)
	}
	commit := strings.TrimSpace(string(commitOut))

	suffix := randomSuffix()
	slug := slugify(req.Name)
	if slug == "" {
		slug = "fork"
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = "snow/" + slug + "-" + suffix
	}
	if _, err := git.Run(ctx, sourceRoot, "check-ref-format", "--branch", branch); err != nil {
		return Result{}, fmt.Errorf("worktree: invalid branch %q: %w", branch, err)
	}

	target := strings.TrimSpace(req.TargetDir)
	if target == "" {
		target = filepath.Join(filepath.Dir(sourceRoot), filepath.Base(sourceRoot)+"-"+slug+"-"+suffix)
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(sourceRoot, target)
	}
	target = filepath.Clean(target)
	if _, err := os.Lstat(target); err == nil {
		return Result{}, ErrDestinationExists
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("worktree: inspect destination: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return Result{}, fmt.Errorf("worktree: canonical destination parent: %w", err)
	}
	target = filepath.Join(parent, filepath.Base(target))
	if pathsOverlap(sourceRoot, target) {
		return Result{}, ErrUnsafeDestination
	}

	result := Result{SourceRoot: sourceRoot, TargetDir: target, Branch: branch, Commit: commit}
	branchRef := "refs/heads/" + branch
	// Create the ref separately with a create-only compare-and-swap. This gives
	// rollback an exact ownership boundary even when worktree add is canceled
	// after creating only part of its destination.
	if _, err := git.Run(ctx, sourceRoot, "update-ref", "-m", "snow worktree fork", branchRef, commit, ""); err != nil {
		return Result{}, fmt.Errorf("worktree: create branch %q: %w", branch, err)
	}
	if _, err := git.Run(ctx, sourceRoot, "worktree", "add", target, branch); err != nil {
		// A canceled Git process can finish registration just before termination.
		// Only remove a path registered to the exact branch. Otherwise remove the
		// still-unmoved ref with CAS and preserve any partial unregistered path for
		// manual inspection rather than guessing filesystem ownership.
		if registeredExactWorktree(context.Background(), gitRunner{}, result) {
			return Result{}, errors.Join(err, Remove(context.Background(), result))
		}
		cleanupErr := deleteCreatedBranch(context.Background(), gitRunner{}, result)
		var partialErr error
		if _, statErr := os.Lstat(target); statErr == nil {
			partialErr = fmt.Errorf("worktree: Git left an unregistered partial destination at %s; inspect it manually", target)
		} else if !os.IsNotExist(statErr) {
			partialErr = fmt.Errorf("worktree: inspect partial destination %s: %w", target, statErr)
		}
		return Result{}, errors.Join(err, cleanupErr, partialErr)
	}
	verified, verifyErr := git.Run(ctx, target, "rev-parse", "--show-toplevel")
	if verifyErr == nil && filepath.Clean(strings.TrimSpace(string(verified))) != target {
		verifyErr = errors.New("worktree: Git returned an unexpected destination")
	}
	verifiedBranch, branchErr := git.Run(ctx, target, "symbolic-ref", "--quiet", "--short", "HEAD")
	if verifyErr == nil && branchErr != nil {
		verifyErr = branchErr
	}
	if verifyErr == nil && strings.TrimSpace(string(verifiedBranch)) != branch {
		verifyErr = errors.New("worktree: Git checked out an unexpected branch")
	}
	verifiedCommit, commitErr := git.Run(ctx, target, "rev-parse", "--verify", "HEAD^{commit}")
	if verifyErr == nil && commitErr != nil {
		verifyErr = commitErr
	}
	if verifyErr == nil && strings.TrimSpace(string(verifiedCommit)) != commit {
		verifyErr = errors.New("worktree: Git checked out an unexpected commit")
	}
	if verifyErr != nil {
		rollbackErr := Remove(context.Background(), result)
		return Result{}, errors.Join(verifyErr, rollbackErr)
	}
	return result, nil
}

func registeredExactWorktree(ctx context.Context, git runner, result Result) bool {
	output, err := git.Run(ctx, result.SourceRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	wantBranch := "refs/heads/" + result.Branch
	for record := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n\n") {
		path, branch := "", ""
		for line := range strings.SplitSeq(record, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "branch "):
				branch = strings.TrimPrefix(line, "branch ")
			}
		}
		if filepath.Clean(path) == filepath.Clean(result.TargetDir) && branch == wantBranch {
			return true
		}
	}
	return false
}

func deleteCreatedBranch(ctx context.Context, git runner, result Result) error {
	if result.Branch == "" || result.Commit == "" {
		return errors.New("worktree: incomplete branch rollback identity")
	}
	_, err := git.Run(ctx, result.SourceRoot, "update-ref", "-d", "refs/heads/"+result.Branch, result.Commit)
	if err != nil {
		return fmt.Errorf("worktree: delete unchanged created branch: %w", err)
	}
	return nil
}

// Remove rolls back an exact worktree created by Create and then deletes its
// unchanged branch with compare-and-swap. Git remains the authority; Snow never
// calls os.RemoveAll or deletes a branch that moved after creation.
func Remove(ctx context.Context, result Result) error {
	if result.SourceRoot == "" || result.TargetDir == "" || result.Branch == "" {
		return errors.New("worktree: incomplete rollback identity")
	}
	git := gitRunner{}
	if !registeredExactWorktree(ctx, git, result) {
		return errors.New("worktree: rollback target is no longer the created worktree")
	}
	head, err := git.Run(ctx, result.TargetDir, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != result.Commit {
		return errors.New("worktree: rollback target commit changed")
	}
	var errs []error
	if _, err := git.Run(ctx, result.SourceRoot, "worktree", "remove", result.TargetDir); err != nil {
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	if err := deleteCreatedBranch(ctx, git, result); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ResolveSessionPath resolves a relative child-session path inside a newly
// created worktree. Absolute paths remain explicit operator choices; relative
// traversal outside root is rejected.
func ResolveSessionPath(_ string, requested string) (string, error) {
	if requested == "" || filepath.IsAbs(requested) {
		return requested, nil
	}
	// SQLite opens its database and sidecars by pathname. Returning a validated
	// relative path would reintroduce a symlink-swap window after this function
	// releases its pinned root. Until the SQLite store can open through an
	// os.Root handle, fail closed and require an explicit absolute destination.
	return "", ErrUnsafeDestination
}

func pathsOverlap(left, right string) bool {
	contains := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return filepath.Clean(left) == filepath.Clean(right) || contains(left, right) || contains(right, left)
}

var invalidSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = invalidSlug.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-_")
	if len(value) > 40 {
		value = strings.Trim(value[:40], ".-_")
	}
	return value
}

func randomSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b[:])
}
