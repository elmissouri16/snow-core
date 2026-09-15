package web

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOrganizationTemplateAccessibleAndEscaped(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "<script>project</script>", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFor(t, s, cookie)
	view, err := s.organizationData(t.Context(), url.Values{"project": {p.ID}}, csrf)
	if err != nil {
		t.Fatal(err)
	}
	view.Sessions[0].Name = "<script>session</script>"
	view.Sessions[0].Archived = true
	// The server fallback and React bootstrap share the same public labels and
	// CSRF value; parsing the attribute must not interpret those labels as HTML.
	props, html := reactBootstrap(t, "organization", pageData{CSRF: csrf, Organization: view})
	var bootstrap organizationFrontendProps
	if err := json.Unmarshal([]byte(props), &bootstrap, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if bootstrap.CSRF != csrf || bootstrap.Organization == nil || bootstrap.Organization.Project == nil || bootstrap.Organization.Project.Name != "<script>project</script>" || len(bootstrap.Organization.Sessions) == 0 || bootstrap.Organization.Sessions[0].Name != "<script>session</script>" || !bootstrap.Organization.Sessions[0].Archived {
		t.Fatal("organization bootstrap lost its public projection")
	}
	for _, want := range []string{"Organize workspaces", "Search titles on this loaded page only", "Loaded-page search does not search other pages", `name="session_id" value="saved-id"`, `name="offset" value="0"`, `name="csrf" value="` + csrf + `"`, `name="confirm" value="restore"`, `role="status"`, `aria-live="polite"`, "Restore conversation", "&lt;script&gt;project&lt;/script&gt;", "&lt;script&gt;session&lt;/script&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(html, "<script>project") || strings.Contains(html, "<script>session") {
		t.Fatal("unescaped organization labels")
	}
}

func TestOrganizationProductionRoutesAndAssets(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	runtime := &fakeRuntime{}
	s.runtimes = runtime
	p, err := s.registry.Add(t.Context(), "Organized workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/?view=organization", "/?view=organization&project=" + p.ID, "/static/organization.css", "/static/generated/app.js"} {
		w := request(t, s, "GET", path, nil, cookie)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if strings.HasPrefix(path, "/?") {
			for _, marker := range []string{"Organize workspaces", `data-react-page="organization"`, `data-react-props="`, `<script type="module" src="/static/generated/app.js"></script>`} {
				if !strings.Contains(w.Body.String(), marker) {
					t.Fatalf("organization page lacks %q", marker)
				}
			}
			if strings.Contains(w.Body.String(), `src="/static/organization.js"`) {
				t.Fatal("organization page loaded the retired DOM owner")
			}
		}
	}
	if retired := request(t, s, "GET", "/static/organization.js", nil, cookie); retired.Code != http.StatusNotFound {
		t.Fatal("retired organization client is still routed")
	}
	csrf := csrfFor(t, s, cookie)
	w := request(t, s, "POST", "/projects/"+p.ID+"/organization/pin", url.Values{"csrf": {csrf}}, cookie)
	if w.Code != 303 {
		t.Fatalf("production mutation route: %d %s", w.Code, w.Body.String())
	}
	if len(runtime.calls) != 0 || catalog.calls != 1 {
		t.Fatalf("organization reads activated runtime or unexpected catalog reads: %v %d", runtime.calls, catalog.calls)
	}
}
