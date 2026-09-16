package web

import (
	"cmp"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// BrowserAccess is an explicit public projection. ID is random and independent
// of the cookie, cookie hash, signing key, pairing code, and CSRF token. None of
// those credentials belongs in an inventory DTO (including as an identifier).
type BrowserAccess struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"`
	Expires  time.Time `json:"expires"`
	Current  bool      `json:"current"`
}

type browserInventory struct {
	Browsers []BrowserAccess `json:"browsers"`
	Limit    int             `json:"limit"`
}

func validBrowserID(id string) bool {
	value, ok := strings.CutPrefix(id, "browser_")
	if !ok || len(value) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func (s *shell) newBrowserIDLocked() string {
	for {
		id := "browser_" + randomToken()[:32]
		duplicate := false
		for _, browser := range s.access.sessions {
			if browser.ID == id {
				duplicate = true
				break
			}
		}
		if !duplicate {
			return id
		}
	}
}

func validBrowserLabel(label string) bool {
	if label == "" || len(label) > 80 || !utf8.ValidString(label) || strings.TrimSpace(label) != label {
		return false
	}
	for _, r := range label {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// Coarse, untrusted hints only: never retain a raw User-Agent, device name,
// platform fingerprint, IP address or arbitrary request text as the label.
func browserLabel(userAgent string) string {
	if len(userAgent) > 1024 {
		return "Paired browser"
	}
	switch {
	case strings.Contains(userAgent, "Edg/"):
		return "Edge browser"
	case strings.Contains(userAgent, "Firefox/"):
		return "Firefox browser"
	case strings.Contains(userAgent, "Chrome/") || strings.Contains(userAgent, "CriOS/"):
		return "Chrome browser"
	case strings.Contains(userAgent, "Safari/"):
		return "Safari browser"
	default:
		return "Paired browser"
	}
}

func (s *shell) registerBrowserAccessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /access/browsers", s.browserInventory)
	mux.HandleFunc("POST /access/browsers/{browser}/revoke", s.revokeBrowser)
}

func (s *shell) browserInventory(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.sessionCookieName(r))
	if err != nil || len(cookie.Value) != 64 {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	if !s.checkAccessLocked(r.Context()) {
		s.access.mu.Unlock()
		accessUnavailable(w)
		return
	}
	s.expireSessionsLocked()
	current, ok := s.sessionForRequestLocked(r, key)
	if !ok {
		s.access.mu.Unlock()
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	current.LastUsed = s.now()
	s.access.sessions[key] = current
	inventory := browserInventory{Browsers: make([]BrowserAccess, 0, len(s.access.sessions)), Limit: maxBrowsers}
	for hash, browser := range s.access.sessions {
		inventory.Browsers = append(inventory.Browsers, BrowserAccess{
			ID: browser.ID, Label: browser.Label, Created: browser.Created,
			LastUsed: browser.LastUsed, Expires: browser.Created.Add(browserLifetime), Current: hash == key,
		})
	}
	s.access.mu.Unlock()
	slices.SortFunc(inventory.Browsers, func(a, b BrowserAccess) int {
		if a.Current != b.Current {
			if a.Current {
				return -1
			}
			return 1
		}
		return cmp.Or(a.Created.Compare(b.Created), cmp.Compare(a.ID, b.ID))
	})
	w.Header().Set("Cache-Control", "no-store")
	s.runtimeJSON(w, inventory)
}

// revokeBrowser uses the public ID only as an exact target, never as authority.
// Both actor and target are checked under the same lock as the durable commit.
// An ambiguous persistence failure disables all access for this shell lifetime.
func (s *shell) revokeBrowser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.authorizeForm(w, r)
	if !ok {
		return
	}
	id := r.PathValue("browser")
	if !validBrowserID(id) {
		http.Error(w, "Invalid browser identifier", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("confirm") != "revoke" {
		http.Error(w, "Confirm revoking this browser", http.StatusBadRequest)
		return
	}
	cookie, _ := r.Cookie(s.sessionCookieName(r))
	actorKey := sha256.Sum256([]byte(cookie.Value))
	s.access.mu.Lock()
	if !s.checkAccessLocked(r.Context()) {
		s.access.mu.Unlock()
		accessUnavailable(w)
		return
	}
	s.expireSessionsLocked()
	current, ok := s.sessionForRequestLocked(r, actorKey)
	if !ok || current.ID != actor.ID || subtle.ConstantTimeCompare([]byte(current.CSRF), []byte(actor.CSRF)) != 1 {
		s.access.mu.Unlock()
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	for key, target := range s.access.sessions {
		if target.ID != id {
			continue
		}
		delete(s.access.sessions, key)
		if !s.saveAccessLocked(r.Context()) {
			s.access.mu.Unlock()
			accessUnavailable(w)
			return
		}
		s.access.mu.Unlock()
		signedOut := key == actorKey
		if signedOut {
			http.SetCookie(w, s.localCookie(s.sessionCookieName(r), "", -1))
			w.Header().Set("Clear-Site-Data", "\"cache\", \"storage\"")
		}
		w.Header().Set("Cache-Control", "no-store")
		s.runtimeJSON(w, struct {
			RevokedID string `json:"revoked_id"`
			SignedOut bool   `json:"signed_out"`
		}{id, signedOut})
		return
	}
	s.access.mu.Unlock()
	http.Error(w, "Browser no longer paired; refresh the inventory", http.StatusNotFound)
}
