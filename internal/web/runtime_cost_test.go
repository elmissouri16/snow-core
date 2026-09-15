package web

import (
	"encoding/json/v2"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRuntimeCostPublicProjection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cost  *protocol.Cost
		known bool
	}{
		{"missing", nil, false},
		{"zero", &protocol.Cost{Currency: "USD"}, true},
		{"estimate", &protocol.Cost{Currency: "EUR", Total: 1.25}, true},
		{"no-currency", &protocol.Cost{Total: 1}, false},
		{"long-currency", &protocol.Cost{Currency: strings.Repeat("S", 8192)}, false},
		{"markup", &protocol.Cost{Currency: "<b>"}, false},
		{"lowercase", &protocol.Cost{Currency: "usd"}, false},
		{"negative", &protocol.Cost{Currency: "USD", Total: -1}, false},
		{"nan", &protocol.Cost{Currency: "USD", Total: math.NaN()}, false},
		{"infinity", &protocol.Cost{Currency: "USD", Total: math.Inf(1)}, false},
		{"negative-component", &protocol.Cost{Currency: "USD", CacheRead: -1, Total: 1}, false},
		{"nan-component", &protocol.Cost{Currency: "USD", Input: math.NaN(), Total: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runtimeCost(tc.cost)
			if known := got != nil && got.Known; known != tc.known {
				t.Fatalf("projection %+v, want known %v", got, tc.known)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatalf("invalid public JSON: %v", err)
			}
			if got != nil && !got.Known && (got.Currency != "" || got.Total != 0) {
				t.Fatalf("unknown projection leaks invalid metadata: %+v", got)
			}
		})
	}
}

func TestRuntimeCostUsageEventsDoNotDoubleCount(t *testing.T) {
	base := RuntimeTelemetry{Available: true, InputTokens: 100, TotalTokens: 100, Cost: runtimeCost(&protocol.Cost{Currency: "USD", Total: 1})}
	r := &liveRuntime{usageBase: *cloneRuntimeTelemetry(&base), snapshot: RuntimeSnapshot{Telemetry: cloneRuntimeTelemetry(&base)}}
	first := protocol.Usage{Input: 10, Total: 10, Cost: &protocol.Cost{Currency: "USD", Total: 0.25}}
	second := protocol.Usage{Input: 20, Total: 20, Cost: &protocol.Cost{Currency: "USD", Total: 0.5}}
	for _, usage := range []protocol.Usage{first, first, second, second} {
		r.projectUsage(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &usage})
		if r.snapshot.Telemetry.Cost.Total != 1 || r.snapshot.Telemetry.TotalTokens != 100 {
			t.Fatal("per-request cumulative usage changed session totals")
		}
	}
	done := protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: new(first.Add(second))}
	for range 2 {
		r.projectUsage(done)
		if got := r.snapshot.Telemetry; got.Cost.Total != 1.75 || got.TotalTokens != 130 {
			t.Fatalf("turn aggregate double-counted: %+v cost %+v", got, got.Cost)
		}
	}
	done.Usage.Cost.Total = 99
	if r.snapshot.Telemetry.Cost.Total != 1.75 || base.Cost.Total != 1 || r.usageBase.Cost.Total != 1 {
		t.Fatal("event or baseline cost pointer aliases projection")
	}
	// Admission advances the baseline once between turns (including goal turns).
	r.usageBase = *cloneRuntimeTelemetry(r.snapshot.Telemetry)
	r.projectUsage(protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: &first})
	if r.snapshot.Telemetry.Cost.Total != 2 {
		t.Fatalf("next turn cost: %+v", r.snapshot.Telemetry.Cost)
	}
}

