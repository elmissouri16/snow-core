package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type messageRegenerateHTTPRuntime struct{ fakeRuntime }

func (f *messageRegenerateHTTPRuntime) PrepareMessageRegenerate(_ context.Context, project, instance, message string) (RuntimeMessageRegeneratePreparation, error) {
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID || message != "reply-row" {
		return RuntimeMessageRegeneratePreparation{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "prepare")
	return RuntimeMessageRegeneratePreparation{ProjectID: project, SessionID: f.snapshot.SessionID, InstanceID: instance, MessageID: message, EditToken: "regenerate-token"}, nil
}
func (f *messageRegenerateHTTPRuntime) CommitMessageRegenerate(_ context.Context, project, instance, token string) (RuntimeSnapshot, error) {
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID || token != "regenerate-token" {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "commit")
	f.snapshot.InstanceID = "new-instance"
	return f.snapshot, nil
}

func TestMessageRegenerateHTTPOptionalConfirmedTypedAndNoReplay(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := "/projects/" + p.ID + "/runtime/"
	csrf := csrfFor(t, s, cookie)
	s.runtimes = &fakeRuntime{}
	data := pageData{}
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if data.MessageRegenerateEnabled {
		t.Fatal("legacy backend advertised regeneration")
	}
	form := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "message_id": {"reply-row"}, "edit_token": {"regenerate-token"}, "confirm": {"regenerate"}}
	for _, action := range []string{"message-regenerate-prepare", "message-regenerate-commit"} {
		if w := request(t, s, "POST", base+action, form, cookie); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("legacy fallback: %d", w.Code)
		}
	}
	backend := &messageRegenerateHTTPRuntime{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, SessionID: "same-session", InstanceID: "instance"}, live: true}}
	s.runtimes = backend
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if !data.MessageRegenerateEnabled || data.MessageEditEnabled {
		t.Fatal("capabilities are not independent")
	}
	for _, action := range []string{"message-regenerate-prepare", "message-regenerate-commit"} {
		if w := request(t, s, "POST", base+action, form); w.Code == http.StatusOK {
			t.Fatal("unpaired regeneration admitted")
		}
		for _, key := range []string{"csrf", "instance_id"} {
			bad := form.Clone()
			bad.Del(key)
			if w := request(t, s, "POST", base+action, bad, cookie); w.Code < 400 {
				t.Fatal("untrusted request admitted")
			}
		}
		if w := request(t, s, "GET", base+action, nil, cookie); w.Code < 400 {
			t.Fatal("read performed mutation")
		}
	}
	for _, confirm := range []string{"", "true", "edit", "Regenerate"} {
		bad := form.Clone()
		bad.Set("confirm", confirm)
		if w := request(t, s, "POST", base+"message-regenerate-commit", bad, cookie); w.Code != http.StatusConflict {
			t.Fatalf("missing exact confirmation: %d", w.Code)
		}
	}
	for _, text := range []string{"", "browser draft", "replacement"} {
		bad := form.Clone()
		bad.Set("text", text)
		if w := request(t, s, "POST", base+"message-regenerate-commit", bad, cookie); w.Code != http.StatusConflict {
			t.Fatalf("text accepted: %d", w.Code)
		}
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid requests reached backend: %v", backend.calls)
	}
	w := request(t, s, "POST", base+"message-regenerate-prepare", form, cookie)
	var prepared RuntimeMessageRegeneratePreparation
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &prepared) != nil || prepared.ProjectID != p.ID || prepared.SessionID != "same-session" || prepared.InstanceID != "instance" || prepared.MessageID != "reply-row" || prepared.EditToken != "regenerate-token" || strings.Contains(w.Body.String(), "text") {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	w = request(t, s, "POST", base+"message-regenerate-commit", form, cookie)
	var snapshot RuntimeSnapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.InstanceID != "new-instance" || snapshot.SessionID != "same-session" {
		t.Fatalf("commit: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, s, "POST", base+"message-regenerate-commit", form, cookie); w.Code != http.StatusConflict {
		t.Fatal("old tab retained authority")
	}
	for range 3 {
		request(t, s, "GET", "/projects/"+p.ID+"/runtime", nil, cookie)
	}
	if strings.Join(backend.calls, ",") != "prepare,commit" {
		t.Fatalf("mutation replay: %v", backend.calls)
	}
}
