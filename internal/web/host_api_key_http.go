package web

import (
	"context"
	"net/http"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *shell) registerHostAPIKeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/providers/{provider}/api-key", s.inspectHostAPIKey)
	mux.HandleFunc("POST /settings/providers/{provider}/api-key", s.setHostAPIKey)
}

// Gate is evaluated before body parsing. Forwarded headers never confer TLS or
// numeric-loopback authority; the actual server TLS connection is mandatory.
func (s *shell) hostAPIKeyGate(w http.ResponseWriter, r *http.Request) (browserSession, HostAPIKeyBackend, bool) {
	if r.TLS == nil || !s.hostAPIKeyTLSOrigin() || r.Host != s.host {
		http.Error(w, "API-key entry requires direct numeric-loopback HTTPS. Use interactive snow login on the host instead.", http.StatusForbidden)
		return browserSession{}, nil, false
	}
	if len(r.Header.Values("Origin")) > 1 || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.origin || r.Method == http.MethodPost && r.Header.Get("Origin") != s.origin {
		http.Error(w, "Same-origin request required", http.StatusForbidden)
		return browserSession{}, nil, false
	}
	browser, ok := s.browser(r)
	if !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return browserSession{}, nil, false
	}
	if !hostProvider(r.PathValue("provider")) || r.URL.RawQuery != "" {
		http.Error(w, "Invalid API-key request", http.StatusBadRequest)
		return browserSession{}, nil, false
	}
	backend, ok := s.hostSettings.(HostAPIKeyBackend)
	if !ok {
		http.Error(w, "API-key controls are unavailable", http.StatusServiceUnavailable)
		return browserSession{}, nil, false
	}
	return browser, backend, true
}
func hostAPIKeyHTTPError(w http.ResponseWriter, status int) {
	// Never expose worker diagnostics, submitted fields, credential type, or the
	// request itself. Uncertain writes must not encourage resending the key.
	http.Error(w, "API-key operation could not be confirmed. The key was not returned. Explicitly inspect local status before any new submission.", status)
}
func (s *shell) inspectHostAPIKey(w http.ResponseWriter, r *http.Request) {
	browser, backend, ok := s.hostAPIKeyGate(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	s.hostAPIKeys.forget(browser.ID)
	result, err := backend.InspectAPIKey(ctx, r.PathValue("provider"))
	if err != nil {
		hostAPIKeyHTTPError(w, http.StatusServiceUnavailable)
		return
	}
	result, err = projectHostAPIKey(result, r.PathValue("provider"))
	if err != nil {
		hostAPIKeyHTTPError(w, http.StatusServiceUnavailable)
		return
	}
	s.hostAPIKeys.remember(browser.ID, result, s.now())
	hostJSON(w, result)
}
func (s *shell) setHostAPIKey(w http.ResponseWriter, r *http.Request) {
	browser, backend, ok := s.hostAPIKeyGate(w, r)
	if !ok {
		return
	}
	// A percent-encoded 4096-byte UTF-8 key can occupy 12288 body bytes. Bound
	// the transport at 16 KiB and independently enforce the 4 KiB secret domain.
	defer func() { r.PostForm.Del("secret"); r.Form.Del("secret") }()
	if browser, ok = s.authorizeFormLimit(w, r, 16<<10); !ok {
		return
	}
	allowed := map[string]bool{"csrf": true, "expected_revision": true, "secret": true, "confirm_replace": true, "confirm_save": true}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			hostAPIKeyHTTPError(w, http.StatusBadRequest)
			return
		}
	}
	for _, field := range []string{"csrf", "expected_revision", "secret", "confirm_replace", "confirm_save"} {
		if len(r.PostForm[field]) != 1 {
			hostAPIKeyHTTPError(w, http.StatusBadRequest)
			return
		}
	}
	input := protocol.HostAPIKeySetRequest{ProviderID: r.PathValue("provider"), ExpectedRevision: r.PostForm.Get("expected_revision"), Secret: r.PostForm.Get("secret"), ConfirmReplace: r.PostForm.Get("confirm_replace") == "true"}
	r.PostForm.Del("secret")
	r.Form.Del("secret")
	if !hostAPIKeyRevision(input.ExpectedRevision) || !hostAPIKeySecret(input.Secret) || input.ProviderID == "chatgpt" || r.PostForm.Get("confirm_save") != "host" || r.PostForm.Get("confirm_replace") != "true" && r.PostForm.Get("confirm_replace") != "false" {
		hostAPIKeyHTTPError(w, http.StatusBadRequest)
		return
	}
	if !s.hostAPIKeys.consume(browser.ID, input.ProviderID, input.ExpectedRevision, input.ConfirmReplace, s.now()) {
		hostAPIKeyHTTPError(w, http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := backend.SetAPIKey(ctx, input)
	input.Secret = ""
	if err != nil {
		hostAPIKeyHTTPError(w, http.StatusConflict)
		return
	}
	result, err = projectHostAPIKeyWritten(result, r.PathValue("provider"))
	if err != nil {
		hostAPIKeyHTTPError(w, http.StatusServiceUnavailable)
		return
	}
	// Deliberately do not issue a fresh inspection grant from a write response.
	// Every additional write must start with a new explicit metadata inspection.
	hostJSON(w, result)
}
