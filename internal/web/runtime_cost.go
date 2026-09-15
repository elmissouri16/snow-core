package web

import (
	"math"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeCost is a bounded public estimate of known priced usage, not a bill or
// a claim that every request was priced. Usage.Add preserves priced subtotals
// when other requests are unpriced, so the worker cannot establish completeness.
// Known distinguishes a genuine zero estimate from unavailable cost. An unknown
// non-nil value also retains a currency conflict until authoritative refresh.
type RuntimeCost struct {
	Known    bool    `json:"known"`
	Currency string  `json:"currency,omitempty"`
	Total    float64 `json:"total"`
}

func runtimeCost(cost *protocol.Cost) *RuntimeCost {
	if cost == nil {
		return nil
	}
	// Do not guess USD for missing currency or sanitize hostile labels into a
	// different currency. Accept only bounded three-letter currency identifiers.
	if len(cost.Currency) != 3 {
		return &RuntimeCost{}
	}
	for _, c := range cost.Currency {
		if c < 'A' || c > 'Z' {
			return &RuntimeCost{}
		}
	}
	for _, value := range [...]float64{cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.Total} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return &RuntimeCost{}
		}
	}
	return &RuntimeCost{Known: true, Currency: cost.Currency, Total: cost.Total}
}

func cloneRuntimeCost(cost *RuntimeCost) *RuntimeCost {
	if cost == nil {
		return nil
	}
	return new(*cost)
}

// cloneRuntimeTelemetry must be used by public snapshot and choices copies.
// RuntimeCost values are replaced, never mutated, during event projection.
func cloneRuntimeTelemetry(telemetry *RuntimeTelemetry) *RuntimeTelemetry {
	if telemetry == nil {
		return nil
	}
	out := *telemetry
	out.Cost = cloneRuntimeCost(telemetry.Cost)
	return &out
}

// runtimeUsageCost gives the conflict marker precedence even if a malformed or
// legacy producer also supplies an amount. Missing cost alone is not conflict.
func runtimeUsageCost(usage protocol.Usage) *RuntimeCost {
	if usage.CostCurrencyConflict {
		return &RuntimeCost{}
	}
	return runtimeCost(usage.Cost)
}

func addRuntimeCost(base *RuntimeCost, usage protocol.Usage) *RuntimeCost {
	next := runtimeUsageCost(usage)
	if base == nil {
		return next
	}
	if next == nil {
		return cloneRuntimeCost(base)
	}
	if !base.Known || !next.Known || base.Currency != next.Currency {
		return &RuntimeCost{}
	}
	total := base.Total + next.Total
	if math.IsInf(total, 0) || math.IsNaN(total) || total < 0 {
		return &RuntimeCost{}
	}
	return &RuntimeCost{Known: true, Currency: base.Currency, Total: total}
}
