package rpc

import (
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func projectCompletionFixture() protocol.RPCProjectCompleted {
	return protocol.RPCProjectCompleted{Type: protocol.RPCTypeProjectCompleted, RequestID: "request", OperationID: "operation", Child: protocol.HostDirectoryIdentity{Path: "/host/project", Device: "1", Inode: "2"}, Status: protocol.HostOperationSucceeded}
}

func TestProjectCompletionTypedStatuses(t *testing.T) {
	for _, status := range []protocol.HostOperationStatus{protocol.HostOperationSucceeded, protocol.HostOperationFailed, protocol.HostOperationCanceled, protocol.HostOperationTimedOut, protocol.HostOperationOutputLimit, protocol.HostOperationCleanupFailed} {
		t.Run(string(status), func(t *testing.T) {
			f := newFixture(t, Options{})
			want := projectCompletionFixture()
			want.Status = status
			writeJSON(t, f.peer, want)
			got := receive(t, f.client.Events())
			if got.ProjectCompleted == nil || *got.ProjectCompleted != want || got.AgentEvent != nil || got.CompactionCompleted != nil || got.PromptCompleted != nil || got.GoalRunCompleted != nil {
				t.Fatalf("completion=%+v", got)
			}
		})
	}
}

func TestProjectCompletionRejectsMalformedAndPrivateFrames(t *testing.T) {
	wire, _ := json.Marshal(projectCompletionFixture())
	for name, mutate := range map[string]func(string) string{
		"missing request":   func(s string) string { return strings.Replace(s, `"request_id":"request",`, "", 1) },
		"missing operation": func(s string) string { return strings.Replace(s, `"operation_id":"operation",`, "", 1) },
		"bad status":        func(s string) string { return strings.Replace(s, `"status":"succeeded"`, `"status":"unknown"`, 1) },
		"bad path":          func(s string) string { return strings.Replace(s, `"path":"/host/project"`, `"path":"relative"`, 1) },
		"bad inode":         func(s string) string { return strings.Replace(s, `"inode":"2"`, `"inode":"+2"`, 1) },
		"bad device":        func(s string) string { return strings.Replace(s, `"device":"1"`, `"device":null`, 1) },
		"duplicate": func(s string) string {
			return strings.Replace(s, `"operation_id":"operation"`, `"operation_id":"operation","operation_id":"foreign"`, 1)
		},
		"private error":         func(s string) string { return strings.TrimSuffix(s, "}") + `,"error":"PRIVATE"}` },
		"private nested output": func(s string) string { return strings.Replace(s, `"inode":"2"`, `"inode":"2","output":"PRIVATE"`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, Options{})
			_, _ = f.peer.Write([]byte(mutate(string(wire)) + "\n"))
			awaitFailure(t, f.client, ErrMalformedFrame)
		})
	}
}

func TestProjectFastCompletionBeforeCallReturns(t *testing.T) {
	f := newFixture(t, Options{})
	result := f.call(f.ctx, protocol.RPCRequest{Type: "project_clone_start"})
	req := f.request(t)
	want := projectCompletionFixture()
	want.RequestID = req.ID
	writeJSON(t, f.peer, want)
	got := receive(t, f.client.Events())
	if got.ProjectCompleted == nil || *got.ProjectCompleted != want {
		t.Fatalf("lost fast completion: %+v", got)
	}
	select {
	case <-result:
		t.Fatal("Call returned before ACK")
	default:
	}
	f.reply(t, req, true)
	ack := receive(t, result)
	if ack.err != nil || !ack.response.Success || ack.response.ID != got.ProjectCompleted.RequestID {
		t.Fatalf("ack=%+v", ack)
	}
}
