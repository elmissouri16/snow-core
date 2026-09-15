package agent

import (
	"encoding/json/v2"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func costConflictAgent(t *testing.T, provider *scriptedProvider) (*Agent, *session.MemoryStore) {
	t.Helper()
	store := session.NewMemoryStore(session.Options{})
	a, err := New(Options{
		Provider: provider, Registry: tools.NewRegistry(), Session: store,
		Permission: permission.NewService(permission.ModeDeny, nil),
		Model:      protocol.Model{Provider: provider.ID(), ID: "priced-model", Pricing: &protocol.ModelPricing{Currency: "USD", InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); store.Close() })
	return a, store
}

func assertConflictUsage(t *testing.T, usage *protocol.Usage, conflict bool) {
	t.Helper()
	if usage == nil || usage.CostCurrencyConflict != conflict || usage.Total != 12 {
		t.Fatalf("usage accounting changed: %+v", usage)
	}
	if conflict && usage.Cost != nil {
		t.Fatalf("conflicted usage was repriced: %+v", usage.Cost)
	}
	if !conflict && (usage.Cost == nil || usage.Cost.Currency != "USD" || usage.Cost.Total != 14) {
		t.Fatalf("ordinary pricing fallback changed: %+v", usage.Cost)
	}
}

func TestCostCurrencyConflictStreamingIsNotRepriced(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
			{Type: protocol.EvStreamTextDelta, Text: "answer"},
			{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 10, Output: 2, CostCurrencyConflict: conflict}},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		}}}
		a, store := costConflictAgent(t, provider)
		var events []protocol.AgentEvent
		a.Subscribe(func(event protocol.AgentEvent) {
			if event.Type == protocol.EvUsage || event.Type == protocol.EvTurnDone {
				events = append(events, event)
			}
		})
		if err := a.Prompt(t.Context(), "hello"); err != nil {
			t.Fatal(err)
		}
		if err := a.DrainEvents(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(events) < 2 {
			t.Fatalf("usage/turn events missing: %+v", events)
		}
		for _, event := range events {
			assertConflictUsage(t, event.Usage, conflict)
		}
		messages, err := store.Messages()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, message := range messages {
			if message.Usage != nil {
				found = true
				assertConflictUsage(t, message.Usage, conflict)
			}
		}
		if !found {
			t.Fatal("assistant usage not persisted")
		}
		total, err := store.AggregateUsage()
		if err != nil {
			t.Fatal(err)
		}
		assertConflictUsage(t, &total, conflict)
	}
}

func TestCostCurrencyConflictCompactionIsNotRepriced(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		a, store := costConflictAgent(t, &scriptedProvider{})
		usage := &protocol.Usage{Input: 10, Output: 2, CostCurrencyConflict: conflict}
		if err := a.recordCompactionUsage(usage); err != nil {
			t.Fatal(err)
		}
		entries, err := store.BranchEntries()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			if entry.Key != session.MetaProviderUsage {
				continue
			}
			found = true
			var persisted protocol.Usage
			if err := json.Unmarshal([]byte(entry.Value), &persisted); err != nil {
				t.Fatal(err)
			}
			assertConflictUsage(t, &persisted, conflict)
		}
		if !found {
			t.Fatal("compaction usage not persisted")
		}
		total, err := store.AggregateUsage()
		if err != nil {
			t.Fatal(err)
		}
		assertConflictUsage(t, &total, conflict)
		a.mu.RLock()
		turn := a.turnUsage
		a.mu.RUnlock()
		assertConflictUsage(t, &turn, conflict)
		if usage.Total != 0 || usage.Cost != nil {
			t.Fatal("normalization mutated provider-owned usage")
		}
	}
}
