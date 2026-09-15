//go:build darwin || linux

package web

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type InspectionChange struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // staged, unstaged, or untracked
	Status string `json:"status"`
}

type InspectionChanges struct {
	Available bool               `json:"available"`
	Reason    string             `json:"reason"`
	Changes   []InspectionChange `json:"changes"`
	Limited   bool               `json:"limited"`
}

type InspectionDiff struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

const inspectionGitRaw = "Raw Git changes: project/global Git configuration, filters, text conversion and external diff drivers are ignored."
const inspectionGitBound = "Git inspection exceeded its time, output or concurrency limit."

var inspectionGitSlots = make(chan struct{}, 2)

func inspectionGitAcquire(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	child, cancel := context.WithTimeout(ctx, 5*time.Second)
	select {
	case inspectionGitSlots <- struct{}{}:
		return child, func() { <-inspectionGitSlots; cancel() }, true
	default:
		cancel()
		return child, func() {}, false
	}
}

func InspectChanges(ctx context.Context, p Project) (InspectionChanges, error) {
	result := InspectionChanges{Changes: []InspectionChange{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ctx, release, ok := inspectionGitAcquire(ctx)
	if !ok {
		result.Reason = inspectionGitBound
		return result, nil
	}
	defer release()
	snapshot, err := newInspectionGitSnapshot(ctx, p)
	if err != nil {
		return inspectionGitChangesFailure(ctx, result, inspectionGitUnsupported)
	}
	defer snapshot.close()
	data, overflow, err := snapshot.run(ctx, 256<<10, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all")
	if overflow {
		return inspectionGitChangesFailure(ctx, result, inspectionGitBound)
	}
	if err != nil {
		return inspectionGitChangesFailure(ctx, result, inspectionGitUnsupported)
	}
	changes, err := inspectionGitParseStatus(ctx, snapshot.root, data)
	if err != nil {
		return inspectionGitChangesFailure(ctx, result, inspectionGitUnsupported)
	}
	if err := snapshot.check(ctx); err != nil {
		return inspectionGitChangesFailure(ctx, result, inspectionGitUnsupported)
	}
	if err := snapshot.checkObjects(ctx); err != nil {
		return inspectionGitChangesFailure(ctx, result, inspectionGitUnsupported)
	}
	result.Available = true
	result.Reason = inspectionGitRaw
	result.Changes = changes
	return result, nil
}

func inspectionGitChangesFailure(ctx context.Context, result InspectionChanges, reason string) (InspectionChanges, error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Reason = reason
	return result, nil
}

func InspectDiff(ctx context.Context, p Project, relativePath, kind string) (InspectionDiff, error) {
	path, err := inspectionPath(relativePath, false)
	if err != nil {
		return InspectionDiff{}, err
	}
	if kind != "staged" && kind != "unstaged" && kind != "untracked" {
		return InspectionDiff{}, ErrInspectionPath
	}
	result := InspectionDiff{Path: path, Kind: kind}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ctx, release, ok := inspectionGitAcquire(ctx)
	if !ok {
		result.Reason = inspectionGitBound
		return result, nil
	}
	defer release()
	fail := func(reason string) (InspectionDiff, error) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Reason = reason
		return result, nil
	}
	snapshot, err := newInspectionGitSnapshot(ctx, p)
	if err != nil {
		return fail(inspectionGitUnsupported)
	}
	defer snapshot.close()
	if !inspectionGitSafePath(ctx, snapshot.root, path) {
		return InspectionDiff{}, ErrInspectionPath
	}
	if kind == "untracked" {
		// Confirm untracked membership; never render a tracked path as untracked.
		data, overflow, err := snapshot.run(ctx, 256<<10, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=all", "--", path)
		if overflow {
			return fail(inspectionGitBound)
		}
		if err != nil {
			return fail(inspectionGitUnsupported)
		}
		changes, err := inspectionGitParseStatus(ctx, snapshot.root, data)
		if err != nil || len(changes) != 1 || changes[0].Path != path || changes[0].Kind != "untracked" {
			return fail("The selected path is not an untracked regular file.")
		}
		file, err := InspectFile(ctx, p, path)
		if err != nil {
			return result, err
		}
		if err := snapshot.check(ctx); err != nil {
			return fail(inspectionGitUnsupported)
		}
		result.Available = true
		result.Text = file.Text
		result.Truncated = file.Truncated
		result.Reason = "Untracked file preview (plain text, not a patch)."
		return result, nil
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all", "--no-renames", "--no-relative", "--src-prefix=a/", "--dst-prefix=b/"}
	if kind == "staged" {
		args = append(args, "--cached")
	}
	// A literal Git pathspec still selects descendants. In particular, a
	// deleted directory passes the filesystem missing-path check but must not
	// expose patches for protected children. Require one exact changed path
	// before requesting any patch content, using the same snapshot and options.
	membershipArgs := append(slices.Clone(args), "--name-only", "-z", "--", path)
	members, overflow, err := snapshot.run(ctx, 256<<10, membershipArgs...)
	if overflow {
		return fail(inspectionGitBound)
	}
	if err != nil {
		return fail(inspectionGitUnsupported)
	}
	if !bytes.Equal(members, []byte(path+"\x00")) || !inspectionGitSafePath(ctx, snapshot.root, path) {
		return InspectionDiff{}, ErrInspectionPath
	}
	args = append(args, "--", path)
	data, overflow, err := snapshot.run(ctx, InspectionPreviewLimit, args...)
	if overflow {
		return fail(inspectionGitBound)
	}
	if err != nil {
		return fail(inspectionGitUnsupported)
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return fail("Git diff is not displayable UTF-8 text.")
	}
	if !inspectionGitSafePath(ctx, snapshot.root, path) {
		return InspectionDiff{}, ErrInspectionPath
	}
	if err := snapshot.check(ctx); err != nil {
		return fail(inspectionGitUnsupported)
	}
	if err := snapshot.checkObjects(ctx); err != nil {
		return fail(inspectionGitUnsupported)
	}
	result.Available = true
	result.Reason = inspectionGitRaw
	result.Text = string(data)
	return result, nil
}

// A removed path is permitted, but every existing component must be no-link.
// Protected paths and unsafe rename source names are omitted before rendering.
func inspectionGitSafePath(ctx context.Context, root *os.Root, path string) bool {
	if _, err := inspectionPath(path, false); err != nil {
		return false
	}
	prefix := ""
	for part := range strings.SplitSeq(path, "/") {
		if ctx.Err() != nil {
			return false
		}
		if prefix == "" {
			prefix = part
		} else {
			prefix += "/" + part
		}
		info, err := root.Lstat(prefix)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if prefix != path && !info.IsDir() {
			return false
		}
		if prefix == path && !inspectionRegular(info) {
			return false
		}
	}
	return true
}

func inspectionGitParseStatus(ctx context.Context, root *os.Root, data []byte) ([]InspectionChange, error) {
	changes := []InspectionChange{}
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, remaining, ok := bytes.Cut(data, []byte{0})
		if !ok || len(record) < 4 || record[2] != ' ' {
			return nil, ErrInspectionUnavailable
		}
		data = remaining
		x, y := record[0], record[1]
		path := string(record[3:])
		safe := inspectionGitSafePath(ctx, root, path)
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			source, remaining, ok := bytes.Cut(data, []byte{0})
			if !ok || len(source) == 0 {
				return nil, ErrInspectionUnavailable
			}
			data = remaining
			safe = safe && inspectionGitSafePath(ctx, root, string(source))
		}
		if !safe {
			continue
		}
		if x == '?' && y == '?' {
			changes = append(changes, InspectionChange{Path: path, Kind: "untracked", Status: "?"})
		} else {
			if !strings.ContainsRune(" MADRCUT", rune(x)) || !strings.ContainsRune(" MADRCUT", rune(y)) || (x == ' ' && y == ' ') {
				return nil, ErrInspectionUnavailable
			}
			if x != ' ' {
				changes = append(changes, InspectionChange{Path: path, Kind: "staged", Status: string(x)})
			}
			if y != ' ' {
				changes = append(changes, InspectionChange{Path: path, Kind: "unstaged", Status: string(y)})
			}
		}
		if len(changes) > 4096 {
			return nil, ErrInspectionUnavailable
		}
	}
	return changes, nil
}

type inspectionGitOutput struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (w *inspectionGitOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - len(w.data)
	w.data = append(w.data, p[:min(len(p), remaining)]...)
	if len(p) > remaining {
		w.overflow = true
		w.cancel()
	}
	return len(p), nil
}

func (s *inspectionGitSnapshot) run(ctx context.Context, limit int, args ...string) ([]byte, bool, error) {
	if err := s.check(ctx); err != nil {
		return nil, false, err
	}
	executable := ""
	for _, path := range []string{"/usr/bin/git", "/bin/git"} {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			executable = path
			break
		}
	}
	if executable == "" {
		return nil, false, ErrInspectionUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fixed := []string{"--no-pager", "--no-optional-locks", "--literal-pathspecs", "--git-dir=" + s.temp, "--work-tree=" + s.project.Path,
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null", "-c", "core.autocrlf=false", "-c", "submodule.recurse=false", "-c", "core.quotePath=true", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}
	cmd := exec.CommandContext(ctx, executable, append(fixed, args...)...)
	cmd.Dir = s.temp
	// Construct a new environment, rather than trying to enumerate dangerous
	// inherited GIT_*, loader, shell startup and platform-specific variables.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(s.temp, "home"), "XDG_CONFIG_HOME=" + filepath.Join(s.temp, "xdg"), "LC_ALL=C", "LANG=C",
		"GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1", "GIT_CONFIG_COUNT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_OBJECT_DIRECTORY=" + filepath.Join(s.project.Path, ".git", "objects")}
	output := &inspectionGitOutput{limit: limit, cancel: cancel}
	diagnostic := &inspectionGitOutput{limit: 8192, cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if output.overflow || diagnostic.overflow {
		return nil, true, ErrInspectionUnavailable
	}
	if err != nil {
		return nil, false, ErrInspectionUnavailable
	}
	return output.data, false, nil
}
