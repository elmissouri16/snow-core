//go:build darwin || linux

package web

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func operationRequest(t *testing.T, s *shell, mux http.Handler, method, path string, form url.Values, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func TestProjectOperationHTTPAuthorityAndStrictAdmission(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	s, err := newShell(testOrigin, "test")
	if err != nil {
		t.Fatal(err)
	}
	cookie := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, cookie)
	mux := http.NewServeMux()
	s.registerProjectOperationRoutes(mux, m)
	form := url.Values{"path": {parent}, "csrf": {csrf}}
	for _, origin := range []string{"", "http://localhost:7441", "https://127.0.0.1:7441", "http://127.0.0.1:7442"} {
		w := operationRequest(t, s, mux, "POST", "/projects/folders/select", form, cookie, origin)
		if w.Code != http.StatusForbidden {
			t.Fatalf("origin %q status %d", origin, w.Code)
		}
	}
	badCSRF := form.Clone()
	badCSRF.Set("csrf", "wrong")
	if w := operationRequest(t, s, mux, "POST", "/projects/folders/select", badCSRF, cookie, s.origin); w.Code != http.StatusForbidden {
		t.Fatalf("CSRF %d", w.Code)
	}
	extra := form.Clone()
	extra.Set("approved_root", parent)
	if w := operationRequest(t, s, mux, "POST", "/projects/folders/select", extra, cookie, s.origin); w.Code != http.StatusBadRequest {
		t.Fatalf("invented field %d", w.Code)
	}
	w := operationRequest(t, s, mux, "POST", "/projects/folders/select", form, cookie, s.origin)
	var grant ParentSelection
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &grant) != nil || grant.OperationID == "" || !strings.Contains(grant.Authority, "host-user OS authority") {
		t.Fatalf("selection %d %s", w.Code, w.Body.String())
	}
	secretForm := url.Values{"csrf": {csrf}, "operation_id": {grant.OperationID}, "name": {"clone"}, "remote": {"https://user:secret@example.com/a/b"}}
	w = operationRequest(t, s, mux, "POST", "/projects/clone", secretForm, cookie, s.origin)
	if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "secret") || b.opened.Load() != 0 {
		t.Fatalf("credential locator %d %s", w.Code, w.Body.String())
	}
	create := url.Values{"csrf": {csrf}, "operation_id": {grant.OperationID}, "name": {"created"}}
	w = operationRequest(t, s, mux, "POST", "/projects/create", create, cookie, s.origin)
	var op ProjectOperation
	if w.Code != http.StatusAccepted || json.Unmarshal(w.Body.Bytes(), &op) != nil || op.ID != grant.OperationID {
		t.Fatalf("admit %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fingerprint") || strings.Contains(w.Body.String(), csrf) {
		t.Fatal("private value projected")
	}
	list := operationRequest(t, s, mux, "GET", "/operations", nil, cookie, s.origin)
	if list.Code != http.StatusOK || list.Body.Len() > 64<<10 {
		t.Fatalf("list %d", list.Code)
	}
	duplicate := operationRequest(t, s, mux, "POST", "/projects/create", create, cookie, s.origin)
	if duplicate.Code != http.StatusAccepted {
		t.Fatalf("duplicate %d %s", duplicate.Code, duplicate.Body.String())
	}
	create.Add("name", "other")
	if w := operationRequest(t, s, mux, "POST", "/projects/create", create, cookie, s.origin); w.Code != http.StatusBadRequest {
		t.Fatalf("duplicate field %d", w.Code)
	}
}
func TestProjectOperationHTTPGrantsUseStableBrowserID(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	s, err := newShell(testOrigin, "test")
	if err != nil {
		t.Fatal(err)
	}
	cookie := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, cookie)
	mux := http.NewServeMux()
	s.registerProjectOperationRoutes(mux, m)
	w := operationRequest(t, s, mux, "POST", "/projects/folders/select", url.Values{"csrf": {csrf}, "path": {parent}}, cookie, s.origin)
	var grant ParentSelection
	if json.Unmarshal(w.Body.Bytes(), &grant) != nil || w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	// CSRF rotation does not change browserSession.ID and must not invalidate a
	// still-live explicit parent selection. Cookie hashes are not grant owners.
	s.access.mu.Lock()
	for key, browser := range s.access.sessions {
		browser.CSRF = randomToken()
		csrf = browser.CSRF
		s.access.sessions[key] = browser
	}
	s.access.mu.Unlock()
	w = operationRequest(t, s, mux, "POST", "/projects/create", url.Values{"csrf": {csrf}, "operation_id": {grant.OperationID}, "name": {"rotation"}}, cookie, s.origin)
	if w.Code != http.StatusAccepted {
		t.Fatalf("stable ID grant %d %s", w.Code, w.Body.String())
	}
	// Cleanup cancellation remains manager-owned, unrelated to this HTTP request.
	if b.opened.Load() > 1 {
		t.Fatal("unexpected replay")
	}
}
