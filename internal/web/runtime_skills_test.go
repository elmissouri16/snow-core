package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func skillsTestManager(t *testing.T) (*RuntimeManager, Project, RuntimeSnapshot, string) {
	t.Helper()
	m, projects, log := runtimeTestManager(t, "")
	m.env = slices.DeleteFunc(m.env, func(s string) bool { return strings.HasPrefix(s, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_SKILLS_TEST_CHILD=1")
	snapshot, err := m.OpenWithSkills(t.Context(), projects[0], "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	return m, projects[0], snapshot, log
}

func TestRuntimeSkillsLegacyDisabledAndNeverStartsWorker(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	if _, err := m.Skills(t.Context(), projects[0].ID, "instance"); err == nil {
		t.Fatal("inactive catalog accepted")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("catalog started worker", err)
	}
	// The existing child fixture enforces the exact legacy tool/flag profile.
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before := reasoningLog(t, log)
	catalog, err := m.Skills(t.Context(), projects[0].ID, snapshot.InstanceID)
	if err != nil || catalog.Enabled || len(catalog.Skills) != 0 || catalog.Skills == nil || catalog.Limited {
		t.Fatalf("disabled catalog: %+v, %v", catalog, err)
	}
	if catalog.ProjectID != projects[0].ID || catalog.InstanceID != snapshot.InstanceID || catalog.SessionID != snapshot.SessionID {
		t.Fatalf("disabled catalog identity: %+v", catalog)
	}
	if reasoningLog(t, log) != before {
		t.Fatal("disabled catalog reached worker")
	}
	if _, err := m.OpenWithSkills(t.Context(), projects[0], snapshot.SessionID, "", "", true); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("opt-in changed a live worker", err)
	}
}

func TestRuntimeSkillsOptInWorkerAndPublicCatalog(t *testing.T) {
	m, project, snapshot, log := skillsTestManager(t)
	before := reasoningLog(t, log)
	if strings.Contains(before, "skills") {
		t.Fatal("startup inspected catalog")
	}
	view, err := m.Skills(t.Context(), project.ID, snapshot.InstanceID)
	if err != nil || !view.Enabled || view.Limited || len(view.Skills) != 2 || !view.Skills[0].Enabled || view.Skills[1].Enabled || view.Skills[1].DisabledBy != "disabled by project named skill policy" {
		t.Fatalf("catalog %+v, %v", view, err)
	}
	if view.ProjectID != project.ID || view.InstanceID != snapshot.InstanceID || view.SessionID != snapshot.SessionID {
		t.Fatalf("catalog identity: %+v", view)
	}
	if reasoningLog(t, log) != before+"skills\n" {
		t.Fatal("catalog used an unexpected RPC")
	}
	data, err := json.Marshal(view)
	if err != nil || strings.Contains(string(data), "PRIVATE") {
		t.Fatalf("private metadata escaped: %s, %v", data, err)
	}
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil || len(fields) != 6 {
		t.Fatalf("unexpected top-level fields %s", data)
	}
	for _, skill := range fields["skills"].([]any) {
		if len(skill.(map[string]any)) != 4 {
			t.Fatalf("unexpected skill fields %s", data)
		}
	}
	if _, err := m.Open(t.Context(), project, snapshot.SessionID, "", ""); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("legacy open inherited opt-in", err)
	}
	if _, err := m.OpenWithSkills(t.Context(), project, snapshot.SessionID, "", "", true); err != nil {
		t.Fatal("same-profile open not idempotent", err)
	}
}

func TestRuntimeSkillsFencesStaleBusyAndCanceledRequests(t *testing.T) {
	m, project, snapshot, log := skillsTestManager(t)
	before := reasoningLog(t, log)
	if _, err := m.Skills(t.Context(), project.ID, "stale"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("stale accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.Skills(ctx, project.ID, snapshot.InstanceID); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatal("canceled accepted", err)
	}
	r, _ := m.runtime(project.ID, snapshot.InstanceID)
	r.control.Lock()
	_, err := m.Skills(t.Context(), project.ID, snapshot.InstanceID)
	r.control.Unlock()
	if !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("overlap admitted", err)
	}
	for _, status := range []string{"running", "permission", "input", "switching", "opening", "closing", "failed"} {
		r.mu.Lock()
		r.snapshot.Status = status
		r.mu.Unlock()
		if _, err := m.Skills(t.Context(), project.ID, snapshot.InstanceID); !errors.Is(err, ErrRuntimeBusy) {
			t.Fatalf("%s accepted: %v", status, err)
		}
	}
	r.mu.Lock()
	r.snapshot.Status = "idle"
	r.snapshot.CancelRequested = true
	r.mu.Unlock()
	if _, err := m.Skills(t.Context(), project.ID, snapshot.InstanceID); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("cancellation admitted", err)
	}
	if reasoningLog(t, log) != before {
		t.Fatal("unadmitted catalog reached worker")
	}
}

func TestRuntimeSkillsProjectionBoundsAndPolicy(t *testing.T) {
	catalog := protocol.RPCSkillsList{Skills: make([]protocol.RPCSkill, runtimeSkillsLimit+1)}
	for i := range catalog.Skills {
		catalog.Skills[i] = protocol.RPCSkill{Name: "skill", Description: strings.Repeat("界", 1000), DisabledBy: "/PRIVATE/config: token", Location: "/PRIVATE/path"}
	}
	view := projectRuntimeSkills(catalog)
	if !view.Limited || len(view.Skills) != runtimeSkillsLimit {
		t.Fatalf("unbounded %+v", view)
	}
	for _, skill := range view.Skills {
		if skill.Enabled || len(skill.Description) > runtimeSkillDescriptionBytes || skill.DisabledBy != "Disabled by skill policy" {
			t.Fatalf("bad projection %+v", skill)
		}
	}
	view = projectRuntimeSkills(protocol.RPCSkillsList{Skills: []protocol.RPCSkill{{Name: "/PRIVATE/path", Description: "ignored"}}})
	if len(view.Skills) != 0 || !view.Limited {
		t.Fatal("invalid skill identifier rendered", view)
	}
}

type skillsHTTPRuntime struct {
	fakeRuntime
	rotate bool
	optIns []bool
}

func (f *skillsHTTPRuntime) OpenWithSkills(ctx context.Context, project Project, sessionID, provider, model string, skills bool) (RuntimeSnapshot, error) {
	f.optIns = append(f.optIns, skills)
	return f.Open(ctx, project, sessionID, provider, model)
}
func (f *skillsHTTPRuntime) Skills(_ context.Context, projectID, instanceID string) (RuntimeSkills, error) {
	if projectID != f.snapshot.ProjectID || instanceID != f.snapshot.InstanceID {
		return RuntimeSkills{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "skills")
	if f.rotate {
		f.snapshot.InstanceID = "replacement"
	}
	return RuntimeSkills{ProjectID: "PRIVATE-project", InstanceID: "PRIVATE-instance", SessionID: "PRIVATE-session", Enabled: true, Skills: []RuntimeSkill{}}, nil
}

func TestRuntimeSkillsHTTPStrictAuthority(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &skillsHTTPRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "session", Status: "idle"}}}
	s.runtimes = backend
	base := url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {"instance"}}
	invoke := func(method, action, query string, form url.Values, paired bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost/skills"+query, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("action", action)
		if paired {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.runtimeSkillsAction(t.Context(), w, r, project)
		return w
	}
	for _, key := range []string{"csrf", "instance_id"} {
		for _, duplicate := range []bool{false, true} {
			bad := base.Clone()
			if duplicate {
				bad.Add(key, bad.Get(key))
			} else {
				bad.Del(key)
			}
			if w := invoke("POST", "skills", "", bad, true); w.Code < 400 {
				t.Fatalf("missing/duplicate %s admitted: %d", key, w.Code)
			}
		}
	}
	for _, key := range []string{"command", "session_id", "enable_skills", "path", "confirm"} {
		bad := base.Clone()
		bad.Set(key, "injected")
		if w := invoke("POST", "skills", "", bad, true); w.Code != http.StatusBadRequest {
			t.Fatalf("extra %s: %d", key, w.Code)
		}
	}
	for _, tc := range []struct {
		method, action, query string
		paired                bool
	}{
		{"GET", "skills", "", true}, {"POST", "activate_skill", "", true}, {"POST", "skills", "?instance_id=instance", true}, {"POST", "skills", "", false},
	} {
		if w := invoke(tc.method, tc.action, tc.query, base, tc.paired); w.Code == http.StatusOK {
			t.Fatalf("invalid request admitted %+v", tc)
		}
	}
	bad := base.Clone()
	bad.Set("instance_id", "stale")
	if w := invoke("POST", "skills", "", bad, true); w.Code != http.StatusConflict {
		t.Fatal("stale instance accepted", w.Code)
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid requests reached backend", backend.calls)
	}
	w := invoke("POST", "skills", "", base, true)
	var result RuntimeSkills
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result, json.RejectUnknownMembers(true)) != nil || result.ProjectID != project.ID || result.InstanceID != "instance" || result.SessionID != "session" || !result.Enabled || result.Skills == nil || len(result.Skills) != 0 || result.Limited || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatalf("catalog response %d %s", w.Code, w.Body)
	}
	backend.rotate = true
	if w := invoke("POST", "skills", "", base, true); w.Code != http.StatusConflict {
		t.Fatal("retired result returned", w.Code)
	}
}

