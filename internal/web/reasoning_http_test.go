package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type fakeReasoningBackend struct {
	fakeRuntime
	view   RuntimeReasoning
	calls  []string
	input  RuntimeReasoningInput
	rotate bool
}

func (f *fakeReasoningBackend) InspectReasoning(_ context.Context, project, instance, session string) (RuntimeReasoning, error) {
	if project != f.view.ProjectID || instance != f.view.InstanceID || session != f.view.SessionID {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "inspect")
	return f.view, nil
}
func (f *fakeReasoningBackend) SetReasoning(_ context.Context, project, instance string, input RuntimeReasoningInput) (RuntimeReasoning, error) {
	if project != f.view.ProjectID || instance != f.view.InstanceID || input.Expected.SessionID != f.view.SessionID {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "set")
	f.input = input
	f.view.Revision++
	f.snapshot.Revision = f.view.Revision
	if f.rotate {
		f.snapshot.InstanceID = "replacement"
	}
	return f.view, nil
}

func reasoningHTTPFixture(t *testing.T) (*shell, Project, *fakeReasoningBackend) {
	t.Helper()
	s, _, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "reasoning", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := RuntimeReasoning{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", BranchID: "branch", TipID: "", Revision: 5, Provider: "p", Model: "m", Mode: "plan", PermissionMode: "deny", Thinking: "medium", ReasoningSummary: "auto", TextVerbosity: "low", CurrentSessionAvailable: true, ThinkingLevels: []string{"off", "medium", "high"}}
	f := &fakeReasoningBackend{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: v.InstanceID, SessionID: v.SessionID, Status: "idle", Revision: v.Revision, Provider: v.Provider, Model: v.Model, Mode: v.Mode, PermissionMode: v.PermissionMode, Thinking: v.Thinking}, live: true}, view: v}
	s.runtimes = f
	return s, p, f
}
func reasoningHTTPValues(v RuntimeReasoning) url.Values {
	return url.Values{"csrf": {"checked-upstream"}, "instance_id": {v.InstanceID}, "session_id": {v.SessionID}, "branch_id": {v.BranchID}, "tip_id": {v.TipID}, "expected_revision": {strconv.FormatUint(v.Revision, 10)}, "provider": {v.Provider}, "model": {v.Model}, "mode": {v.Mode}, "permission_mode": {v.PermissionMode}, "thinking": {v.Thinking}, "reasoning_summary": {v.ReasoningSummary}, "text_verbosity": {v.TextVerbosity}, "scope": {"session"}, "field": {"thinking"}, "value": {"high"}, "confirm": {"session"}}
}
func reasoningHTTPCall(t *testing.T, s *shell, p Project, action string, values url.Values, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/projects/"+p.ID+"/runtime/"+action+query, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("action", action)
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.runtimeReasoningAction(t.Context(), w, r, p)
	return w
}
func TestReasoningHTTPAllowlistConfirmationAndCAS(t *testing.T) {
	for name, mutate := range map[string]func(url.Values){
		"extra debug": func(v url.Values) { v.Set("debug_enabled", "true") }, "extra skills": func(v url.Values) { v.Set("skills_enabled", "true") }, "extra mcp": func(v url.Values) { v.Set("mcp", "true") }, "extra plugin": func(v url.Values) { v.Set("plugins", "true") }, "extra subagents": func(v url.Values) { v.Set("subagents_enabled", "true") },
		"duplicate field": func(v url.Values) { v.Add("field", "text_verbosity") }, "no confirm": func(v url.Values) { v.Del("confirm") }, "ambiguous confirm": func(v url.Values) { v.Set("confirm", "yes") }, "defaults scope": func(v url.Values) { v.Set("scope", "defaults") }, "no model": func(v url.Values) { v.Del("model") }, "no mode": func(v url.Values) { v.Del("mode") }, "no policy": func(v url.Values) { v.Del("permission_mode") }, "no summary": func(v url.Values) { v.Del("reasoning_summary") }, "no old thinking": func(v url.Values) { v.Del("thinking") }, "leading revision": func(v url.Values) { v.Set("expected_revision", "05") }, "zero revision": func(v url.Values) { v.Set("expected_revision", "0") }, "unsafe field": func(v url.Values) { v.Set("field", "debug_enabled") },
	} {
		t.Run(name, func(t *testing.T) {
			s, p, f := reasoningHTTPFixture(t)
			v := reasoningHTTPValues(f.view)
			mutate(v)
			w := reasoningHTTPCall(t, s, p, "reasoning-set", v, "")
			if w.Code != http.StatusConflict || len(f.calls) != 0 {
				t.Fatalf("status %d calls %v", w.Code, f.calls)
			}
		})
	}
	s, p, f := reasoningHTTPFixture(t)
	w := reasoningHTTPCall(t, s, p, "reasoning-set", reasoningHTTPValues(f.view), "")
	if w.Code != http.StatusOK || len(f.calls) != 1 || !f.input.Confirm || f.input.Scope != "session" || !reasoningFactsEqual(f.input.Expected, f.view) {
		t.Fatalf("status %d input %+v", w.Code, f.input)
	}
	var result RuntimeReasoning
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Revision != 6 {
		t.Fatalf("response %s error %v", w.Body, err)
	}
}
func TestReasoningHTTPRejectsQueryAuthorityAndRetiredResponse(t *testing.T) {
	for _, query := range []string{"?instance_id=instance", "?command=settings_update", "?debug_enabled=true"} {
		t.Run(query, func(t *testing.T) {
			s, p, f := reasoningHTTPFixture(t)
			w := reasoningHTTPCall(t, s, p, "reasoning-set", reasoningHTTPValues(f.view), query)
			if w.Code != http.StatusConflict || len(f.calls) != 0 {
				t.Fatalf("status %d calls %v", w.Code, f.calls)
			}
		})
	}
	s, p, f := reasoningHTTPFixture(t)
	f.rotate = true
	w := reasoningHTTPCall(t, s, p, "reasoning-set", reasoningHTTPValues(f.view), "")
	if w.Code != http.StatusConflict {
		t.Fatalf("retired response %d", w.Code)
	}
}
func TestReasoningHTTPInspectionIsSeparateRead(t *testing.T) {
	s, p, f := reasoningHTTPFixture(t)
	v := url.Values{"csrf": {"checked-upstream"}, "instance_id": {f.view.InstanceID}, "session_id": {f.view.SessionID}}
	if w := reasoningHTTPCall(t, s, p, "reasoning-inspect", v, ""); w.Code != http.StatusOK {
		t.Fatalf("inspection %d %s", w.Code, w.Body)
	}
	v.Set("field", "thinking")
	if w := reasoningHTTPCall(t, s, p, "reasoning-inspect", v, ""); w.Code != http.StatusConflict || len(f.calls) != 1 {
		t.Fatalf("read tunneled mutation %d %v", w.Code, f.calls)
	}
}
