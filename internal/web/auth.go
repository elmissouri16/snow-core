package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie   = "snow_manager_local_session"
	pairCookie      = "snow_manager_local_pair_csrf"
	maxBrowsers     = 8
	browserLifetime = 30 * 24 * time.Hour
	pairingLifetime = 30 * 24 * time.Hour
)

type browserSession struct {
	ID             string
	Label          string
	CSRF           string
	Created        time.Time
	LastUsed       time.Time
	PairingCode    string
	PairingExpires time.Time
}

type accessState struct {
	mu          sync.Mutex
	key         [32]byte
	pairHash    [32]byte
	pairExpires time.Time
	pairCode    string
	store       *accessStore
	failed      bool
	sessions    map[[32]byte]browserSession
	window      time.Time
	attempts    int
}

func randomToken() string {
	var value [32]byte
	_, _ = rand.Read(value[:]) // crypto/rand.Read terminates on entropy failure.
	return hex.EncodeToString(value[:])
}

func (s *shell) issuePairingLocked() string {
	code := randomToken()
	s.access.pairHash = sha256.Sum256([]byte(code))
	s.access.pairExpires = s.now().Add(pairingLifetime)
	s.access.pairCode = code
	return code
}

func (s *shell) browser(r *http.Request) (browserSession, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return browserSession{}, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	if !s.checkAccessLocked(r.Context()) {
		return browserSession{}, false
	}
	s.expireSessionsLocked()
	browser, ok := s.access.sessions[key]
	if ok {
		// Absolute and idle limits are both 30 days, so reads need not rewrite
		// storage: the durable Created deadline always expires no later.
		browser.LastUsed = s.now()
		s.access.sessions[key] = browser
	}
	return browser, ok
}

func (s *shell) expireSessionsLocked() {
	now := s.now()
	for key, browser := range s.access.sessions {
		if !now.Before(browser.LastUsed.Add(browserLifetime)) || !now.Before(browser.Created.Add(browserLifetime)) {
			delete(s.access.sessions, key)
		}
	}
}

func localCookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
}

// localCookie derives transport policy only from the configured listener origin,
// never from forwarded headers or other browser-controlled request fields.
func (s *shell) localCookie(name, value string, maxAge int) *http.Cookie {
	cookie := localCookie(name, value, maxAge)
	cookie.Secure = strings.HasPrefix(s.origin, "https://")
	return cookie
}

func (s *shell) pairCSRF() string {
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	nonce := randomToken()
	mac := hmac.New(sha256.New, s.access.key[:])
	_, _ = mac.Write([]byte(nonce))
	return nonce + "." + hex.EncodeToString(mac.Sum(nil))
}

func (s *shell) validPairCSRF(r *http.Request) bool {
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	cookie, err := r.Cookie(pairCookie)
	value := r.PostForm.Get("csrf")
	if err != nil || len(value) != 129 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(value)) != 1 {
		return false
	}
	nonce, signature, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	mac := hmac.New(sha256.New, s.access.key[:])
	_, _ = mac.Write([]byte(nonce))
	return hmac.Equal([]byte(signature), []byte(hex.EncodeToString(mac.Sum(nil))))
}

func (s *shell) form(w http.ResponseWriter, r *http.Request) bool {
	return s.formLimit(w, r, 8<<10)
}

func (s *shell) formLimit(w http.ResponseWriter, r *http.Request, maxBytes int64) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		http.Error(w, "Expected an encoded form", http.StatusUnsupportedMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid or oversized form", http.StatusBadRequest)
		return false
	}
	for _, values := range r.PostForm {
		if len(values) != 1 {
			http.Error(w, "Duplicate form field", http.StatusBadRequest)
			return false
		}
	}
	return true
}

func (s *shell) login(w http.ResponseWriter, r *http.Request) {
	if !s.form(w, r) {
		return
	}
	if !s.validPairCSRF(r) {
		http.Error(w, "Invalid form token; reload the pairing page", http.StatusForbidden)
		return
	}
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	if !s.checkAccessLocked(r.Context()) {
		accessUnavailable(w)
		return
	}
	now := s.now()
	if !now.Before(s.access.window.Add(time.Minute)) {
		s.access.window, s.access.attempts = now, 0
	}
	if s.access.attempts >= 20 {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many pairing attempts; try again in a minute", http.StatusTooManyRequests)
		return
	}
	s.access.attempts++
	candidate := sha256.Sum256([]byte(strings.TrimSpace(r.PostForm.Get("code"))))
	if !now.Before(s.access.pairExpires) || subtle.ConstantTimeCompare(candidate[:], s.access.pairHash[:]) != 1 {
		if !s.saveAccessLocked(r.Context()) {
			accessUnavailable(w)
			return
		}
		s.render(w, http.StatusUnauthorized, "login", pageData{CSRF: r.PostForm.Get("csrf"), Error: "That code is invalid or expired. Ask a paired browser to rotate the code, or check the manager startup output."})
		return
	}
	s.expireSessionsLocked()
	if previous, err := r.Cookie(sessionCookie); err == nil {
		delete(s.access.sessions, sha256.Sum256([]byte(previous.Value)))
	}
	if len(s.access.sessions) >= maxBrowsers {
		if !s.saveAccessLocked(r.Context()) {
			accessUnavailable(w)
			return
		}
		http.Error(w, "Browser limit reached; revoke an individual browser from Browser access or sign out another browser", http.StatusConflict)
		return
	}
	token := randomToken()
	s.access.sessions[sha256.Sum256([]byte(token))] = browserSession{ID: s.newBrowserIDLocked(), Label: browserLabel(r.UserAgent()), CSRF: randomToken(), Created: now, LastUsed: now}
	if !s.saveAccessLocked(r.Context()) {
		accessUnavailable(w)
		return
	}
	http.SetCookie(w, s.localCookie(sessionCookie, token, int(browserLifetime/time.Second)))
	http.SetCookie(w, s.localCookie(pairCookie, "", -1))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *shell) authorizeForm(w http.ResponseWriter, r *http.Request) (browserSession, bool) {
	return s.authorizeFormLimit(w, r, 8<<10)
}

