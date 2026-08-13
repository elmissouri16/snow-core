package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsageCacheReadKnownJSONPresence(t *testing.T) {
	unknown, err := json.Marshal(Usage{CacheRead: 0})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unknown), "cache_read_known") {
		t.Fatalf("unknown JSON = %s", unknown)
	}
	known, err := json.Marshal(Usage{CacheReadKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(known), `"cache_read_known":true`) {
		t.Fatalf("known JSON = %s", known)
	}
}

func TestUsageAddAndCost(t *testing.T) {
	pricing := &ModelPricing{
		Currency:             "USD",
		InputPerMillion:      1,
		OutputPerMillion:     2,
		CacheReadPerMillion:  0.1,
		CacheWritePerMillion: 0.2,
	}
	first := Usage{Input: 100, Output: 20, CacheRead: 40, Total: 120}
	first.Cost = first.CostFor(pricing)
	second := Usage{Input: 50, Output: 10, CacheWrite: 20}
	second.Cost = second.CostFor(pricing)
	got := first.Add(second)
	if got.Input != 150 || got.Output != 30 || got.CacheRead != 40 || got.CacheWrite != 20 || got.Total != 180 {
		t.Fatalf("usage = %+v", got)
	}
	if got.Requests != 2 {
		t.Fatalf("requests = %d, want 2", got.Requests)
	}
	if got.Cost == nil || got.Cost.Total <= 0 {
		t.Fatalf("cost = %+v", got.Cost)
	}
	// Add must not mutate the caller's Cost record (value receiver aliasing).
	costBefore := first.Cost.Input
	if first.Cost.Input != costBefore {
		t.Fatalf("first.Cost mutated by Add: %+v", first.Cost)
	}
}

func TestUsageAddPreservesCacheReadKnowledgeOnlyWhenComplete(t *testing.T) {
	knownHit := Usage{Input: 100, CacheRead: 80, CacheReadKnown: true}
	knownMiss := Usage{Input: 50, CacheReadKnown: true}
	got := knownHit.Add(knownMiss)
	if !got.CacheReadKnown || got.CacheRead != 80 || got.Requests != 2 {
		t.Fatalf("known aggregate = %+v", got)
	}
	got = got.Add(Usage{Input: 25})
	if got.CacheReadKnown {
		t.Fatalf("aggregate with omitted cache metric remained known: %+v", got)
	}
}

func TestUsageAddTreatsPositiveCacheReadAsKnownForCompatibility(t *testing.T) {
	got := (Usage{}).Add(Usage{Input: 100, CacheRead: 50})
	if !got.CacheReadKnown || got.Requests != 1 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestUsageAddDoesNotUpgradePartiallyKnownLegacyAggregate(t *testing.T) {
	partial := Usage{Input: 200, CacheRead: 50, Requests: 2}
	got := partial.Add(Usage{Input: 100, CacheRead: 25, CacheReadKnown: true})
	if got.CacheReadKnown {
		t.Fatalf("partially known aggregate was upgraded: %+v", got)
	}
}

func TestUsageAddRecognizesKnownZeroAsARequest(t *testing.T) {
	got := (Usage{}).Add(Usage{CacheReadKnown: true})
	if !got.CacheReadKnown || got.Requests != 1 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestUsageAddKeepsCostAcrossMissingRecords(t *testing.T) {
	pricing := &ModelPricing{Currency: "USD", InputPerMillion: 1, OutputPerMillion: 2}
	first := Usage{Input: 10, Total: 10}
	first.Cost = first.CostFor(pricing)
	middle := Usage{Input: 5, Total: 5} // no cost
	last := Usage{Input: 5, Total: 5}
	last.Cost = last.CostFor(pricing)
	got := first.Add(middle).Add(last)
	if got.Cost == nil {
		t.Fatal("cost lost when a middle record lacks cost")
	}
	if got.Input != 20 || got.Cost.Total <= 0 {
		t.Fatalf("usage = %+v cost = %+v", got, got.Cost)
	}
}

func TestUsageUnknownPricingDoesNotInventCost(t *testing.T) {
	usage := Usage{Input: 10, Output: 4, CacheRead: 2}
	if got := usage.CostFor(nil); got != nil {
		t.Fatalf("cost = %+v, want nil", got)
	}
}
