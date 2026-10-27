package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func requestJSON(t *testing.T, s *shell, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, testOrigin+path, strings.NewReader(form.Encode()))
	r.RemoteAddr = "192.0.2.1:1234"
	r.URL.Scheme, r.URL.Host = "", ""
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}

// Removing a live project closes its worker first only when the confirmation
// asks for that, and a JSON-accepting caller gets a receipt instead of a redirect.
func TestRemoveProjectClosesLiveRuntimeOnRequest(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &skillsHTTPRuntime{}
	s.runtimes = backend
	csrf := csrfFor(t, s, cookie)
	if w := request(t, s, "POST", "/projects/"+project.ID+"/runtime/open", url.Values{"csrf": {csrf}, "confirm": {"activate"}}, cookie); w.Code != 200 || !backend.live {
		t.Fatalf("activation: %d", w.Code)
	}
	path := "/projects/" + project.ID + "/remove"
	if w := request(t, s, "POST", path, url.Values{"csrf": {csrf}, "confirm": {"remove"}}, cookie); w.Code != http.StatusConflict || !backend.live {
		t.Fatalf("plain removal of a live project must refuse: %d", w.Code)
	}
	if w := request(t, s, "POST", path, url.Values{"csrf": {csrf}, "close": {"live"}}, cookie); w.Code != http.StatusBadRequest || !backend.live {
		t.Fatalf("close without confirmation must not touch the worker: %d", w.Code)
	}
	w := requestJSON(t, s, path, url.Values{"csrf": {csrf}, "confirm": {"remove"}, "close": {"live"}}, cookie)
	if w.Code != 200 || backend.live || !strings.Contains(w.Body.String(), `"removed":true`) {
		t.Fatalf("confirmed removal with close: %d %s live=%v", w.Code, w.Body.String(), backend.live)
	}
	if projects, err := s.registry.List(t.Context()); err != nil || len(projects) != 0 {
		t.Fatalf("registration remained: %v %d", err, len(projects))
	}
}
