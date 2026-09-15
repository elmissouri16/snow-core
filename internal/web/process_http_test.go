package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type fakeProcessBackend struct {
	fakeRuntime
	rotate   bool
	failStop bool
}

var processTestID = "proc_" + strings.Repeat("a", 32)

func (f *fakeProcessBackend) ProcessList(_ context.Context, project, instance string, p protocol.RPCProcessControlListParams) (protocol.RPCProcessControlList, error) {
	f.calls = append(f.calls, "list")
	if f.rotate {
		f.snapshot.InstanceID = "replacement"
	}
	return protocol.RPCProcessControlList{SessionID: p.SessionID, Processes: []protocol.RPCManagedProcess{{ProcessID: processTestID, Name: "fixture", Status: "running"}}}, nil
}
func (f *fakeProcessBackend) ProcessLogs(_ context.Context, project, instance string, p protocol.RPCProcessControlLogsParams) (protocol.RPCProcessControlLogs, error) {
	f.calls = append(f.calls, "logs")
	return protocol.RPCProcessControlLogs{SessionID: p.SessionID, ProcessID: p.ProcessID, Status: "running", Output: "\x1b[31mpublic\x1b[0m", NextCursor: 20}, nil
}
func (f *fakeProcessBackend) ProcessStop(_ context.Context, project, instance string, p protocol.RPCProcessControlStopParams) (protocol.RPCProcessControlStop, error) {
	f.calls = append(f.calls, "stop")
	if f.failStop {
		return protocol.RPCProcessControlStop{}, ErrRuntimeUnavailable
	}
	return protocol.RPCProcessControlStop{SessionID: p.SessionID, Process: protocol.RPCManagedProcess{ProcessID: p.ProcessID, Name: "fixture", Status: "stopped"}}, nil
}

func processHTTPRequest(s *shell, cookie *http.Cookie, method, path string, values url.Values, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, testOrigin+path, strings.NewReader(values.Encode()))
	req.URL.Scheme, req.URL.Host = "", ""
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", origin)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	out := httptest.NewRecorder()
	s.handler().ServeHTTP(out, req)
	return out
}
func TestProcessHTTPStrictSessionBoundCapability(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "processes", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProcessBackend{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "session", Status: "idle"}}}
	s.runtimes = f
	base := "/projects/" + project.ID + "/processes/"
	valid := func() url.Values {
		return url.Values{"csrf": {csrfFor(t, s, cookie)}, "instance_id": {"instance"}, "session_id": {"session"}}
	}
	for _, tc := range []struct {
		name, action string
		change       func(url.Values)
		want         int
	}{
		{"csrf", "list", func(v url.Values) { v.Del("csrf") }, 403},
		{"duplicate", "list", func(v url.Values) { v.Add("session_id", "other") }, 400},
		{"stale session", "list", func(v url.Values) { v.Set("session_id", "other") }, 409},
		{"stale instance", "list", func(v url.Values) { v.Set("instance_id", "other") }, 409},
		{"command", "list", func(v url.Values) { v.Set("command", "secret") }, 400},
		{"launch", "start", func(v url.Values) {}, 404},
		{"raw pid", "stop", func(v url.Values) { v.Set("process_id", "1234") }, 409},
		{"max logs", "logs", func(v url.Values) { v.Set("process_id", processTestID); v.Set("max_bytes", "32769") }, 409},
		{"cursor", "logs", func(v url.Values) { v.Set("process_id", processTestID); v.Set("cursor", "-1") }, 409},
		{"grace", "stop", func(v url.Values) { v.Set("process_id", processTestID); v.Set("grace_ms", "5001") }, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := valid()
			tc.change(v)
			w := processHTTPRequest(s, cookie, "POST", base+tc.action, v, testOrigin)
			if w.Code != tc.want {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
		})
	}
	if len(f.calls) != 0 {
		t.Fatalf("invalid requests reached backend: %v", f.calls)
	}
	if w := processHTTPRequest(s, cookie, "POST", base+"list", valid(), "https://other.invalid"); w.Code != 403 {
		t.Fatalf("cross origin=%d", w.Code)
	}
	if w := processHTTPRequest(s, nil, "POST", base+"list", valid(), testOrigin); w.Code != 303 {
		t.Fatalf("unpaired=%d", w.Code)
	}
	if w := processHTTPRequest(s, cookie, "GET", base+"stop", valid(), testOrigin); w.Code != 405 {
		t.Fatalf("GET mutation=%d", w.Code)
	}
	for _, action := range []string{"list", "logs", "stop"} {
		v := valid()
		if action != "list" {
			v.Set("process_id", processTestID)
		}
		w := processHTTPRequest(s, cookie, "POST", base+action, v, testOrigin)
		if w.Code != 200 || strings.Contains(w.Body.String(), `\u001b`) || !strings.Contains(w.Body.String(), `"session_id":"session"`) {
			t.Fatalf("%s=%d %s", action, w.Code, w.Body.String())
		}
	}
	f.rotate = true
	if w := processHTTPRequest(s, cookie, "POST", base+"list", valid(), testOrigin); w.Code != 409 {
		t.Fatal("acknowledged replacement session")
	}
	f.rotate = false
	f.snapshot.InstanceID = "instance"
	f.failStop = true
	before := len(f.calls)
	v := valid()
	v.Set("process_id", processTestID)
	if w := processHTTPRequest(s, cookie, "POST", base+"stop", v, testOrigin); w.Code != 409 {
		t.Fatal("acknowledged lost stop")
	}
	if len(f.calls) != before+1 {
		t.Fatal("stop automatically retried")
	}
	s.runtimes = &fakeRuntime{}
	if w := processHTTPRequest(s, cookie, "POST", base+"list", valid(), testOrigin); w.Code != 503 {
		t.Fatal("missing optional capability did not fail closed")
	}
}
