package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPrivateNetworkDeploymentValidation(t *testing.T) {
	for _, opts := range []Options{
		{},
		{Listen: "127.0.0.1:7331"},
		{Listen: "[::1]:7331"},
		{Listen: "192.168.1.50:7331"},
		{Listen: "10.0.0.4:7331"},
		{Listen: "[fd00::50]:7331"},
	} {
		if err := opts.Validate(); err != nil {
			t.Errorf("valid deployment rejected (%+v): %v", opts, err)
		}
	}
	for _, address := range []string{
		"localhost:7331",
		"0.0.0.0:7331",
		"[::]:7331",
		"203.0.113.4:7331",
		"224.0.0.1:7331",
		"[ff02::1]:7331",
		"[fe80::1%en0]:7331",
		"192.168.1.50:07331",
	} {
		if err := (Options{Listen: address}).Validate(); err == nil {
			t.Errorf("unsafe listener accepted: %q", address)
		}
	}
}

func TestTrustedLANHTTPUsesExactHostOriginAndCookies(t *testing.T) {
	deployment, err := (Options{Listen: "192.168.1.50:7331"}).deployment()
	if err != nil {
		t.Fatal(err)
	}
	if deployment.mode != deploymentTrustedLANHTTP || deployment.publicOrigin != "http://192.168.1.50:7331" || deployment.publicHost != "192.168.1.50:7331" {
		t.Fatalf("trusted-LAN deployment = %+v", deployment)
	}
	s, err := newShellWithNetwork(deployment.publicOrigin, "test", deployment.policy())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Host = deployment.publicHost
	request.RemoteAddr = "192.168.1.51:48200"
	response := httptest.NewRecorder()
	s.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login response = %d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != lanPairCookie || cookies[0].Secure {
		t.Fatalf("trusted-LAN pairing cookies = %+v", cookies)
	}

	form := url.Values{"csrf": {cookies[0].Value}, "code": {s.initialCode}}
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	login.Host = deployment.publicHost
	login.RemoteAddr = "192.168.1.51:48200"
	login.Header.Set("Origin", deployment.publicOrigin)
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.AddCookie(cookies[0])
	loginResponse := httptest.NewRecorder()
	s.handler().ServeHTTP(loginResponse, login)
	var sessionSet, pairCleared bool
	for _, cookie := range loginResponse.Result().Cookies() {
		switch cookie.Name {
		case lanSessionCookie:
			sessionSet = cookie.Value != "" && !cookie.Secure
		case lanPairCookie:
			pairCleared = cookie.MaxAge < 0
		case sessionCookie, pairCookie:
			t.Fatalf("trusted-LAN login reused loopback cookie %q", cookie.Name)
		}
	}
	if !sessionSet || !pairCleared {
		t.Fatalf("trusted-LAN login cookies = %+v", loginResponse.Result().Cookies())
	}

	for name, mutate := range map[string]func(*http.Request){
		"foreign Host":   func(r *http.Request) { r.Host = "192.168.1.51:7331" },
		"foreign Origin": func(r *http.Request) { r.Header.Set("Origin", "http://192.168.1.51:7331") },
		"forwarded Host": func(r *http.Request) {
			r.Header.Set("X-Forwarded-Host", deployment.publicHost)
			r.Host = "127.0.0.1:7331"
		},
	} {
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		r.Host = deployment.publicHost
		r.RemoteAddr = "192.168.1.51:48200"
		r.Header.Set("Origin", deployment.publicOrigin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mutate(r)
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s response = %d", name, w.Code)
		}
	}
}

func TestTrustedLANLocalhostRedirect(t *testing.T) {
	handler := localRedirectHandler("http://127.0.0.1:7331", "http://192.168.1.50:7331")
	request := httptest.NewRequest(http.MethodGet, "/settings?view=network", nil)
	request.Host = "127.0.0.1:7331"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "http://192.168.1.50:7331/settings?view=network" {
		t.Fatalf("localhost redirect = %d, %q", response.Code, response.Header().Get("Location"))
	}
	for name, request := range map[string]*http.Request{
		"foreign host": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Host = "localhost:7331"
			return r
		}(),
		"mutation": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/login", nil)
			r.Host = "127.0.0.1:7331"
			return r
		}(),
		"absolute URL": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7331/", nil)
			r.Host = "127.0.0.1:7331"
			return r
		}(),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s response = %d", name, response.Code)
		}
	}
	for input, want := range map[string]string{
		"192.168.1.50:7331": "127.0.0.1:7331",
		"[fd00::50]:7441":   "127.0.0.1:7441",
	} {
		address, err := loopbackAddressFor(stringAddress(input))
		if err != nil || address != want {
			t.Errorf("loopbackAddressFor(%q) = %q, %v; want %q", input, address, err, want)
		}
	}
}

type stringAddress string

func (a stringAddress) Network() string { return "tcp" }
func (a stringAddress) String() string  { return string(a) }

func TestCanonicalBrowserOrigin(t *testing.T) {
	for input, expected := range map[string]string{
		"http://127.0.0.1:80": "http://127.0.0.1",
		"http://[::1]:7331":   "http://[::1]:7331",
	} {
		canonical, host, err := canonicalBrowserOrigin(input)
		if err != nil || canonical != expected || host != strings.TrimPrefix(expected, "http://") {
			t.Errorf("canonicalBrowserOrigin(%q) = %q, %q, %v; want %q", input, canonical, host, err, expected)
		}
	}
	canonical, host, err := canonicalTrustedLANOrigin("http://192.168.1.50:7331")
	if err != nil || canonical != "http://192.168.1.50:7331" || host != "192.168.1.50:7331" {
		t.Fatalf("trusted LAN origin = %q, %q, %v", canonical, host, err)
	}
	for _, input := range []string{
		"https://127.0.0.1:7331",
		"http://localhost:7331",
		"http://192.168.1.50:7331",
		"http://127.0.0.1:0",
		"http://127.0.0.1:07331",
		"http://127.0.0.1:7331/",
		"http://user@127.0.0.1:7331",
	} {
		if _, _, err := canonicalBrowserOrigin(input); err == nil {
			t.Errorf("unsafe loopback origin accepted: %q", input)
		}
	}
}