func TestRuntimeCostUnknownMixedCurrencyAndOverflow(t *testing.T) {
	usd := &protocol.Cost{Currency: "USD", Total: 1}
	known := runtimeCost(usd)
	if got := addRuntimeCost(nil, protocol.Usage{}); got != nil {
		t.Fatalf("unknown became zero: %+v", got)
	}
	if got := addRuntimeCost(known, protocol.Usage{}); !got.Known || got.Total != 1 || got == known {
		t.Fatalf("known subtotal not independently preserved: %+v", got)
	}
	if got := addRuntimeCost(nil, protocol.Usage{Cost: usd}); !got.Known || got.Total != 1 {
		t.Fatalf("new known subtotal: %+v", got)
	}
	mixed := addRuntimeCost(known, protocol.Usage{Cost: &protocol.Cost{Currency: "EUR", Total: 2}})
	if mixed == nil || mixed.Known || mixed.Total != 0 || mixed.Currency != "" {
		t.Fatalf("mixed currencies fabricated total: %+v", mixed)
	}
	for _, next := range []*protocol.Cost{nil, usd, {Currency: "EUR", Total: 3}} {
		if got := addRuntimeCost(mixed, protocol.Usage{Cost: next}); got == nil || got.Known {
			t.Fatalf("lost currency conflict: %+v", got)
		}
	}
	large := runtimeCost(&protocol.Cost{Currency: "USD", Total: math.MaxFloat64})
	if got := addRuntimeCost(large, protocol.Usage{Cost: &protocol.Cost{Currency: "USD", Total: math.MaxFloat64}}); got.Known {
		t.Fatalf("overflow is known: %+v", got)
	}
	// Usage.Add intentionally retains the priced subset, not total coverage.
	aggregate := (protocol.Usage{}).Add(protocol.Usage{Cost: usd}).Add(protocol.Usage{Input: 20})
	if got := runtimeCost(aggregate.Cost); !got.Known || got.Total != 1 {
		t.Fatalf("authoritative known subtotal lost: %+v", got)
	}
}

