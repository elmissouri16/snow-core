package web

import (
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Optional capabilities may still be rejected by older worker fixtures. A
// rejection is unavailable data, never fabricated zero-token accounting.
func (r *liveRuntime) refreshTelemetry() error {
	telemetry := RuntimeTelemetry{}
	if r.supports("usage") {
		var usage protocol.Usage
		err := r.call(protocol.RPCRequest{Type: "usage"}, nil, &usage)
		if err != nil && !errors.Is(err, ErrRuntimeInvalid) {
			return err
		}
		if err == nil {
			telemetry.Available = true
			telemetry.InputTokens = max(0, usage.Input)
			telemetry.OutputTokens = max(0, usage.Output)
			telemetry.TotalTokens = max(0, usage.Total)
			telemetry.Cost = runtimeUsageCost(usage)
		}
	}
	if r.supports("context_report") {
		// Decode only counts: category names/text and private future fields never
		// enter the retained runtime projection.
		var report struct {
			EstimatedInputTokens int             `json:"estimated_input_tokens"`
			ContextWindow        int             `json:"context_window"`
			Usage                *protocol.Usage `json:"usage"`
		}
		err := r.call(protocol.RPCRequest{Type: "context"}, nil, &report)
		if err != nil && !errors.Is(err, ErrRuntimeInvalid) {
			return err
		}
		if err == nil {
			telemetry.ContextAvailable = true
			telemetry.ContextTokens = max(0, report.EstimatedInputTokens)
			telemetry.ContextWindow = max(0, report.ContextWindow)
			telemetry.Estimated = true
			if report.Usage != nil {
				telemetry.ContextTokens = max(0, report.Usage.Input)
				telemetry.Estimated = false
			}
		}
	}
	r.mu.Lock()
	r.snapshot.Telemetry = &telemetry
	r.usageBase = *cloneRuntimeTelemetry(&telemetry)
	r.publishLocked()
	r.mu.Unlock()
	return nil
}

// EvUsage is a cumulative snapshot of ONE provider request, not a delta and
// not a session aggregate. Only EvTurnDone's complete turn aggregate advances
// totals; usage updates during streaming update current context counts alone.
func (r *liveRuntime) projectUsage(e protocol.AgentEvent) {
	if e.Usage == nil {
		return
	}
	if r.snapshot.Telemetry == nil {
		r.snapshot.Telemetry = &RuntimeTelemetry{}
	}
	telemetry := r.snapshot.Telemetry
	if e.Type == protocol.EvUsage {
		telemetry.ContextTokens = max(0, e.Usage.Input)
		telemetry.ContextAvailable = true
		telemetry.Estimated = false
	} else if e.Type == protocol.EvTurnDone && r.usageBase.Available {
		telemetry.InputTokens = r.usageBase.InputTokens + max(0, e.Usage.Input)
		telemetry.OutputTokens = r.usageBase.OutputTokens + max(0, e.Usage.Output)
		telemetry.TotalTokens = r.usageBase.TotalTokens + max(0, e.Usage.Total)
		telemetry.Cost = addRuntimeCost(r.usageBase.Cost, *e.Usage)
	}
	r.publishLocked()
}
