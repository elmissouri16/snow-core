//go:build darwin || linux

package web

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func inspectionTestGit(t *testing.T, p Project, args ...string) string {
	t.Helper()
	executable := "/usr/bin/git"
	if _, err := os.Stat(executable); err != nil {
		t.Skip("fixed trusted Git executable unavailable")
	}
	fixed := []string{"-c", "user.name=Inspection Test", "-c", "user.email=inspection@example.invalid", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false"}
	cmd := exec.CommandContext(t.Context(), executable, append(fixed, args...)...)
	cmd.Dir = p.Path
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "LC_ALL=C"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func inspectionTestRepository(t *testing.T) Project {
	t.Helper()
	p := inspectionTestProject(t)
	inspectionTestGit(t, p, "init", "--template=")
	inspectionTestWrite(t, p, "file", "original\n")
	inspectionTestGit(t, p, "add", "--", "file")
	inspectionTestGit(t, p, "commit", "-m", "Initial")
	return p
}

func TestInspectionGitNeverExecutesConfiguration(t *testing.T) {
	p := inspectionTestRepository(t)
	inspectionTestWrite(t, p, "file", "staged\n")
	inspectionTestGit(t, p, "add", "--", "file")
	inspectionTestWrite(t, p, "file", "unstaged\n")
	inspectionTestWrite(t, p, "new file", "untracked\n")
	inspectionTestWrite(t, p, ".env.example", "hidden secret")
	hostile := t.TempDir()
	marker := filepath.Join(hostile, "executed")
	executable := filepath.Join(hostile, "git")
	script := "#!/bin/sh\nprintf executed > '" + marker + "'\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"post-index-change", "pre-auto-gc", "fsmonitor-watchman"} {
		if err := os.WriteFile(filepath.Join(hostile, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	config := "[core]\n repositoryformatversion = 0\n bare = false\n fsmonitor = " + executable + "\n hooksPath = " + hostile + "\n[filter \"hostile\"]\n clean = " + executable + "\n process = " + executable + "\n required = true\n[diff]\n external = " + executable + "\n[diff \"hostile\"]\n textconv = " + executable + "\n[include]\n path = " + filepath.Join(hostile, "included") + "\n"
	inspectionTestWrite(t, p, ".git/config", config)
	inspectionTestWrite(t, p, ".gitattributes", "file filter=hostile diff=hostile\n")
	if err := os.WriteFile(filepath.Join(hostile, "included"), []byte("[core]\n fsmonitor = "+executable+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostile, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(p.Path, ".git/index")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	t.Setenv("PATH", hostile)
	t.Setenv("HOME", hostile)
	t.Setenv("XDG_CONFIG_HOME", hostile)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(hostile, "config"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(hostile, "config"))
	t.Setenv("GIT_EXTERNAL_DIFF", executable)
	t.Setenv("GIT_DIR", filepath.Join(hostile, "not-a-repo"))
	t.Setenv("GIT_WORK_TREE", hostile)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
	t.Setenv("GIT_CONFIG_VALUE_0", executable)
	changes, err := InspectChanges(t.Context(), p)
	if err != nil || !changes.Available || changes.Reason != inspectionGitRaw {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	found := map[string]bool{}
	for _, change := range changes.Changes {
		found[change.Kind+":"+change.Path] = true
	}
	for _, key := range []string{"staged:file", "unstaged:file", "untracked:new file"} {
		if !found[key] {
			t.Errorf("missing %s in %+v", key, changes)
		}
	}
	if found["untracked:.env.example"] {
		t.Fatal("credential path disclosed")
	}
	for kind, want := range map[string]string{"staged": "+staged", "unstaged": "+unstaged"} {
		diff, err := InspectDiff(t.Context(), p, "file", kind)
		if err != nil || !diff.Available || !strings.Contains(diff.Text, want) {
			t.Fatalf("%s diff=%+v err=%v", kind, diff, err)
		}
	}
	preview, err := InspectDiff(t.Context(), p, "new file", "untracked")
	if err != nil || !preview.Available || preview.Text != "untracked\n" || !strings.Contains(preview.Reason, "plain text") {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project command executed: %v", err)
	}
	after, err := os.ReadFile(indexPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("original index changed")
	}
	afterInfo, err := os.Stat(indexPath)
	if err != nil || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) || !os.SameFile(beforeInfo, afterInfo) {
		t.Fatal("original index identity/mtime changed")
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary artifacts=%v err=%v", entries, err)
	}
	if _, err := InspectDiff(t.Context(), p, "file", "--ext-diff"); !errors.Is(err, ErrInspectionPath) {
		t.Fatalf("kind injection=%v", err)
	}
}

func TestInspectionGitUnsupportedLayoutsFailClosed(t *testing.T) {
	for _, layout := range []string{"missing", "linked-worktree", "symlink", "alternates", "http-alternates", "shallow", "object-link", "split-index"} {
		t.Run(layout, func(t *testing.T) {
			p := inspectionTestProject(t)
			if layout != "missing" && layout != "linked-worktree" && layout != "symlink" {
				inspectionTestGit(t, p, "init", "--template=")
			}
			switch layout {
			case "linked-worktree":
				inspectionTestWrite(t, p, ".git", "gitdir: /arbitrary/host/path\n")
			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(p.Path, ".git")); err != nil {
					t.Fatal(err)
				}
			case "alternates", "http-alternates":
				inspectionTestWrite(t, p, ".git/objects/info/"+layout, "/arbitrary/host/path\n")
			case "shallow":
				inspectionTestWrite(t, p, ".git/shallow", strings.Repeat("a", 40))
			case "object-link":
				if err := os.Symlink(t.TempDir(), filepath.Join(p.Path, ".git/objects/aa")); err != nil {
					t.Fatal(err)
				}
			case "split-index":
				inspectionTestWrite(t, p, "file", "content")
				inspectionTestGit(t, p, "add", "file")
				inspectionTestGit(t, p, "update-index", "--split-index")
			}
			result, err := InspectChanges(t.Context(), p)
			if err != nil || result.Available || result.Reason == "" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestInspectionGitUnbornPackedRefsAndLiteralPaths(t *testing.T) {
	p := inspectionTestProject(t)
	inspectionTestGit(t, p, "init", "--template=")
	name := "-literal[glob].txt"
	inspectionTestWrite(t, p, name, "first\n")
	inspectionTestGit(t, p, "add", "--", name)
	changes, err := InspectChanges(t.Context(), p)
	if err != nil || !changes.Available || len(changes.Changes) != 1 || changes.Changes[0].Kind != "staged" {
		t.Fatalf("unborn=%+v err=%v", changes, err)
	}
	diff, err := InspectDiff(t.Context(), p, name, "staged")
	if err != nil || !diff.Available || !strings.Contains(diff.Text, "+first") {
		t.Fatalf("unborn diff=%+v err=%v", diff, err)
	}
	inspectionTestGit(t, p, "commit", "-m", "Initial")
	inspectionTestGit(t, p, "pack-refs", "--all")
	inspectionTestWrite(t, p, name, "second\n")
	diff, err = InspectDiff(t.Context(), p, name, "unstaged")
	if err != nil || !diff.Available || !strings.Contains(diff.Text, "+second") {
		t.Fatalf("literal diff=%+v err=%v", diff, err)
	}
	inspectionTestGit(t, p, "checkout", "--detach")
	changes, err = InspectChanges(t.Context(), p)
	if err != nil || !changes.Available {
		t.Fatalf("detached=%+v err=%v", changes, err)
	}
}

func TestInspectionGitStatusRenamesAndSymlinks(t *testing.T) {
	p := inspectionTestRepository(t)
	root, err := inspectionRoot(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	inspectionTestWrite(t, p, "new name", "new")
	if err := os.Symlink("file", filepath.Join(p.Path, "link")); err != nil {
		t.Fatal(err)
	}
	data := []byte("RM new name\x00old name\x00?? .env\x00?? link\x00 D deleted\x00R  visible\x00.env\x00")
	changes, err := inspectionGitParseStatus(t.Context(), root, data)
	if err != nil || len(changes) != 3 || changes[0].Kind != "staged" || changes[1].Kind != "unstaged" || changes[2].Path != "deleted" {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
	for _, data := range []string{"M file\x00", " M file", "R  new\x00", "XX file\x00"} {
		if _, err := inspectionGitParseStatus(t.Context(), root, []byte(data)); err == nil {
			t.Errorf("accepted malformed %q", data)
		}
	}
	if _, err := InspectDiff(t.Context(), p, "link", "unstaged"); !errors.Is(err, ErrInspectionPath) {
		t.Fatalf("link diff err=%v", err)
	}
}

func TestInspectionGitBoundsCancellationAndCleanup(t *testing.T) {
	p := inspectionTestRepository(t)
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	inspectionTestWrite(t, p, "file", strings.Repeat("large changed line\n", 6000))
	result, err := InspectDiff(t.Context(), p, "file", "unstaged")
	if err != nil || result.Available || result.Text != "" || result.Reason != inspectionGitBound {
		t.Fatalf("overflow=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := InspectChanges(ctx, p); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline err=%v", err)
	}
	inspectionGitSlots <- struct{}{}
	inspectionGitSlots <- struct{}{}
	busy, err := InspectChanges(t.Context(), p)
	<-inspectionGitSlots
	<-inspectionGitSlots
	if err != nil || busy.Available || busy.Reason != inspectionGitBound {
		t.Fatalf("busy=%+v err=%v", busy, err)
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary artifacts=%v err=%v", entries, err)
	}
	t.Setenv("TMPDIR", p.Path)
	blocked, err := InspectChanges(t.Context(), p)
	if err != nil || blocked.Available {
		t.Fatalf("project tempdir=%+v err=%v", blocked, err)
	}
	names, err := os.ReadDir(p.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range names {
		if strings.HasPrefix(entry.Name(), "snow-inspection-git-") {
			t.Fatal("created temporary files in project")
		}
	}
}

func TestInspectionGitPackedObjectsAndInvalidHEAD(t *testing.T) {
	p := inspectionTestRepository(t)
	inspectionTestGit(t, p, "repack", "-ad")
	inspectionTestGit(t, p, "update-server-info")
	inspectionTestWrite(t, p, "file", "packed object change\n")
	result, err := InspectDiff(t.Context(), p, "file", "unstaged")
	if err != nil || !result.Available || !strings.Contains(result.Text, "+packed object change") {
		t.Fatalf("packed diff=%+v err=%v", result, err)
	}
	inspectionTestWrite(t, p, ".git/HEAD", "")
	changes, err := InspectChanges(t.Context(), p)
	if err != nil || changes.Available || changes.Reason == "" {
		t.Fatalf("invalid HEAD=%+v err=%v", changes, err)
	}
}

func TestInspectionGitDeletedDirectoryCannotSelectProtectedDescendants(t *testing.T) {
	for _, kind := range []string{"staged", "unstaged"} {
		t.Run(kind, func(t *testing.T) {
			p := inspectionTestRepository(t)
			inspectionTestWrite(t, p, "dir/.env", "protected-descendant-content\n")
			inspectionTestWrite(t, p, "safe.txt", "safe-deleted-content\n")
			inspectionTestGit(t, p, "add", "--", "dir/.env", "safe.txt")
			inspectionTestGit(t, p, "commit", "-m", "Deletion fixtures")
			if err := os.RemoveAll(filepath.Join(p.Path, "dir")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(p.Path, "safe.txt")); err != nil {
				t.Fatal(err)
			}
			if kind == "staged" {
				inspectionTestGit(t, p, "add", "-u")
			}
			for _, path := range []string{"dir", "dir/.env", "missing", "file"} {
				result, err := InspectDiff(t.Context(), p, path, kind)
				if !errors.Is(err, ErrInspectionPath) || result.Available || result.Text != "" {
					t.Fatalf("path=%q result=%+v err=%v", path, result, err)
				}
			}
			result, err := InspectDiff(t.Context(), p, "safe.txt", kind)
			if err != nil || !result.Available || !strings.Contains(result.Text, "-safe-deleted-content") || strings.Contains(result.Text, "protected-descendant-content") || strings.Count(result.Text, "diff --git ") != 1 {
				t.Fatalf("safe deletion=%+v err=%v", result, err)
			}
		})
	}
}
