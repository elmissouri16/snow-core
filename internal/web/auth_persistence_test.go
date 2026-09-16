//go:build darwin || linux

package web

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"io"
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

func persistentShell(t *testing.T, manager string) (*shell, *Registry) {
	t.Helper()
	registry := registryTestOpen(t, manager)
	s := testShell(t)
	if err := s.restoreAccess(t.Context(), registry); err != nil {
		t.Fatal(err)
	}
	return s, registry
}

func browserRequest(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	return r
}

func TestAccessPersistenceRestartAndReusableCode(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	code := s.initialCode
	first := pairBrowser(t, s, code)
	csrf := csrfFor(t, s, first)
	if first.MaxAge != int(browserLifetime/time.Second) {
		t.Fatal("wrong cookie lifetime")
	}
	// Pre-auth CSRF also survives restart because its signing key is durable.
	preauth := request(t, s, "GET", "/login", nil).Result().Cookies()[0]
	data, err := os.ReadFile(filepath.Join(manager, accessFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), first.Value) {
		t.Fatal("raw browser token persisted")
	}
	info, err := os.Lstat(filepath.Join(manager, accessFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("access file is not private")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = persistentShell(t, manager)
	if s.initialCode != code {
		t.Fatal("restart rotated unexpired pairing code")
	}
	if got := csrfFor(t, s, first); got != csrf {
		t.Fatal("restart changed browser CSRF")
	}
	login := request(t, s, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {code}}, preauth)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("restart lost pairing/CSRF: %d", login.Code)
	}
	pairBrowser(t, s, code)
	if len(s.access.sessions) != 3 {
		t.Fatal("pairing code was not reusable")
	}
}

func TestAccessPersistenceRotationAndLogout(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	original := s.initialCode
	first := pairBrowser(t, s, original)
	second := pairBrowser(t, s, original)
	csrf := csrfFor(t, s, first)
	if w := request(t, s, "POST", "/access/pair", url.Values{"csrf": {csrf}}, first); w.Code != http.StatusSeeOther {
		t.Fatalf("rotate: %d", w.Code)
	}
	rotated := s.takePairingCode(browserRequest(first))
	if rotated == "" || rotated == original {
		t.Fatal("pairing code did not rotate")
	}
	if w := request(t, s, "POST", "/logout", url.Values{"csrf": {csrf}}, first); w.Code != http.StatusSeeOther {
		t.Fatalf("logout: %d", w.Code)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = persistentShell(t, manager)
	if s.initialCode != rotated {
		t.Fatal("rotation did not persist")
	}
	if _, ok := s.browser(browserRequest(first)); ok {
		t.Fatal("logout did not persist")
	}
	if _, ok := s.browser(browserRequest(second)); !ok {
		t.Fatal("logout revoked another browser")
	}
	preauth := request(t, s, "GET", "/login", nil).Result().Cookies()[0]
	if w := request(t, s, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {original}}, preauth); w.Code != http.StatusUnauthorized {
		t.Fatalf("old code accepted: %d", w.Code)
	}
	pairBrowser(t, s, rotated)
}

// Invoke the handler directly so this test does not depend on route wiring,
// which is owned by the manager surface rather than the persistence layer.
func revokeAllRequest(s *shell, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/access/revoke-all", strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.revokeAll(w, r)
	return w
}

