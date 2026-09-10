package modelsdev

import (
	"encoding/json/v2"
	"testing"
)

func TestFreePricingRequiresExplicitZeroPrices(t *testing.T) {
	for _, tc := range []struct {
		data string
		free bool
	}{
		{`null`, false},
		{`{}`, false},
		{`{"input":0}`, false},
		{`{"output":0}`, false},
		{`{"input":null,"output":0}`, false},
		{`{"input":0,"output":0}`, true},
		{`{"input":1,"output":0}`, false},
		{`{"input":-1,"output":0}`, false},
		{`{"input":0,"output":0,"cache_read":1}`, false},
		{`{"input":0,"output":0,"cache_write":1}`, false},
		{`{"input":0,"output":0,"context_over_200k":{"input":1,"output":0}}`, false},
		{`{"input":0,"output":0,"context_over_200k":{"input":0,"output":0}}`, true},
		{`{"input":0,"output":0,"tiers":[{"input":1,"output":0}]}`, false},
		{`{"input":0,"output":0,"tiers":[{"input":0,"output":0}]}`, true},
	} {
		t.Run(tc.data, func(t *testing.T) {
			var cost *Cost
			if err := json.Unmarshal([]byte(tc.data), &cost); err != nil {
				t.Fatal(err)
			}
			if got := cost.Free(); got != tc.free {
				t.Fatalf("Free()=%v want=%v", got, tc.free)
			}
		})
	}
}
