package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

func TestRemovePreservesWorktreeWithUserChanges(t *testing.T) {
	repo := newRepository(t)
	target := filepath.Join(filepath.Dir(repo), "dirty rollback worktree")
	result, err := Create(context.Background(), Request{SourceDir: repo, TargetDir: target, Branch: "snow/dirty-rollback"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", repo, "worktree", "remove", "--force", target).Run()
		_ = exec.Command("git", "-C", repo, "branch", "-D", result.Branch).Run()
	})
	if err := os.WriteFile(filepath.Join(target, "README.md"), []byte("user change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(context.Background(), result); err == nil {
		t.Fatal("rollback removed a worktree containing user changes")
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatalf("dirty worktree was not preserved: %v", err)
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

func TestResolveSessionPathRequiresAbsoluteExplicitDestination(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"sessions/child.db", "../outside.db"} {
		if _, err := ResolveSessionPath(root, relative); !errors.Is(err, ErrUnsafeDestination) {
			t.Fatalf("relative %q error=%v", relative, err)
		}
	}
	if got, err := ResolveSessionPath(root, ""); err != nil || got != "" {
		t.Fatalf("default destination=%q err=%v", got, err)
	}
	absolute := filepath.Join(t.TempDir(), "explicit.db")
	if got, err := ResolveSessionPath(root, absolute); err != nil || got != absolute {
		t.Fatalf("absolute=%q err=%v", got, err)
	}
}

func TestResolveSessionPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "sessions")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ResolveSessionPath(root, "sessions/child.db"); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("symlink escape error=%v", err)
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

func TestBoundedGitOutputCapsRetainedBytes(t *testing.T) {
	var output boundedGitOutput
	payload := bytes.Repeat([]byte("x"), maxGitOutput*4)
	if n, err := output.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	got := output.Bytes()
	marker := []byte("\n… output truncated")
	if len(got) != maxGitOutput+len(marker) {
		t.Fatalf("retained bytes = %d, want %d", len(got), maxGitOutput+len(marker))
	}
	if !bytes.Equal(got[maxGitOutput:], marker) {
		t.Fatalf("missing truncation marker: %q", got[maxGitOutput:])
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