func TestRuntimeSkillsActivationOptInValidation(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		for _, choice := range []string{"", "runtime", "true", "project", "remember"} {
			s := &shell{}
			confirm := "activate"
			if trusted {
				confirm = "trusted"
			}
			form := url.Values{"csrf": {"upstream"}, "confirm": {confirm}, "enable_skills": {choice}}
			r := httptest.NewRequest("POST", "/", nil)
			r.PostForm = form
			w := httptest.NewRecorder()
			ok := s.authorizeProjectActivation(t.Context(), w, r, Project{Trusted: trusted})
			if ok != (choice == "" || choice == "runtime") {
				t.Fatalf("trusted=%v choice=%q accepted=%v", trusted, choice, ok)
			}
			form.Add("enable_skills", choice)
			if s.authorizeProjectActivation(t.Context(), httptest.NewRecorder(), r, Project{Trusted: trusted}) {
				t.Fatal("duplicate skill choice accepted")
			}
		}
	}
}

func TestRuntimeSkillsActivationCheckboxUsesSavedPreference(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeRuntime{}
	s.runtimes = backend
	for _, trusted := range []bool{false, true} {
		if trusted {
			if err := s.registry.RememberProjectTrust(t.Context(), project); err != nil {
				t.Fatal(err)
			}
		}
		for _, enabled := range []bool{false, true, false} {
			if err := s.registry.SetProjectSkills(t.Context(), project, enabled); err != nil {
				t.Fatal(err)
			}
			body := request(t, s, "GET", "/?view=projects&project="+project.ID, nil, cookie).Body.String()
			_, input, ok := strings.Cut(body, `<input name="enable_skills"`)
			if !ok {
				t.Fatalf("trusted=%v missing opt-in", trusted)
			}
			input, _, _ = strings.Cut(input, ">")
			if !strings.Contains(input, `value="runtime"`) || strings.Contains(input, "checked") != enabled || strings.Contains(input, "required") {
				t.Fatalf("trusted=%v enabled=%v unexpected input: %s", trusted, enabled, input)
			}
			if !strings.Contains(body, "separate CLI extension trust") || !strings.Contains(body, "The skills choice is remembered for this workspace when you start.") || len(backend.calls) != 0 {
				t.Fatalf("trusted=%v disclosure missing or render activated runtime", trusted)
			}
		}
	}
}

