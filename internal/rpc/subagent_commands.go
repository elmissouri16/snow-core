package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isSubagentCommand(command string) bool {
	switch command {
	case "subagent_spawn", "subagent_send_message", "subagent_followup", "subagent_wait", "subagent_interrupt", "subagent_close", "subagent_resume", "subagent_list", "subagent_get", "subagent_ready":
		return true
	default:
		return false
	}
}

func (s *Server) handleSubagentCommand(ctx context.Context, req Request) error {
	switch req.Type {
	case "subagent_spawn":
		var p protocol.SpawnSubagentRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		state, err := s.app.SpawnSubagent(ctx, p)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: state})
		return nil
	case "subagent_send_message", "subagent_followup":
		var p struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		var err error
		if req.Type == "subagent_followup" {
			err = s.app.FollowupSubagent(ctx, p.Target, p.Message)
		} else {
			err = s.app.SendSubagentMessage(ctx, p.Target, p.Message)
		}
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	case "subagent_wait":
		var p struct {
			TimeoutMS int    `json:"timeout_ms"`
			Until     string `json:"until,omitempty"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return err
			}
		}
		timeout, err := subagent.ParseWaitTimeoutMS(p.TimeoutMS)
		if err != nil {
			return err
		}
		select {
		case s.waitSlots <- struct{}{}:
		default:
			return fmt.Errorf("rpc: concurrent subagent_wait limit %d reached", maxConcurrentWaits)
		}
		s.promptWG.Add(1)
		go func() {
			defer s.promptWG.Done()
			defer func() { <-s.waitSlots }()
			var (
				res protocol.WaitSubagentsResult
				err error
			)
			switch p.Until {
			case "", "activity":
				res, err = s.app.WaitSubagents(ctx, timeout)
			case "all":
				res, err = s.app.WaitSubagentsUntilAll(ctx, timeout)
			default:
				err = fmt.Errorf("rpc: invalid subagent_wait mode %q (use activity or all)", p.Until)
			}
			if err != nil {
				s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: err.Error()})
				return
			}
			s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: res})
		}()
		return nil
	case "subagent_interrupt":
		var p struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		previous, err := s.app.InterruptSubagent(ctx, p.Target)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: map[string]any{"previous_status": previous}})
		return nil
	case "subagent_close", "subagent_resume":
		var p struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		if req.Type == "subagent_close" {
			previous, err := s.app.CloseSubagent(ctx, p.Target)
			if err != nil {
				return err
			}
			s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: map[string]any{"previous_status": previous, "status": protocol.AgentClosed}})
			return nil
		}
		state, err := s.app.ResumeSubagent(ctx, p.Target)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: state})
		return nil
	case "subagent_list":
		var p struct {
			PathPrefix string `json:"path_prefix"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return err
			}
		}
		list, err := s.app.ListSubagents(ctx, p.PathPrefix)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: list})
		return nil
	case "subagent_get":
		var p struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return err
		}
		state, err := s.app.Subagent(ctx, p.Target)
		if err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: state})
		return nil
	case "subagent_ready":
		if err := s.app.ReadySubagents(); err != nil {
			return err
		}
		s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
		return nil
	default:
		return fmt.Errorf("rpc: unsupported subagent command %q", req.Type)
	}
}
