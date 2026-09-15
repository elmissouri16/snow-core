package web

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestProcessControlNeverActivatesRuntime(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	id := "proc_" + strings.Repeat("a", 32)
	if _, err := m.ProcessList(t.Context(), projects[0].ID, "instance", protocol.RPCProcessControlListParams{SessionID: "session"}); err == nil {
		t.Fatal("inactive list accepted")
	}
	if _, err := m.ProcessLogs(t.Context(), projects[0].ID, "instance", protocol.RPCProcessControlLogsParams{SessionID: "session", ProcessID: id}); err == nil {
		t.Fatal("inactive logs accepted")
	}
	if _, err := m.ProcessStop(t.Context(), projects[0].ID, "instance", protocol.RPCProcessControlStopParams{SessionID: "session", ProcessID: id}); err == nil {
		t.Fatal("inactive stop accepted")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("process control activated a worker", err)
	}
}
func TestProcessControlRejectsStaleWithoutWorkerCall(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "")
	snapshot, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ProcessList(t.Context(), projects[0].ID, snapshot.InstanceID, protocol.RPCProcessControlListParams{SessionID: "stale"}); err == nil {
		t.Fatal("stale session accepted")
	}
	if _, err := m.ProcessList(t.Context(), projects[0].ID, "stale", protocol.RPCProcessControlListParams{SessionID: snapshot.SessionID}); err == nil {
		t.Fatal("stale instance accepted")
	}
	if _, err := m.ProcessStop(t.Context(), projects[0].ID, snapshot.InstanceID, protocol.RPCProcessControlStopParams{SessionID: snapshot.SessionID, ProcessID: "1234"}); err == nil {
		t.Fatal("raw PID accepted")
	}
	after, err := os.ReadFile(log)
	if err != nil || string(before) != string(after) {
		t.Fatalf("invalid operation reached worker: %s %v", after, err)
	}
}
func TestProcessLogsAreBoundedPlainText(t *testing.T) {
	p := protocol.RPCProcessControlLogsParams{SessionID: "session", ProcessID: "proc_" + strings.Repeat("a", 32), Cursor: new(int64(10)), MaxBytes: 32}
	good := protocol.RPCProcessControlLogs{SessionID: p.SessionID, ProcessID: p.ProcessID, Status: "running", Output: "hello", NextCursor: 15}
	if !validProcessLogs(good, p) {
		t.Fatal("valid logs rejected")
	}
	for _, mutate := range []func(*protocol.RPCProcessControlLogs){
		func(v *protocol.RPCProcessControlLogs) { v.Output = strings.Repeat("x", 33) },
		func(v *protocol.RPCProcessControlLogs) { v.NextCursor = 9 },
		func(v *protocol.RPCProcessControlLogs) { v.Omitted = -1 },
		func(v *protocol.RPCProcessControlLogs) { v.SessionID = "stale" },
		func(v *protocol.RPCProcessControlLogs) { v.ProcessID = "1234" },
	} {
		bad := good
		mutate(&bad)
		if validProcessLogs(bad, p) {
			t.Fatalf("invalid logs accepted: %+v", bad)
		}
	}
	if got := processPlainText("\x1b[31mred\x1b[0m\x1b]0;PRIVATE TITLE\x07\n<script>untrusted</script>\x00"); got != "red\n<script>untrusted</script>" {
		t.Fatalf("plain logs: %q", got)
	}
}
