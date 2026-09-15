package rpc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isProcessControlCommand(command string) bool {
	return command == "process_control_list" || command == "process_control_logs" || command == "process_control_stop"
}

// This strict additive namespace does not silently change the legacy sessionless
// processes_list/process_logs RPC contracts. Dispatch before ordinary busy gates.
func (s *Server) handleProcessControl(ctx context.Context, req Request) error {
	if err := validateProcessControlParams(req); err != nil {
		return err
	}
	var result any
	var err error
	switch req.Type {
	case "process_control_list":
		var p protocol.RPCProcessControlListParams
		err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true))
		if err == nil {
			result, err = s.app.ProcessControlList(ctx, p)
		}
	case "process_control_logs":
		var p protocol.RPCProcessControlLogsParams
		err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true))
		if err == nil {
			result, err = s.app.ProcessControlLogs(ctx, p)
		}
	case "process_control_stop":
		var p protocol.RPCProcessControlStopParams
		err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true))
		if err == nil {
			result, err = s.app.ProcessControlStop(ctx, p)
		}
	}
	if err != nil {
		return err
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
}
func validateProcessControlParams(req Request) error {
	invalid := errors.New("invalid managed process control parameters")
	if !isProcessControlCommand(req.Type) || len(req.Params) == 0 || len(req.Params) > 2048 {
		return invalid
	}
	var fields map[string]jsontext.Value
	if json.Unmarshal(req.Params, &fields) != nil || fields == nil {
		return invalid
	}
	allowed := map[string]bool{"session_id": true}
	if req.Type != "process_control_list" {
		allowed["process_id"] = true
	}
	switch req.Type {
	case "process_control_logs":
		allowed["cursor"], allowed["max_bytes"] = true, true
	case "process_control_stop":
		allowed["grace_ms"] = true
	}
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid
		}
	}
	var sessionID string
	if json.Unmarshal(fields["session_id"], &sessionID) != nil || !protocol.ValidProcessControlSession(sessionID) {
		return invalid
	}
	switch req.Type {
	case "process_control_logs":
		var p protocol.RPCProcessControlLogsParams
		if json.Unmarshal(req.Params, &p) != nil || !p.Valid() {
			return invalid
		}
	case "process_control_stop":
		var p protocol.RPCProcessControlStopParams
		if json.Unmarshal(req.Params, &p) != nil || !p.Valid() {
			return invalid
		}
	}
	return nil
}
