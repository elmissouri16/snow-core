package web

import (
	"encoding/json/v2"
	"strings"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const messageEditEventCount = 512
const messageEditEventBytes = 512 << 10

// During the RPC commit ACK race retain only the existing public projection's
// allowlisted inputs, never raw events/provider continuity/arguments. Overflow
// closes admission rather than silently losing a fast replacement response.
// Caller holds eventMu and mu; returns (consumed, valid).
func (r *liveRuntime) bufferMessageEditEventLocked(event clientrpc.Event) (bool, bool) {
	if !r.messageEdit.pending {
		return false, true
	}
	public, keep, valid := publicMessageEditEvent(event)
	if !valid {
		return true, false
	}
	if !keep {
		return true, true
	}
	data, err := json.Marshal(public)
	if err != nil || len(r.messageEdit.events) >= messageEditEventCount || len(data) > messageEditEventBytes-r.messageEdit.bytes {
		return true, false
	}
	// Decode the bounded allowlist into owned storage: clipped public strings
	// must not retain backing allocations from a much larger private event.
	var owned clientrpc.Event
	if json.Unmarshal(data, &owned) != nil {
		return true, false
	}
	r.messageEdit.events = append(r.messageEdit.events, owned)
	r.messageEdit.bytes += len(data)
	return true, true
}

func publicMessageEditEvent(event clientrpc.Event) (clientrpc.Event, bool, bool) {
	if c := event.PromptCompleted; c != nil {
		if len(c.RequestID) > 128 {
			return clientrpc.Event{}, false, false
		}
		return clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{Type: c.Type, RequestID: strings.Clone(c.RequestID), Status: c.Status}}, true, true
	}
	e := event.AgentEvent
	if e == nil {
		return clientrpc.Event{}, false, true
	}
	if ref := e.Agent; ref != nil && (ref.Path != protocol.RootAgentPath || ref.Depth != 0 || ref.ParentPath != "" || ref.ParentThreadID != "" || ref.Validate() != nil) {
		return clientrpc.Event{}, false, true
	}
	if len(e.TurnID) > 256 {
		return clientrpc.Event{}, false, false
	}
	p := protocol.AgentEvent{Type: e.Type, TurnID: strings.Clone(e.TurnID), RootEpoch: e.RootEpoch, TurnSequence: e.TurnSequence}
	switch e.Type {
	case protocol.EvQueueUpdated:
		if !validQueueControl(e.QueueControl) {
			return clientrpc.Event{}, false, false
		}
		p.QueueControl = e.QueueControl.Clone()
	case protocol.EvTextDelta:
		if len(e.Text) > runtimeMessageBytes {
			return clientrpc.Event{}, false, false
		}
		p.Text = strings.Clone(e.Text)
	case protocol.EvPlanStarted, protocol.EvPlanDelta, protocol.EvPlanCompleted:
		if len(e.Text) > runtimeMessageBytes || e.Plan != nil && (len(e.Plan.Text) > runtimeMessageBytes || len(e.Plan.ID) > 256) {
			return clientrpc.Event{}, false, false
		}
		p.Text = strings.Clone(e.Text)
		if e.Plan != nil {
			p.Plan = &protocol.PlanItem{ID: strings.Clone(e.Plan.ID), Text: strings.Clone(e.Plan.Text)}
		}
	case protocol.EvUsage, protocol.EvTurnDone:
		if e.Usage != nil {
			p.Usage = &protocol.Usage{Input: e.Usage.Input, Output: e.Usage.Output, Total: e.Usage.Total, CostCurrencyConflict: e.Usage.CostCurrencyConflict}
			// Retain only the cost fields used by public telemetry, after the
			// same bounded validation as ordinary events. Never retain an
			// arbitrary currency string or reintroduce a conflicted amount.
			if e.Usage.Cost != nil && !e.Usage.CostCurrencyConflict {
				cost := runtimeCost(e.Usage.Cost)
				if !cost.Known {
					return clientrpc.Event{}, false, false
				}
				p.Usage.Cost = &protocol.Cost{Currency: strings.Clone(cost.Currency), Total: cost.Total}
			}
		}
	case protocol.EvModeChanged:
		if e.Mode != nil {
			p.Mode = new(*e.Mode)
		}
	case protocol.EvToolStart, protocol.EvToolEnd:
		if len(e.ToolCallID) > 256 {
			return clientrpc.Event{}, false, false
		}
		p.ToolCallID = strings.Clone(e.ToolCallID)
		p.ToolName = strings.Clone(runtimeText(e.ToolName, 129))
		p.IsError = e.IsError
		if e.ToolResult != nil {
			p.ToolResult = &protocol.ToolResultPreview{Text: strings.Clone(runtimeText(e.ToolResult.Text, runtimeActivityOutputBytes)), Truncated: e.ToolResult.Truncated || len(e.ToolResult.Text) > runtimeActivityOutputBytes}
		}
	case protocol.EvPermissionRequest:
		if e.Permission == nil {
			return clientrpc.Event{}, false, false
		}
		v, ok := projectPermission(e.Permission.Request)
		if !ok {
			return clientrpc.Event{}, false, false
		}
		p.Permission = &protocol.Permission{Request: protocol.PermissionRequest{ID: v.ID, Tool: v.Tool, Risk: v.Risk, Reason: v.Reason, ScopeLabel: v.ScopeLabel, Paths: v.Paths, Capabilities: v.Capabilities, Effects: v.Effects, Unknown: v.Unknown, PathsTruncated: v.Truncated}}
	case protocol.EvUserInputRequest:
		input, ok := projectInput(e.UserInput)
		if !ok {
			return clientrpc.Event{}, false, false
		}
		p.UserInput = input
	case protocol.EvAborted:
	default:
		return clientrpc.Event{}, false, true
	}
	return clientrpc.Event{AgentEvent: &p}, true, true
}
