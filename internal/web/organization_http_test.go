package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Exercise the independently registered organization routes. Production mux
// integration separately owns host/origin checks and the page/static allowlist.
func organizationRequest(t *testing.T, s *shell, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.registerOrganizationRoutes(mux)
	mux.ServeHTTP(w, r)
	return w
}

func TestOrganizationHTTPAuthDuplicatesBoundsAndMetadataOnly(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	runtime := &fakeRuntime{}
	s.runtimes = runtime
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFor(t, s, cookie)
	base := "/projects/" + p.ID + "/organization/"
	if w := organizationRequest(t, s, "POST", base+"pin", url.Values{"csrf": {csrf}}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatal("unauthenticated mutation")
	}
	for _, tc := range []struct {
		action string
		form   url.Values
	}{
		{"pin", url.Values{"csrf": {"wrong"}}},
		{"pin", url.Values{"csrf": {csrf, csrf}}},
		{"rename", url.Values{"csrf": {csrf}, "name": {"one", "two"}}},
		{"rename", url.Values{"csrf": {csrf}, "name": {strings.Repeat("x", 4097)}}},
		{"pin", url.Values{"csrf": {csrf}, "extra": {"value"}}},
		{"archive", url.Values{"csrf": {csrf}, "confirm": {"archive", "archive"}}},
		{"archive", url.Values{"csrf": {csrf}, "confirm": {"remove"}}},
	} {
		w := organizationRequest(t, s, "POST", base+tc.action, tc.form, cookie)
		if w.Code < 400 {
			t.Fatalf("invalid form accepted %s %v: %d", tc.action, tc.form, w.Code)
		}
	}
	if w := organizationRequest(t, s, "POST", base+"pin?csrf="+csrf, url.Values{"csrf": {csrf}}, cookie); w.Code != http.StatusBadRequest {
		t.Fatal("query authority accepted")
	}
	if w := organizationRequest(t, s, "GET", base+"pin", nil, cookie); w.Code != http.StatusMethodNotAllowed {
		t.Fatal("GET mutation route")
	}
	for _, tc := range []struct {
		action string
		form   url.Values
	}{
		{"rename", url.Values{"csrf": {csrf}, "name": {"renamed"}}},
		{"pin", url.Values{"csrf": {csrf}}},
		{"archive", url.Values{"csrf": {csrf}, "confirm": {"archive"}}},
		{"restore", url.Values{"csrf": {csrf}, "confirm": {"restore"}}},
	} {
		w := organizationRequest(t, s, "POST", base+tc.action, tc.form, cookie)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("%s: %d %s", tc.action, w.Code, w.Body.String())
		}
	}
	if catalog.calls != 0 || len(runtime.calls) != 0 {
		t.Fatal("project organization activated catalog/runtime")
	}
	got, err := s.registry.Lookup(t.Context(), p.ID)
	if err != nil || got.Name != "renamed" {
		t.Fatalf("rename %v %v", got, err)
	}
}

