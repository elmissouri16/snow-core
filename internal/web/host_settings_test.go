package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type hostSettingsFake struct {
	reads, writes, statuses int
	last                    protocol.HostDefaultsUpdateRequest
	project                 string
	err                     error
}

func (f *hostSettingsFake) Defaults(_ context.Context, scope, project string) (HostSettings, error) {
	f.reads++
	if f.err != nil {
		return HostSettings{}, f.err
	}
	return projectHostSettings(hostTestDefaults(scope), scope, project)
}
func (f *hostSettingsFake) UpdateDefaults(_ context.Context, scope, project string, input protocol.HostDefaultsUpdateRequest) (HostSettings, error) {
	f.writes++
	f.last = input
	f.project = project
	if f.err != nil {
		return HostSettings{}, f.err
	}
	return projectHostSettings(hostTestDefaults(scope), scope, project)
}
func (f *hostSettingsFake) ProviderStatus(context.Context) (protocol.HostProviderStatusResponse, error) {
	f.statuses++
	return protocol.HostProviderStatusResponse{CheckedLocally: true, Providers: []protocol.HostProviderStatus{{ProviderID: "opencode-go", State: "unavailable", Reason: "credential_missing", CheckedLocally: true}}}, f.err
}
func TestHostSettingsHTTPReadAndNoAutomaticLoad(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	fake := &hostSettingsFake{}
	s.hostSettings = fake
	if w := request(t, s, "GET", "/", nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("page %d", w.Code)
	}
	if fake.reads+fake.writes+fake.statuses+catalog.calls != 0 {
		t.Fatal("opening shell loaded host or runtime")
	}
	for _, path := range []string{"/settings/host?scope=global", "/settings/providers"} {
		if w := request(t, s, "GET", path, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauth %s = %d", path, w.Code)
		}
		if w := request(t, s, "GET", path, nil, cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "\"global\"") && !strings.Contains(w.Body.String(), "credential_missing") {
			t.Fatalf("read %s = %d %s", path, w.Code, w.Body.String())
		}
	}
	before := fake.reads + fake.statuses
	for _, path := range []string{"/settings/host?scope=global&cwd=/tmp", "/settings/host?scope=global&project=foo", "/settings/host?scope=project&project=/tmp", "/settings/host?scope=global&scope=project", "/settings/host?scope=global&trust=true", "/settings/providers?config=raw", "/settings/host?scope=global;%"} {
		if w := request(t, s, "GET", path, nil, cookie); w.Code != http.StatusBadRequest {
			t.Errorf("invalid %s: %d", path, w.Code)
		}
	}
	if fake.reads+fake.statuses != before {
		t.Fatal("invalid read reached backend")
	}
}
func TestHostSettingsHTTPStrictMutation(t *testing.T) {
	s, cookie, _ := projectShell(t)
	fake := &hostSettingsFake{}
	s.hostSettings = fake
	csrf := csrfFor(t, s, cookie)
	base := url.Values{"csrf": {csrf}, "scope": {"global"}, "expected_revision": {strings.Repeat("a", 64)}, "thinking_op": {"set"}, "thinking": {"high"}}
	for _, mutate := range []func(url.Values){
		func(v url.Values) { v.Set("csrf", "wrong") }, func(v url.Values) { v.Add("csrf", csrf) },
		func(v url.Values) { v.Set("path", "/tmp") }, func(v url.Values) { v.Set("cwd", "/tmp") }, func(v url.Values) { v.Set("trust", "true") }, func(v url.Values) { v.Set("permission_mode", "allow") }, func(v url.Values) { v.Set("debug", "true") }, func(v url.Values) { v.Set("enabled", "true") }, func(v url.Values) { v.Set("api_key", "SECRET") },
		func(v url.Values) { v.Set("thinking", "unsupported") }, func(v url.Values) { v.Set("thinking_op", "reset") }, func(v url.Values) { v.Del("thinking_op") }, func(v url.Values) { v.Add("thinking", "low") }, func(v url.Values) { v.Del("expected_revision") },
		func(v url.Values) { v.Set("provider_model_op", "set"); v.Set("provider", "chatgpt") },
		func(v url.Values) { v.Set("provider_model_op", "reset"); v.Set("model", "bad") },
		func(v url.Values) {
			v.Set("scope", "project")
			v.Set("project", "project_00000000000000000000000000000000")
			v.Set("text_verbosity_op", "set")
			v.Set("text_verbosity", "high")
		},
	} {
		input := base.Clone()
		mutate(input)
		if w := request(t, s, "POST", "/settings/host", input, cookie); w.Code < 400 {
			t.Errorf("invalid write admitted: %v %d", input, w.Code)
		}
	}
	if fake.writes != 0 {
		t.Fatal("invalid write reached backend")
	}
	// The exact-origin fence also protects direct handler embedding.
	r := httptest.NewRequest("POST", "/settings/host", strings.NewReader(base.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.updateHostSettings(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("missing origin accepted")
	}
	if w := request(t, s, "POST", "/settings/host?scope=project", base, cookie); w.Code != http.StatusBadRequest {
		t.Fatal("query mutation accepted")
	}
	w = request(t, s, "POST", "/settings/host", base, cookie)
	if w.Code != http.StatusOK || fake.writes != 1 || fake.last.CWD != "" || fake.last.Global.Thinking == nil || *fake.last.Global.Thinking.Value != "high" {
		t.Fatalf("valid write %d: %s %+v", w.Code, w.Body.String(), fake.last)
	}
	fake.err = errors.New("SECRET config credentials /private/path")
	w = request(t, s, "POST", "/settings/host", base, cookie)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "private") {
		t.Fatal("worker diagnostics escaped")
	}
	fake.err = ErrHostControlConflict
	if w := request(t, s, "POST", "/settings/host", base, cookie); w.Code != http.StatusConflict {
		t.Fatal("stale revision not conflict")
	}
}
func TestHostSettingsProjectionAndPatch(t *testing.T) {
	raw := hostTestDefaults("global")
	raw.Global.ProviderModel.Explicit = &protocol.HostProviderModel{Model: "configured-model"}
	raw.Global.Thinking.Effective = "ultra"
	result, err := projectHostSettings(raw, "global", "")
	if err != nil || result.Availability != "not_network_verified" {
		t.Fatal("valid inherited config rejected", err)
	}
	raw.Global.Thinking.Source = "project"
	if _, err := projectHostSettings(raw, "global", ""); err == nil {
		t.Fatal("project source in global scope accepted")
	}
	values := url.Values{"scope": {"global"}, "csrf": {"token"}, "expected_revision": {strings.Repeat("a", 64)}, "provider_model_op": {"set"}, "provider": {"custom-profile"}, "model": {"model-v1"}, "thinking_op": {"reset"}}
	patch, ok := hostPatch(values)
	if !ok || patch.Global.ProviderModel.Value.Provider != "custom-profile" || patch.Global.Thinking.Value != nil {
		t.Fatal("pair/reset rejected")
	}
	encoded, err := json.Marshal(patch)
	if err != nil || strings.Contains(string(encoded), "csrf") || strings.Contains(string(encoded), "cwd") {
		t.Fatalf("private input projection: %s %v", encoded, err)
	}
	statuses := protocol.HostProviderStatusResponse{CheckedLocally: true, Providers: []protocol.HostProviderStatus{{ProviderID: "custom-profile", State: "unavailable", Reason: "credential_missing", CheckedLocally: true}}}
	if _, err := projectProviderStatus(statuses); err != nil {
		t.Fatal("local custom profile rejected", err)
	}
	statuses.Providers[0].Reason = "SECRET"
	if _, err := projectProviderStatus(statuses); err == nil {
		t.Fatal("arbitrary reason echoed")
	}
}
