package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type queueHTTPRuntime struct{ fakeRuntime }

func (f *queueHTTPRuntime) queueValid(instance, session, token string, revision uint64) bool {
	return instance == f.snapshot.InstanceID && session == f.snapshot.SessionID && f.snapshot.Queue != nil && token == f.snapshot.Queue.Token && revision == f.snapshot.Queue.Revision
}
func (f *queueHTTPRuntime) EnqueueFollowUp(_ context.Context, _, instance, session, token string, revision uint64, text string) (RuntimeSnapshot, error) {
	if !f.queueValid(instance, session, token, revision) {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "enqueue")
	f.snapshot.Queue.Items = append(f.snapshot.Queue.Items, RuntimeQueueItem{ID: "queued-id", Text: text, State: "pending"})
	f.snapshot.Queue.Revision++
	return f.snapshot.clone(), nil
}
func (f *queueHTTPRuntime) UpdateQueuedFollowUp(_ context.Context, _, instance, session, token string, revision uint64, id, text string) (RuntimeSnapshot, error) {
	if !f.queueValid(instance, session, token, revision) || len(f.snapshot.Queue.Items) != 1 || id != f.snapshot.Queue.Items[0].ID {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "update")
	f.snapshot.Queue.Items[0].Text = text
	f.snapshot.Queue.Revision++
	return f.snapshot.clone(), nil
}
func (f *queueHTTPRuntime) RemoveQueuedFollowUp(_ context.Context, _, instance, session, token string, revision uint64, id string) (RuntimeSnapshot, error) {
	if !f.queueValid(instance, session, token, revision) || len(f.snapshot.Queue.Items) != 1 || id != f.snapshot.Queue.Items[0].ID {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "remove")
	f.snapshot.Queue.Items = nil
	f.snapshot.Queue.Revision++
	return f.snapshot.clone(), nil
}

func TestQueueHTTPTypedCapabilityIdentityCASAndNoReadReplay(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFor(t, s, cookie)
	base := "/projects/" + p.ID + "/runtime/"
	form := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "session_id": {"session"}, "queue_token": {"queue-token"}, "queue_revision": {"1"}, "text": {"queued text"}}
	s.runtimes = &fakeRuntime{}
	if w := request(t, s, "POST", base+"queue-enqueue", form, cookie); w.Code != http.StatusServiceUnavailable {
		t.Fatal("legacy queue fallback")
	}
	data := pageData{}
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if data.QueueNextEnabled {
		t.Fatal("legacy queue capability")
	}
	backend := &queueHTTPRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Queue: &RuntimeQueue{Token: "queue-token", Revision: 1, CanEnqueue: true}}}}
	s.runtimes = backend
	if err := s.projectData(t.Context(), url.Values{}, &data); err != nil {
		t.Fatal(err)
	}
	if !data.QueueNextEnabled {
		t.Fatal("typed queue capability absent")
	}
	for _, key := range []string{"csrf", "instance_id", "session_id", "queue_token", "queue_revision"} {
		bad := form.Clone()
		bad.Del(key)
		if w := request(t, s, "POST", base+"queue-enqueue", bad, cookie); w.Code == http.StatusOK {
			t.Fatalf("missing %s admitted", key)
		}
		if key != "csrf" {
			bad = form.Clone()
			bad.Add(key, bad.Get(key))
			if w := request(t, s, "POST", base+"queue-enqueue", bad, cookie); w.Code < 400 {
				t.Fatalf("duplicate %s admitted", key)
			}
		}
	}
	for _, value := range []string{"", "-1", "+1", "01", "18446744073709551616"} {
		bad := form.Clone()
		bad.Set("queue_revision", value)
		if w := request(t, s, "POST", base+"queue-enqueue", bad, cookie); w.Code != http.StatusConflict {
			t.Fatalf("invalid CAS: %q", value)
		}
	}
	for _, value := range []string{"", "\xff", "x\x00y", strings.Repeat("x", (64<<10)+1)} {
		bad := form.Clone()
		bad.Set("text", value)
		if w := request(t, s, "POST", base+"queue-enqueue", bad, cookie); w.Code != http.StatusConflict {
			t.Fatal("invalid text admitted")
		}
	}
	if w := request(t, s, "POST", base+"queue-enqueue", form); w.Code == http.StatusOK {
		t.Fatal("unpaired enqueue")
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid queue request reached backend")
	}
	w := request(t, s, "POST", base+"queue-enqueue", form, cookie)
	var snapshot RuntimeSnapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.Queue == nil || len(snapshot.Queue.Items) != 1 || snapshot.Queue.Items[0].Text != "queued text" || len(snapshot.Messages) != 0 {
		t.Fatalf("enqueue projection: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, s, "POST", base+"queue-enqueue", form, cookie); w.Code != http.StatusConflict {
		t.Fatal("stale CAS duplicated enqueue")
	}
	form.Set("queue_revision", "2")
	form.Set("item_id", "queued-id")
	form.Set("text", "updated text")
	if w := request(t, s, "POST", base+"queue-update", form, cookie); w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	form.Set("queue_revision", "3")
	form.Del("text")
	if w := request(t, s, "POST", base+"queue-remove", form, cookie); w.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	for range 3 {
		request(t, s, "GET", "/projects/"+p.ID+"/runtime", nil, cookie)
	}
	if !slices.Equal(backend.calls, []string{"enqueue", "update", "remove"}) {
		t.Fatalf("read replay or fallback: %v", backend.calls)
	}
}

func TestQueueSnapshotOwnsPendingTextProjection(t *testing.T) {
	source := RuntimeSnapshot{Queue: &RuntimeQueue{Token: "queue", Revision: 4, Items: []RuntimeQueueItem{{ID: "item", Text: "original", State: "held"}}}}
	clone := source.clone()
	clone.Queue.Token = "different"
	clone.Queue.Items[0].Text = "changed"
	if source.Queue.Token != "queue" || source.Queue.Items[0].Text != "original" {
		t.Fatal("snapshot caller mutated queue authority or pending text")
	}
}
