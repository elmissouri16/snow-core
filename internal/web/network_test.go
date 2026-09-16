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

func TestTrustedLANShellServesLocalhostAsIndependentExactOrigin(t *testing.T) {
	deployment, err := (Options{Listen: "192.168.1.50:7331"}).deployment()
	if err != nil {
		t.Fatal(err)
	}
	s, err := newShellWithNetwork(deployment.publicOrigin, "test", deployment.policy())
	if err != nil {
		t.Fatal(err)
	}
	const localOrigin = "http://127.0.0.1:7331"
	handler, err := s.handlerForOrigin(localOrigin, requestNetworkPolicy{profile: "local"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Host = "127.0.0.1:7331"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Location") != "" {
		t.Fatalf("localhost login = %d, %q", response.Code, response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != pairCookie {
		t.Fatalf("localhost pairing cookies = %+v", cookies)
	}

	form := url.Values{"csrf": {cookies[0].Value}, "code": {s.initialCode}}
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	login.Host = "127.0.0.1:7331"
	login.Header.Set("Origin", localOrigin)
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.AddCookie(cookies[0])
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther {
		t.Fatalf("localhost pairing response = %d", loginResponse.Code)
	}
	var localSession *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		switch cookie.Name {
		case sessionCookie:
			if cookie.Value != "" {
				localSession = cookie
			}
		case lanSessionCookie, lanPairCookie:
			t.Fatalf("localhost login issued LAN cookie %q", cookie.Name)
		}
	}
	if localSession == nil {
		t.Fatalf("localhost login cookies = %+v", loginResponse.Result().Cookies())
	}

	lanHandler := s.handler()
	lanPage := httptest.NewRequest(http.MethodGet, "/login", nil)
	lanPage.Host = deployment.publicHost
	lanPageResponse := httptest.NewRecorder()
	lanHandler.ServeHTTP(lanPageResponse, lanPage)
	lanPair := lanPageResponse.Result().Cookies()[0]
	lanForm := url.Values{"csrf": {lanPair.Value}, "code": {s.initialCode}}
	lanLogin := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(lanForm.Encode()))
	lanLogin.Host = deployment.publicHost
	lanLogin.Header.Set("Origin", deployment.publicOrigin)
	lanLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lanLogin.AddCookie(lanPair)
	lanLoginResponse := httptest.NewRecorder()
	lanHandler.ServeHTTP(lanLoginResponse, lanLogin)
	var lanSession *http.Cookie
	for _, cookie := range lanLoginResponse.Result().Cookies() {
		if cookie.Name == lanSessionCookie && cookie.Value != "" {
			lanSession = cookie
		}
	}
	if lanLoginResponse.Code != http.StatusSeeOther || lanSession == nil {
		t.Fatalf("LAN login = %d, %+v", lanLoginResponse.Code, lanLoginResponse.Result().Cookies())
	}
	if len(s.access.sessions) != 2 {
		t.Fatalf("local and LAN handlers do not share access state: %d sessions", len(s.access.sessions))
	}
	for _, authenticated := range []struct {
		name    string
		handler http.Handler
		host    string
		cookie  *http.Cookie
	}{
		{name: "localhost", handler: handler, host: "127.0.0.1:7331", cookie: localSession},
		{name: "LAN", handler: lanHandler, host: deployment.publicHost, cookie: lanSession},
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = authenticated.host
		request.AddCookie(authenticated.cookie)
		response := httptest.NewRecorder()
		authenticated.handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Errorf("%s authenticated response = %d", authenticated.name, response.Code)
		}
	}
	for _, replay := range []struct {
		name    string
		handler http.Handler
		host    string
		cookie  *http.Cookie
	}{
		{name: "local token renamed for LAN", handler: lanHandler, host: deployment.publicHost, cookie: localCookie(lanSessionCookie, localSession.Value, 0)},
		{name: "LAN token renamed for localhost", handler: handler, host: "127.0.0.1:7331", cookie: localCookie(sessionCookie, lanSession.Value, 0)},
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Host = replay.host
		request.AddCookie(replay.cookie)
		response := httptest.NewRecorder()
		replay.handler.ServeHTTP(response, request)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
			t.Errorf("%s response = %d, %q", replay.name, response.Code, response.Header().Get("Location"))
		}
	}

	for name, request := range map[string]*http.Request{
		"foreign host": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Host = "localhost:7331"
			return r
		}(),
		"LAN origin": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/login", nil)
			r.Host = "127.0.0.1:7331"
			r.Header.Set("Origin", deployment.publicOrigin)
			return r
		}(),
		"absolute URL": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, localOrigin+"/", nil)
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
