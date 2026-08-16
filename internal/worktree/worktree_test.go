package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListLinkedWorktreesPorcelainAndDirtyState(t *testing.T) {
	repo := newRepository(t)
	target := filepath.Join(filepath.Dir(repo), "linked worktree with spaces")
	command := exec.Command("git", "-C", repo, "worktree", "add", "-b", "snow/list-test", target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", target).Run() })
	if err := os.WriteFile(filepath.Join(target, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repo, "worktree", "lock", "--reason", "test lock", target).CombinedOutput(); err != nil {
		t.Fatalf("git worktree lock: %v: %s", err, output)
	}

	inventory, err := List(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.CommonDir == "" || len(inventory.Worktrees) != 2 || !inventory.Worktrees[0].Current {
		t.Fatalf("inventory = %+v", inventory)
	}
	var linked *Linked
	for i := range inventory.Worktrees {
		if inventory.Worktrees[i].Branch == "snow/list-test" {
			linked = &inventory.Worktrees[i]
		}
	}
	if linked == nil {
		t.Fatalf("missing linked branch: %+v", inventory.Worktrees)
	}
	canonicalTarget, canonicalErr := filepath.EvalSymlinks(target)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if linked.ID == "" || linked.Path != canonicalTarget || !linked.Dirty || !linked.Locked || linked.LockReason != "test lock" {
		t.Fatalf("linked = %+v", *linked)
	}
}

func TestParsePorcelainZPreservesPathsAndFlags(t *testing.T) {
	output := []byte("worktree /tmp/a b\nline\x00HEAD deadbeef\x00detached\x00locked reason here\x00\x00worktree /tmp/c\x00HEAD cafe\x00branch refs/heads/snow/c\x00prunable missing\x00\x00")
	entries, err := parsePorcelainZ(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Path != "/tmp/a b\nline" || !entries[0].Detached || entries[0].LockReason != "reason here" {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[1].Branch != "snow/c" || !entries[1].Prunable || entries[1].PrunableReason != "missing" {
		t.Fatalf("second = %+v", entries[1])
	}
}

func TestCreateAndRemove(t *testing.T) {
	repo := newRepository(t)
	target := filepath.Join(filepath.Dir(repo), "isolated worktree")
	result, err := Create(context.Background(), Request{
		SourceDir: repo,
		TargetDir: "../isolated worktree",
		Branch:    "snow/test-worktree",
		Name:      "test worktree",
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalTarget, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	canonicalTarget = filepath.Join(canonicalTarget, filepath.Base(target))
	if result.TargetDir != canonicalTarget || result.Branch != "snow/test-worktree" || result.Commit == "" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatalf("created worktree missing committed file: %v", err)
	}
	if !registeredExactWorktree(context.Background(), gitRunner{}, result) {
		t.Fatal("created worktree was not identified by exact target and branch")
	}
	if err := Remove(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target remains after removal: %v", err)
	}
}

func TestCreateRejectsDirtyAndUnsafeDestinations(t *testing.T) {
	repo := newRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Request{SourceDir: repo}); !errors.Is(err, ErrDirty) {
		t.Fatalf("dirty error = %v", err)
	}
	if err := os.Remove(filepath.Join(repo, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Request{SourceDir: repo, TargetDir: filepath.Join(repo, "nested")}); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestCreateRejectsExistingDestinationAndNonRepository(t *testing.T) {
	repo := newRepository(t)
	target := filepath.Join(filepath.Dir(repo), "occupied")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Request{SourceDir: repo, TargetDir: target}); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("collision error = %v", err)
	}
	if _, err := Create(context.Background(), Request{SourceDir: t.TempDir()}); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("non-repository error = %v", err)
	}
}

func TestResolveSessionPathRejectsRelativeEscape(t *testing.T) {
	root := t.TempDir()
	inside, err := ResolveSessionPath(root, "sessions/child.db")
	if err != nil || inside != filepath.Join(root, "sessions", "child.db") {
		t.Fatalf("inside=%q err=%v", inside, err)
	}
	if _, err := ResolveSessionPath(root, "../outside.db"); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("escape error=%v", err)
	}
	absolute := filepath.Join(t.TempDir(), "explicit.db")
	if got, err := ResolveSessionPath(root, absolute); err != nil || got != absolute {
		t.Fatalf("absolute=%q err=%v", got, err)
	}
}

func TestCreatePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := create(ctx, canceledRunner{}, Request{SourceDir: t.TempDir()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

type canceledRunner struct{}

func (canceledRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, ctx.Err()
}

func newRepository(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "snow@example.invalid")
	runGit("config", "user.name", "Snow Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("snow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-q", "-m", "initial")
	return repo
}
