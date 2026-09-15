package web

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func formAdmissionFixtureChange(s *shell, cookie *http.Cookie, mode string) {
	key := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	browser := s.access.sessions[key]
	switch mode {
	case "removed":
		delete(s.access.sessions, key)
		return
	case "expired":
		browser.Created = s.now().Add(-browserLifetime)
	case "identity":
		browser.ID = s.newBrowserIDLocked()
	case "token":
		browser.CSRF = "changed-fixture-form-token"
	case "storage":
		s.access.failed = true
	}
	s.access.sessions[key] = browser
}

func formAdmissionStatus(mode string) int {
	switch mode {
	case "current":
		return http.StatusOK
	case "storage":
		return http.StatusServiceUnavailable
	default:
		return http.StatusUnauthorized
	}
}

func TestFormAuthorizationAdmission(t *testing.T) {
	for _, limit := range []int64{8 << 10, 16 << 10} {
		for _, mode := range []string{"current", "removed", "expired", "identity", "token", "storage"} {
			t.Run(mode, func(t *testing.T) {
				s, registry := persistentShell(t, filepath.Join(t.TempDir(), "manager"))
				cookie := pairBrowser(t, s, s.initialCode)
				values := url.Values{"csrf": {csrfFor(t, s, cookie)}}
				body := &accessMutationReader{Reader: strings.NewReader(values.Encode()), mutate: func() {
					if mode == "storage" {
						if err := registry.Close(); err != nil {
							t.Fatal("fixture storage close failed")
						}
						return
					}
					formAdmissionFixtureChange(s, cookie, mode)
				}}
				r := httptest.NewRequest(http.MethodPost, "/", body)
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.AddCookie(cookie)
				w := httptest.NewRecorder()
				var browser browserSession
				var admitted bool
				if limit == 8<<10 {
					browser, admitted = s.authorizeForm(w, r)
				} else {
					browser, admitted = s.authorizeFormLimit(w, r, limit)
				}
				if w.Code != formAdmissionStatus(mode) || admitted != (mode == "current") {
					t.Fatal("form admission result mismatch")
				}
				if !admitted && browser != (browserSession{}) {
					t.Fatal("rejected form retained admission data")
				}
				if admitted && (!validBrowserID(browser.ID) || browser.CSRF != values.Get("csrf")) {
					t.Fatal("admitted form identity mismatch")
				}
				if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), values.Get("csrf")) {
					t.Fatal("form response hygiene mismatch")
				}
			})
		}
	}
}

func TestHostMutationAdmission(t *testing.T) {
	for _, endpoint := range []string{"defaults", "api-key"} {
		for _, mode := range []string{"current", "removed", "expired", "identity", "token", "storage"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				s, cookie, backend := apiKeyTestShell(t)
				path := "/settings/host"
				values := url.Values{"csrf": {"csrf-fixture"}, "scope": {"global"}, "expected_revision": {strings.Repeat("a", 64)}, "thinking_op": {"set"}, "thinking": {"high"}}
				if endpoint == "api-key" {
					path = "/settings/providers/openai-compatible/api-key"
					values = apiKeyForm()
					if w := apiKeyRequest(s, cookie, http.MethodGet, "openai-compatible", nil); w.Code != http.StatusOK {
						t.Fatal("fixture inspection failed")
					}
				}
				body := &accessMutationReader{Reader: strings.NewReader(values.Encode()), mutate: func() {
					formAdmissionFixtureChange(s, cookie, mode)
				}}
				r := httptest.NewRequest(http.MethodPost, s.origin+path, body)
				r.URL.Scheme, r.URL.Host = "", ""
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", s.origin)
				r.AddCookie(cookie)
				w := httptest.NewRecorder()
				s.handler().ServeHTTP(w, r)
				wantWrites := 0
				if mode == "current" {
					wantWrites = 1
				}
				if w.Code != formAdmissionStatus(mode) || backend.writes+backend.sets != wantWrites {
					t.Fatal("host admission result mismatch")
				}
				if strings.Contains(w.Body.String(), apiKeyCanary) || strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), values.Get("csrf")) {
					t.Fatal("host response hygiene mismatch")
				}
				if endpoint == "api-key" && (r.PostForm.Get("secret") != "" || r.Form.Get("secret") != "") {
					t.Fatal("host form hygiene mismatch")
				}
			})
		}
	}
}

type admittedAPIKeyFixture struct {
	apiKeyFake
	onAdmission func()
}

func (f *admittedAPIKeyFixture) SetAPIKey(ctx context.Context, input protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error) {
	f.onAdmission()
	if err := ctx.Err(); err != nil {
		return protocol.HostAPIKeyStatusResponse{}, err
	}
	return f.apiKeyFake.SetAPIKey(ctx, input)
}

func TestHostMutationAdmittedCompletion(t *testing.T) {
	s, cookie, _ := apiKeyTestShell(t)
	backend := &admittedAPIKeyFixture{onAdmission: func() { formAdmissionFixtureChange(s, cookie, "removed") }}
	s.hostSettings = backend
	if w := apiKeyRequest(s, cookie, http.MethodGet, "openai-compatible", nil); w.Code != http.StatusOK {
		t.Fatal("fixture inspection failed")
	}
	w := apiKeyRequest(s, cookie, http.MethodPost, "openai-compatible", apiKeyForm())
	if w.Code != http.StatusOK || backend.sets != 1 || !backend.sawSecret {
		t.Fatal("admitted completion mismatch")
	}
	if _, ok := s.browser(browserRequest(cookie)); ok {
		t.Fatal("fixture authority state mismatch")
	}
	if strings.Contains(w.Body.String(), apiKeyCanary) {
		t.Fatal("completion response hygiene mismatch")
	}
}
