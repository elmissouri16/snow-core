package agent

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Usage events are cumulative snapshots for one provider request. Keep only
// the last snapshot per attempt, including attempts that fail after reporting.
type compactionUsageStream struct {
	protocol.EventStream
	usage *protocol.Usage
}

func (s *compactionUsageStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	event, err := s.EventStream.Next(ctx)
	if event.Type == protocol.EvStreamUsage && event.Usage != nil {
		s.usage = event.Usage.Clone()
	}
	return event, err
}

type compactionAccountingError struct{ err error }

func (e *compactionAccountingError) Error() string { return "compaction accounting: " + e.err.Error() }
func (e *compactionAccountingError) Unwrap() error { return e.err }

func (a *Agent) recordCompactionUsage(reported *protocol.Usage) error {
	if reported == nil {
		return nil
	}
	usage := *reported.Clone()
	if usage.Total == 0 {
		usage.Total = usage.Input + usage.Output
	}
	if usage.Cost == nil {
		usage.Cost = usage.CostFor(a.Model().Pricing)
	}
	wire, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	// Auxiliary usage belongs to the append-only branch, but must not create a
	// conversation turn or become the next request's context-occupancy estimate.
	a.mailboxPersistMu.Lock()
	err = a.opts.Session.Append(session.Entry{Type: session.EntryMeta, ID: newID(), Key: session.MetaProviderUsage, Value: string(wire)})
	a.mailboxPersistMu.Unlock()
	if err != nil {
		return fmt.Errorf("persist summary usage: %w", err)
	}
	a.mu.Lock()
	if a.turnOrigin != "compact" {
		a.turnUsage = a.turnUsage.Add(usage)
		a.usageSet = true
	}
	a.mu.Unlock()
	if err := a.accountGoalUsage(usage); err != nil {
		return err
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	return nil
}
