//go:build darwin || linux

package web

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The parent-owned production mux calls this same registration method. This
// fixture lets the inventory implementation be verified before surface wiring;
// origin/host rejection below is exercised through the production handler.
func inventoryRequest(s *shell, cookie *http.Cookie, method, path string, values url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	r.Host = s.host
	r.Header.Set("Origin", s.origin)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	mux := http.NewServeMux()
	s.registerBrowserAccessRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func inventoryFor(t *testing.T, s *shell, cookie *http.Cookie) browserInventory {
	t.Helper()
	response := inventoryRequest(s, cookie, "GET", "/access/browsers", nil)
	var result browserInventory
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("inventory: %d %s", response.Code, response.Body.String())
	}
	return result
}

func revokeValues(csrf string) url.Values { return url.Values{"csrf": {csrf}, "confirm": {"revoke"}} }
func revokePath(id string) string         { return "/access/browsers/" + id + "/revoke" }

func TestBrowserInventoryPrivateProjectionAndExactRevokeRestart(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	first := pairBrowser(t, s, s.initialCode)
	second := pairBrowser(t, s, s.initialCode)
	third := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, first)
	before := inventoryFor(t, s, first)
	if len(before.Browsers) != 3 || before.Limit != 8 || !before.Browsers[0].Current {
		t.Fatal("incorrect inventory/current/limit")
	}
	ids := make(map[string]bool)
	for _, browser := range before.Browsers {
		if !validBrowserID(browser.ID) || !validBrowserLabel(browser.Label) || ids[browser.ID] || browser.LastUsed.Before(browser.Created) || !browser.Expires.Equal(browser.Created.Add(browserLifetime)) {
			t.Fatal("invalid public browser metadata")
		}
		ids[browser.ID] = true
	}
	public := inventoryRequest(s, first, "GET", "/access/browsers", nil).Body.String()
	for _, cookie := range []*http.Cookie{first, second, third} {
		hash := sha256.Sum256([]byte(cookie.Value))
		for _, secret := range []string{cookie.Value, hex.EncodeToString(hash[:]), csrfFor(t, s, cookie), s.initialCode, hex.EncodeToString(s.access.key[:])} {
			if strings.Contains(public, secret) {
				t.Fatal("private access value in inventory")
			}
		}
	}
	if strings.Contains(public, "csrf") || strings.Contains(public, "hash") || strings.Contains(public, "pair_code") {
		t.Fatal("private field in DTO")
	}
	secondID := inventoryFor(t, s, second).Browsers[0].ID
	path := filepath.Join(manager, accessFile)
	oldInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	response := inventoryRequest(s, first, "POST", revokePath(secondID), revokeValues(csrf))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"signed_out":false`) || len(response.Result().Cookies()) != 0 {
		t.Fatalf("revoke other: %d %s", response.Code, response.Body.String())
	}
	newInfo, err := os.Lstat(path)
	if err != nil || newInfo.Mode().Perm() != 0o600 || os.SameFile(oldInfo, newInfo) {
		t.Fatal("revoke was not private atomic replacement")
	}
	if _, ok := s.browser(browserRequest(second)); ok {
		t.Fatal("target retained authority")
	}
	if _, ok := s.browser(browserRequest(first)); !ok {
		t.Fatal("actor revoked")
	}
	if _, ok := s.browser(browserRequest(third)); !ok {
		t.Fatal("unrelated browser revoked")
	}
	if s.access.pairCode != s.initialCode {
		t.Fatal("individual revoke rotated pairing code")
	}
	if response := inventoryRequest(s, first, "POST", revokePath(secondID), revokeValues(csrf)); response.Code != 404 {
		t.Fatal("stale target accepted")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, registry = persistentShell(t, manager)
	after := inventoryFor(t, s, first)
	if len(after.Browsers) != 2 || after.Browsers[0].ID != before.Browsers[0].ID {
		t.Fatal("IDs/revocation not durable")
	}
	if _, ok := s.browser(browserRequest(second)); ok {
		t.Fatal("revoked browser resurrected")
	}
	response = inventoryRequest(s, first, "POST", revokePath(after.Browsers[0].ID), revokeValues(csrf))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"signed_out":true`) || response.Header().Get("Clear-Site-Data") == "" {
		t.Fatalf("current revoke: %d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || cookies[0].MaxAge != -1 {
		t.Fatal("current browser cookie not cleared")
	}
	if _, ok := s.browser(browserRequest(first)); ok {
		t.Fatal("current browser retained authority")
	}
	if len(inventoryFor(t, s, third).Browsers) != 1 {
		t.Fatal("current revoke changed another browser")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = persistentShell(t, manager)
	if _, ok := s.browser(browserRequest(first)); ok {
		t.Fatal("current revoke did not survive restart")
	}
	if len(inventoryFor(t, s, third).Browsers) != 1 {
		t.Fatal("unrelated browser lost on restart")
	}
}

func TestBrowserInventoryLegacyMigrationRevokesUnscopedSessions(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	cookie := pairBrowser(t, s, s.initialCode)
	pairing := s.initialCode
	state := s.access.snapshot()
	state.Version = 2
	for i := range state.Browsers {
		state.Browsers[i].Profile = ""
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(manager, accessFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	s, registry = persistentShell(t, manager)
	if _, ok := s.browser(browserRequest(cookie)); ok || len(s.access.sessions) != 0 {
		t.Fatal("legacy unscoped browser retained authority")
	}
	if s.initialCode != pairing {
		t.Fatal("migration rotated reusable pairing code")
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode().Perm() != 0o600 || os.SameFile(before, after) {
		t.Fatal("migration not atomic/private")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &state); err != nil || state.Version != 3 || len(state.Browsers) != 0 {
		t.Fatal("scoped schema migration not saved")
	}
	paired := pairBrowser(t, s, pairing)
	if _, ok := s.browser(browserRequest(paired)); !ok {
		t.Fatal("migration prevented re-pairing")
	}
}

func TestBrowserInventoryValidationAndCapacity(t *testing.T) {
	s := testShell(t)
	cookie := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, cookie)
	id := inventoryFor(t, s, cookie).Browsers[0].ID
	for _, tc := range []struct {
		name, id string
		values   url.Values
		want     int
	}{
		{"cookie not ID", cookie.Value, revokeValues(csrf), 400},
		{"invalid ID", "browser_no", revokeValues(csrf), 400},
		{"stale", "browser_" + strings.Repeat("0", 32), revokeValues(csrf), 404},
		{"missing confirmation", id, url.Values{"csrf": {csrf}}, 400},
		{"bad CSRF", id, revokeValues("bad"), 403},
		{"duplicate confirmation", id, url.Values{"csrf": {csrf}, "confirm": {"revoke", "revoke"}}, 400},
		{"oversized", id, url.Values{"csrf": {csrf}, "confirm": {strings.Repeat("x", 9<<10)}}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if response := inventoryRequest(s, cookie, "POST", revokePath(tc.id), tc.values); response.Code != tc.want {
				t.Fatalf("status %d want %d", response.Code, tc.want)
			}
		})
	}
	if len(inventoryFor(t, s, cookie).Browsers) != 1 {
		t.Fatal("invalid request changed access")
	}
	if response := inventoryRequest(s, nil, "GET", "/access/browsers", nil); response.Code != 401 {
		t.Fatal("unauthenticated inventory")
	}
	if response := inventoryRequest(s, nil, "POST", revokePath(id), revokeValues(csrf)); response.Code != 401 {
		t.Fatal("unauthenticated revoke")
	}
	for range 7 {
		pairBrowser(t, s, s.initialCode)
	}
	if len(inventoryFor(t, s, cookie).Browsers) != 8 {
		t.Fatal("inventory not bounded by browser limit")
	}
	preauth := request(t, s, "GET", "/login", nil).Result().Cookies()[0]
	if response := request(t, s, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {s.initialCode}}, preauth); response.Code != 409 {
		t.Fatal("ninth browser admitted")
	}
	target := inventoryFor(t, s, cookie).Browsers[1].ID
	if response := inventoryRequest(s, cookie, "POST", revokePath(target), revokeValues(csrf)); response.Code != 200 {
		t.Fatal("revoke failed")
	}
	pairBrowser(t, s, s.initialCode)
	if len(inventoryFor(t, s, cookie).Browsers) != 8 {
		t.Fatal("individual revocation did not free a slot")
	}
}

func TestBrowserInventoryProductionOriginBoundary(t *testing.T) {
	s := testShell(t)
	cookie := pairBrowser(t, s, s.initialCode)
	id := inventoryFor(t, s, cookie).Browsers[0].ID
	for _, tc := range []struct{ name, host, origin, site string }{
		{"foreign host", "attacker.test", testOrigin, ""},
		{"foreign origin", s.host, "https://attacker.test", ""},
		{"missing origin", s.host, "", ""},
		{"null origin", s.host, "null", ""},
		{"cross site", s.host, testOrigin, "cross-site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", revokePath(id), strings.NewReader(revokeValues(csrfFor(t, s, cookie)).Encode()))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("production origin boundary: %d", w.Code)
			}
		})
	}
	if len(inventoryFor(t, s, cookie).Browsers) != 1 {
		t.Fatal("boundary failure mutated browser")
	}
}