func TestOrganizationHTTPSessionMembershipLiveGuardAndNoActivation(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	runtime := &fakeRuntime{}
	s.runtimes = runtime
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFor(t, s, cookie)
	base := "/projects/" + p.ID + "/sessions/organization/"
	good := url.Values{"csrf": {csrf}, "session_id": {"saved-id"}, "offset": {"0"}}
	for _, field := range []string{"csrf", "session_id", "offset"} {
		bad := good.Clone()
		bad.Add(field, bad.Get(field))
		if w := organizationRequest(t, s, "POST", base+"pin", bad, cookie); w.Code < 400 {
			t.Fatalf("duplicate %s accepted", field)
		}
	}
	for _, value := range []string{"-1", "10001", "01", strings.Repeat("1", 513)} {
		bad := good.Clone()
		bad.Set("offset", value)
		if w := organizationRequest(t, s, "POST", base+"pin", bad, cookie); w.Code < 400 {
			t.Fatalf("offset %s accepted", value)
		}
	}
	if catalog.calls != 0 {
		t.Fatal("invalid fields reached catalog")
	}
	runtime.live = true
	before := catalog.calls
	for _, action := range []string{"pin", "unpin", "archive", "restore"} {
		form := good.Clone()
		if action == "archive" || action == "restore" {
			form.Set("confirm", action)
		}
		if w := organizationRequest(t, s, "POST", base+action, form, cookie); w.Code != http.StatusConflict {
			t.Fatalf("live %s accepted: %d", action, w.Code)
		}
	}
	archiveProject := "/projects/" + p.ID + "/organization/archive"
	if w := organizationRequest(t, s, "POST", archiveProject, url.Values{"csrf": {csrf}, "confirm": {"archive"}}, cookie); w.Code != http.StatusConflict {
		t.Fatal("live workspace archived")
	}
	if catalog.calls != before {
		t.Fatal("live project opened inactive catalog")
	}
	view, err := s.organizationData(t.Context(), url.Values{"project": {p.ID}}, csrf)
	if err != nil || !view.Live || len(view.Sessions) != 0 || catalog.calls != before {
		t.Fatalf("live view %+v %v", view, err)
	}
	runtime.live = false
	s.projectControl.Lock()
	if w := organizationRequest(t, s, "POST", base+"pin", good, cookie); w.Code != http.StatusConflict {
		t.Fatal("project admission gate bypassed")
	}
	s.projectControl.Unlock()
	bad := good.Clone()
	bad.Set("session_id", "not-on-page")
	if w := organizationRequest(t, s, "POST", base+"pin", bad, cookie); w.Code != http.StatusConflict {
		t.Fatal("absent session accepted")
	}
	for _, action := range []string{"pin", "archive", "restore", "unpin"} {
		form := good.Clone()
		if action == "archive" || action == "restore" {
			form.Set("confirm", action)
		}
		if w := organizationRequest(t, s, "POST", base+action, form, cookie); w.Code != http.StatusSeeOther {
			t.Fatalf("session %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	view, err = s.organizationData(t.Context(), url.Values{"project": {p.ID}}, csrf)
	if err != nil || len(view.Sessions) != 1 || view.Sessions[0].ID != "saved-id" || view.NextURL == "" {
		t.Fatalf("view %+v %v", view, err)
	}
	if len(runtime.calls) != 0 {
		t.Fatal("metadata/read called runtime activation or control")
	}
}

type organizationChangingCatalog struct {
	root  string
	calls int
}

func (c *organizationChangingCatalog) Sessions(context.Context, Project, int) (CatalogSessions, error) {
	c.calls++
	err := os.Rename(c.root, c.root+"-moved")
	return CatalogSessions{Sessions: []SessionSummary{{ID: "saved-id"}}}, err
}
func (c *organizationChangingCatalog) Messages(context.Context, Project, string, int) (CatalogMessages, error) {
	panic("organization must not read transcript")
}

func TestOrganizationHTTPRevalidatesAfterCatalogAndRejectsStaleReads(t *testing.T) {
	s, cookie, _ := projectShell(t)
	root := t.TempDir()
	p, err := s.registry.Add(t.Context(), "project", root)
	if err != nil {
		t.Fatal(err)
	}
	c := &organizationChangingCatalog{root: root}
	s.catalog = c
	t.Cleanup(func() { _ = os.Rename(root+"-moved", root) })
	csrf := csrfFor(t, s, cookie)
	path := "/projects/" + p.ID + "/sessions/organization/pin"
	form := url.Values{"csrf": {csrf}, "session_id": {"saved-id"}, "offset": {"0"}}
	if w := organizationRequest(t, s, "POST", path, form, cookie); w.Code != http.StatusConflict {
		t.Fatal("catalog root race accepted")
	}
	var count int
	if err := s.registry.db.QueryRow(`SELECT count(*) FROM session_organization`).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale root saved metadata")
	}
	if w := organizationRequest(t, s, "POST", path, form, cookie); w.Code != http.StatusConflict || c.calls != 1 {
		t.Fatal("stale root reached catalog")
	}
	if _, err := s.organizationData(t.Context(), url.Values{"project": {p.ID}}, csrf); err == nil || c.calls != 1 {
		t.Fatal("stale read opened catalog")
	}
	for _, query := range []url.Values{{"project": {p.ID, p.ID}}, {"offset": {"0", "1"}}, {"archived_offset": {"-1"}}, {"offset": {"10001"}}} {
		if _, err := s.organizationData(t.Context(), query, csrf); err == nil {
			t.Fatal("invalid view parameter accepted")
		}
	}
}
