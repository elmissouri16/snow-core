package web

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const apiKeyCanary = "FIXTURE_ONLY_SECRET_DO_NOT_ECHO"

type apiKeyFake struct {
	hostSettingsFake
	inspected, sets                    int
	replace                            bool
	fail                               error
	mismatch                           bool
	sawExpected, sawSecret, sawReplace bool
}

func apiKeyTestStatus(provider string, replace bool) protocol.HostAPIKeyStatusResponse {
	return protocol.HostAPIKeyStatusResponse{ProviderID: provider, APIKeySupported: provider != "chatgpt", ReplaceRequired: replace, Revision: "missing", Status: protocol.HostProviderStatus{ProviderID: provider, State: "unavailable", Reason: "credential_missing", CheckedLocally: true}, CheckedLocally: true, AppliesTo: "future_runtime"}
}
func (f *apiKeyFake) InspectAPIKey(_ context.Context, provider string) (protocol.HostAPIKeyStatusResponse, error) {
	f.inspected++
	result := apiKeyTestStatus(provider, f.replace)
	if f.mismatch {
		result.ProviderID = "another-provider"
	}
	return result, f.fail
}
func (f *apiKeyFake) SetAPIKey(_ context.Context, input protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error) {
	f.sets++
	f.sawExpected = input.ExpectedRevision == "missing"
	f.sawSecret = input.Secret == apiKeyCanary
	f.sawReplace = input.ConfirmReplace
	result := apiKeyTestStatus(input.ProviderID, true)
	result.Revision = strings.Repeat("b", 64)
	result.Status.State, result.Status.Reason = "configured", "credential_present"
	return result, f.fail
}
func apiKeyTestShell(t *testing.T) (*shell, *http.Cookie, *apiKeyFake) {
	t.Helper()
	s, err := newShell("https://127.0.0.1:18443", "api-key-test")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("e", 64)
	s.access.sessions[sha256.Sum256([]byte(token))] = browserSession{ID: "browser_fixture", CSRF: "csrf-fixture", Created: s.now(), LastUsed: s.now()}
	fake := &apiKeyFake{}
	s.hostSettings = fake
	return s, &http.Cookie{Name: sessionCookie, Value: token, Secure: true}, fake
}
func apiKeyForm() url.Values {
	return url.Values{"csrf": {"csrf-fixture"}, "expected_revision": {"missing"}, "secret": {apiKeyCanary}, "confirm_replace": {"false"}, "confirm_save": {"host"}}
}
func apiKeyRequest(s *shell, cookie *http.Cookie, method, provider string, values url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	r := httptest.NewRequest(method, s.origin+"/settings/providers/"+provider+"/api-key", body)
	r.URL.Scheme, r.URL.Host = "", ""
	r.SetPathValue("provider", provider)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if method == "POST" {
		r.Header.Set("Origin", s.origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	if method == "GET" {
		s.inspectHostAPIKey(w, r)
	} else {
		s.setHostAPIKey(w, r)
	}
	return w
}

type apiKeyUnreadBody struct{ reads int }

func (r *apiKeyUnreadBody) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("secret body must not be read")
}
func (*apiKeyUnreadBody) Close() error { return nil }

func TestHostAPIKeyTransportGateBeforeSecretBody(t *testing.T) {
	for _, mode := range []string{"plain", "forwarded", "false-origin", "missing-origin", "duplicate-origin", "no-browser", "nonloopback", "false-tls-origin"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie, fake := apiKeyTestShell(t)
			body := &apiKeyUnreadBody{}
			r := httptest.NewRequest("POST", s.origin+"/settings/providers/openai-compatible/api-key", nil)
			r.URL.Scheme, r.URL.Host = "", ""
			r.Body = body
			r.ContentLength = 100
			r.SetPathValue("provider", "openai-compatible")
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", s.origin)
			r.AddCookie(cookie)
			switch mode {
			case "plain":
				r.TLS = nil
			case "forwarded":
				r.TLS = nil
				r.Header.Set("X-Forwarded-Proto", "https")
				r.Header.Set("Forwarded", "proto=https;host=127.0.0.1")
			case "false-origin":
				r.Header.Set("Origin", "https://evil.invalid")
			case "missing-origin":
				r.Header.Del("Origin")
			case "duplicate-origin":
				r.Header.Add("Origin", s.origin)
			case "no-browser":
				r.Header.Del("Cookie")
			case "nonloopback":
				s.origin = "https://example.invalid:18443"
				s.host = "example.invalid:18443"
				r.Host = s.host
				r.Header.Set("Origin", s.origin)
			case "false-tls-origin":
				s.origin = "http://127.0.0.1:18443"
				r.Header.Set("Origin", s.origin)
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			s.setHostAPIKey(w, r)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
				t.Fatalf("gate status = %d", w.Code)
			}
			if body.reads != 0 || fake.sets != 0 {
				t.Fatal("untrusted request read secret or reached backend")
			}
		})
	}
}
func TestHostAPIKeyHTTPInspectionBoundAndOneUse(t *testing.T) {
	s, cookie, fake := apiKeyTestShell(t)
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 0 {
		t.Fatal("write without inspection")
	}
	if w := apiKeyRequest(s, cookie, "GET", "openai-compatible", nil); w.Code != http.StatusOK {
		t.Fatalf("inspect %d", w.Code)
	}
	if w := apiKeyRequest(s, cookie, "POST", "opencode-go", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 0 {
		t.Fatal("inspection transferred to another provider")
	}
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	input := apiKeyForm()
	input.Set("expected_revision", strings.Repeat("a", 64))
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", input); w.Code != http.StatusConflict || fake.sets != 0 {
		t.Fatal("inspection revision not pinned")
	}
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm())
	if w.Code != http.StatusOK || fake.sets != 1 || !fake.sawSecret || !fake.sawExpected {
		t.Fatalf("valid write status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), apiKeyCanary) || strings.Contains(w.Body.String(), "\"secret\"") {
		t.Fatal("secret escaped response")
	}
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 1 {
		t.Fatal("inspection replay accepted")
	}
}
func TestHostAPIKeyHTTPStrictFieldsAndConsent(t *testing.T) {
	s, cookie, fake := apiKeyTestShell(t)
	for _, mutate := range []func(url.Values){
		func(v url.Values) { v.Set("csrf", "wrong") }, func(v url.Values) { v.Add("csrf", "csrf-fixture") }, func(v url.Values) { v.Set("provider_id", "opencode-go") }, func(v url.Values) { v.Set("cwd", "/tmp") }, func(v url.Values) { v.Set("scope", "project") }, func(v url.Values) { v.Set("headers", apiKeyCanary) },
		func(v url.Values) { v.Del("secret") }, func(v url.Values) { v.Add("secret", apiKeyCanary) }, func(v url.Values) { v.Set("secret", strings.Repeat("x", 4097)) }, func(v url.Values) { v.Set("secret", "key\ncontrol") }, func(v url.Values) { v.Set("secret", " padded ") },
		func(v url.Values) { v.Del("confirm_replace") }, func(v url.Values) { v.Set("confirm_replace", "yes") }, func(v url.Values) { v.Set("confirm_save", "other") }, func(v url.Values) { v.Del("confirm_save") }, func(v url.Values) { v.Set("expected_revision", "invalid") },
	} {
		apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
		input := apiKeyForm()
		mutate(input)
		w := apiKeyRequest(s, cookie, "POST", "openai-compatible", input)
		if w.Code < 400 || fake.sets != 0 || strings.Contains(w.Body.String(), apiKeyCanary) {
			t.Fatalf("invalid request admitted or echoed: status %d", w.Code)
		}
	}
	fake.replace = true
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 0 {
		t.Fatal("replacement without consent")
	}
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	input := apiKeyForm()
	input.Set("confirm_replace", "true")
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", input); w.Code != http.StatusOK || !fake.sawReplace {
		t.Fatal("explicit replacement rejected")
	}
}
func TestHostAPIKeyHTTPUnknownOutcomeAndExpiredInspection(t *testing.T) {
	s, cookie, fake := apiKeyTestShell(t)
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	fake.fail = errors.New(apiKeyCanary)
	w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm())
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), apiKeyCanary) {
		t.Fatal("worker error echoed or uncertainty not fenced")
	}
	fake.fail = nil
	apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm())
	if fake.sets != 1 {
		t.Fatal("unknown outcome auto-retry grant remained")
	}
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	oldNow := s.now
	s.now = func() time.Time { return oldNow().Add(6 * time.Minute) }
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 1 {
		t.Fatal("expired inspection accepted")
	}
	fake.mismatch = true
	if w := apiKeyRequest(s, cookie, "GET", "openai-compatible", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatal("mismatched projection accepted")
	}
}
func TestHostAPIKeyActualTLSConnectionAndUnicodeBound(t *testing.T) {
	s, cookie, fake := apiKeyTestShell(t)
	server := httptest.NewTLSServer(s.handler())
	defer server.Close()
	s.origin = server.URL
	s.host = strings.TrimPrefix(server.URL, "https://")
	client := server.Client()
	send := func(method string, values url.Values) (int, []byte) {
		t.Helper()
		var body io.Reader
		if values != nil {
			body = strings.NewReader(values.Encode())
		}
		request, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/settings/providers/openai-compatible/api-key", body)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Origin", s.origin)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	if code, _ := send("GET", nil); code != http.StatusOK {
		t.Fatalf("TLS inspection %d", code)
	}
	input := apiKeyForm()
	input.Set("secret", strings.Repeat("界", 1365)) // 4095 bytes, 12285 escaped bytes.
	code, data := send("POST", input)
	if code != http.StatusOK || fake.sets != 1 {
		t.Fatalf("bounded Unicode write %d", code)
	}
	var result protocol.HostAPIKeyStatusResponse
	if json.Unmarshal(data, &result) != nil || result.ProviderID != "openai-compatible" || strings.Contains(string(data), "界") {
		t.Fatal("secret response not redacted")
	}
}

func TestHostAPIKeyInspectionIsBrowserBoundAndFailureRetiresIt(t *testing.T) {
	s, cookie, fake := apiKeyTestShell(t)
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	token := strings.Repeat("f", 64)
	s.access.sessions[sha256.Sum256([]byte(token))] = browserSession{ID: "browser_other", CSRF: "csrf-fixture", Created: s.now(), LastUsed: s.now()}
	other := &http.Cookie{Name: sessionCookie, Value: token, Secure: true}
	if w := apiKeyRequest(s, other, "POST", "openai-compatible", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 0 {
		t.Fatal("another browser reused inspection")
	}
	fake.fail = errors.New("fixture unavailable")
	apiKeyRequest(s, cookie, "GET", "openai-compatible", nil)
	fake.fail = nil
	if w := apiKeyRequest(s, cookie, "POST", "openai-compatible", apiKeyForm()); w.Code != http.StatusConflict || fake.sets != 0 {
		t.Fatal("failed inspection retained prior write grant")
	}
}
