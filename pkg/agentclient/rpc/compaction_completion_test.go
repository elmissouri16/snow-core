package rpc

import (
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func compactionCompletionFixture() protocol.RPCCompactionCompleted {
	return protocol.RPCCompactionCompleted{Type: protocol.RPCTypeCompactionCompleted, RequestID: "request", CompactionID: "compact-run", SessionID: "session", BranchID: "main", TurnID: "compact-run", TurnOrigin: "compact", RootEpoch: 1, TurnSequence: 2, Status: "completed", SummarizedMessages: 5, RetainedMessages: 2}
}

func TestCompactionCompletionTypedStatusesAndNativeProgress(t *testing.T) {
	for _, status := range []string{"completed", "noop", "fallback", "canceled", "failed"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, Options{})
			writeJSON(t, f.peer, protocol.AgentEvent{Type: protocol.EvCompactionDone})
			if got := receive(t, f.client.Events()); got.AgentEvent == nil || got.CompactionCompleted != nil {
				t.Fatalf("progress conflated: %+v", got)
			}
			want := compactionCompletionFixture()
			want.Status = status
			writeJSON(t, f.peer, want)
			got := receive(t, f.client.Events())
			if got.CompactionCompleted == nil || *got.CompactionCompleted != want || got.AgentEvent != nil || got.GoalRunCompleted != nil || got.PromptCompleted != nil {
				t.Fatalf("completion=%+v", got)
			}
		})
	}
}

func TestCompactionCompletionMalformedFramesFailClosed(t *testing.T) {
	wire, _ := json.Marshal(compactionCompletionFixture())
	for name, change := range map[string]func(string) string{
		"missing request":    func(s string) string { return strings.Replace(s, `"request_id":"request",`, "", 1) },
		"missing branch":     func(s string) string { return strings.Replace(s, `"branch_id":"main",`, "", 1) },
		"foreign turn":       func(s string) string { return strings.Replace(s, `"turn_id":"compact-run"`, `"turn_id":"foreign"`, 1) },
		"bad origin":         func(s string) string { return strings.Replace(s, `"turn_origin":"compact"`, `"turn_origin":"user"`, 1) },
		"bad status":         func(s string) string { return strings.Replace(s, `"status":"completed"`, `"status":"finished"`, 1) },
		"negative count":     func(s string) string { return strings.Replace(s, `"retained_messages":2`, `"retained_messages":-1`, 1) },
		"duplicate":          func(s string) string { return strings.Replace(s, `"root_epoch":1`, `"root_epoch":1,"root_epoch":2`, 1) },
		"private summary":    func(s string) string { return strings.TrimSuffix(s, "}") + `,"summary":"PRIVATE"}` },
		"raw provider error": func(s string) string { return strings.TrimSuffix(s, "}") + `,"error":"PRIVATE"}` },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, Options{})
			_, _ = f.peer.Write([]byte(change(string(wire)) + "\n"))
			awaitFailure(t, f.client, ErrMalformedFrame)
		})
	}
}

func TestCompactionFastCompletionBeforeCallReturns(t *testing.T) {
	f := newFixture(t, Options{})
	result := f.call(f.ctx, protocol.RPCRequest{Type: "compaction_start"})
	req := f.request(t)
	// Deliberately deliver the terminal before the response to establish that the
	// event reader never depends on Call's ACK handling or pending-slot removal.
	want := compactionCompletionFixture()
	want.RequestID = req.ID
	writeJSON(t, f.peer, want)
	got := receive(t, f.client.Events())
	if got.CompactionCompleted == nil || *got.CompactionCompleted != want {
		t.Fatalf("lost fast completion: %+v", got)
	}
	select {
	case <-result:
		t.Fatal("Call returned without its ACK")
	default:
	}
	f.reply(t, req, true)
	ack := receive(t, result)
	if ack.err != nil || !ack.response.Success || ack.response.ID != got.CompactionCompleted.RequestID {
		t.Fatalf("ack=%+v", ack)
	}
}
