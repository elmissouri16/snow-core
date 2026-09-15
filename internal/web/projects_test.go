package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCatalog struct{ calls int }

func (c *fakeCatalog) Sessions(context.Context, Project, int) (CatalogSessions, error) {
	c.calls++
	return CatalogSessions{Sessions: []SessionSummary{{ID: "saved-id", Name: "Saved conversation"}}, HasMore: true, NextOffset: 25}, nil
}

func (c *fakeCatalog) Messages(context.Context, Project, string, int) (CatalogMessages, error) {
	c.calls++
	return CatalogMessages{Messages: []HistoryMessage{{ID: "message", Role: "assistant", Text: "<script>not executable</script>"}}}, nil
}

func projectShell(t *testing.T) (*shell, *http.Cookie, *fakeCatalog) {
	t.Helper()
	s := testShell(t)
	registry, err := OpenRegistry(t.Context(), filepath.Join(t.TempDir(), "manager"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	s.registry = registry
	catalog := &fakeCatalog{}
	s.catalog = catalog
	return s, pairBrowser(t, s, s.initialCode), catalog
}

func TestProjectRegistrationHistoryAndRemoval(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	csrf := csrfFor(t, s, cookie)
	root := t.TempDir()
	file := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(file, []byte("keep project data"), 0600); err != nil {
		t.Fatal(err)
	}
	// An unauthenticated or CSRF-invalid browser cannot register a host root.
	for _, token := range []string{"", "wrong"} {
		w := request(t, s, "POST", "/projects/add", url.Values{"csrf": {token}, "path": {root}}, cookie)
		if w.Code != http.StatusForbidden {
			t.Fatalf("untrusted registration = %d", w.Code)
		}
	}
	w := request(t, s, "POST", "/projects/add", url.Values{"csrf": {csrf}, "path": {root}, "name": {"<project>"}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("registration = %d: %s", w.Code, w.Body.String())
	}
	if catalog.calls != 0 {
		t.Fatal("registration started a catalog worker")
	}
	projectURL := w.Header().Get("Location")
	w = request(t, s, "GET", projectURL, nil, cookie)
	if w.Code != http.StatusOK || !strings.Contains(projectURL, "&new=1") || catalog.calls != 0 || strings.Contains(w.Body.String(), "<project>") {
		t.Fatalf("session page = %d: %s", w.Code, w.Body.String())
	}
	w = request(t, s, "GET", strings.TrimSuffix(projectURL, "&new=1")+"&session=saved-id", nil, cookie)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<script>not executable") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("unsafe or missing history: %s", w.Body.String())
	}
	projects, err := s.registry.List(t.Context())
	if err != nil || len(projects) != 1 {
		t.Fatalf("registry: %v", err)
	}
	path := "/projects/" + projects[0].ID + "/remove"
	w = request(t, s, "POST", path, url.Values{"csrf": {csrf}}, cookie)
	if w.Code != http.StatusBadRequest {
		t.Fatal("removal must require explicit confirmation")
	}
	w = request(t, s, "POST", path, url.Values{"csrf": {csrf}, "confirm": {"remove"}}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("removal = %d", w.Code)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep project data" {
		t.Fatal("removing registration changed project data")
	}
	before := catalog.calls
	request(t, s, "GET", projectURL, nil, cookie)
	if catalog.calls != before {
		t.Fatal("removed project started a worker")
	}
}

func TestCatalogRequiresAuthAndValidProjectIdentity(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := "/?view=projects&project=" + project.ID
	if w := request(t, s, "GET", path, nil); w.Code != http.StatusSeeOther {
		t.Fatal("unauthenticated catalog access")
	}
	for _, suffix := range []string{"&offset=-1", "&offset=1000001", "&offset=x", "&offset=1&offset=2", "&session=x&session=y"} {
		request(t, s, "GET", path+suffix, nil, cookie)
	}
	if catalog.calls != 0 {
		t.Fatal("invalid request reached catalog")
	}
	if err := os.Rename(project.Path, project.Path+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(project.Path+"-moved", project.Path) })
	request(t, s, "GET", path, nil, cookie)
	if catalog.calls != 0 {
		t.Fatal("missing project started a worker")
	}
}

func TestWorkerCatalogRejectsOverCapacityAndRelativeExecutable(t *testing.T) {
	catalog := newWorkerCatalog("snow", "")
	if _, err := catalog.Sessions(t.Context(), Project{}, 0); err == nil {
		t.Fatal("relative executable accepted")
	}
	catalog = newWorkerCatalog("/no/such/snow", "")
	catalog.slots <- struct{}{}
	catalog.slots <- struct{}{}
	if _, err := catalog.Sessions(t.Context(), Project{}, 0); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("capacity must reject without starting/queuing: %v", err)
	}
}

func TestFailedRegistrationUsesCanonicalGet(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	path := "/missing-private-project"
	w := request(t, s, "POST", "/projects/add", url.Values{"csrf": {csrfFor(t, s, cookie)}, "path": {path}}, cookie)
	location := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || location != "/?view=projects&notice=register_failed" || strings.Contains(w.Body.String(), path) {
		t.Fatalf("failed registration did not use safe PRG: %d %s", w.Code, location)
	}
	w = request(t, s, "GET", location, nil, cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Could not register this folder") || catalog.calls != 0 {
		t.Fatal("canonical error page is missing or started a worker")
	}
}

func TestWorkerCatalogPinsSessionRootEnvironment(t *testing.T) {
	t.Setenv("SNOW_SESSIONS_DIR", "relative-sessions")
	root := t.TempDir()
	catalog := newWorkerCatalog("/absolute/snow", root)
	var values []string
	for _, variable := range catalog.env {
		if strings.HasPrefix(variable, "SNOW_SESSIONS_DIR=") {
			values = append(values, variable)
		}
	}
	if len(values) != 1 || values[0] != "SNOW_SESSIONS_DIR="+root {
		t.Fatalf("worker session root environment: %v", values)
	}
}
