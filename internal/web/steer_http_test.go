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

type steerHTTPRuntime struct{ fakeRuntime }

func (f *steerHTTPRuntime) SteerCurrentRun(_ context.Context, _, instance, session, token string, revision uint64, requestID, text string) (RuntimeSnapshot, error) {
	if instance != "instance" || session != "session" || token != "steer-token" || revision != 1 {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "steer")
	f.snapshot.Steer = &RuntimeSteer{Token: token, Revision: 2, Items: []RuntimeSteerItem{{RequestID: requestID, ItemID: "native-item", Text: text, Status: "accepted"}}}
	f.snapshot.SteerACK = &RuntimeSteerACK{Token: token, RequestID: requestID, ItemID: "native-item", Status: "accepted"}
	return f.snapshot, nil
}

func TestSteerHTTPAuthorityHasSingleExplicitPostSource(t *testing.T) {
	backend := &steerHTTPRuntime{}
	s := &shell{runtimes: backend}
	form := url.Values{"instance_id": {"instance"}, "session_id": {"session"}, "live_steer_token": {"steer-token"}, "steer_revision": {"1"}, "request_id": {"request"}, "text": {"/literal $skill\r\n text"}}
	post := func(form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/?live_steer_token=other", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.runtimeSteerAction(t.Context(), w, r, Project{ID: "project"})
		return w
	}
	for _, key := range []string{"instance_id", "session_id", "live_steer_token", "steer_revision", "request_id", "text"} {
		bad := form.Clone()
		bad.Del(key)
		if w := post(bad); w.Code != http.StatusConflict {
			t.Fatalf("missing %s admitted", key)
		}
		bad = form.Clone()
		bad.Add(key, bad.Get(key))
		if w := post(bad); w.Code != http.StatusConflict {
			t.Fatalf("duplicate %s admitted", key)
		}
	}
	for _, revision := range []string{"0", "01", "+1", "-1", "18446744073709551616"} {
		bad := form.Clone()
		bad.Set("steer_revision", revision)
		if w := post(bad); w.Code != http.StatusConflict {
			t.Fatalf("invalid revision %s admitted", revision)
		}
	}
	for _, text := range []string{"", "\x00", "\xff", strings.Repeat("x", (64<<10)+1)} {
		bad := form.Clone()
		bad.Set("text", text)
		if w := post(bad); w.Code != http.StatusConflict {
			t.Fatal("invalid text admitted")
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid request dispatched")
	}
	w := post(form)
	var snapshot RuntimeSnapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.SteerACK == nil || snapshot.SteerACK.Status != "accepted" || snapshot.Steer.Items[0].Text != form.Get("text") || len(snapshot.Messages) != 0 || len(backend.calls) != 1 {
		t.Fatalf("literal receipt: %d %s", w.Code, w.Body.String())
	}
	s.runtimes = &fakeRuntime{}
	if w := post(form); w.Code != http.StatusServiceUnavailable {
		t.Fatal("unsupported backend fallback")
	}
}