func (s *shell) authorizeFormLimit(w http.ResponseWriter, r *http.Request, maxBytes int64) (browserSession, bool) {
	browser, ok := s.browser(r)
	if !ok {
		s.requireLogin(w, r)
		return browserSession{}, false
	}
	if !s.formLimit(w, r, maxBytes) {
		return browserSession{}, false
	}
	if subtle.ConstantTimeCompare([]byte(browser.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
		http.Error(w, "Invalid form token; reload this page", http.StatusForbidden)
		return browserSession{}, false
	}
	// Admit the completed form against current durable browser authority. This
	// gate ends before handler work; it does not cancel already admitted work.
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return browserSession{}, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	if !s.checkAccessLocked(r.Context()) {
		s.access.mu.Unlock()
		accessUnavailable(w)
		return browserSession{}, false
	}
	s.expireSessionsLocked()
	current, ok := s.access.sessions[key]
	if !ok || current.ID != browser.ID || subtle.ConstantTimeCompare([]byte(current.CSRF), []byte(browser.CSRF)) != 1 {
		s.access.mu.Unlock()
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return browserSession{}, false
	}
	current.LastUsed = s.now()
	s.access.sessions[key] = current
	s.access.mu.Unlock()
	return current, true
}

func (s *shell) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	s.access.mu.Lock()
	delete(s.access.sessions, sha256.Sum256([]byte(cookie.Value)))
	saved := s.saveAccessLocked(r.Context())
	s.access.mu.Unlock()
	if !saved {
		accessUnavailable(w)
		return
	}
	http.SetCookie(w, s.localCookie(sessionCookie, "", -1))
	w.Header().Set("Clear-Site-Data", "\"cache\", \"storage\"")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *shell) pair(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	key := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	browser, ok := s.access.sessions[key]
	if !s.checkAccessLocked(r.Context()) {
		s.access.mu.Unlock()
		accessUnavailable(w)
		return
	}
	if ok {
		browser.PairingCode = s.issuePairingLocked()
		browser.PairingExpires = s.access.pairExpires
		s.access.sessions[key] = browser
	}
	saved := s.saveAccessLocked(r.Context())
	s.access.mu.Unlock()
	if !saved {
		accessUnavailable(w)
		return
	}
	if !ok {
		s.requireLogin(w, r)
		return
	}
	http.Redirect(w, r, "/?view=access", http.StatusSeeOther)
}

func (s *shell) takePairingCode(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	if !s.checkAccessLocked(r.Context()) {
		return ""
	}
	s.expireSessionsLocked()
	browser, ok := s.access.sessions[key]
	if !ok {
		return ""
	}
	code := browser.PairingCode
	browser.PairingCode = ""
	s.access.sessions[key] = browser
	if code != s.access.pairCode || !s.now().Before(browser.PairingExpires) {
		return ""
	}
	return code
}

func accessUnavailable(w http.ResponseWriter) {
	http.Error(w, "Browser access storage is unavailable; restart the manager after checking private storage", http.StatusServiceUnavailable)
}

// revokeAll revokes every browser and rotates the pairing code atomically.
// The operator can obtain the replacement code from the next startup output.
func (s *shell) revokeAll(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	s.access.mu.Lock()
	if !s.checkAccessLocked(r.Context()) {
		s.access.mu.Unlock()
		accessUnavailable(w)
		return
	}
	s.expireSessionsLocked()
	cookie, _ := r.Cookie(sessionCookie)
	if _, ok := s.access.sessions[sha256.Sum256([]byte(cookie.Value))]; !ok {
		s.access.mu.Unlock()
		s.requireLogin(w, r)
		return
	}
	clear(s.access.sessions)
	s.issuePairingLocked()
	saved := s.saveAccessLocked(r.Context())
	s.access.mu.Unlock()
	if !saved {
		accessUnavailable(w)
		return
	}
	http.SetCookie(w, s.localCookie(sessionCookie, "", -1))
	w.Header().Set("Clear-Site-Data", "\"cache\", \"storage\"")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
