package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// A bare name becomes a folder under the projects folder; absolute paths keep
// their existing-folder rule; the setting round-trips through its route.
func TestAddWorkspaceByNameUnderProjectsDir(t *testing.T) {
	s, cookie, _ := projectShell(t)
	csrf := csrfFor(t, s, cookie)
	root := filepath.Join(t.TempDir(), "projects")
	if w := request(t, s, "POST", "/settings/projects-dir", url.Values{"csrf": {csrf}, "path": {root}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatalf("save projects dir = %d", w.Code)
	}
	if w := request(t, s, "POST", "/settings/projects-dir", url.Values{"csrf": {csrf}, "path": {"relative/dir"}}, cookie); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/?view=projects&notice=projects_dir_invalid" {
		t.Fatalf("relative projects dir accepted: %d %s", w.Code, w.Header().Get("Location"))
	}
	if got, _ := s.registry.ProjectsDir(t.Context()); got != root {
		t.Fatalf("projects dir = %q", got)
	}
	w := request(t, s, "POST", "/projects/add", url.Values{"csrf": {csrf}, "path": {"demo"}}, cookie)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") == "/?view=projects&notice=register_failed" {
		t.Fatalf("name-only add = %d %s", w.Code, w.Header().Get("Location"))
	}
	if info, err := os.Stat(filepath.Join(root, "demo")); err != nil || !info.IsDir() {
		t.Fatal("workspace folder was not created under the projects folder")
	}
	projects, err := s.registry.List(t.Context())
	if err != nil || len(projects) != 1 || projects[0].Name != "demo" {
		t.Fatalf("registry after add: %v %+v", err, projects)
	}
	for _, bad := range []string{"../escape", "a/b", ".."} {
		if w := request(t, s, "POST", "/projects/add", url.Values{"csrf": {csrf}, "path": {bad}}, cookie); w.Header().Get("Location") != "/?view=projects&notice=register_failed" {
			t.Fatalf("name %q accepted", bad)
		}
	}
	missing := filepath.Join(t.TempDir(), "absent")
	if w := request(t, s, "POST", "/projects/add", url.Values{"csrf": {csrf}, "path": {missing}}, cookie); w.Header().Get("Location") != "/?view=projects&notice=register_failed" {
		t.Fatal("absolute path to a missing folder must not be created")
	}
	if w := request(t, s, "POST", "/settings/projects-dir", url.Values{"csrf": {csrf}, "path": {""}}, cookie); w.Code != http.StatusSeeOther {
		t.Fatalf("clear projects dir = %d", w.Code)
	}
	if got, _ := s.registry.ProjectsDir(t.Context()); got != "" {
		t.Fatalf("projects dir after clear = %q", got)
	}
}
