package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

type versionHTTPRuntime struct{ fakeRuntime }

func (f *versionHTTPRuntime) ListVersions(_ context.Context, project, instance, session, cursor string) (RuntimeVersionsPage, error) {
	f.calls = append(f.calls, "list:"+cursor)
	return RuntimeVersionsPage{ProjectID: project, InstanceID: instance, SessionID: session, Revision: 9, CurrentBranchID: "current", CurrentTipID: "current-tip", Versions: []RuntimeVersion{{BranchID: "target", TipID: "target-tip", Name: "Earlier version"}}, HasMore: true, NextCursor: "page-two"}, nil
}
func (f *versionHTTPRuntime) PreviewVersion(_ context.Context, project, instance, session, branch, tip, cursor string) (RuntimeVersionPreview, error) {
	f.calls = append(f.calls, "preview:"+branch+":"+tip+":"+cursor)
	return RuntimeVersionPreview{ProjectID: project, InstanceID: instance, SessionID: session, Revision: 9, BranchID: branch, TipID: tip, Messages: []RuntimeMessage{{Role: "user", Text: "Preview only"}}}, nil
}
func (f *versionHTTPRuntime) PrepareVersionRestore(_ context.Context, project, instance, session, current, currentTip, branch, tip string) (RuntimeVersionRestorePreparation, error) {
	f.calls = append(f.calls, "prepare:"+current+":"+currentTip+":"+branch+":"+tip)
	return RuntimeVersionRestorePreparation{ProjectID: project, InstanceID: instance, SessionID: session, CurrentBranchID: current, CurrentTipID: currentTip, BranchID: branch, TipID: tip, RestoreToken: "restore-only-token", ExpiresAt: time.Now().Add(2 * time.Minute)}, nil
}
func (f *versionHTTPRuntime) CommitVersionRestore(_ context.Context, _, instance, session, token string) (RuntimeSnapshot, error) {
	if instance != f.snapshot.InstanceID || session != f.snapshot.SessionID || token != "restore-only-token" {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "commit")
	f.snapshot.InstanceID = "rotated"
	return f.snapshot, nil
}

func TestVersionHTTPStrictTypedReadPrepareAndCommit(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFor(t, s, cookie)
	backend := &versionHTTPRuntime{fakeRuntime: fakeRuntime{snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Status: "idle", Messages: []RuntimeMessage{{Role: "user", Text: "Current transcript"}}}}}
	s.runtimes = backend
	invoke := func(action string, form url.Values, paired bool, method, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://localhost/versions"+query, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("action", action)
		if paired {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.runtimeVersionAction(t.Context(), w, req, p)
		return w
	}
	base := url.Values{"csrf": {csrf}, "instance_id": {"instance"}, "session_id": {"session"}}
	for _, action := range []string{"versions-list", "version-preview", "version-restore-prepare", "version-restore-commit"} {
		form := base.Clone()
		switch action {
		case "versions-list":
			form.Set("cursor", "page-one")
		case "version-preview":
			form.Set("branch_id", "target")
			form.Set("tip_id", "target-tip")
			form.Set("cursor", "preview-page")
		case "version-restore-prepare":
			form.Set("current_branch_id", "current")
			form.Set("current_tip_id", "current-tip")
			form.Set("branch_id", "target")
			form.Set("tip_id", "target-tip")
		case "version-restore-commit":
			form.Set("restore_token", "restore-only-token")
		}
		before := len(backend.calls)
		for key := range form {
			bad := form.Clone()
			bad.Add(key, bad.Get(key))
			if w := invoke(action, bad, true, "POST", ""); w.Code < 400 {
				t.Fatalf("%s duplicate %s admitted", action, key)
			}
			if key != "cursor" {
				bad = form.Clone()
				bad.Del(key)
				if w := invoke(action, bad, true, "POST", ""); w.Code == http.StatusOK {
					t.Fatalf("%s missing %s admitted", action, key)
				}
			}
		}
		for _, key := range []string{"rpc_command", "text", "permission", "queue_token", "edit_token", "unknown"} {
			bad := form.Clone()
			bad.Set(key, "injected")
			if w := invoke(action, bad, true, "POST", ""); w.Code < 400 {
				t.Fatalf("%s unknown field admitted", action)
			}
		}
		if w := invoke(action, form, true, "POST", "?session_id=elsewhere"); w.Code < 400 {
			t.Fatal("query identity admitted")
		}
		if w := invoke(action, form, false, "POST", ""); w.Code == http.StatusOK {
			t.Fatal("unpaired history action")
		}
		if w := invoke(action, form, true, "GET", ""); w.Code != http.StatusMethodNotAllowed {
			t.Fatal("GET may invoke restore")
		}
		if len(backend.calls) != before {
			t.Fatal("invalid request reached backend")
		}
		w := invoke(action, form, true, "POST", "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
		var scope struct {
			ProjectID  string `json:"project_id"`
			InstanceID string `json:"instance_id"`
			SessionID  string `json:"session_id"`
		}
		if json.Unmarshal(w.Body.Bytes(), &scope) != nil || scope.ProjectID != p.ID || scope.SessionID != "session" {
			t.Fatalf("unbound version response: %s", w.Body.String())
		}
		if action != "version-restore-commit" && backend.snapshot.Messages[0].Text != "Current transcript" {
			t.Fatal("read/prepare replaced active transcript")
		}
	}
	if !slices.Equal(backend.calls, []string{"list:page-one", "preview:target:target-tip:preview-page", "prepare:current:current-tip:target:target-tip", "commit"}) {
		t.Fatalf("fallback or replay: %v", backend.calls)
	}
	s.runtimes = &fakeRuntime{}
	if w := invoke("versions-list", base, true, "POST", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatal("legacy backend exposes versions")
	}
}
