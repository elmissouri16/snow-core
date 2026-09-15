package rpc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isManagedRuntimeCommand(command string) bool {
	switch command {
	case "history_branch_fork", "history_session_fork", "history_branch_rename", "compaction_start", "managed_steer":
		return true
	}
	return false
}

func managedRuntimeRejected(command string) error {
	switch command {
	case "session_reasoning_get", "session_reasoning_set":
		return errSessionReasoningRejected
	case "compaction_start":
		return app.ErrCompactionRejected
	case "managed_steer":
		return agent.ErrManagedSteerRejected
	default:
		return app.ErrHistoryControlRejected
	}
}

// Check the original frame, not only the compatibility-decoded request: legacy
// decoding loses unknown, repeated and explicitly empty envelope fields.
func validateManagedRuntimeFrame(frame []byte, req Request) error {
	rejected := managedRuntimeRejected(req.Type)
	if len(req.ID) == 0 || len(req.ID) > 256 || strings.ContainsAny(req.ID, "\x00\r\n\t") || strings.TrimSpace(req.ID) != req.ID {
		return rejected
	}
	var fields map[string]jsontext.Value
	if json.Unmarshal(frame, &fields) != nil || len(fields) != 3 {
		return rejected
	}
	for _, key := range []string{"id", "type", "params"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return rejected
		}
		if key != "params" {
			var text string
			if json.Unmarshal(value, &text) != nil || text == "" {
				return rejected
			}
		}
	}
	return nil
}

// All required compare-and-swap fields must be present even when their asserted
// value is empty. v2 rejects duplicate members recursively and invalid UTF-8.
func validateManagedRuntimeParams(req Request, required []string, maxBytes int) error {
	rejected := managedRuntimeRejected(req.Type)
	frame, err := json.Marshal(req)
	if err != nil || validateManagedRuntimeFrame(frame, req) != nil || len(req.Params) == 0 || len(req.Params) > maxBytes {
		return rejected
	}
	var fields map[string]jsontext.Value
	if json.Unmarshal(req.Params, &fields) != nil || len(fields) != len(required) {
		return rejected
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return rejected
		}
	}
	return nil
}

func (s *Server) handleHistoryControl(ctx context.Context, req Request) (retErr error) {
	defer func() {
		if retErr != nil {
			if errors.Is(retErr, app.ErrHistoryControlUnknown) {
				retErr = app.ErrHistoryControlUnknown
			} else {
				retErr = app.ErrHistoryControlRejected
			}
		}
	}()
	required := []string{"session_id", "source_branch_id", "source_tip_id", "target_branch_id", "target_tip_id", "name"}
	if req.Type == "history_branch_rename" {
		required = append(required, "old_name")
	}
	if err := validateManagedRuntimeParams(req, required, 8192); err != nil {
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
		return app.ErrHistoryControlRejected
	}
	var result any
	switch req.Type {
	case "history_branch_fork":
		var p protocol.RPCManagedBranchForkParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.ManagedBranchFork(ctx, p)
		if err != nil {
			return err
		}
		result = value
	case "history_session_fork":
		var p protocol.RPCManagedSessionForkParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.ManagedSessionFork(ctx, p)
		if err != nil {
			return err
		}
		result = value
	case "history_branch_rename":
		var p protocol.RPCManagedBranchRenameParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.ManagedBranchRename(ctx, p)
		if err != nil {
			return err
		}
		result = value
	default:
		return app.ErrHistoryControlRejected
	}
	if err := s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result}); err != nil {
		return app.ErrHistoryControlUnknown
	}
	return nil
}
