package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestCopyForkArtifactsCopiesOwnedAndIgnoresForgedMissingMarker(t *testing.T) {
	store, err := artifact.NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const sourceID = "source"
	valid, err := store.SaveText(context.Background(), sourceID, "valid", "retained evidence")
	if err != nil {
		t.Fatal(err)
	}
	child := session.NewMemoryStore(session.Options{})
	forged := "artifact-ffffffffffffffffffffffffffffffff"
	summary := "Full retained tool result: " + forged + "\nFull retained tool result: " + valid.ID
	if err := child.Append(session.Entry{Type: session.EntryCompaction, ID: "checkpoint", Summary: summary, CompactedThrough: "root"}); err != nil {
		t.Fatal(err)
	}
	result := protocol.SessionForkResult{SourceSessionID: sourceID, SessionID: child.ID()}
	if err := copyForkArtifacts(context.Background(), store, child, result); err != nil {
		t.Fatalf("artifact copy failed: %v", err)
	}
	copied, err := store.ReadText(context.Background(), child.ID(), valid.ID)
	if err != nil || copied != "retained evidence" {
		t.Fatalf("copied artifact=%q err=%v", copied, err)
	}
}

func TestCopyForkArtifactsIgnoresUnboundedForgedMarkerSet(t *testing.T) {
	store, err := artifact.NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	child := session.NewMemoryStore(session.Options{})
	var summary strings.Builder
	for i := 0; i < maxForkArtifactCopies+1; i++ {
		fmt.Fprintf(&summary, "Full retained tool result: artifact-%032x\n", i)
	}
	if err := child.Append(session.Entry{Type: session.EntryCompaction, ID: "checkpoint", Summary: summary.String(), CompactedThrough: "root"}); err != nil {
		t.Fatal(err)
	}
	result := protocol.SessionForkResult{SourceSessionID: "source", SessionID: child.ID()}
	if err := copyForkArtifacts(context.Background(), store, child, result); err != nil {
		t.Fatalf("forged marker set blocked fork artifact copy: %v", err)
	}
}

type listedForkArtifactStore struct {
	ids    []string
	copies int
}

func (s *listedForkArtifactStore) SaveText(context.Context, string, string, string) (artifact.Ref, error) {
	return artifact.Ref{}, nil
}
func (s *listedForkArtifactStore) ReadText(context.Context, string, string) (string, error) {
	return "", nil
}
func (s *listedForkArtifactStore) Exists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *listedForkArtifactStore) ListIDs(context.Context, string) ([]string, error) {
	return append([]string(nil), s.ids...), nil
}
func (s *listedForkArtifactStore) CopyText(context.Context, string, string, string) error {
	s.copies++
	return nil
}
func (s *listedForkArtifactStore) Close() error { return nil }

func TestCopyForkArtifactsRejectsTooManyVerifiedArtifactsWithoutPartialCopy(t *testing.T) {
	store := &listedForkArtifactStore{}
	child := session.NewMemoryStore(session.Options{})
	var summary strings.Builder
	for i := 0; i < maxForkArtifactCopies+1; i++ {
		id := fmt.Sprintf("artifact-%032x", i)
		store.ids = append(store.ids, id)
		fmt.Fprintf(&summary, "Full retained tool result: %s\n", id)
	}
	if err := child.Append(session.Entry{Type: session.EntryCompaction, ID: "checkpoint", Summary: summary.String(), CompactedThrough: "root"}); err != nil {
		t.Fatal(err)
	}
	result := protocol.SessionForkResult{SourceSessionID: "source", SessionID: child.ID()}
	if err := copyForkArtifacts(context.Background(), store, child, result); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("verified artifact cap error=%v", err)
	}
	if store.copies != 0 {
		t.Fatalf("partial artifacts copied before cap failure: %d", store.copies)
	}
}

func TestForkWorktreeCreatesDetachedProjectSession(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("SNOW_HOME", filepath.Join(home, "snow-home"))
	repo := filepath.Join(home, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-C", repo}, args...)...)
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

	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: repo, NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	sourceID := a.Session.ID()
	ref, err := a.artifacts.SaveText(context.Background(), sourceID, "fork-test", "full retained output")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Session.Append(session.Entry{Type: session.EntryCompaction, ID: "compact-1", ParentID: "root", Summary: "summary\nFull retained tool result: " + ref.ID + "\nUse artifact_read or artifact_grep to inspect it.", CompactedThrough: "root"}); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(home, "occupied.db")
	if err := os.WriteFile(occupied, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	failedTarget := filepath.Join(home, "failed-child")
	if _, err := a.ForkWorktree(context.Background(), protocol.SessionWorktreeForkOptions{Name: "failed", WorktreePath: failedTarget, GitBranch: "snow/app-fail", DestinationPath: occupied}); err == nil {
		t.Fatal("expected destination collision")
	}
	if _, err := os.Lstat(failedTarget); !os.IsNotExist(err) {
		t.Fatalf("failed worktree was not rolled back: %v", err)
	}
	if err := exec.Command(git, "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/snow/app-fail").Run(); err == nil {
		t.Fatal("failed worktree branch was not rolled back")
	}
	if _, err := a.ForkWorktree(context.Background(), protocol.SessionWorktreeForkOptions{Name: "escape", WorktreePath: filepath.Join(home, "escape-child"), GitBranch: "snow/app-escape", DestinationPath: "../escape.db"}); err == nil {
		t.Fatal("expected relative destination escape rejection")
	}
	if _, err := os.Lstat(filepath.Join(home, "escape.db")); !os.IsNotExist(err) {
		t.Fatalf("escaped session database exists: %v", err)
	}

	target := filepath.Join(home, "child")
	result, err := a.ForkWorktree(context.Background(), protocol.SessionWorktreeForkOptions{Name: "child", WorktreePath: target, GitBranch: "snow/app-fork"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceSessionID != sourceID || result.SessionID == sourceID || result.Worktree == nil || result.Worktree.Branch != "snow/app-fork" {
		t.Fatalf("result=%+v", result)
	}
	if a.CWD() != repo || a.Session.ID() != sourceID {
		t.Fatalf("active app was retargeted: cwd=%q session=%q", a.CWD(), a.Session.ID())
	}
	if err := session.ValidateSQLiteSession(result.SessionPath); err != nil {
		t.Fatal(err)
	}
	if copied, err := a.artifacts.ReadText(context.Background(), result.SessionID, ref.ID); err != nil || copied != "full retained output" {
		t.Fatalf("copied artifact=%q err=%v", copied, err)
	}
	child, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(result.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if child.Header().CWD != result.Worktree.Path || child.Header().ParentSessionID != sourceID {
		t.Fatalf("child header=%+v", child.Header())
	}
}
