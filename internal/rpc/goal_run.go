package rpc

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func strictGoalRequest(req Request) error {
	if req.ID == "" {
		return errors.New("goal control requires a correlated request id")
	}
	if req.Message != "" || req.Content != nil || req.Model != "" || req.Thinking != "" || req.ReasoningSummary != "" || req.TextVerbosity != "" || req.Mode != "" || req.Provider != "" || req.Method != "" || req.Secret != "" {
		return errors.New("goal control accepts only id, type and strict params")
	}
	return nil
}

func (s *Server) handleGoalInspect(ctx context.Context, req Request) error {
	if err := strictGoalRequest(req); err != nil {
		return err
	}
	var params protocol.RPCGoalInspectParams
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	result, err := s.app.InspectGoal(ctx, params)
	if err != nil {
		return err
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
}

func (s *Server) handleGoalRun(ctx context.Context, req Request) (retErr error) {
	defer func() {
		if retErr != nil && !errors.Is(retErr, app.ErrGoalRunOutcomeUnknown) {
			retErr = errors.Join(app.ErrGoalRunRejected, retErr)
		}
	}()
	if err := strictGoalRequest(req); err != nil {
		return err
	}
	var params protocol.RPCGoalRunParams
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	// Requiring the presence of both assertions distinguishes absence/empty-tip
	// assertions from omitted optimistic concurrency checks.
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(req.Params, &fields); err != nil {
		return err
	}
	for _, key := range []string{"action", "session_id", "branch_id", "expected_tip_id", "expected_goal_id"} {
		if raw, ok := fields[key]; !ok || string(raw) == "null" {
			return errors.New("goal run requires explicit binding and goal identity assertions")
		}
	}
	if params.Action == "resume" {
		if _, ok := fields["objective"]; ok {
			return errors.New("goal resume does not accept objective")
		}
		if _, ok := fields["token_budget"]; ok {
			return errors.New("goal resume does not accept token_budget")
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.promptLifecycleMu.Lock()
	defer s.promptLifecycleMu.Unlock()
	s.mu.Lock()
	if s.promptDone != nil {
		s.mu.Unlock()
		cancel()
		return errors.New("rpc: goal run requires an idle prompt lifecycle")
	}
	s.cancel, s.promptDone = cancel, done
	s.mu.Unlock()
	handle, err := s.app.StartGoalRun(runCtx, params)
	if err != nil {
		cancel()
		s.releasePrompt(done)
		return err
	}
	accepted := protocol.RPCGoalRunAccepted{GoalRunID: handle.ID(), GoalID: handle.GoalID(), SessionID: params.SessionID, BranchID: params.BranchID}
	ackErr := s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: accepted})
	if ackErr != nil {
		cancel()
		handle.Cancel()
		err := handle.Wait(context.Background())
		s.releasePrompt(done)
		return errors.Join(app.ErrGoalRunOutcomeUnknown, ackErr, err)
	}
	handle.Release()
	s.promptWG.Go(func() {
		defer cancel()
		err := handle.Wait(context.Background())
		// Serialize the terminal frame after native events without delaying owner
		// cleanup on a subscriber that calls Abort from its event callback.
		flushCtx, stopFlush := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.app.Agent.DrainEvents(flushCtx)
		stopFlush()
		completed := protocol.RPCGoalRunCompleted{Type: protocol.RPCTypeGoalRunCompleted, RequestID: req.ID, GoalRunID: handle.ID(), GoalID: handle.GoalID(), Status: "finished", GoalStatus: handle.GoalStatus()}
		if runCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			completed.Status = "canceled"
		} else if err != nil {
			completed.Status, completed.Error = "failed", err.Error()
		}
		s.promptLifecycleMu.Lock()
		s.releasePrompt(done)
		_ = s.write(completed)
		s.promptLifecycleMu.Unlock()
	})
	return nil
}
