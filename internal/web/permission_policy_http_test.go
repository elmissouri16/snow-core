package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakePermissionPolicy struct {
	fakeWorkflow
	confirmed bool
	rotate    bool
}

func (f *fakePermissionPolicy) SetPermissionMode(_ context.Context, projectID, instanceID, mode string, confirm bool) error {
	if projectID != f.snapshot.ProjectID || instanceID != f.snapshot.InstanceID || !f.live {
		return ErrRuntimeInvalid
	}
	if f.snapshot.Status != "idle" {
		return ErrRuntimeBusy
	}
	f.calls = append(f.calls, "permission-mode")
	f.confirmed = confirm
	f.snapshot.PermissionMode = mode
	if f.rotate {
		f.snapshot.InstanceID = "replacement"
	}
	return nil
}

func TestPermissionPolicyHTTPValidationAndAuthoritativeResponse(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "policy", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.registry.Add(t.Context(), "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakePermissionPolicy{fakeWorkflow: fakeWorkflow{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{
		ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Status: "idle", PermissionMode: "ask",
	}, live: true}}}
	s.runtimes = backend
	csrf := csrfFor(t, s, cookie)
	path := "/projects/" + p.ID + "/runtime/permission-mode"
	valid := func(mode string) url.Values {
		v := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "mode": {mode}}
		if mode == "allow" {
			v.Set("confirm_allow", "allow")
		}
		return v
	}
	if w := request(t, s, "POST", path, valid("ask")); w.Code != http.StatusSeeOther {
		t.Fatalf("unpaired mutation: %d", w.Code)
	}
	if w := request(t, s, "GET", path, nil, cookie); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET mutation: %d", w.Code)
	}
	for _, tc := range []struct {
		name   string
		mutate func(url.Values)
		want   int
	}{
		{"csrf absent", func(v url.Values) { v.Del("csrf") }, http.StatusForbidden},
		{"csrf wrong", func(v url.Values) { v.Set("csrf", "wrong") }, http.StatusForbidden},
		{"instance absent", func(v url.Values) { v.Del("instance_id") }, http.StatusConflict},
		{"instance stale", func(v url.Values) { v.Set("instance_id", "stale") }, http.StatusConflict},
		{"instance duplicate", func(v url.Values) { v.Add("instance_id", "stale") }, http.StatusBadRequest},
		{"mode invalid", func(v url.Values) { v.Set("mode", "auto") }, http.StatusConflict},
		{"mode uppercase", func(v url.Values) { v.Set("mode", "ALLOW") }, http.StatusConflict},
		{"mode absent", func(v url.Values) { v.Del("mode") }, http.StatusConflict},
		{"mode duplicate", func(v url.Values) { v.Add("mode", "deny") }, http.StatusBadRequest},
		{"unconfirmed allow", func(v url.Values) { v.Del("confirm_allow") }, http.StatusConflict},
		{"ambiguous allow", func(v url.Values) { v.Set("confirm_allow", "true") }, http.StatusConflict},
		{"duplicate confirmation", func(v url.Values) { v.Add("confirm_allow", "allow") }, http.StatusBadRequest},
		{"confirmation not token", func(v url.Values) { v.Set("mode", "ask") }, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := valid("allow")
			tc.mutate(v)
			if w := request(t, s, "POST", path, v, cookie); w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
	if w := request(t, s, "POST", "/projects/"+other.ID+"/runtime/permission-mode", valid("deny"), cookie); w.Code != http.StatusConflict {
		t.Fatalf("wrong project: %d", w.Code)
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid mutation reached policy backend")
	}
	for _, mode := range []string{"ask", "deny", "allow"} {
		w := request(t, s, "POST", path, valid(mode), cookie)
		var snapshot RuntimeSnapshot
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || snapshot.PermissionMode != mode || snapshot.InstanceID != "instance" || snapshot.SessionID != "session" {
			t.Fatalf("policy response %s: %d %s", mode, w.Code, w.Body.String())
		}
		if backend.confirmed != (mode == "allow") {
			t.Fatal("wrong explicit confirmation")
		}
	}
	backend.rotate = true
	if w := request(t, s, "POST", path, valid("deny"), cookie); w.Code != http.StatusConflict {
		t.Fatal("response acknowledged replacement session")
	}
}

func TestPermissionPolicyHTTPOptionalCapability(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "policy", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.runtimes = &fakeWorkflow{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Status: "idle"}, live: true}}
	data := pageData{}
	if err := s.projectData(t.Context(), url.Values{"project": {p.ID}}, &data); err != nil {
		t.Fatal(err)
	}
	if data.PermissionPolicyEnabled {
		t.Fatal("legacy backend exposed policy editing")
	}
	w := request(t, s, "POST", "/projects/"+p.ID+"/runtime/permission-mode", url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {"instance"}, "mode": {"deny"}}, cookie)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("optional backend: %d", w.Code)
	}
	s.runtimes = &fakePermissionPolicy{fakeWorkflow: *s.runtimes.(*fakeWorkflow)}
	if err := s.projectData(t.Context(), url.Values{"project": {p.ID}}, &data); err != nil {
		t.Fatal(err)
	}
	if !data.PermissionPolicyEnabled {
		t.Fatal("policy backend capability not exposed")
	}
}

func TestPermissionPolicyHTTPRejectsBusyAndPendingInteractions(t *testing.T) {
	for _, prompt := range []string{"hold", "permission", "input"} {
		t.Run(prompt, func(t *testing.T) {
			s, cookie, _ := projectShell(t)
			p, err := s.registry.Add(t.Context(), "policy", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			m, _, log := runtimeTestManager(t, "")
			s.runtimes = m
			snapshot, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prompt(t.Context(), p.ID, snapshot.InstanceID, prompt); err != nil {
				t.Fatal(err)
			}
			if prompt != "hold" {
				runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == prompt })
			}
			for _, mode := range []string{"ask", "deny", "allow"} {
				v := url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {snapshot.InstanceID}, "mode": {mode}}
				if mode == "allow" {
					v.Set("confirm_allow", "allow")
				}
				w := request(t, s, "POST", "/projects/"+p.ID+"/runtime/permission-mode", v, cookie)
				if w.Code != http.StatusConflict {
					t.Fatalf("busy policy admitted: %d", w.Code)
				}
			}
			if strings.Contains(policyLog(t, log), "permission_mode_set") || strings.Contains(policyLog(t, log), "permission_reply") {
				t.Fatal("policy confirmation changed or approved pending work")
			}
		})
	}
}
