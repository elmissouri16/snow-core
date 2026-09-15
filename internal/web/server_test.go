package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testOrigin = "http://127.0.0.1:7331"

func testShell(t *testing.T) *shell {
	t.Helper()
	s, err := newShell(testOrigin, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func request(t *testing.T, s *shell, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, testOrigin+path, body)
	// Incoming server requests use origin form, not proxy absolute form.
	r.URL.Scheme, r.URL.Host = "", ""
	if method == http.MethodPost {
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}

func pairBrowser(t *testing.T, s *shell, code string) *http.Cookie {
	t.Helper()
	page := request(t, s, "GET", "/login", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("login page status = %d", page.Code)
	}
	csrf := page.Result().Cookies()[0]
	login := request(t, s, "POST", "/login", url.Values{"csrf": {csrf.Value}, "code": {code}}, csrf)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("pairing status = %d: %s", login.Code, login.Body.String())
	}
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == sessionCookie {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" || cookie.Secure {
				t.Fatalf("unsafe local cookie attributes: %+v", cookie)
			}
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func csrfFor(t *testing.T, s *shell, cookie *http.Cookie) string {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	browser, ok := s.browser(r)
	if !ok {
		t.Fatal("browser not paired")
	}
	return browser.CSRF
}

func TestPairingAndPages(t *testing.T) {
	s := testShell(t)
	if w := request(t, s, "GET", "/", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("unpaired home = %d", w.Code)
	}
	page := request(t, s, "GET", "/login", nil)
	if strings.Contains(page.Body.String(), s.initialCode) {
		t.Fatal("pairing credential disclosed in unauthenticated page")
	}
	cookie := pairBrowser(t, s, s.initialCode)
	for _, view := range []string{"overview", "projects", "preview", "access"} {
		w := request(t, s, "GET", "/?view="+view, nil, cookie)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data-view="`+view+`"`) {
			t.Fatalf("view %s = %d: %s", view, w.Code, w.Body.String())
		}
		if w.Header().Get("Referrer-Policy") != "same-origin" {
			t.Fatal("normal browser form POSTs require same-origin referrer policy (BUG-090)")
		}
		if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-") {
			t.Fatal("missing sensitive cache/CSP protection")
		}
	}
	csrf := request(t, s, "GET", "/login", nil).Result().Cookies()
	// Authenticated navigation to login redirects; this request has no cookie
	// and obtains a fresh pre-auth token. The durable code remains reusable.
	w := request(t, s, "POST", "/login", url.Values{"csrf": {csrf[0].Value}, "code": {s.initialCode}}, csrf[0])
	if w.Code != http.StatusSeeOther {
		t.Fatalf("reused code = %d", w.Code)
	}
}

func TestOriginHostAndFormGuards(t *testing.T) {
	s := testShell(t)
	for _, tc := range []struct{ name, host, origin, site string }{
		{"foreign host", "attacker.test", testOrigin, ""},
		{"foreign origin", "127.0.0.1:7331", "https://attacker.test", ""},
		{"missing origin", "127.0.0.1:7331", "", ""},
		{"null origin", "127.0.0.1:7331", "null", ""},
		{"cross site", "127.0.0.1:7331", testOrigin, "cross-site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/login", strings.NewReader("code=x"))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Forwarded-Host", "127.0.0.1:7331")
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
	for _, values := range []url.Values{
		{"code": {s.initialCode}},
		{"code": {s.initialCode}, "csrf": {"forged"}},
		{"code": {s.initialCode}, "csrf": {"one", "two"}},
		{"code": {strings.Repeat("x", 9<<10)}},
	} {
		w := request(t, s, "POST", "/login", values)
		if w.Code < 400 {
			t.Fatalf("invalid form accepted: %d", w.Code)
		}
	}
	if s.access.pairCode != s.initialCode || len(s.access.sessions) != 0 {
		t.Fatal("invalid form changed credentials or granted access")
	}
}

func TestPairingExpiryRateLimitAndBrowserExpiry(t *testing.T) {
	s := testShell(t)
	start := s.now()
	s.now = func() time.Time { return start }
	page := request(t, s, "GET", "/login", nil)
	csrf := page.Result().Cookies()[0]
	for range 20 {
		w := request(t, s, "POST", "/login", url.Values{"csrf": {csrf.Value}, "code": {"wrong"}}, csrf)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid code = %d", w.Code)
		}
	}
	w := request(t, s, "POST", "/login", url.Values{"csrf": {csrf.Value}, "code": {s.initialCode}}, csrf)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit = %d", w.Code)
	}
	s.now = func() time.Time { return start.Add(31 * 24 * time.Hour) }
	w = request(t, s, "POST", "/login", url.Values{"csrf": {csrf.Value}, "code": {s.initialCode}}, csrf)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired code = %d", w.Code)
	}
	s = testShell(t)
	cookie := pairBrowser(t, s, s.initialCode)
	start = s.now()
	s.now = func() time.Time { return start.Add(31 * 24 * time.Hour) }
	if w := request(t, s, "GET", "/", nil, cookie); w.Code != http.StatusSeeOther {
		t.Fatalf("expired browser = %d", w.Code)
	}
}

func TestPairAnotherBrowserLogoutAndFreshShell(t *testing.T) {
	s := testShell(t)
	first := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, first)
	bad := request(t, s, "POST", "/access/pair", url.Values{"csrf": {"wrong"}}, first)
	if bad.Code != http.StatusForbidden {
		t.Fatalf("pair without csrf = %d", bad.Code)
	}
	w := request(t, s, "POST", "/access/pair", url.Values{"csrf": {csrf}}, first)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/?view=access" {
		t.Fatalf("new pairing must redirect to a GET history URL: %d", w.Code)
	}
	w = request(t, s, "GET", w.Header().Get("Location"), nil, first)
	_, code, ok := strings.Cut(w.Body.String(), `id="pairing-code" type="text" value="`)
	if !ok {
		t.Fatal("missing new code")
	}
	code, _, _ = strings.Cut(code, `"`)
	if repeat := request(t, s, "GET", "/?view=access", nil, first); strings.Contains(repeat.Body.String(), code) {
		t.Fatal("one-time pairing result appeared again after refresh")
	}
	second := pairBrowser(t, s, code)
	w = request(t, s, "POST", "/logout", url.Values{"csrf": {csrf}}, first)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d", w.Code)
	}
	if request(t, s, "GET", "/", nil, first).Code != http.StatusSeeOther || request(t, s, "GET", "/", nil, second).Code != http.StatusOK {
		t.Fatal("logout failed to revoke exactly one browser")
	}
	if request(t, testShell(t), "GET", "/", nil, second).Code != http.StatusSeeOther {
		t.Fatal("browser survived manager restart")
	}
}

func TestFragmentsAssetsAndUnknownRoutes(t *testing.T) {
	s := testShell(t)
	cookie := pairBrowser(t, s, s.initialCode)
	for _, restore := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/?view=projects", nil)
		r.Host = "127.0.0.1:7331"
		r.AddCookie(cookie)
		r.Header.Set("HX-Request", "true")
		if restore {
			r.Header.Set("HX-History-Restore-Request", "true")
		}
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<!doctype html>") != restore {
			t.Fatalf("restore=%v response = %d", restore, w.Code)
		}
	}
	for _, path := range []string{"/static/app.css", "/static/app.js", "/static/vendor/htmx-2.0.10.min.js", "/healthz"} {
		if w := request(t, s, "GET", path, nil); w.Code != http.StatusOK {
			t.Fatalf("public asset %s = %d", path, w.Code)
		}
	}
	for _, path := range []string{"/static/", "/templates/pages.html", "/static/vendor/htmx-LICENSE", "/?view=unknown"} {
		if w := request(t, s, "GET", path, nil, cookie); w.Code != http.StatusNotFound {
			t.Fatalf("unknown %s = %d", path, w.Code)
		}
	}
}

func TestListenValidationAndCanceledStart(t *testing.T) {
	for _, address := range []string{"0.0.0.0:7331", ":7331", "192.168.0.1:7331", "localhost:7331", "[::]:7331", "127.0.0.1:99999"} {
		if err := (Options{Listen: address}).Validate(); err == nil {
			t.Errorf("accepted non-local/invalid address %s", address)
		}
	}
	for _, address := range []string{"", "127.0.0.1:0", "[::1]:7331"} {
		if err := (Options{Listen: address}).Validate(); err != nil {
			t.Errorf("rejected loopback %s: %v", address, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	if err := Run(ctx, Options{}, &output); !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("canceled startup: %v, output %q", err, output.String())
	}
}