func TestAccessPersistenceRevokeAll(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	original := s.initialCode
	first := pairBrowser(t, s, original)
	second := pairBrowser(t, s, original)
	if w := revokeAllRequest(s, first, "wrong"); w.Code != http.StatusForbidden {
		t.Fatal("revoke-all lacked CSRF enforcement")
	}
	if w := revokeAllRequest(s, first, csrfFor(t, s, first)); w.Code != http.StatusSeeOther {
		t.Fatalf("revoke-all: %d", w.Code)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = persistentShell(t, manager)
	for _, cookie := range []*http.Cookie{first, second} {
		if _, ok := s.browser(browserRequest(cookie)); ok {
			t.Fatal("revoked browser survived restart")
		}
	}
	if s.initialCode == original {
		t.Fatal("revoke-all retained pairing code")
	}
	pairBrowser(t, s, s.initialCode)
}

func TestAccessPersistenceThirtyDayExpiry(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	start := s.now()
	code := s.initialCode
	first := pairBrowser(t, s, code)
	s.now = func() time.Time { return start.Add(29 * 24 * time.Hour) }
	if _, ok := s.browser(browserRequest(first)); !ok {
		t.Fatal("browser expired before 30 days")
	}
	pairBrowser(t, s, code)
	s.now = func() time.Time { return start.Add(31 * 24 * time.Hour) }
	if _, ok := s.browser(browserRequest(first)); ok {
		t.Fatal("recent activity extended absolute expiry")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	registry = registryTestOpen(t, manager)
	restored := testShell(t)
	restored.now = s.now
	if err := restored.restoreAccess(t.Context(), registry); err != nil {
		t.Fatal(err)
	}
	if restored.initialCode == code {
		t.Fatal("expired startup code not rotated")
	}
	if _, ok := restored.browser(browserRequest(first)); ok {
		t.Fatal("expired browser resurrected")
	}
	pairBrowser(t, restored, restored.initialCode)
}

func TestAccessPersistenceRejectsUnsafeOrMalformedFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "public", "malformed", "oversized", "directory", "duplicate", "unknown", "invalid-state"} {
		t.Run(kind, func(t *testing.T) {
			manager := filepath.Join(t.TempDir(), "manager")
			s, registry := persistentShell(t, manager)
			cookie := pairBrowser(t, s, s.initialCode)
			if err := registry.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(manager, accessFile)
			switch kind {
			case "symlink", "hardlink":
				target := filepath.Join(manager, "saved-access")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "symlink" {
					err = os.Symlink(target, path)
				} else {
					err = os.Link(target, path)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "public":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				data := []byte("{")
				switch kind {
				case "oversized":
					data = []byte(strings.Repeat("x", maxAccessBytes+1))
				case "duplicate":
					data = []byte(`{"version":1,"version":1}`)
				case "unknown":
					data = []byte(`{"version":1,"unexpected":true}`)
				case "invalid-state":
					state := s.access.snapshot()
					state.Attempts = 21
					var err error
					data, err = json.Marshal(state)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			registry = registryTestOpen(t, manager)
			restored := testShell(t)
			if err := restored.restoreAccess(t.Context(), registry); err == nil {
				t.Fatal("unsafe/malformed access restored")
			}
			if _, ok := restored.browser(browserRequest(cookie)); ok {
				t.Fatal("failed restore granted browser access")
			}
			preauth := request(t, restored, "GET", "/login", nil).Result().Cookies()[0]
			w := request(t, restored, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {restored.initialCode}}, preauth)
			if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
				t.Fatal("failed restore granted login")
			}
		})
	}
}

func TestAccessPersistenceStaleAndInPlaceWritesFailClosed(t *testing.T) {
	for _, kind := range []string{"stale-shell", "in-place", "deleted", "closed", "directory-replaced"} {
		t.Run(kind, func(t *testing.T) {
			manager := filepath.Join(t.TempDir(), "manager")
			s, registry := persistentShell(t, manager)
			cookie := pairBrowser(t, s, s.initialCode)
			preauth := request(t, s, "GET", "/login", nil).Result().Cookies()[0]
			switch kind {
			case "stale-shell":
				other := testShell(t)
				if err := other.restoreAccess(t.Context(), registry); err != nil {
					t.Fatal(err)
				}
				pairBrowser(t, other, other.initialCode)
			case "in-place":
				data, err := os.ReadFile(filepath.Join(manager, accessFile))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(manager, accessFile), append(data, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := os.Remove(filepath.Join(manager, accessFile)); err != nil {
					t.Fatal(err)
				}
			case "closed":
				if err := registry.Close(); err != nil {
					t.Fatal(err)
				}
			case "directory-replaced":
				if err := os.Rename(manager, manager+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(manager, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if _, ok := s.browser(browserRequest(cookie)); ok {
				t.Fatal("unsafe backing granted browser access")
			}
			w := request(t, s, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {s.initialCode}}, preauth)
			if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
				t.Fatal("unsafe backing granted login")
			}
		})
	}
}

func TestAccessPersistenceRateLimitSurvivesRestart(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	now := s.now()
	s.now = func() time.Time { return now }
	preauth := request(t, s, "GET", "/login", nil).Result().Cookies()[0]
	for attempt := range maxPairingAttempts {
		source := fmt.Sprintf("192.0.2.%d:1234", attempt/maxPairingAttemptsPerSource+1)
		if w := requestFrom(t, s, source, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {"wrong"}}, preauth); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt: %d", w.Code)
		}
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = persistentShell(t, manager)
	s.now = func() time.Time { return now }
	for range 25 {
		if w := request(t, s, "POST", "/login", url.Values{"csrf": {preauth.Value}, "code": {s.initialCode}}, preauth); w.Code != http.StatusTooManyRequests {
			t.Fatalf("limit lost: %d", w.Code)
		}
	}
	if s.access.attempts != maxPairingAttempts {
		t.Fatal("attempt counter unbounded")
	}
	s.now = func() time.Time { return now.Add(time.Minute) }
	pairBrowser(t, s, s.initialCode)
}

func TestAccessPersistenceConcurrentPairingAndLogout(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, registry := persistentShell(t, manager)
	var wg sync.WaitGroup
	cookies := make(chan *http.Cookie, maxBrowsers)
	for range maxBrowsers {
		wg.Go(func() { cookies <- pairBrowser(t, s, s.initialCode) })
	}
	wg.Wait()
	close(cookies)
	if len(s.access.sessions) != maxBrowsers {
		t.Fatal("concurrent pairing lost sessions")
	}
	for cookie := range cookies {
		wg.Go(func() {
			csrf := csrfFor(t, s, cookie)
			if w := request(t, s, "POST", "/logout", url.Values{"csrf": {csrf}}, cookie); w.Code != http.StatusSeeOther {
				t.Errorf("logout: %d", w.Code)
			}
		})
	}
	wg.Wait()
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = persistentShell(t, manager)
	if len(s.access.sessions) != 0 {
		t.Fatal("concurrent revocations did not persist")
	}
}

type accessMutationReader struct {
	io.Reader
	mutate func()
}

func (r *accessMutationReader) Read(p []byte) (int, error) {
	if r.mutate != nil {
		r.mutate()
		r.mutate = nil
	}
	return r.Reader.Read(p)
}

func TestAccessPersistenceFailedRevocationNeverReportsSuccess(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, _ := persistentShell(t, manager)
	cookie := pairBrowser(t, s, s.initialCode)
	body := &accessMutationReader{Reader: strings.NewReader(url.Values{"csrf": {csrfFor(t, s, cookie)}}.Encode()), mutate: func() {
		// Happens after browser authorization, before committing the revocation.
		if err := os.Chmod(filepath.Join(manager, accessFile), 0o644); err != nil {
			t.Fatal(err)
		}
	}}
	r := httptest.NewRequest("POST", "/logout", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.logout(w, r)
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("failed revocation reported success: %d", w.Code)
	}
	if _, ok := s.browser(browserRequest(cookie)); ok {
		t.Fatal("ambiguous failure retained authority")
	}
	// Even if storage is repaired, a shell with an ambiguous write failure must
	// restart rather than silently trust possibly divergent in-memory state.
	if err := os.Chmod(filepath.Join(manager, accessFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.access.failed {
		t.Fatal("storage failure did not latch")
	}
}

func TestAccessPersistenceCanceledMutationDoesNotGrant(t *testing.T) {
	manager := filepath.Join(t.TempDir(), "manager")
	s, _ := persistentShell(t, manager)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.access.mu.Lock()
	token := randomToken()
	now := s.now()
	s.access.sessions[sha256.Sum256([]byte(token))] = browserSession{CSRF: randomToken(), Created: now, LastUsed: now}
	if s.saveAccessLocked(ctx) {
		t.Fatal("canceled write succeeded")
	}
	s.access.mu.Unlock()
	if _, ok := s.browser(browserRequest(localCookie(sessionCookie, token, 0))); ok {
		t.Fatal("canceled mutation granted authority")
	}
}

func TestAuthFormLimitPreservesValidation(t *testing.T) {
	s := testShell(t)
	cookie := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, cookie)
	for _, tc := range []struct {
		name   string
		limit  int64
		values url.Values
		want   bool
	}{
		{"default bounded", 8 << 10, url.Values{"csrf": {csrf}, "prompt": {strings.Repeat("p", 32<<10)}}, false},
		{"runtime larger", 64 << 10, url.Values{"csrf": {csrf}, "prompt": {strings.Repeat("p", 32<<10)}}, true},
		{"runtime bounded", 64 << 10, url.Values{"csrf": {csrf}, "prompt": {strings.Repeat("p", 65<<10)}}, false},
		{"duplicates", 64 << 10, url.Values{"csrf": {csrf}, "prompt": {"one", "two"}}, false},
		{"csrf", 64 << 10, url.Values{"csrf": {"wrong"}, "prompt": {"one"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.values.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			if _, ok := s.authorizeFormLimit(w, r, tc.limit); ok != tc.want {
				t.Fatalf("authorization = %v, status %d", ok, w.Code)
			}
		})
	}
}
