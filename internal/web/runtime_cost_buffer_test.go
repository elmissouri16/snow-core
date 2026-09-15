package web

import (
	"encoding/json/v2"
	"math"
	"strings"
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimeCostBufferedEventsPreserveSafeProjection(t *testing.T) {
	for _, eventType := range []protocol.AgentEventType{protocol.EvUsage, protocol.EvTurnDone} {
		for _, conflict := range []bool{false, true} {
			usage := &protocol.Usage{Input: 10, Output: 2, Total: 12, CostCurrencyConflict: conflict, Cost: &protocol.Cost{Currency: "USD", Total: 1.25}}
			event := clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: eventType, TurnID: "turn", Usage: usage, Message: "PRIVATE-EVENT"}}
			r := &liveRuntime{}
			r.messageEdit.pending = true
			consumed, valid := r.bufferMessageEditEventLocked(event)
			if !consumed || !valid || len(r.messageEdit.events) != 1 {
				t.Fatalf("buffer rejected bounded cost: consumed=%v valid=%v", consumed, valid)
			}
			got := r.messageEdit.events[0].AgentEvent.Usage
			if got == nil || got.Input != 10 || got.Total != 12 || got.CostCurrencyConflict != conflict {
				t.Fatalf("buffered usage changed: %+v", got)
			}
			if conflict && got.Cost != nil || !conflict && (got.Cost == nil || got.Cost.Currency != "USD" || got.Cost.Total != 1.25) {
				t.Fatalf("buffered cost changed: %+v", got)
			}
			usage.Cost.Total = 99
			if !conflict && got.Cost.Total != 1.25 {
				t.Fatal("buffered cost aliases source")
			}
			data, err := json.Marshal(r.messageEdit.events)
			if err != nil || strings.Contains(string(data), "PRIVATE") || len(data) > 1024 {
				t.Fatalf("unbounded/private buffer: bytes=%d err=%v", len(data), err)
			}
			if eventType == protocol.EvTurnDone {
				r.usageBase = RuntimeTelemetry{Available: true, Cost: runtimeCost(&protocol.Cost{Currency: "USD", Total: 1})}
				r.snapshot.Telemetry = cloneRuntimeTelemetry(&r.usageBase)
				r.projectUsage(*r.messageEdit.events[0].AgentEvent)
				cost := r.snapshot.Telemetry.Cost
				if conflict && (cost == nil || cost.Known) || !conflict && (cost == nil || !cost.Known || cost.Total != 2.25) {
					t.Fatalf("buffer replay cost: %+v", cost)
				}
			}
		}
	}
}

func TestRuntimeCostBufferedInvalidCostDoesNotEnterPublicBuffer(t *testing.T) {
	for _, cost := range []*protocol.Cost{
		{Currency: strings.Repeat("SECRET", 100000), Total: 1},
		{Currency: "USD", Total: math.NaN()},
		{Currency: "USD", Total: math.Inf(1)},
		{Currency: "USD", Total: -1},
		{Currency: "USD", Input: -1, Total: 1},
	} {
		r := &liveRuntime{}
		r.messageEdit.pending = true
		event := clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: &protocol.Usage{Cost: cost}}}
		consumed, valid := r.bufferMessageEditEventLocked(event)
		if !consumed || valid || len(r.messageEdit.events) != 0 {
			t.Fatal("invalid cost retained in edit ACK buffer")
		}
		// Conflict dominates even a malformed accompanying amount: retain only
		// its boolean so replay cannot restore an old or newly priced subtotal.
		event.AgentEvent.Usage.CostCurrencyConflict = true
		consumed, valid = r.bufferMessageEditEventLocked(event)
		if !consumed || !valid || len(r.messageEdit.events) != 1 {
			t.Fatal("conflict marker lost with discarded invalid cost")
		}
		got := r.messageEdit.events[0].AgentEvent.Usage
		if !got.CostCurrencyConflict || got.Cost != nil {
			t.Fatalf("invalid conflicted cost retained: %+v", got)
		}
	}
}
