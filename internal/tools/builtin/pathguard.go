// Package builtin implements Snow's file, shell, search, and deferred web tools,
// plus the path confinement guard used to keep file access inside allowed roots.
package builtin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PathGuard confines file access to a set of allowed roots.
type PathGuard struct {
	roots []guardedRoot
	cwd   string
}

type guardedRoot struct {
	path string
	root *os.Root
}

type rootedPath struct {
	root     *os.Root
	name     string
	resolved string
}

// NewPathGuard creates a guard. Roots are normalized to absolute cleaned
// paths with symlinks resolved (via the closest existing ancestor), matching
// the resolution applied to target paths. An empty root list means the guard
// allows nothing (deny by default).
func NewPathGuard(roots []string, cwd string) *PathGuard {
	norm := make([]guardedRoot, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if resolved, err := evalWithAncestors(abs); err == nil {
			abs = resolved
		}
		handle, err := os.OpenRoot(abs)
		if err != nil {
			continue
		}
		norm = append(norm, guardedRoot{path: abs, root: handle})
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		absCwd = cwd
	}
	absCwd = filepath.Clean(absCwd)
	if resolved, err := evalWithAncestors(absCwd); err == nil {
		absCwd = resolved
	}
	return &PathGuard{roots: norm, cwd: absCwd}
}

// Roots returns the normalized allowed roots.
func (g *PathGuard) Roots() []string {
	if g == nil {
		return nil
	}
	out := make([]string, 0, len(g.roots))
	for _, root := range g.roots {
		out = append(out, root.path)
	}
	return out
}

// Close releases the pinned directory handles. Callers must stop tool use
// before closing the guard.
func (g *PathGuard) Close() error {
	if g == nil {
		return nil
	}
	var errs []error
	for i := range g.roots {
		if g.roots[i].root != nil {
			errs = append(errs, g.roots[i].root.Close())
			g.roots[i].root = nil
		}
	}
	return errors.Join(errs...)
}

// CWD returns the guard's working directory.
func (g *PathGuard) CWD() string { return g.cwd }

// Resolve cleans the path, joins relative paths against the guard's cwd,
// resolves symlinks via the closest existing ancestor, and verifies the
// result stays inside one of the allowed roots.
func (g *PathGuard) Resolve(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}

	if err := validatePlatformPath(path); err != nil {
		return "", err
	}
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(g.cwd, p)
	}
	p = filepath.Clean(p)

	// Resolve symlinks. EvalSymlinks fails if the final path does not exist;
	// walk up to the closest existing ancestor and rejoin the remainder.
	resolved, err := evalWithAncestors(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}

	for _, root := range g.roots {
		if root.root != nil && within(root.path, resolved) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q resolves outside allowed roots", path)
}

// rooted resolves path for user-facing diagnostics, then converts it to a
// name operated on through a pinned os.Root handle. The subsequent operation
// remains confined even if an ancestor is swapped after validation.
func (g *PathGuard) rooted(path string) (rootedPath, error) {
	resolved, err := g.Resolve(path)
	if err != nil {
		return rootedPath{}, err
	}
	for _, root := range g.roots {
		if root.root == nil || !within(root.path, resolved) {
			continue
		}
		rel, err := filepath.Rel(root.path, resolved)
		if err != nil {
			return rootedPath{}, err
		}
		return rootedPath{root: root.root, name: rel, resolved: resolved}, nil
	}
	return rootedPath{}, fmt.Errorf("path %q has no open allowed root", path)
}

// within reports whether path is equal to root or nested inside it, comparing
// on path components rather than raw string prefix (so /foo/bar2 is not inside
// /foo/bar).
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return platformPathWithin(rel)
}

// evalWithAncestors resolves symlinks on the longest existing prefix of path,
// then appends the remaining components. This handles paths whose final target
// does not yet exist (e.g. write to a new file) while still resolving any
// symlinked directories in the ancestor chain.
func evalWithAncestors(path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if dir == path { // reached the root without finding an existing ancestor
		return "", fmt.Errorf("no existing ancestor for %q", path)
	}
	resolvedDir, err := evalWithAncestors(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedDir, base), nil
}
