package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type messageEditHTTPRuntime struct {
	fakeRuntime
	messageID, text string
}

func (f *messageEditHTTPRuntime) PrepareMessageEdit(_ context.Context, _, instance, message string) (RuntimeMessageEditPreparation, error) {
	if instance != f.snapshot.InstanceID || message != f.messageID {
		return RuntimeMessageEditPreparation{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "prepare")
	return RuntimeMessageEditPreparation{ProjectID: f.snapshot.ProjectID, SessionID: f.snapshot.SessionID, InstanceID: instance, MessageID: message, EditToken: "edit-token", Text: "full original"}, nil
}
func (f *messageEditHTTPRuntime) CommitMessageEdit(_ context.Context, _, instance, token, text string) (RuntimeSnapshot, error) {
	if instance != f.snapshot.InstanceID || token != "edit-token" {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "commit")
	f.text = text
	f.snapshot.InstanceID = "replacement-instance"
	return f.snapshot, nil
}
func TestMessageEditHTTPTypedOptionalBoundAndCSRF(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := "/projects/" + p.ID + "/runtime/"
	csrf := csrfFor(t, s, cookie)
	legacy := &fakeRuntime{}
	s.runtimes = legacy
	data := pageData{}
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if data.MessageEditEnabled {
		t.Fatal("legacy backend advertises editing")
	}
	form := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "message_id": {"local-display-id"}, "edit_token": {"edit-token"}, "text": {"replacement"}}
	for _, action := range []string{"message-edit-prepare", "message-edit-commit"} {
		if w := request(t, s, "POST", base+action, form, cookie); w.Code != http.StatusServiceUnavailable {
			t.Fatal("legacy edit fell back to prompt")
		}
	}
	backend := &messageEditHTTPRuntime{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "same-session"}, live: true}, messageID: "local-display-id"}
	s.runtimes = backend
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if !data.MessageEditEnabled {
		t.Fatal("capable backend did not advertise editing")
	}
	for _, action := range []string{"message-edit-prepare", "message-edit-commit"} {
		for _, bad := range []url.Values{
			{"instance_id": {"instance"}, "message_id": {"local-display-id"}, "edit_token": {"edit-token"}, "text": {"replacement"}},
			{"csrf": {csrf}, "instance_id": {"stale"}, "message_id": {"local-display-id"}, "edit_token": {"edit-token"}, "text": {"replacement"}},
			{"csrf": {csrf}},
		} {
			if w := request(t, s, "POST", base+action, bad, cookie); w.Code < 400 {
				t.Fatal("untrusted edit admitted")
			}
		}
		if w := request(t, s, "POST", base+action, form); w.Code == http.StatusOK {
			t.Fatal("unpaired edit admitted")
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid edits reached backend")
	}
	w := request(t, s, "POST", base+"message-edit-prepare", form, cookie)
	var prepared RuntimeMessageEditPreparation
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &prepared) != nil || prepared.Text != "full original" || prepared.EditToken != "edit-token" {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	for _, text := range []string{"", "\xff", "a\x00b", strings.Repeat("x", (64<<10)+1)} {
		form.Set("text", text)
		if w := request(t, s, "POST", base+"message-edit-commit", form, cookie); w.Code != http.StatusConflict {
			t.Fatal("invalid text reached edit backend")
		}
	}
	form.Set("text", "replacement")
	w = request(t, s, "POST", base+"message-edit-commit", form, cookie)
	var snapshot RuntimeSnapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.InstanceID != "replacement-instance" || snapshot.SessionID != "same-session" || backend.text != "replacement" {
		t.Fatalf("commit: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, s, "POST", base+"message-edit-commit", form, cookie); w.Code != http.StatusConflict {
		t.Fatal("old tab edited replacement")
	}
	for range 3 {
		request(t, s, "GET", "/projects/"+p.ID+"/runtime", nil, cookie)
	}
	if strings.Join(backend.calls, ",") != "prepare,commit" {
		t.Fatalf("read/unknown mutation retry: %v", backend.calls)
	}
}
