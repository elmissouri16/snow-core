package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeAuthBackend struct {
	fakeRuntime
	calls   []string
	secrets []string
}

func (f *fakeAuthBackend) inventory() RuntimeAuth {
	return RuntimeAuth{ProjectID: f.snapshot.ProjectID, InstanceID: f.snapshot.InstanceID, SessionID: f.snapshot.SessionID, Providers: []RuntimeAuthProvider{{ID: "chatgpt", DisplayName: "ChatGPT", Required: true, Environment: []string{}, Methods: []RuntimeAuthMethod{{ID: "device", DisplayName: "Device code", Kind: "oauth"}}, Status: RuntimeAuthStatus{State: "expired"}}}}
}
func (f *fakeAuthBackend) job(id string) RuntimeAuthJob {
	return RuntimeAuthJob{ProjectID: f.snapshot.ProjectID, InstanceID: f.snapshot.InstanceID, SessionID: f.snapshot.SessionID, JobID: id, ProviderID: "chatgpt", Method: "device", State: "running", Progress: []RuntimeAuthProgress{}}
}
func (f *fakeAuthBackend) AuthProviders(_ context.Context, project, instance string) (RuntimeAuth, error) {
	f.calls = append(f.calls, "inspect")
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID {
		return RuntimeAuth{}, ErrRuntimeInvalid
	}
	return f.inventory(), nil
}
func (f *fakeAuthBackend) AuthLoginStart(_ context.Context, project, instance string, input RuntimeAuthLoginInput) (RuntimeAuthJob, error) {
	f.calls = append(f.calls, "start")
	f.secrets = append(f.secrets, input.Secret)
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID || input.ProviderID != "chatgpt" {
		return RuntimeAuthJob{}, ErrRuntimeInvalid
	}
	return f.job("auth-1"), nil
}
func (f *fakeAuthBackend) AuthLoginStatus(_ context.Context, _, _, jobID string) (RuntimeAuthJob, error) {
	f.calls = append(f.calls, "status")
	return f.job(jobID), nil
}
func (f *fakeAuthBackend) AuthLoginCancel(_ context.Context, _, _, jobID string) (RuntimeAuthJob, error) {
	f.calls = append(f.calls, "cancel")
	job := f.job(jobID)
	job.State = "canceled"
	return job, nil
}
func (f *fakeAuthBackend) AuthLogout(_ context.Context, _, _, _ string) (RuntimeAuth, error) {
	f.calls = append(f.calls, "logout")
	return f.inventory(), nil
}

func authHTTPFixture(t *testing.T) (*shell, Project, *fakeAuthBackend) {
	t.Helper()
	s, _, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "auth", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAuthBackend{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Status: "idle", Revision: 3, AuthEnabled: true}, live: true}}
	s.runtimes = f
	return s, p, f
}
func authHTTPCall(t *testing.T, s *shell, p Project, action string, values url.Values, profile string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/projects/"+p.ID+"/runtime/"+action, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("action", action)
	if profile != "" {
		r = r.WithContext(context.WithValue(r.Context(), requestBoundaryKey{}, requestBoundary{network: requestNetworkPolicy{profile: profile}}))
	}
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.runtimeAuthAction(t.Context(), w, r, p)
	return w
}
func authHTTPValues(extra ...string) url.Values {
	v := url.Values{"csrf": {"checked-upstream"}, "instance_id": {"instance"}, "session_id": {"session"}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v
}

func TestAuthHTTPInventoryStartAndIdentityBinding(t *testing.T) {
	s, p, f := authHTTPFixture(t)
	w := authHTTPCall(t, s, p, "auth-inspect", authHTTPValues(), "")
	if w.Code != http.StatusOK {
		t.Fatalf("inspect %d %s", w.Code, w.Body)
	}
	var inventory RuntimeAuth
	if err := json.Unmarshal(w.Body.Bytes(), &inventory); err != nil || len(inventory.Providers) != 1 || inventory.InstanceID != "instance" {
		t.Fatalf("inventory %v %s", err, w.Body)
	}
	if w := authHTTPCall(t, s, p, "auth-login-start", authHTTPValues("provider", "chatgpt", "method", "device"), ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"job_id":"auth-1"`) {
		t.Fatalf("start %d %s", w.Code, w.Body)
	}
	if w := authHTTPCall(t, s, p, "auth-login-cancel", authHTTPValues("job_id", "auth-1"), ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"canceled"`) {
		t.Fatalf("cancel %d %s", w.Code, w.Body)
	}
	// Stale instance or session identity never reaches the backend.
	for _, stale := range []url.Values{authHTTPValues("instance_id", "other"), authHTTPValues("session_id", "other")} {
		before := len(f.calls)
		if w := authHTTPCall(t, s, p, "auth-inspect", stale, ""); w.Code != http.StatusConflict || len(f.calls) > before+1 || w.Code == http.StatusOK {
			t.Fatalf("stale identity %d %s", w.Code, w.Body)
		}
	}
	// Fields outside the action allowlist and query authority are rejected.
	for _, bad := range []url.Values{authHTTPValues("job_id", "auth-1"), authHTTPValues("secret", "x")} {
		before := len(f.calls)
		if w := authHTTPCall(t, s, p, "auth-inspect", bad, ""); w.Code != http.StatusConflict || len(f.calls) != before {
			t.Fatalf("allowlist %d %v", w.Code, f.calls)
		}
	}
}

func TestAuthHTTPSecretOnlyFromLocalOrigin(t *testing.T) {
	s, p, f := authHTTPFixture(t)
	values := authHTTPValues("provider", "chatgpt", "method", "api_key", "secret", "sk-test-value")
	if w := authHTTPCall(t, s, p, "auth-login-start", values, "trusted-lan-http"); w.Code != http.StatusForbidden || len(f.calls) != 0 || strings.Contains(w.Body.String(), "sk-test-value") {
		t.Fatalf("LAN secret relay %d %s calls %v", w.Code, w.Body, f.calls)
	}
	if w := authHTTPCall(t, s, p, "auth-login-start", values, "local"); w.Code != http.StatusOK || len(f.secrets) != 1 || f.secrets[0] != "sk-test-value" || strings.Contains(w.Body.String(), "sk-test-value") {
		t.Fatalf("local secret relay %d %s secrets %d", w.Code, w.Body, len(f.secrets))
	}
}
