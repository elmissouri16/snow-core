package rpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// forwardAgentEvents monitors the whole subscriber callback, including time
// spent waiting for the shared RPC writer. A successful individual write does
// not prove the event bus retained this subscriber. Eviction must wake Serve
// even if the client is waiting for an event before sending its next command.
func (s *Server) forwardAgentEvents() func() error {
	unsubscribe, failure := s.app.Agent.SubscribeMonitored(func(event protocol.AgentEvent) {
		_ = s.write(event)
	})
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := failure(); err != nil {
					s.recordWriteErr(err)
					return
				}
			}
		}
	}()
	return sync.OnceValue(func() error {
		// Flush final prompt events before unsubscribing. Check synchronously too:
		// EOF can race eviction before the monitoring goroutine's next tick.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		drainErr := s.app.Agent.DrainEvents(ctx)
		unsubscribe()
		close(stop)
		<-done
		err := errors.Join(drainErr, failure())
		if err != nil {
			s.recordWriteErr(err)
		}
		return err
	})
}
