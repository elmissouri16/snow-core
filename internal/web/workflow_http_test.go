package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeWorkflow struct {
	fakeRuntime
	confirmed                   bool
	target                      string
	provider, model, mode, name string
}

func (f *fakeWorkflow) check(instance string) error {
	if instance != f.snapshot.InstanceID {
		return ErrRuntimeInvalid
	}
	return nil
}
func (f *fakeWorkflow) Choices(_ context.Context, _, instance string) (RuntimeChoices, error) {
	if err := f.check(instance); err != nil {
		return RuntimeChoices{}, err
	}
	f.calls = append(f.calls, "choices")
	return RuntimeChoices{}, nil
}
func (f *fakeWorkflow) SetModel(_ context.Context, _, instance, provider, model string) error {
	if err := f.check(instance); err != nil {
		return err
	}
	f.provider, f.model = provider, model
	f.calls = append(f.calls, "model")
	return nil
}
func (f *fakeWorkflow) SetMode(_ context.Context, _, instance, mode string) error {
	if err := f.check(instance); err != nil {
		return err
	}
	f.mode = mode
	f.calls = append(f.calls, "mode")
	return nil
}
func (f *fakeWorkflow) Rename(_ context.Context, _, instance, name string) error {
	if err := f.check(instance); err != nil {
		return err
	}
	f.name = name
	f.calls = append(f.calls, "rename")
	return nil
}
func (f *fakeWorkflow) Switch(_ context.Context, _, instance, target string, confirm bool) (RuntimeSnapshot, error) {
	if err := f.check(instance); err != nil {
		return RuntimeSnapshot{}, err
	}
	f.confirmed, f.target = confirm, target
	f.calls = append(f.calls, "switch")
	f.snapshot.InstanceID = "replacement"
	f.snapshot.SessionID = target
	return f.snapshot, nil
}
func TestWorkflowHTTPRendersOptionalControlsAndReactAsset(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workflow", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeWorkflow{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{
		ProjectID: project.ID, InstanceID: "instance", SessionID: "session", Status: "idle", Mode: "plan",
		Messages: []RuntimeMessage{{ID: "proposal", Role: "plan", Text: "**Public proposal**"}},
	}, live: true}}
	s.runtimes = backend
	response := request(t, s, "GET", "/?view=projects&project="+project.ID, nil, cookie)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "data-model-menu") || !strings.Contains(body, "<strong>Public proposal</strong>") {
		t.Fatalf("workflow page missing controls or public plan: status %d", response.Code)
	}
	if !strings.Contains(body, `<script type="module" src="/static/generated/app.js"></script>`) || !strings.Contains(body, `data-react-conversation="heading"`) || !strings.Contains(body, `data-workflow-enabled="true"`) {
		t.Fatal("conversation workflow React entrypoint, mount or capability missing")
	}
	if strings.Contains(body, `src="/static/conversation.js"`) {
		t.Fatal("retired workflow DOM owner still loaded")
	}
	if asset := request(t, s, "GET", "/static/conversation.js", nil, cookie); asset.Code != http.StatusNotFound {
		t.Fatal("retired workflow JavaScript is still served")
	}
	s.runtimes = &backend.fakeRuntime
	legacy := request(t, s, "GET", "/?view=projects&project="+project.ID, nil, cookie)
	if legacy.Code != http.StatusOK || strings.Contains(legacy.Body.String(), "data-model-menu") {
		t.Fatal("legacy runtime backend must not advertise workflow controls")
	}
}

func TestWorkflowHTTPUsesTypedInstanceBoundControls(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "workflow", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeWorkflow{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "original", SessionID: "session", Status: "idle"}, live: true}}
	s.runtimes = backend
	csrf := csrfFor(t, s, cookie)
	base := "/projects/" + p.ID + "/runtime/"
	for _, action := range []string{"choices", "model", "mode", "rename", "switch"} {
		for _, bad := range []url.Values{
			{"instance_id": {"original"}},
			{"csrf": {csrf}},
			{"csrf": {csrf}, "instance_id": {"stale"}, "mode": {"plan"}, "provider": {"fake"}, "model": {"fake-1"}},
		} {
			w := request(t, s, "POST", base+action, bad, cookie)
			if w.Code < 400 {
				t.Fatalf("untrusted %s: %d", action, w.Code)
			}
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid controls reached backend")
	}
	for _, tc := range []struct {
		action string
		values url.Values
	}{
		{"choices", url.Values{}},
		{"model", url.Values{"provider": {"fake"}, "model": {"fake-1"}}},
		{"mode", url.Values{"mode": {"plan"}}},
		{"rename", url.Values{"name": {"New title"}}},
	} {
		tc.values.Set("csrf", csrf)
		tc.values.Set("instance_id", "original")
		w := request(t, s, "POST", base+tc.action, tc.values, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.action, w.Code, w.Body.String())
		}
	}
	before := len(backend.calls)
	for _, mode := range []string{"allow", "auto", "PLAN", ""} {
		w := request(t, s, "POST", base+"mode", url.Values{"csrf": {csrf}, "instance_id": {"original"}, "mode": {mode}}, cookie)
		if w.Code != http.StatusConflict {
			t.Fatalf("invalid mode %q: %d", mode, w.Code)
		}
	}
	if len(backend.calls) != before {
		t.Fatal("invalid mode tunneled to RPC")
	}
	w := request(t, s, "POST", base+"switch", url.Values{"csrf": {csrf}, "instance_id": {"original"}, "session_id": {"next"}, "confirm_stop": {"true"}}, cookie)
	if w.Code != http.StatusConflict {
		t.Fatal("ambiguous stop confirmation accepted")
	}
	w = request(t, s, "POST", base+"switch", url.Values{"csrf": {csrf}, "instance_id": {"original"}, "session_id": {"next"}, "confirm_stop": {"stop"}}, cookie)
	if w.Code != http.StatusOK || !backend.confirmed || backend.target != "next" {
		t.Fatalf("confirmed switch: %d", w.Code)
	}
	w = request(t, s, "POST", base+"rename", url.Values{"csrf": {csrf}, "instance_id": {"original"}, "name": {"stale title"}}, cookie)
	if w.Code != http.StatusConflict || backend.name != "New title" {
		t.Fatal("stale rename reached new session")
	}
}
func TestWorkflowHTTPIsOptionalAndSwitchAdmissionDoesNotQueue(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.runtimes = &fakeRuntime{}
	base := "/projects/" + p.ID + "/runtime/"
	values := url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {"original"}}
	if w := request(t, s, "POST", base+"choices", values, cookie); w.Code != http.StatusServiceUnavailable {
		t.Fatal("legacy backend exposed unavailable workflow")
	}
	s.projectControl.Lock()
	defer s.projectControl.Unlock()
	if w := request(t, s, "POST", base+"switch", values, cookie); w.Code != http.StatusConflict {
		t.Fatal("switch admission queued")
	}
}
