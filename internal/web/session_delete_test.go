package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type deletionCatalog struct {
	sidebarCatalog
	calls int
	id    string
	after func()
	err   error
}

func (c *deletionCatalog) DeleteSession(ctx context.Context, p Project, id string) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded deletion")
	}
	c.calls++
	c.id = id
	if c.after != nil {
		c.after()
	}
	return c.err
}

type deletionRuntime struct {
	sidebarRuntime
	deletes int
	err     error
}

func (r *deletionRuntime) SessionDeleteSupported(string, string) bool { return true }
func (r *deletionRuntime) DeleteSession(_ context.Context, p, i, id string) error {
	r.deletes++
	if p != r.snapshot.ProjectID || i != r.snapshot.InstanceID {
		panic("unbound deletion")
	}
	return r.err
}
func TestSessionDeleteHTTPColdAuthorizationAndExactBody(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &deletionCatalog{}
	s.catalog = c
	path := "/projects/" + p.ID + "/sessions/saved/delete"
	csrf := csrfFor(t, s, cookie)
	body := url.Values{"csrf": {csrf}, "confirm": {"delete"}, "instance_id": {""}}
	if w := request(t, s, "POST", path, body); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(t, s, "GET", path, nil, cookie); w.Code == 200 {
		t.Fatal("GET mutation")
	}
	for name, change := range map[string]func(url.Values){
		"csrf":          func(v url.Values) { v.Set("csrf", "bad") },
		"confirmation":  func(v url.Values) { v.Set("confirm", "") },
		"duplicate":     func(v url.Values) { v.Add("confirm", "delete") },
		"unknown":       func(v url.Values) { v.Set("path", "/private/session.db") },
		"missing-owner": func(v url.Values) { v.Del("instance_id") },
		"stale-owner":   func(v url.Values) { v.Set("instance_id", "stale") },
		"bound":         func(v url.Values) { v.Set("csrf", strings.Repeat("x", 5000)) },
	} {
		t.Run(name, func(t *testing.T) {
			v := body.Clone()
			change(v)
			w := request(t, s, "POST", path, v, cookie)
			if w.Code < 400 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	if c.calls != 0 {
		t.Fatal("invalid request reached mutation")
	}
	w := request(t, s, "POST", path+"?confirm=delete", body, cookie)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = request(t, s, "POST", path, body, cookie)
	var result SessionDeleteResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Deleted || result.InstanceID != "" || result.ProjectID != p.ID || result.SessionID != "saved" || c.calls != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(c.offsets) != 0 {
		t.Fatal("mutation performed catalog discovery")
	}
	c.err = errors.New("PRIVATE partial cleanup")
	w = request(t, s, "POST", path, body, cookie)
	if w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "unchanged") || c.calls != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestSessionDeleteHTTPLiveOwnerAndActiveGuards(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &deletionCatalog{}
	s.catalog = c
	r := &deletionRuntime{sidebarRuntime: sidebarRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "owner", SessionID: "active", Status: "idle"}}}}
	s.runtimes = r
	body := url.Values{"csrf": {csrfFor(t, s, cookie)}, "confirm": {"delete"}, "instance_id": {"owner"}}
	path := "/projects/" + p.ID + "/sessions/"
	for _, id := range []string{"", "stale"} {
		v := body.Clone()
		v.Set("instance_id", id)
		if w := request(t, s, "POST", path+"saved/delete", v, cookie); w.Code != 409 {
			t.Fatal(w.Code)
		}
	}
	if w := request(t, s, "POST", path+"active/delete", body, cookie); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if r.deletes != 0 || c.calls != 0 {
		t.Fatal("guard bypass")
	}
	r.err = ErrRuntimeBusy
	if w := request(t, s, "POST", path+"saved/delete", body, cookie); w.Code != 409 {
		t.Fatal(w.Code)
	}
	r.err = nil
	if w := request(t, s, "POST", path+"saved/delete", body, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if c.calls != 0 || len(r.calls) != 0 || r.reads != 0 {
		t.Fatal("delete activated/discovered/read via HTTP")
	}
}
func TestSessionDeleteHTTPOwnerChangeAfterMutationIsUnknown(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	s.runtimes = runtime
	c := &deletionCatalog{after: func() {
		runtime.live = true
		runtime.snapshot = RuntimeSnapshot{ProjectID: p.ID, InstanceID: "replacement"}
	}}
	s.catalog = c
	body := url.Values{"csrf": {csrfFor(t, s, cookie)}, "confirm": {"delete"}, "instance_id": {""}}
	w := request(t, s, "POST", "/projects/"+p.ID+"/sessions/saved/delete", body, cookie)
	if w.Code != http.StatusServiceUnavailable || c.calls != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestSessionDeleteSidebarCapabilities(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.catalog = &deletionCatalog{}
	path := "/projects/" + p.ID + "/sidebar-sessions"
	w := request(t, s, "GET", path, nil, cookie)
	var page SidebarSessions
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || !page.DeleteSupported || page.ActiveSessionID != "" {
		t.Fatal(w.Body.String())
	}
	s.runtimes = &deletionRuntime{sidebarRuntime: sidebarRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "owner", SessionID: "active"}}, inventory: RuntimeSessionInventory{ProjectID: p.ID, InstanceID: "owner", Available: true}}}
	w = request(t, s, "GET", path, nil, cookie)
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || !page.DeleteSupported || page.ActiveSessionID != "active" {
		t.Fatal(w.Body.String())
	}
}
