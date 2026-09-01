package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/permission"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isRuntimeParityCommand(command string) bool {
	switch command {
	case "permission_mode_get", "permission_mode_set", "trust_get", "trust_set",
		"processes_list", "process_logs", "project_init":
		return true
	default:
		return false
	}
}

func (s *Server) handleRuntimeParityCommand(ctx context.Context, req Request) error {
	switch req.Type {
	case "permission_mode_get":
		mode, err := s.app.PermissionMode()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCPermissionMode{Mode: string(mode)}})
	case "permission_mode_set":
		var params struct {
			Mode string `json:"mode"`
		}
		if len(req.Params) == 0 {
			return errors.New("permission_mode_set requires params")
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("permission_mode_set params: %w", err)
		}
		if err := s.app.SetPermissionMode(permission.Mode(params.Mode)); err != nil {
			return err
		}
		mode, err := s.app.PermissionMode()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCPermissionMode{Mode: string(mode)}})
	case "trust_get":
		state, err := s.app.ProjectTrust()
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: rpcProjectTrust(state)})
	case "trust_set":
		var params struct {
			Level string `json:"level"`
		}
		if len(req.Params) == 0 {
			return errors.New("trust_set requires params")
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("trust_set params: %w", err)
		}
		state, err := s.app.SetProjectTrust(trust.Level(params.Level))
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: rpcProjectTrust(state)})
	case "processes_list":
		states, err := s.app.ListManagedProcesses(ctx)
		if err != nil {
			return err
		}
		processes := make([]protocol.RPCManagedProcess, len(states))
		for i, state := range states {
			processes[i] = rpcManagedProcess(state)
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCManagedProcessList{Processes: processes}})
	case "process_logs":
		var params struct {
			ProcessID string `json:"process_id"`
			Cursor    *int64 `json:"cursor"`
			MaxBytes  int    `json:"max_bytes"`
		}
		if len(req.Params) == 0 {
			return errors.New("process_logs requires params")
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fmt.Errorf("process_logs params: %w", err)
		}
		if params.ProcessID == "" {
			return errors.New("process_logs requires process_id")
		}
		logs, err := s.app.ManagedProcessLogs(ctx, params.ProcessID, params.Cursor, params.MaxBytes)
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCManagedProcessLogs{
			ProcessID: logs.ProcessID, Status: logs.Status, Output: logs.Output,
			NextCursor: logs.NextCursor, Omitted: logs.Omitted, EOF: logs.EOF,
		}})
	case "project_init":
		prompt, err := s.app.PrepareProjectInit()
		if err != nil {
			return err
		}
		req.Message = prompt
		req.Content = nil
		req.Mode = ""
		return s.handlePrompt(ctx, req)
	default:
		return fmt.Errorf("unknown runtime parity command %q", req.Type)
	}
}

func rpcProjectTrust(state app.ProjectTrustState) protocol.RPCProjectTrust {
	return protocol.RPCProjectTrust{
		Path: state.Path, Level: string(state.Level), Prompt: state.Prompt,
		Loaded: state.Loaded, RestartRequired: state.RestartRequired,
	}
}

func rpcManagedProcess(state managedprocess.State) protocol.RPCManagedProcess {
	return protocol.RPCManagedProcess{
		ProcessID: state.ProcessID, Name: state.Name, Status: state.Status,
		StartedAt: state.StartedAt, FinishedAt: state.FinishedAt, ExitCode: state.ExitCode,
		Signal: state.Signal, Reason: state.Reason, Ready: state.Ready,
	}
}
