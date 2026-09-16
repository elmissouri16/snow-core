package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *shell) registerHostSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/host", s.getHostSettings)
	mux.HandleFunc("POST /settings/host", s.updateHostSettings)
	mux.HandleFunc("GET /settings/providers", s.getHostProviderStatus)
}
func (s *shell) hostSettingsReady(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return false
	}
	if s.hostSettings == nil {
		http.Error(w, "Host settings are unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}
func hostScope(values url.Values) (string, string, bool) {
	scope, project := values.Get("scope"), values.Get("project")
	return scope, project, len(values["scope"]) == 1 && (scope == "global" && len(values["project"]) == 0 || scope == "project" && len(values["project"]) == 1 && validProjectID(project))
}
func hostFields(values url.Values, allowed map[string]bool) bool {
	for key, list := range values {
		if !allowed[key] || len(list) != 1 || len(list[0]) > 512 {
			return false
		}
	}
	return true
}
func hostHTTPError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, ErrHostControlUnavailable.Error()
	switch {
	case errors.Is(err, ErrHostControlInvalid):
		status, message = http.StatusBadRequest, ErrHostControlInvalid.Error()
	case errors.Is(err, ErrHostControlConflict):
		status, message = http.StatusConflict, ErrHostControlConflict.Error()
	case errors.Is(err, ErrHostControlBusy):
		status, message = http.StatusConflict, ErrHostControlBusy.Error()
	}
	http.Error(w, message, status)
}
func hostJSON(w http.ResponseWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64<<10 {
		hostHTTPError(w, ErrHostControlUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}
func (s *shell) getHostSettings(w http.ResponseWriter, r *http.Request) {
	if !s.hostSettingsReady(w, r) {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	scope, project, valid := hostScope(values)
	if err != nil || !valid || !hostFields(values, map[string]bool{"scope": true, "project": true}) {
		hostHTTPError(w, ErrHostControlInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := s.hostSettings.Defaults(ctx, scope, project)
	if err != nil {
		hostHTTPError(w, err)
		return
	}
	result, err = validatePublicHostSettings(result, scope, project)
	if err != nil {
		hostHTTPError(w, err)
		return
	}
	hostJSON(w, result)
}
func validatePublicHostSettings(value HostSettings, scope, project string) (HostSettings, error) {
	if value.ProjectID != project {
		return HostSettings{}, ErrHostControlUnavailable
	}
	return projectHostSettings(protocol.HostDefaultsResponse{Scope: value.Scope, Revision: value.Revision, AppliesTo: value.AppliesTo, Global: value.Global, Project: value.Project}, scope, project)
}
func hostPatch(values url.Values) (protocol.HostDefaultsUpdateRequest, bool) {
	scope, _, valid := hostScope(values)
	allowed := map[string]bool{"scope": true, "project": true, "csrf": true, "expected_revision": true, "provider_model_op": true, "provider": true, "model": true, "thinking_op": true, "thinking": true}
	if scope == "global" {
		for _, key := range []string{"reasoning_summary_op", "reasoning_summary", "text_verbosity_op", "text_verbosity"} {
			allowed[key] = true
		}
	}
	if !valid || !hostFields(values, allowed) || len(values["csrf"]) != 1 || len(values["expected_revision"]) != 1 {
		return protocol.HostDefaultsUpdateRequest{}, false
	}
	input := protocol.HostDefaultsUpdateRequest{Scope: scope, Revision: values.Get("expected_revision")}
	var pair *protocol.HostProviderModelOperation
	if op, exists := values["provider_model_op"]; exists {
		pair = &protocol.HostProviderModelOperation{Op: op[0]}
		if pair.Op == "set" {
			if len(values["provider"]) != 1 || len(values["model"]) != 1 {
				return input, false
			}
			pair.Value = &protocol.HostProviderModel{Provider: values.Get("provider"), Model: values.Get("model")}
		} else if len(values["provider"]) != 0 || len(values["model"]) != 0 {
			return input, false
		}
	} else if len(values["provider"]) != 0 || len(values["model"]) != 0 {
		return input, false
	}
	ops := make(map[string]*protocol.HostStringOperation)
	for _, field := range []string{"thinking", "reasoning_summary", "text_verbosity"} {
		op, exists := values[field+"_op"]
		if !exists {
			if len(values[field]) != 0 {
				return input, false
			}
			continue
		}
		operation := &protocol.HostStringOperation{Op: op[0]}
		if operation.Op == "set" {
			if len(values[field]) != 1 {
				return input, false
			}
			operation.Value = new(values.Get(field))
		} else if len(values[field]) != 0 {
			return input, false
		}
		ops[field] = operation
	}
	if scope == "global" {
		input.Global = &protocol.HostGlobalDefaultsPatch{ProviderModel: pair, Thinking: ops["thinking"], ReasoningSummary: ops["reasoning_summary"], TextVerbosity: ops["text_verbosity"]}
	} else {
		input.Project = &protocol.HostProjectDefaultsPatch{ProviderModel: pair, Thinking: ops["thinking"]}
	}
	return input, validHostPatch(input)
}
func (s *shell) updateHostSettings(w http.ResponseWriter, r *http.Request) {
	// Also enforce exact origin here: handlers remain fail-closed when embedded
	// without the outer mux's same-origin middleware.
	if r.Header.Get("Origin") != s.requestBoundary(r).origin || len(r.Header.Values("Origin")) != 1 {
		http.Error(w, "Same-origin request required", http.StatusForbidden)
		return
	}
	if !s.hostSettingsReady(w, r) {
		return
	}
	if _, ok := s.authorizeFormLimit(w, r, 8<<10); !ok {
		return
	}
	input, valid := hostPatch(r.PostForm)
	if r.URL.RawQuery != "" || !valid {
		hostHTTPError(w, ErrHostControlInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := s.hostSettings.UpdateDefaults(ctx, input.Scope, r.PostForm.Get("project"), input)
	if err != nil {
		hostHTTPError(w, err)
		return
	}
	result, err = validatePublicHostSettings(result, input.Scope, r.PostForm.Get("project"))
	if err != nil {
		hostHTTPError(w, err)
		return
	}
	hostJSON(w, result)
}
func (s *shell) getHostProviderStatus(w http.ResponseWriter, r *http.Request) {
	if !s.hostSettingsReady(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		hostHTTPError(w, ErrHostControlInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := s.hostSettings.ProviderStatus(ctx)
	if err != nil {
		hostHTTPError(w, err)
		return
	}
	result, err = projectProviderStatus(result)
	if err != nil {
		hostHTTPError(w, err)
		return
	}
	hostJSON(w, result)
}
