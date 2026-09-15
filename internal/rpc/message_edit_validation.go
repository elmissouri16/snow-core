package rpc

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// These commands deliberately have stricter envelopes than legacy RPC. Decode
// the original frame too, so unknown/duplicate or explicitly empty extra fields
// cannot disappear through the compatibility RPCRequest decoder.
func validateMessageEditFrame(frame []byte) error {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(frame, &fields); err != nil {
		return fmt.Errorf("message edit frame: %w", err)
	}
	for key, value := range fields {
		switch key {
		case "id", "type":
			var text string
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return errors.New("message edit envelope strings cannot be null")
			}
			if err := json.Unmarshal(value, &text); err != nil {
				return err
			}
		case "params":
		default:
			return errors.New("message edit accepts only id, type and params")
		}
	}
	if _, ok := fields["params"]; !ok {
		return errors.New("message edit requires params")
	}
	return nil
}

func validateMessageEditParams(req Request) error {
	if len(req.Params) == 0 || len(req.Params) > 512*1024 {
		return errors.New("message edit params are missing or exceed input bound")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(req.Params, &fields); err != nil {
		return fmt.Errorf("message edit params: %w", err)
	}
	required := []string{"session_id", "edit_token", "text"}
	if req.Type == "message_regenerate_commit" {
		required = []string{"session_id", "edit_token"}
	}
	if isMessageRevisionPrepare(req.Type) {
		_, entry := fields["entry_id"]
		_, turn := fields["turn_id"]
		if entry == turn {
			return errors.New("message edit requires exactly one entry_id or turn_id")
		}
		required = []string{"session_id", "entry_id"}
		if turn {
			required[1] = "turn_id"
		}
	}
	if len(fields) != len(required) {
		return errors.New("message edit params contain missing or unsupported fields")
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("message edit requires non-null %s", key)
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("message edit %s must be a string", key)
		}
		if text == "" || (key != "text" && len(text) > 256) {
			return fmt.Errorf("message edit %s is empty or exceeds identity bound", key)
		}
	}
	return nil
}

func messageEditFrameErrorCode(command string) string {
	if isBranchVersionCommand(command) {
		if command == "branch_restore_prepare" || command == "branch_restore_commit" {
			return protocol.RPCBranchRestoreRejectedErrorCode
		}
		return "branch_versions_rejected"
	}
	if isQueueControlCommand(command) {
		return protocol.RPCQueueRejectedErrorCode
	}
	if isMessageRevisionCommit(command) {
		return protocol.RPCMessageEditRejectedErrorCode
	}
	return "invalid"
}

func isMessageRevisionPrepare(command string) bool {
	return command == "message_edit_prepare" || command == "message_regenerate_prepare"
}
func isMessageRevisionCommit(command string) bool {
	return command == "message_edit_commit" || command == "message_regenerate_commit"
}