func TestBrowserInventoryFailClosedAfterAuthorization(t *testing.T) {
	for _, reason := range []string{"actor-revoked", "store-closed"} {
		t.Run(reason, func(t *testing.T) {
			manager := filepath.Join(t.TempDir(), "manager")
			s, registry := persistentShell(t, manager)
			actor := pairBrowser(t, s, s.initialCode)
			target := pairBrowser(t, s, s.initialCode)
			targetID := inventoryFor(t, s, target).Browsers[0].ID
			actorID := inventoryFor(t, s, actor).Browsers[0].ID
			actorCSRF, targetCSRF := csrfFor(t, s, actor), csrfFor(t, s, target)
			reader := &accessMutationReader{Reader: strings.NewReader(revokeValues(actorCSRF).Encode()), mutate: func() {
				if reason == "store-closed" {
					if err := registry.Close(); err != nil {
						t.Fatal(err)
					}
					return
				}
				if response := inventoryRequest(s, target, "POST", revokePath(actorID), revokeValues(targetCSRF)); response.Code != 200 {
					t.Fatal("fixture actor revoke failed")
				}
			}}
			r := httptest.NewRequest("POST", revokePath(targetID), reader)
			r.SetPathValue("browser", targetID)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.AddCookie(actor)
			w := httptest.NewRecorder()
			s.revokeBrowser(w, r)
			want := 401
			if reason == "store-closed" {
				want = 503
			}
			if w.Code != want || len(w.Result().Cookies()) != 0 {
				t.Fatalf("late authority failure: %d want %d", w.Code, want)
			}
			_, ok := s.browser(browserRequest(target))
			if reason == "actor-revoked" && !ok {
				t.Fatal("stale actor revoked target")
			}
			if reason == "store-closed" && ok {
				t.Fatal("storage failure retained authority")
			}
		})
	}
}

