package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type fakeRuntime struct {
	snapshot RuntimeSnapshot
	live     bool
	calls    []string
}

func (f *fakeRuntime) Open(_ context.Context, p Project, session, provider, model string) (RuntimeSnapshot, error) {
	f.calls = append(f.calls, "open")
	f.live = true
	f.snapshot = RuntimeSnapshot{ProjectID: p.ID, SessionID: "saved", Status: "idle"}
	return f.snapshot, nil
}
func (f *fakeRuntime) Snapshot(string) (RuntimeSnapshot, bool) { return f.snapshot, f.live }
func (f *fakeRuntime) Prompt(context.Context, string, string, string) error {
	f.calls = append(f.calls, "prompt")
	return nil
}
func (f *fakeRuntime) Abort(context.Context, string, string) error {
	f.calls = append(f.calls, "abort")
	return nil
}
func (f *fakeRuntime) ReplyPermission(_ context.Context, _, _, _ string, decision protocol.PermissionDecision) error {
	if decision != "allow" && decision != "deny" {
		return ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "permission")
	return nil
}
func (f *fakeRuntime) ReplyInput(context.Context, string, string, protocol.UserInputResponse) error {
	f.calls = append(f.calls, "input")
	return nil
}
func (f *fakeRuntime) CloseProject(context.Context, string, string) error {
	f.calls = append(f.calls, "close")
	f.live = false
	return nil
}
func (f *fakeRuntime) Close() error { return nil }

func TestRuntimeHTTPExplicitActivationAndSafeActions(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeRuntime{}
	s.runtimes = backend
	base := "/projects/" + project.ID + "/runtime"
	csrf := csrfFor(t, s, cookie)
	// Merely selecting a project or requesting a snapshot never activates it.
	request(t, s, "GET", "/?view=projects&project="+project.ID, nil, cookie)
	request(t, s, "GET", base, nil, cookie)
	if len(backend.calls) != 0 {
		t.Fatal("read activated runtime")
	}
	for _, values := range []url.Values{{"confirm": {"activate"}}, {"csrf": {csrf}}} {
		w := request(t, s, "POST", base+"/open", values, cookie)
		if w.Code < 400 {
			t.Fatal("activation lacks confirmation or CSRF")
		}
	}
	w := request(t, s, "POST", base+"/open", url.Values{"csrf": {csrf}, "confirm": {"activate"}}, cookie)
	if w.Code != http.StatusOK || len(backend.calls) != 1 {
		t.Fatalf("activation: %d %s", w.Code, w.Body.String())
	}
	before := catalog.calls
	request(t, s, "GET", "/?view=projects&project="+project.ID, nil, cookie)
	if catalog.calls != before {
		t.Fatal("live session read went through inactive catalog")
	}
	if w := request(t, s, "GET", base, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("unpaired snapshot access")
	}
	backend.snapshot.Messages = []RuntimeMessage{{Role: "assistant", Text: "<script>untrusted</script>"}}
	w = request(t, s, "GET", base, nil, cookie)
	var snapshot RuntimeSnapshot
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.Messages[0].Text != "<script>untrusted</script>" {
		t.Fatal("snapshot projection")
	}
	w = request(t, s, "POST", base+"/prompt", url.Values{"csrf": {csrf}, "text": {strings.Repeat("hello", 3000)}}, cookie)
	if w.Code != 200 {
		t.Fatalf("bounded larger composer form: %d", w.Code)
	}
	w = request(t, s, "POST", base+"/permission", url.Values{"csrf": {csrf}, "request_id": {"pending"}, "decision": {"allow_always"}}, cookie)
	if w.Code != http.StatusConflict {
		t.Fatal("persistent permission grant accepted")
	}
	w = request(t, s, "POST", base+"/arbitrary_rpc_command", url.Values{"csrf": {csrf}}, cookie)
	if w.Code != http.StatusNotFound {
		t.Fatal("browser can tunnel arbitrary RPC")
	}
	w = request(t, s, "POST", "/projects/"+project.ID+"/remove", url.Values{"csrf": {csrf}, "confirm": {"remove"}}, cookie)
	if w.Code != http.StatusConflict {
		t.Fatal("live registration removed")
	}
	w = request(t, s, "POST", base+"/close", url.Values{"csrf": {csrf}}, cookie)
	if w.Code != 200 || backend.live {
		t.Fatal("close failed")
	}
}

func TestProjectActivationRemovalAdmissionIsNonqueued(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.runtimes = &fakeRuntime{}
	s.projectControl.Lock()
	defer s.projectControl.Unlock()
	csrf := csrfFor(t, s, cookie)
	for _, path := range []string{"/projects/" + project.ID + "/remove", "/projects/" + project.ID + "/runtime/open"} {
		w := request(t, s, "POST", path, url.Values{"csrf": {csrf}, "confirm": {"activate"}}, cookie)
		if w.Code != http.StatusConflict {
			t.Fatalf("concurrent admission: %d", w.Code)
		}
	}
}
