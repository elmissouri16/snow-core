package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// projectsDir is the folder a name-only workspace is created under: the
// configured manager setting, else <home>/snow-projects. Reading it never
// creates anything.
func (s *shell) projectsDir(ctx context.Context) string {
	if s.registry != nil {
		if configured, err := s.registry.ProjectsDir(ctx); err == nil && configured != "" {
			return configured
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "snow-projects")
}

// resolveNewWorkspace turns a name into an existing folder under the projects
// folder, creating both when needed. Absolute paths are returned unchanged
// and must already exist (Registry.Add checks that).
func (s *shell) resolveNewWorkspace(ctx context.Context, input string) (string, error) {
	if filepath.IsAbs(input) {
		return input, nil
	}
	if !workspaceName(input) {
		return "", ErrProjectInvalid
	}
	root := s.projectsDir(ctx)
	if root == "" {
		return "", ErrProjectInvalid
	}
	target := filepath.Join(root, input)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", ErrProjectInvalid
	}
	return target, nil
}

func (s *shell) saveProjectsDir(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	if s.registry == nil {
		http.Error(w, "Project registry unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(r.PostForm["path"]) != 1 {
		http.Error(w, "Invalid projects folder", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.registry.SetProjectsDir(ctx, r.PostForm.Get("path")); err != nil {
		http.Redirect(w, r, "/?view=projects&notice=projects_dir_invalid", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?view=projects", http.StatusSeeOther)
}