func TestRuntimeCostCloneSnapshotAndReset(t *testing.T) {
	s := RuntimeSnapshot{Telemetry: &RuntimeTelemetry{Available: true, Cost: runtimeCost(&protocol.Cost{Currency: "USD", Total: 2})}}
	out := s.clone()
	out.Telemetry.Cost.Total = 99
	if s.Telemetry.Cost.Total != 2 {
		t.Fatal("snapshot cost aliases internal telemetry")
	}
	clone := cloneRuntimeTelemetry(s.Telemetry)
	clone.Cost.Currency = "EUR"
	if s.Telemetry.Cost.Currency != "USD" || cloneRuntimeTelemetry(nil) != nil {
		t.Fatal("telemetry clone aliases cost")
	}
	// Actual authoritative usage refresh must replace previous costs rather than
	// carrying a priced old session into an unpriced selected session.
	m, projects, _ := runtimeTestManager(t, "workflow")
	opened, err := m.Open(t.Context(), projects[0], "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.controlRuntime(t.Context(), projects[0].ID, opened.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.snapshot.Telemetry.Cost = runtimeCost(&protocol.Cost{Currency: "USD", Total: 5})
	r.usageBase = *cloneRuntimeTelemetry(r.snapshot.Telemetry)
	r.mu.Unlock()
	r.control.Unlock()
	choices, err := m.Choices(t.Context(), projects[0].ID, opened.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if choices.Telemetry.Cost != nil {
		t.Fatalf("refresh retained stale cost: %+v", choices.Telemetry.Cost)
	}
	r.mu.Lock()
	baseCost := r.usageBase.Cost
	r.mu.Unlock()
	if baseCost != nil {
		t.Fatal("authoritative refresh retained stale cost baseline")
	}
	next, err := m.Switch(t.Context(), projects[0].ID, opened.InstanceID, "saved", false)
	if err != nil {
		t.Fatal(err)
	}
	if next.Telemetry.Cost != nil {
		t.Fatal("switch fabricated cost for unpriced usage")
	}
	if err := m.CloseProject(t.Context(), projects[0].ID, next.InstanceID); err != nil {
		t.Fatal(err)
	}
	reopened, err := m.Open(t.Context(), projects[0], "saved", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Telemetry.Cost != nil {
		t.Fatal("reopen fabricated cost for unpriced usage")
	}
}

func TestRuntimeCostAuthoritativeChoicesSwitchAndReopen(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "cost")
	m.env = slices.DeleteFunc(m.env, func(value string) bool { return strings.HasPrefix(value, "SNOW_WEB_RUNTIME_TEST_CHILD=") })
	m.env = append(m.env, "SNOW_WEB_COST_TEST_CHILD=1")
	p := projects[0]
	opened, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cost := opened.Telemetry.Cost; cost == nil || !cost.Known || cost.Currency != "USD" || cost.Total != 1.25 {
		t.Fatalf("open cost: %+v", cost)
	}
	opened.Telemetry.Cost.Total = 99
	choices, err := m.Choices(t.Context(), p.ID, opened.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if choices.Telemetry.Cost.Total != 1.25 {
		t.Fatal("refresh cost differs from authoritative usage")
	}
	choices.Telemetry.Cost.Total = 999
	fresh, _ := m.Snapshot(p.ID)
	if fresh.Telemetry.Cost.Total != 1.25 {
		t.Fatal("choices cost aliases runtime")
	}
	saved, err := m.Switch(t.Context(), p.ID, fresh.InstanceID, "saved", false)
	if err != nil {
		t.Fatal(err)
	}
	if cost := saved.Telemetry.Cost; cost == nil || !cost.Known || cost.Currency != "EUR" || cost.Total != 2.5 {
		t.Fatalf("switch accumulated old session costs: %+v", cost)
	}
	if err := m.CloseProject(t.Context(), p.ID, saved.InstanceID); err != nil {
		t.Fatal(err)
	}
	reopened, err := m.Open(t.Context(), p, "saved", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cost := reopened.Telemetry.Cost; cost == nil || cost.Currency != "EUR" || cost.Total != 2.5 {
		t.Fatalf("reopen cost: %+v", cost)
	}
	unknown, err := m.Switch(t.Context(), p.ID, reopened.InstanceID, "unknown", false)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Telemetry.Cost != nil {
		t.Fatal("unknown session inherited cost")
	}
	conflict, err := m.Switch(t.Context(), p.ID, unknown.InstanceID, "conflict", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := conflict.Telemetry.Cost; got == nil || got.Known {
		t.Fatalf("authoritative conflicted usage: %+v", got)
	}
	if err := m.CloseProject(t.Context(), p.ID, conflict.InstanceID); err != nil {
		t.Fatal(err)
	}
	conflict, err = m.Open(t.Context(), p, "conflict", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := conflict.Telemetry.Cost; got == nil || got.Known {
		t.Fatalf("reopened conflict lost: %+v", got)
	}
}

func TestRuntimeCostProtocolConflictIsAuthoritative(t *testing.T) {
	known := runtimeCost(&protocol.Cost{Currency: "USD", Total: 1})
	for _, conflictingCost := range []*protocol.Cost{nil, {Currency: "USD", Total: 999}} {
		usage := protocol.Usage{Input: 20, Total: 20, CostCurrencyConflict: true, Cost: conflictingCost}
		if got := runtimeUsageCost(usage); got == nil || got.Known || got.Total != 0 || got.Currency != "" {
			t.Fatalf("conflict marker ignored: %+v", got)
		}
		r := &liveRuntime{usageBase: RuntimeTelemetry{Available: true, Cost: known}, snapshot: RuntimeSnapshot{Telemetry: &RuntimeTelemetry{Available: true, Cost: cloneRuntimeCost(known)}}}
		r.projectUsage(protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: &usage})
		if got := r.snapshot.Telemetry.Cost; got == nil || got.Known {
			t.Fatalf("incoming conflict retained prior amount: %+v", got)
		}
		r.usageBase = *cloneRuntimeTelemetry(r.snapshot.Telemetry)
		r.projectUsage(protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: &protocol.Usage{Cost: &protocol.Cost{Currency: "USD", Total: 2}}})
		if got := r.snapshot.Telemetry.Cost; got == nil || got.Known {
			t.Fatalf("baseline conflict lost: %+v", got)
		}
	}
}
