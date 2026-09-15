//go:build darwin || linux

package main

import (
	"encoding/json/v2"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (f *managerExecutionHTTP) process(action string, s web.RuntimeSnapshot, id string, extra url.Values, out any) {
	f.t.Helper()
	v := url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}}
	if id != "" {
		v.Set("process_id", id)
	}
	for key, values := range extra {
		v[key] = values
	}
	code, body := f.request("POST", "/projects/"+f.project+"/processes/"+action, v)
	if code != 200 {
		f.t.Fatalf("process %s: %d %s", action, code, body)
	}
	if strings.Contains(string(body), `"pid"`) || strings.Contains(string(body), `"command"`) || strings.Contains(string(body), `"env"`) {
		f.t.Fatal("public process control leaked execution internals")
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			f.t.Fatal(err)
		}
	}
}
func (f *managerExecutionHTTP) processStart(s web.RuntimeSnapshot) (web.RuntimeSnapshot, string) {
	f.t.Helper()
	call := f.count() + 1
	f.runtime("prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"Start one fictional managed worker in this isolated fixture project"}}, nil)
	f.release(call, "process")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "permission" || f.count() == call+1 })
	if s.PermissionMode == "ask" {
		if s.Permission == nil || s.Permission.Tool != "process_start" {
			f.t.Fatal("Ask launch bypassed approval")
		}
		var inventory struct {
			Result protocol.RPCProcessControlList `json:"result"`
		}
		f.process("list", s, "", nil, &inventory)
		if len(inventory.Result.Processes) != 0 {
			f.t.Fatal("process exists before approval")
		}
		f.runtime("permission", url.Values{"instance_id": {s.InstanceID}, "request_id": {s.Permission.ID}, "decision": {"allow"}}, nil)
	}
	f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == call+1 })
	f.release(call+1, "fictional process started")
	s = f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	var inventory struct {
		Result protocol.RPCProcessControlList `json:"result"`
	}
	f.process("list", s, "", nil, &inventory)
	var id string
	for _, p := range inventory.Result.Processes {
		if p.Status == "running" {
			if id != "" {
				f.t.Fatal("duplicate running fixture process")
			}
			id = p.ProcessID
		}
	}
	if !protocol.ValidManagedProcessID(id) {
		f.t.Fatalf("missing opaque current handle: %+v", inventory.Result)
	}
	var logs struct {
		Result protocol.RPCProcessControlLogs `json:"result"`
	}
	f.process("logs", s, id, url.Values{"max_bytes": {"4096"}}, &logs)
	if !strings.Contains(logs.Result.Output, managerExecutionMarker) {
		f.t.Fatalf("real readiness marker absent: %q", logs.Result.Output)
	}
	return s, id
}
func (f *managerExecutionHTTP) rejectStop(s web.RuntimeSnapshot, id string, extra url.Values) {
	f.t.Helper()
	v := url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "process_id": {id}}
	for key, values := range extra {
		v[key] = values
	}
	code, body := f.request("POST", "/projects/"+f.project+"/processes/stop", v)
	if code < 400 {
		f.t.Fatalf("unauthorized process stop: %s", body)
	}
	var inventory struct {
		Result protocol.RPCProcessControlList `json:"result"`
	}
	f.process("list", s, "", nil, &inventory)
	for _, p := range inventory.Result.Processes {
		if p.ProcessID == id && p.Status != "running" {
			f.t.Fatal("rejected stop had a process effect")
		}
	}
}
func TestWebManagerManagedProcessRealWorker(t *testing.T) {
	f := newManagerExecutionHTTP(t, "")
	s := f.open("")
	if s.PermissionMode != "ask" {
		t.Fatal("fresh session did not preserve Ask")
	}
	s, id := f.processStart(s)
	// Direct browser Stop cannot manufacture an approval. It needs current
	// Permission Allow and must still pass authoritative collaboration policy.
	f.rejectStop(s, id, nil)
	s = f.permissionMode(s, "deny")
	f.rejectStop(s, id, nil)
	s = f.permissionMode(s, "allow")
	s = f.mode(s, "plan")
	f.rejectStop(s, id, nil)
	s = f.mode(s, "default")
	for _, extra := range []url.Values{{"process_id": {"123"}}, {"pid": {"123"}}, {"command": {"fictional"}}, {"grace_ms": {"-1"}}, {"session_id": {"stale-fictional-session"}}, {"instance_id": {"stale-fictional-instance"}}} {
		f.rejectStop(s, id, extra)
	}
	var stopped struct {
		Result protocol.RPCProcessControlStop `json:"result"`
	}
	f.process("stop", s, id, url.Values{"grace_ms": {"0"}}, &stopped)
	if stopped.Result.Process.ProcessID != id || stopped.Result.Process.Status == "running" || stopped.Result.Process.FinishedAt == 0 {
		t.Fatalf("not stopped/reaped: %+v", stopped.Result)
	}
	var logs struct {
		Result protocol.RPCProcessControlLogs `json:"result"`
	}
	f.process("logs", s, id, nil, &logs)
	if !logs.Result.EOF {
		t.Fatal("stopped process output has not reached EOF")
	}
}

func TestWebManagerManagedProcessSwitchAndClose(t *testing.T) {
	for _, action := range []string{"switch", "close"} {
		t.Run(action, func(t *testing.T) {
			f := newManagerExecutionHTTP(t, "")
			s := f.open("")
			s, id := f.processStart(s)
			old := s
			if action == "switch" {
				f.runtime("switch", url.Values{"instance_id": {s.InstanceID}, "session_id": {""}, "confirm_stop": {"stop"}}, &s)
				if s.InstanceID == old.InstanceID || s.SessionID == old.SessionID {
					t.Fatal("switch did not rotate session authority")
				}
			} else {
				f.runtime("close", url.Values{"instance_id": {s.InstanceID}}, nil)
				s = f.open(old.SessionID)
			}
			if marker, err := os.ReadFile(filepath.Join(f.directory, "project-a", "fictional-process-stopped")); err != nil || string(marker) != "stopped" {
				t.Fatalf("%s did not terminate its real process group: %q %v", action, marker, err)
			}
			var inventory struct {
				Result protocol.RPCProcessControlList `json:"result"`
			}
			f.process("list", s, "", nil, &inventory)
			if len(inventory.Result.Processes) != 0 {
				t.Fatalf("old processes survived %s: %+v", action, inventory.Result)
			}
			v := url.Values{"instance_id": {old.InstanceID}, "session_id": {old.SessionID}, "process_id": {id}}
			code, _ := f.request("POST", "/projects/"+f.project+"/processes/stop", v)
			if code < 400 {
				t.Fatal("retired process authority accepted")
			}
		})
	}
}
