package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestManagedSteerRPCExactRootAndNativeItem(t *testing.T) {
	a, p, _ := goalRunRPCApp(t)
	p.block = true
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- a.Agent.Prompt(ctx, "ordinary root") }()
	defer func() { cancel(); <-finished }()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("root not started")
	}
	turn := a.Agent.ActiveTurnSnapshot()
	params := protocol.RPCManagedSteerParams{SessionID: a.Session.ID(), TurnID: turn.ID, RootEpoch: turn.Epoch, RequestID: "submission", Text: "/literal $unexpanded"}
	var output bytes.Buffer
	srv := New(ctx, a, strings.NewReader(""), &output)
	call := func(p protocol.RPCManagedSteerParams) error {
		raw, _ := json.Marshal(p)
		return srv.handle(ctx, Request{ID: "steer-ack", Type: "managed_steer", Params: raw})
	}
	stale := params
	stale.TurnID = "old-root"
	if err := call(stale); !errors.Is(err, agent.ErrManagedSteerStale) {
		t.Fatalf("stale=%v", err)
	}
	if err := call(params); err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Data protocol.RPCManagedSteerResult `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Data.ItemID == "" || ack.Data.Status != "accepted" || ack.Data.RequestID != params.RequestID || ack.Data.TurnID != turn.ID || ack.Data.RootEpoch != turn.Epoch {
		t.Fatalf("ack=%+v", ack)
	}
	snapshot := a.Agent.PendingInputs()
	found := false
	for _, item := range snapshot.Items {
		if item.ID == ack.Data.ItemID && item.Text == params.Text {
			found = true
		}
	}
	if !found {
		t.Fatalf("ACK not native queue item: %+v", snapshot)
	}
}
