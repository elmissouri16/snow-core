package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

type compactionHTTPRuntime struct {
	fakeRuntime
	input RuntimeCompactionInput
}

func (f *compactionHTTPRuntime) StartCompaction(_ context.Context, project, instance string, input RuntimeCompactionInput) (RuntimeSnapshot, error) {
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID || input.SessionID != f.snapshot.SessionID || input.BranchID != "main" {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	f.calls = append(f.calls, "compaction-start")
	f.input = input
	result := f.snapshot.clone()
	result.CompactionACK = new(compactionAccepted())
	return result, nil
}
func TestCompactionHTTPExactAuthorityAndNoReplay(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := "/projects/" + p.ID + "/runtime/"
	backend := &compactionHTTPRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance", SessionID: "session", Status: "idle", Revision: 9}}}
	s.runtimes = backend
	form := url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {"instance"}, "session_id": {"session"}, "branch_id": {"main"}, "expected_tip_id": {""}, "expected_revision": {"7"}}
	for key := range form {
		missing := form.Clone()
		missing.Del(key)
		if w := request(t, s, "POST", base+"compaction-start", missing, cookie); w.Code < 400 {
			t.Fatalf("missing %s admitted", key)
		}
		duplicate := form.Clone()
		duplicate.Add(key, duplicate.Get(key))
		if w := request(t, s, "POST", base+"compaction-start", duplicate, cookie); w.Code < 400 {
			t.Fatalf("duplicate %s admitted", key)
		}
		if w := request(t, s, "POST", base+"compaction-start?"+key+"=foreign", form, cookie); w.Code < 400 {
			t.Fatalf("query shadow %s admitted", key)
		}
	}
	for _, key := range []string{"command", "summary", "prompt", "provider", "model", "mode", "permission_mode", "tool", "force", "action"} {
		bad := form.Clone()
		bad.Set(key, "untrusted")
		if w := request(t, s, "POST", base+"compaction-start", bad, cookie); w.Code < 400 {
			t.Fatalf("untyped %s admitted", key)
		}
	}
	for _, revision := range []string{"", "0", "-1", "+7", "07", "18446744073709551616"} {
		bad := form.Clone()
		bad.Set("expected_revision", revision)
		if w := request(t, s, "POST", base+"compaction-start", bad, cookie); w.Code < 400 {
			t.Fatalf("invalid revision %q admitted", revision)
		}
	}
	if w := request(t, s, "POST", base+"compaction-start", form); w.Code == http.StatusOK {
		t.Fatal("unpaired admission")
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid authority reached backend: %v", backend.calls)
	}
	if w := request(t, s, "POST", base+"compaction-start", form, cookie); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if backend.input.ExpectedRevision != 7 || backend.input.ExpectedTipID != "" {
		t.Fatal("empty tip/revision authority changed")
	}
	for range 2 {
		request(t, s, "GET", base[:len(base)-1], nil, cookie)
	}
	if len(backend.calls) != 1 {
		t.Fatal("read-only reconciliation replayed operation")
	}
}
