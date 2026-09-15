package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
)

type cancelHTTPRuntime struct {
	fakeRuntime
	projectID, instanceID, token string
}

func (f *cancelHTTPRuntime) CancelTurn(_ context.Context, projectID, instanceID, token string) error {
	if projectID != f.projectID || instanceID != f.instanceID || token != f.token {
		return ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "cancel")
	return nil
}

func TestRuntimeTurnCancelHTTPCapabilityAndBinding(t *testing.T) {
	s, cookie, _ := projectShell(t)
	path := t.TempDir()
	p, err := s.registry.Add(t.Context(), "project", path)
	if err != nil {
		t.Fatal(err)
	}
	base := "/projects/" + p.ID + "/runtime/cancel"
	csrf := csrfFor(t, s, cookie)
	form := url.Values{"csrf": {csrf}, "instance_id": {"bound-instance"}, "cancel_token": {"bound-turn"}}
	legacy := &fakeRuntime{}
	s.runtimes = legacy
	data := pageData{}
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if data.TurnCancelEnabled {
		t.Fatal("legacy backend advertised turn cancellation")
	}
	w := request(t, s, "POST", base, form, cookie)
	if w.Code != http.StatusServiceUnavailable || len(legacy.calls) != 0 {
		t.Fatalf("legacy cancel fell back to unbound Abort: %d %v", w.Code, legacy.calls)
	}
	backend := &cancelHTTPRuntime{projectID: p.ID, instanceID: "bound-instance", token: "bound-turn"}
	s.runtimes = backend
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if !data.TurnCancelEnabled {
		t.Fatal("capable backend did not advertise cancellation")
	}
	for _, invalid := range []url.Values{
		{"instance_id": {"bound-instance"}, "cancel_token": {"bound-turn"}},
		{"csrf": {csrf}, "instance_id": {"stale"}, "cancel_token": {"bound-turn"}},
		{"csrf": {csrf}, "instance_id": {"bound-instance"}, "cancel_token": {"stale"}},
		{"csrf": {csrf}, "instance_id": {"bound-instance"}},
	} {
		if w := request(t, s, "POST", base, invalid, cookie); w.Code < 400 {
			t.Fatalf("invalid form admitted: %v", invalid)
		}
	}
	if w := request(t, s, "POST", base, form); w.Code == http.StatusOK {
		t.Fatal("unpaired cancellation admitted")
	}
	if len(backend.calls) != 0 {
		t.Fatal("rejected cancellation reached backend")
	}
	// Stop remains reachable even after the project directory disappears.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w = request(t, s, "POST", base, form, cookie)
	if w.Code != http.StatusOK || len(backend.calls) != 1 || backend.calls[0] != "cancel" {
		t.Fatalf("bound cancellation: %d %s %v", w.Code, w.Body.String(), backend.calls)
	}
	// Snapshot/read reconnects must never replay the mutation.
	for range 3 {
		request(t, s, "GET", "/projects/"+p.ID+"/runtime", nil, cookie)
	}
	if len(backend.calls) != 1 {
		t.Fatal("read replayed cancellation")
	}
}
