package rpc

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestQueueControlRPCAndSchemaContract(t *testing.T) {
	a := editRPCApp(t)
	p := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	if err := a.Agent.SetProvider(p); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(t.Context(), "initial") }()
	<-p.started
	defer func() { a.Agent.Abort(); <-done }()
	_, turn, _ := a.Agent.ActiveTurn()
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	call := func(command string, params any) protocol.QueueControl {
		t.Helper()
		output.Reset()
		wire, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		req := Request{ID: "queue", Type: command, Params: wire}
		requestWire, _ := json.Marshal(req)
		if err := resolveWireSchema(t, "request.schema.json").Validate(decodedJSON(t, requestWire)); err != nil {
			t.Fatal(err)
		}
		if err := srv.handle(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Success bool                  `json:"success"`
			Data    protocol.QueueControl `json:"data"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Success {
			t.Fatal("queue mutation not acknowledged")
		}
		if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, bytes.TrimSpace(output.Bytes()))); err != nil {
			t.Fatal(err)
		}
		event, _ := json.Marshal(protocol.AgentEvent{Type: protocol.EvQueueUpdated, QueueControl: &response.Data})
		if err := resolveWireSchema(t, "agent-event.schema.json").Validate(decodedJSON(t, event)); err != nil {
			t.Fatal(err)
		}
		return response.Data
	}
	q := call("queue_list", protocol.RPCQueueListParams{SessionID: a.Session.ID(), TurnID: turn})
	q = call("queue_enqueue", protocol.RPCQueueEnqueueParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, Text: "next"})
	if len(q.Items) != 1 || q.Items[0].State != "pending" {
		t.Fatalf("queue=%+v", q)
	}
	q = call("queue_update", protocol.RPCQueueUpdateParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.Items[0].ID, Text: "edited"})
	if q.Items[0].Text != "edited" {
		t.Fatal("update not atomic")
	}
	q = call("queue_remove", protocol.RPCQueueRemoveParams{SessionID: q.SessionID, TurnID: q.TurnID, Revision: q.Revision, ItemID: q.Items[0].ID})
	if len(q.Items) != 0 || q.Change.Kind != "removed" {
		t.Fatal("remove inferred as delivery")
	}
	if !slices.Contains(protocol.KnownRPCCapabilities(), "queue_next") {
		t.Fatal("missing capability")
	}
	for err, code := range map[error]string{agent.ErrQueueRejected: "queue_rejected", agent.ErrQueueStale: "queue_stale", agent.ErrQueueUnknown: "queue_unknown"} {
		if rpcErrorCode(errors.Join(errors.New("outer"), err)) != code {
			t.Fatal("queue error classification lost")
		}
	}
}

func TestQueueControlStrictWireAndSchemaRejections(t *testing.T) {
	a := editRPCApp(t)
	for _, frame := range []string{
		`{"type":"queue_enqueue","params":{"session_id":"s","turn_id":"t","text":"x"}}`,
		`{"type":"queue_enqueue","params":{"session_id":"s","turn_id":"t","revision":null,"text":"x"}}`,
		`{"type":"queue_enqueue","params":{"session_id":"s","turn_id":"t","revision":-1,"text":"x"}}`,
		`{"type":"queue_enqueue","params":{"session_id":"s","turn_id":"t","revision":0,"text":"x","kind":"steer"}}`,
		`{"type":"queue_update","params":{"session_id":"s","turn_id":"t","revision":0,"text":"x"}}`,
		`{"type":"queue_remove","params":{"session_id":"s","turn_id":"t","revision":0,"item_id":"i","text":"x"}}`,
		`{"type":"queue_list","params":{"session_id":"s","turn_id":"t","revision":0}}`,
		`{"type":"queue_list","message":"","params":{"session_id":"s","turn_id":"t"}}`,
	} {
		t.Run(frame, func(t *testing.T) {
			if err := resolveWireSchema(t, "request.schema.json").Validate(decodedJSON(t, []byte(frame))); err == nil {
				t.Fatal("invalid schema accepted")
			}
			var output bytes.Buffer
			srv := New(t.Context(), a, strings.NewReader(frame+"\n"), &output)
			if err := srv.Serve(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(output.Bytes(), []byte(`"error_code":"queue_rejected"`)) {
				t.Fatalf("unsafe rejection=%s", output.Bytes())
			}
		})
	}
	for _, frame := range []string{
		`{"type":"queue_enqueue","params":{"session_id":"s","turn_id":"t","revision":0,"revision":1,"text":"x"}}`,
		`{"type":"queue_list","params":{"session_id":"s","turn_id":"t"},"params":{"session_id":"s","turn_id":"t"}}`,
	} {
		var output bytes.Buffer
		srv := New(t.Context(), a, strings.NewReader(frame+"\n"), &output)
		if err := srv.Serve(t.Context()); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(output.Bytes(), []byte(`"success":false`)) {
			t.Fatal("duplicate field accepted")
		}
	}
}
