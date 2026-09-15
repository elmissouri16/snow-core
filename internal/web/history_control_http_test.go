package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type historyControlHTTPRuntime struct {
	fakeRuntime
	last RuntimeHistoryControlRequest
}

func (f *historyControlHTTPRuntime) HistoryBranchFork(_ context.Context, project, instance string, p RuntimeHistoryControlRequest) (RuntimeSnapshot, error) {
	f.calls = append(f.calls, "fork")
	f.last = p
	return RuntimeSnapshot{ProjectID: project, InstanceID: "replacement", SessionID: p.SessionID, Status: "idle"}, nil
}
func (f *historyControlHTTPRuntime) HistorySessionFork(_ context.Context, project, instance string, p RuntimeHistoryControlRequest) (RuntimeHistoryControlResult, error) {
	f.calls = append(f.calls, "detached")
	f.last = p
	return RuntimeHistoryControlResult{ProjectID: project, InstanceID: instance, SessionID: p.SessionID, ChildSessionID: "child"}, nil
}
func (f *historyControlHTTPRuntime) HistoryBranchRename(_ context.Context, project, instance string, p RuntimeHistoryControlRequest) (RuntimeHistoryControlResult, error) {
	f.calls = append(f.calls, "rename")
	f.last = p
	return RuntimeHistoryControlResult{ProjectID: project, InstanceID: instance, SessionID: p.SessionID, BranchID: p.TargetBranchID, TipID: p.TargetTipID, Name: p.Name}, nil
}

func TestHistoryControlHTTPStrictFormsAndDedicatedActions(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &historyControlHTTPRuntime{}
	s.runtimes = backend
	csrf := csrfFor(t, s, cookie)
	invoke := func(action string, form url.Values, method, query string, paired bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://localhost/history"+query, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("action", action)
		if paired {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		s.runtimeHistoryControlAction(t.Context(), response, req, project)
		return response
	}
	base := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "session_id": {"session"}, "expected_revision": {"12"}, "current_branch_id": {"current"}, "current_tip_id": {"current-tip"}, "branch_id": {"target"}, "tip_id": {"target-tip"}, "name": {"Display name"}}
	for _, action := range []string{"history-branch-fork", "history-session-fork", "history-branch-rename"} {
		form := base.Clone()
		if action == "history-branch-rename" {
			form.Set("old_name", "Exact previous label")
		}
		count := len(backend.calls)
		for key := range form {
			bad := form.Clone()
			bad.Del(key)
			if result := invoke(action, bad, "POST", "", true); result.Code < 400 {
				t.Fatalf("missing %s accepted", key)
			}
			bad = form.Clone()
			bad.Add(key, bad.Get(key))
			if result := invoke(action, bad, "POST", "", true); result.Code < 400 {
				t.Fatalf("duplicate %s accepted", key)
			}
		}
		for _, key := range []string{"rpc_command", "from_entry_id", "destination", "worktree", "activate", "text", "permissions", "queue_token"} {
			bad := form.Clone()
			bad.Set(key, "injected")
			if result := invoke(action, bad, "POST", "", true); result.Code < 400 {
				t.Fatalf("unbounded field %s accepted", key)
			}
		}
		for _, revision := range []string{"0", "-1", "abc", "18446744073709551616"} {
			bad := form.Clone()
			bad.Set("expected_revision", revision)
			if result := invoke(action, bad, "POST", "", true); result.Code < 400 {
				t.Fatalf("bad revision %s accepted", revision)
			}
		}
		if result := invoke(action, form, "GET", "", true); result.Code != http.StatusMethodNotAllowed {
			t.Fatal("GET mutation")
		}
		if result := invoke(action, form, "POST", "?instance_id=other", true); result.Code < 400 {
			t.Fatal("query overrides form")
		}
		if result := invoke(action, form, "POST", "", false); result.Code == http.StatusOK {
			t.Fatal("unpaired mutation")
		}
		if len(backend.calls) != count {
			t.Fatal("invalid form reached backend")
		}
		if result := invoke(action, form, "POST", "", true); result.Code != http.StatusOK {
			t.Fatalf("valid form rejected: %d %s", result.Code, result.Body.String())
		}
		if backend.last.SessionID != "session" || backend.last.SourceBranchID != "current" || backend.last.TargetBranchID != "target" || backend.last.SourceTipID != "current-tip" || backend.last.TargetTipID != "target-tip" || backend.last.ExpectedRevision != 12 || action == "history-branch-rename" && backend.last.OldName != "Exact previous label" {
			t.Fatal("binding lost")
		}
	}
	s.runtimes = &fakeRuntime{}
	if result := invoke("history-branch-fork", base, "POST", "", true); result.Code != http.StatusServiceUnavailable {
		t.Fatal("optional interface not required")
	}
}