func TestProjectSkillsSettings(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeRuntime{}
	s.runtimes = backend
	path, csrf := "/projects/"+project.ID+"/skills", csrfFor(t, s, cookie)
	for _, tc := range []struct {
		form    url.Values
		status  int
		enabled bool
	}{
		{url.Values{"csrf": {csrf}, "enable_skills": {"runtime"}}, http.StatusSeeOther, true},
		{url.Values{"csrf": {"invalid"}, "enable_skills": {""}}, http.StatusForbidden, true},
		{url.Values{"csrf": {csrf}, "enable_skills": {"", "runtime"}}, http.StatusBadRequest, true},
		{url.Values{"csrf": {csrf}, "enable_skills": {"project"}}, http.StatusBadRequest, true},
		{url.Values{"csrf": {csrf}}, http.StatusBadRequest, true},
		{url.Values{"csrf": {csrf}, "enable_skills": {""}}, http.StatusSeeOther, false},
	} {
		w := request(t, s, "POST", path, tc.form, cookie)
		if w.Code != tc.status {
			t.Fatalf("settings response: %d %s", w.Code, w.Body)
		}
		p, err := s.registry.Lookup(t.Context(), project.ID)
		if err != nil || p.SkillsEnabled != tc.enabled || p.Trusted || len(backend.calls) != 0 {
			t.Fatalf("settings changed authority/runtime or lost preference: %+v, %v, %v", p, err, backend.calls)
		}
	}
}

