package app

import (
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Usage excludes legacy inherited usage without changing exact child history.
func (r *childAgentRuntime) Usage() (protocol.Usage, error) {
	if aggregate, ok := r.store.(interface {
		AggregateChildUsage() (protocol.Usage, error)
	}); ok {
		return aggregate.AggregateChildUsage()
	}
	if entries, ok := r.store.(session.BranchEntryStore); ok {
		branch, err := entries.BranchEntries()
		if err != nil {
			return protocol.Usage{}, err
		}
		return session.ChildIncurredUsage(branch)
	}
	return r.Agent.Usage()
}
