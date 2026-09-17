package rpc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var errSessionReasoningRejected = errors.New("session reasoning request rejected; inspect exact current authority")

func isSessionReasoningCommand(command string) bool {
	return command == "session_reasoning_get" || command == "session_reasoning_set"
}

func validateSessionReasoningParams(req Request) error {
	if !isSessionReasoningCommand(req.Type) || len(req.Params) == 0 || len(req.Params) > 8192 {
		return errSessionReasoningRejected
	}
	wire, err := json.Marshal(req)
	if err != nil {
		return errSessionReasoningRejected
	}
	if err := validateMessageEditFrame(wire); err != nil {
		return errSessionReasoningRejected
	}
	var fields map[string]jsontext.Value
	if json.Unmarshal(req.Params, &fields) != nil {
		return errSessionReasoningRejected
	}
	stringField := func(fields map[string]jsontext.Value, key string, empty bool) bool {
		raw, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
		var value string
		return json.Unmarshal(raw, &value) == nil && len(value) <= 256 && (empty || value != "")
	}
	if req.Type == "session_reasoning_get" {
		if len(fields) != 1 || !stringField(fields, "session_id", false) {
			return errSessionReasoningRejected
		}
		return nil
	}
	if len(fields) != 3 || !stringField(fields, "field", false) || !stringField(fields, "value", false) {
		return errSessionReasoningRejected
	}
	var expected map[string]jsontext.Value
	if json.Unmarshal(fields["expected"], &expected) != nil || len(expected) != 10 {
		return errSessionReasoningRejected
	}
	for _, key := range []string{"session_id", "branch_id", "tip_id", "provider", "model", "mode", "permission_mode", "thinking", "reasoning_summary", "text_verbosity"} {
		if !stringField(expected, key, key == "tip_id") {
			return errSessionReasoningRejected
		}
	}
	return nil
}

// handleSessionReasoning is invoked only through the strict-envelope routing
// used by history/queue commands. Both reads and writes serialize against RPC
// prompt admission; App then owns state/admission/agent locks. Legacy settings
// commands and model discovery are deliberately not used as fallback.
func (s *Server) handleSessionReasoning(ctx context.Context, req Request) (retErr error) {
	defer func() {
		if retErr != nil && !errors.Is(retErr, agent.ErrSessionReasoningUnknown) {
			retErr = errSessionReasoningRejected
		}
	}()
	if err := validateSessionReasoningParams(req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.promptLifecycleMu.Lock()
	defer s.promptLifecycleMu.Unlock()
	s.mu.Lock()
	busy := s.promptDone != nil
	s.mu.Unlock()
	if busy {
		return errSessionReasoningRejected
	}
	var result protocol.RPCSessionReasoning
	if req.Type == "session_reasoning_get" {
		var p protocol.RPCSessionReasoningGetParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.SessionReasoning(ctx, p)
		if err != nil {
			return err
		}
		result = value
	} else {
		var p protocol.RPCSessionReasoningSetParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.SetSessionReasoning(ctx, p)
		if err != nil {
			return err
		}
		result = value
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
}
