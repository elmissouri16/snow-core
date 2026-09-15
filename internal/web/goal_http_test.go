package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"
)

type goalHTTPRuntime struct {
	fakeRuntime
	params RuntimeGoalRunInput
}

func (f *goalHTTPRuntime) InspectGoal(_ context.Context, project, instance, session, branch string) (RuntimeSnapshot, error) {
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID || session != f.snapshot.SessionID || branch != "" && branch != "main" {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "inspect")
	return f.snapshot.clone(), nil
}
func (f *goalHTTPRuntime) RunGoal(_ context.Context, project, instance string, params RuntimeGoalRunInput) (RuntimeSnapshot, error) {
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID || params.SessionID != f.snapshot.SessionID || params.BranchID != "main" {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.params = params
	f.calls = append(f.calls, params.Action)
	return f.snapshot.clone(), nil
}

func TestGoalHTTPAuthorityConsentAndReadOnlyRefresh(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := "/projects/" + p.ID + "/runtime/"
	form := url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {"instance"}, "session_id": {"session"}, "branch_id": {"main"}, "expected_tip_id": {"tip"}, "expected_goal_id": {""}, "objective": {"ship the reviewed work"}, "token_budget": {"1000"}, "expected_revision": {"5"}}
	backend := &goalHTTPRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Status: "idle", Goal: &RuntimeGoal{SessionID: "session", BranchID: "main", TipID: "tip", Status: "none"}}}}
	s.runtimes = backend
	for _, key := range []string{"csrf", "instance_id", "session_id", "branch_id", "expected_tip_id", "expected_goal_id", "objective", "expected_revision"} {
		missing := form.Clone()
		missing.Del(key)
		if w := request(t, s, "POST", base+"goal-start", missing, cookie); w.Code < 400 {
			t.Fatalf("missing %s dispatched", key)
		}
		duplicate := form.Clone()
		duplicate.Add(key, duplicate.Get(key))
		if w := request(t, s, "POST", base+"goal-start", duplicate, cookie); w.Code < 400 {
			t.Fatalf("duplicate %s dispatched", key)
		}
	}
	for _, key := range []string{"command", "provider", "model", "permission_mode", "goal_id", "action"} {
		extra := form.Clone()
		extra.Set(key, "untrusted")
		if w := request(t, s, "POST", base+"goal-start", extra, cookie); w.Code < 400 {
			t.Fatalf("untyped field %s dispatched", key)
		}
	}
	for _, budget := range []string{"0", "-1", "+1", "01", "9223372036854775808"} {
		bad := form.Clone()
		bad.Set("token_budget", budget)
		if w := request(t, s, "POST", base+"goal-start", bad, cookie); w.Code < 400 {
			t.Fatalf("budget %q dispatched", budget)
		}
	}

	for _, revision := range []string{"", "0", "-1", "+1", "01", "18446744073709551616"} {
		bad := form.Clone()
		bad.Set("expected_revision", revision)
		if w := request(t, s, "POST", base+"goal-start", bad, cookie); w.Code < 400 {
			t.Fatalf("invalid revision %q dispatched", revision)
		}
	}
	if w := request(t, s, "POST", base+"goal-start?instance_id=other", form, cookie); w.Code < 400 {
		t.Fatal("shadow query authority")
	}
	if w := request(t, s, "POST", base+"goal-start", form); w.Code == http.StatusOK {
		t.Fatal("unpaired mutation")
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid request reached backend: %v", backend.calls)
	}
	if w := request(t, s, "POST", base+"goal-start", form, cookie); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if backend.params.ExpectedRevision != 5 || backend.params.Action != "create" || backend.params.ExpectedGoalID != "" || backend.params.TokenBudget == nil || *backend.params.TokenBudget != 1000 {
		t.Fatalf("explicit create contract: %+v", backend.params)
	}
	inspect := url.Values{"csrf": {form.Get("csrf")}, "instance_id": {"instance"}, "session_id": {"session"}, "branch_id": {"main"}}
	for range 2 {
		if w := request(t, s, "POST", base+"goal-inspect", inspect, cookie); w.Code != http.StatusOK {
			t.Fatal("read-only inspect failed")
		}
	}
	resume := form.Clone()
	resume.Del("objective")
	resume.Del("token_budget")
	resume.Set("expected_goal_id", "goal")
	if w := request(t, s, "POST", base+"goal-resume", resume, cookie); w.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", w.Code, w.Body.String())
	}
	if backend.params.Action != "resume" || backend.params.ExpectedGoalID != "goal" || backend.params.Objective != "" || backend.params.TokenBudget != nil {
		t.Fatal("resume changed objective/budget")
	}
	for range 2 {
		request(t, s, "GET", base[:len(base)-1], nil, cookie)
	}
	if !slices.Equal(backend.calls, []string{"create", "inspect", "inspect", "resume"}) {
		t.Fatalf("read started/replayed work: %v", backend.calls)
	}
}