func TestBrowserInventoryConcurrentExactRevocation(t *testing.T) {
	s, _ := persistentShell(t, filepath.Join(t.TempDir(), "manager"))
	actor := pairBrowser(t, s, s.initialCode)
	target := pairBrowser(t, s, s.initialCode)
	id, csrf := inventoryFor(t, s, target).Browsers[0].ID, csrfFor(t, s, actor)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Go(func() { results <- inventoryRequest(s, actor, "POST", revokePath(id), revokeValues(csrf)).Code })
	}
	wg.Wait()
	close(results)
	counts := make(map[int]int)
	for status := range results {
		counts[status]++
	}
	if counts[200] != 1 || counts[404] != 1 || len(inventoryFor(t, s, actor).Browsers) != 1 {
		t.Fatal("concurrent revoke was not one exact commit")
	}
}

func TestBrowserInventoryLabelsAndStoredValidation(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("a", 81), "line\nbreak", "\u202eevil", " spaced", string([]byte{0xff})} {
		if validBrowserLabel(value) {
			t.Fatalf("invalid label accepted: %q", value)
		}
	}
	for _, value := range []string{"secret-device-123", "Chrome/123 secret-device", strings.Repeat("x", 1025)} {
		label := browserLabel(value)
		if !validBrowserLabel(label) || strings.Contains(label, "secret-device") {
			t.Fatal("raw UA retained")
		}
	}
	s := testShell(t)
	pairBrowser(t, s, s.initialCode)
	valid := s.access.snapshot()
	for _, mutate := range []func(*storedAccess){
		func(state *storedAccess) { state.Browsers[0].ID = state.Browsers[0].Hash },
		func(state *storedAccess) { state.Browsers[0].ID = "" },
		func(state *storedAccess) { state.Browsers[0].Label = strings.Repeat("x", 81) },
		func(state *storedAccess) {
			other := state.Browsers[0]
			other.Hash = randomToken()
			state.Browsers = append(state.Browsers, other)
		},
		func(state *storedAccess) { state.Version = 1 },
	} {
		data, err := json.Marshal(valid)
		if err != nil {
			t.Fatal(err)
		}
		var state storedAccess
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		mutate(&state)
		if state.valid() {
			t.Fatal("invalid inventory schema accepted")
		}
	}
	var rendered bytes.Buffer
	if err := templates.ExecuteTemplate(&rendered, "browser-inventory", pageData{CSRF: "fictional-form-token"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "Confirm revoke browser") || !strings.Contains(rendered.String(), "every 5 seconds") {
		t.Fatal("confirmation or reauthorization boundary absent")
	}
}

func TestBrowserInventoryRevokeExistingSSEFiveSecondReauthorization(t *testing.T) {
	if defaultStreamPolicy.auth != 5*time.Second {
		t.Fatal("existing reauthorization budget changed")
	}
	s, target, _, _, server, path := streamHTTPFixture(t, defaultStreamPolicy)
	actor := pairBrowser(t, s, s.initialCode)
	targetID := inventoryFor(t, s, target).Browsers[0].ID
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(target)
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), streamSnapshotBytes+1024)
	stream := streamTestReader{response: response, scanner: scanner}
	if event, _ := stream.next(t); event != "snapshot" {
		t.Fatal("missing initial stream")
	}
	start := time.Now()
	revoked := inventoryRequest(s, actor, "POST", revokePath(targetID), revokeValues(csrfFor(t, s, actor)))
	if revoked.Code != 200 {
		t.Fatalf("individual revoke: %d", revoked.Code)
	}
	if event, _ := stream.next(t); event != "auth_required" {
		t.Fatalf("revoked stream received %s", event)
	}
	// The unchanged production policy is 5 seconds, not an immediate kill.
	// Allow scheduling overhead in the wall-clock assertion (including race mode).
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("reauthorization exceeded existing timer budget: %v", elapsed)
	}
	if scanner.Scan() {
		t.Fatal("revoked stream did not close")
	}
	if _, ok := s.streamBrowser(browserRequest(target)); ok {
		t.Fatal("revoked stream browser can reconnect")
	}
	if _, ok := s.streamBrowser(browserRequest(actor)); !ok {
		t.Fatal("revocation rejected unrelated browser")
	}
}