func TestRuntimeSkillsActivationHTTPExplicitOptInOnly(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &skillsHTTPRuntime{}
	s.runtimes = backend
	path := "/projects/" + project.ID + "/runtime/open"
	csrf := csrfFor(t, s, cookie)
	for _, trusted := range []bool{false, true} {
		confirm := "activate"
		if trusted {
			if err := s.registry.RememberProjectTrust(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			confirm = "trusted"
		}
		for _, choice := range []string{"runtime", "", "omitted"} {
			backend.live = false
			before, beforeOptIns := len(backend.calls), len(backend.optIns)
			form := url.Values{"csrf": {csrf}, "confirm": {confirm}}
			if choice != "omitted" {
				form.Set("enable_skills", choice)
			}
			w := request(t, s, "POST", path, form, cookie)
			if w.Code != http.StatusOK || len(backend.calls) != before+1 || backend.calls[before] != "open" {
				t.Fatalf("trusted=%v choice=%q activation: %d %s calls=%v", trusted, choice, w.Code, w.Body, backend.calls)
			}
			saved, err := s.registry.Lookup(t.Context(), project.ID)
			if err != nil || saved.SkillsEnabled != (choice == "runtime") {
				t.Fatalf("startup preference not saved: %+v, %v", saved, err)
			}
			if choice == "runtime" {
				if len(backend.optIns) != beforeOptIns+1 || !backend.optIns[beforeOptIns] {
					t.Fatalf("explicit opt-in did not reach OpenWithSkills(true): %v", backend.optIns)
				}
			} else if len(backend.optIns) != beforeOptIns {
				t.Fatalf("default activation inherited skill opt-in: %v", backend.optIns)
			}
		}
	}
	// An optional backend must never be silently bypassed for an opt-in start.
	legacy := &fakeRuntime{}
	s.runtimes = legacy
	w := request(t, s, "POST", path, url.Values{"csrf": {csrf}, "confirm": {"trusted"}, "enable_skills": {"runtime"}}, cookie)
	if w.Code != http.StatusServiceUnavailable || len(legacy.calls) != 0 {
		t.Fatalf("unsupported opt-in started legacy worker: %d calls=%v", w.Code, legacy.calls)
	}
}
