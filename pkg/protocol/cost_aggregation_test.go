package protocol

import (
	"encoding/json/v2"
	"testing"
)

// A currency label cannot make a sum of different currencies meaningful.
// Keep this regression on Usage.Add: every surface consumes these aggregates.
func TestUsageAddRejectsMixedCurrencyCost(t *testing.T) {
	for _, tc := range []struct {
		name       string
		currencies []string
	}{
		{"two-currencies", []string{"USD", "EUR"}},
		{"conflict-then-first-currency", []string{"USD", "EUR", "USD"}},
		{"conflict-then-second-currency", []string{"USD", "EUR", "EUR"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var usage Usage
			for _, currency := range tc.currencies {
				usage = usage.Add(Usage{Input: 10, Cost: &Cost{Currency: currency, Total: 1}})
			}
			if usage.Cost != nil {
				t.Fatalf("mixed currency aggregate fabricated %s %g from %v; want unavailable cost", usage.Cost.Currency, usage.Cost.Total, tc.currencies)
			}
			if usage.Input != 10*len(tc.currencies) {
				t.Fatal("cost conflict must not discard token usage")
			}
		})
	}
}

func TestUsageAddMixedCurrencyConflictSurvivesJSONAndMissingCost(t *testing.T) {
	usd := Usage{Input: 10, Cost: &Cost{Currency: "USD", Total: 1}}
	eur := Usage{Input: 20, Cost: &Cost{Currency: "EUR", Total: 2}}
	mixed := usd.Add(eur)
	encoded, err := json.Marshal(mixed)
	if err != nil {
		t.Fatal(err)
	}
	var restored Usage
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	got := restored.Add(Usage{Input: 30}).Add(usd)
	if got.Cost != nil {
		t.Fatalf("currency conflict forgotten after persistence/unpriced request: %+v; want unavailable cost", got.Cost)
	}
	if got.Input != 70 || got.Requests != 4 {
		t.Fatalf("token/request accounting changed: %+v", got)
	}
	// A conflicting aggregate passed as the right-hand operand must poison
	// currency summation too, not silently contribute only its token counts.
	if got := usd.Add(restored); got.Cost != nil {
		t.Fatalf("right-hand conflict lost: %+v", got.Cost)
	}
}

func TestUsageAddCurrencyConflictMarkerAndLegacyLabels(t *testing.T) {
	usd := Usage{Input: 10, Cost: &Cost{Currency: "USD", Total: 1}}
	for _, tc := range []struct {
		name        string
		left, right Usage
		conflict    bool
	}{
		{"same", usd, usd, false},
		{"missing-left", Usage{Cost: &Cost{Total: 1}}, usd, true},
		{"missing-right", usd, Usage{Cost: &Cost{Total: 1}}, true},
		{"legacy-unlabeled", Usage{Cost: &Cost{Total: 1}}, Usage{Cost: &Cost{Total: 2}}, false},
		{"different-case", usd, Usage{Cost: &Cost{Currency: "usd", Total: 2}}, true},
		{"unpriced", usd, Usage{Input: 10}, false},
		{"left-conflict", Usage{CostCurrencyConflict: true}, usd, true},
		{"right-conflict", usd, Usage{CostCurrencyConflict: true}, true},
		{"malformed-marked-cost", usd, Usage{CostCurrencyConflict: true, Cost: &Cost{Currency: "USD", Total: 2}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.left.Add(tc.right)
			if got.CostCurrencyConflict != tc.conflict || (tc.conflict && got.Cost != nil) {
				t.Fatalf("got %+v; want conflict %v", got, tc.conflict)
			}
			if !tc.conflict && got.Cost == nil {
				t.Fatal("compatible known cost lost")
			}
			clone := got.Clone()
			if clone.CostCurrencyConflict != got.CostCurrencyConflict {
				t.Fatal("clone lost conflict marker")
			}
			if got.Cost != nil {
				got.Cost.Total = 999
				if tc.left.Cost != nil && tc.left.Cost.Total == 999 || tc.right.Cost != nil && tc.right.Cost.Total == 999 {
					t.Fatal("aggregation aliases source cost")
				}
			}
		})
	}
	// CostFor, rather than Add, owns the existing catalog default to USD.
	priced := Usage{Input: 10}
	priced.Cost = priced.CostFor(&ModelPricing{InputPerMillion: 1})
	if got := usd.Add(priced); got.CostCurrencyConflict || got.Cost == nil || got.Cost.Currency != "USD" {
		t.Fatalf("catalog currency default changed: %+v", got)
	}
}

func TestUsageCurrencyConflictSchemasAndWireOmission(t *testing.T) {
	usage := Usage{Input: 10, Requests: 2, CostCurrencyConflict: true}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"response.schema.json", RPCResponse{Type: "response", ID: "usage", Command: "usage", Success: true, Data: usage}},
		{"agent-event.schema.json", AgentEvent{Type: EvUsage, Usage: &usage}},
		{"message.schema.json", Message{ID: "message", Role: RoleAssistant, Content: []ContentBlock{}, Usage: &usage}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := resolveRPCSchema(t, tc.name).Validate(jsonValue(t, tc.value)); err != nil {
				t.Fatalf("conflict usage rejected by schema: %v", err)
			}
		})
	}
	for _, conflict := range []bool{false, true} {
		data, err := json.Marshal(Usage{CostCurrencyConflict: conflict})
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]any
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		_, present := object["cost_currency_conflict"]
		if present != conflict {
			t.Fatalf("optional marker presence %v, want %v: %s", present, conflict, data)
		}
	}
}
