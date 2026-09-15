package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (s *Server) handleCompactionStart(ctx context.Context, req Request) (retErr error) {
	defer func() {
		if retErr != nil {
			if errors.Is(retErr, app.ErrCompactionOutcomeUnknown) {
				retErr = app.ErrCompactionOutcomeUnknown
			} else {
				retErr = app.ErrCompactionRejected
			}
		}
	}()
	if err := validateManagedRuntimeParams(req, []string{"session_id", "branch_id", "expected_tip_id"}, 8192); err != nil {
		return err
	}
	var params protocol.RPCCompactionStartParams
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.promptLifecycleMu.Lock()
	defer s.promptLifecycleMu.Unlock()
	s.mu.Lock()
	if s.promptDone != nil {
		s.mu.Unlock()
		cancel()
		return app.ErrCompactionRejected
	}
	s.cancel, s.promptDone = cancel, done
	s.mu.Unlock()
	handle, err := s.app.StartCompaction(runCtx, params)
	if err != nil {
		cancel()
		s.releasePrompt(done)
		return err
	}
	// The captured owner is gated: even a synchronous provider cannot execute
	// before the ACK write succeeds. A failed write cancels and joins that owner.
	if err := s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: handle.Accepted()}); err != nil {
		cancel()
		handle.Cancel()
		_ = handle.Wait(context.Background())
		s.releasePrompt(done)
		return app.ErrCompactionOutcomeUnknown
	}
	handle.Release()
	s.promptWG.Go(func() {
		defer cancel()
		_ = handle.Wait(context.Background())
		// Native compaction_done is progress, not completion. Wait includes durable
		// accounting and mailbox cleanup; drain those events before one terminal frame.
		flushCtx, stopFlush := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.app.Agent.DrainEvents(flushCtx)
		stopFlush()
		completed, _ := handle.Completion()
		completed.Type, completed.RequestID = protocol.RPCTypeCompactionCompleted, req.ID
		s.promptLifecycleMu.Lock()
		s.releasePrompt(done)
		_ = s.write(completed)
		s.promptLifecycleMu.Unlock()
	})
	return nil
}
