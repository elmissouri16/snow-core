package rpc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var errBranchVersionsRejected = errors.New("branch versions request rejected; refresh exact version identities")

const branchVersionFrameBytes = 2 << 20

func isBranchVersionCommand(command string) bool {
	return command == "branches_page" || command == "branch_messages_page" || command == "branch_restore_prepare" || command == "branch_restore_commit"
}
func validateBranchVersionParams(req Request) error {
	if len(req.Params) == 0 || len(req.Params) > 8192 {
		return errBranchVersionsRejected
	}
	wire, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := validateMessageEditFrame(wire); err != nil {
		return err
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(req.Params, &fields); err != nil {
		return err
	}
	required := []string{"session_id"}
	allowed := []string{}
	switch req.Type {
	case "branches_page":
		allowed = []string{"limit", "cursor"}
	case "branch_messages_page":
		required = append(required, "branch_id", "tip_id")
		allowed = []string{"limit", "cursor"}
	case "branch_restore_prepare":
		required = append(required, "source_branch_id", "source_tip_id", "target_branch_id", "target_tip_id")
	case "branch_restore_commit":
		required = append(required, "restore_token")
	default:
		return errBranchVersionsRejected
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return errBranchVersionsRejected
		}
	}
	known := make(map[string]bool)
	for _, key := range append(required, allowed...) {
		known[key] = true
	}
	for key, value := range fields {
		if !known[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errBranchVersionsRejected
		}
		if key == "limit" {
			var n int
			maximum := protocol.RPCBranchesPageMaxItems
			if req.Type == "branch_messages_page" {
				maximum = protocol.RPCBranchMessagesPageMaxItems
			}
			if json.Unmarshal(value, &n) != nil || n < 1 || n > maximum {
				return errBranchVersionsRejected
			}
			continue
		}
		var text string
		maximum := 256
		if key == "cursor" {
			maximum = 2048
		}
		if json.Unmarshal(value, &text) != nil || len(text) > maximum || strings.ContainsRune(text, 0) {
			return errBranchVersionsRejected
		}
		emptyAllowed := strings.HasSuffix(key, "tip_id") || key == "cursor"
		if text == "" && !emptyAllowed {
			return errBranchVersionsRejected
		}
	}
	return nil
}
func (s *Server) handleBranchVersions(ctx context.Context, req Request) (retErr error) {
	defer func() {
		if retErr == nil {
			return
		}
		if errors.Is(retErr, app.ErrBranchRestoreUnknown) {
			retErr = app.ErrBranchRestoreUnknown
		} else if strings.HasPrefix(req.Type, "branch_restore_") {
			retErr = app.ErrBranchRestoreRejected
		} else {
			retErr = errBranchVersionsRejected
		}
	}()
	if err := validateBranchVersionParams(req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if strings.HasPrefix(req.Type, "branch_restore_") {
		s.promptLifecycleMu.Lock()
		defer s.promptLifecycleMu.Unlock()
		s.mu.Lock()
		busy := s.promptDone != nil
		s.mu.Unlock()
		if busy {
			return app.ErrBranchRestoreRejected
		}
	}
	var result any
	switch req.Type {
	case "branches_page":
		var p protocol.RPCBranchesPageParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.BranchesPage(ctx, p)
		if err != nil {
			return err
		}
		result = value
	case "branch_messages_page":
		var p protocol.RPCBranchMessagesPageParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.BranchMessagesPage(ctx, p)
		if err != nil {
			return err
		}
		value, err = boundVersionHistory(value, false)
		if err != nil {
			return err
		}
		result = value
	case "branch_restore_prepare":
		var p protocol.RPCBranchRestorePrepareParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		value, err := s.app.PrepareBranchRestore(ctx, p)
		if err != nil {
			return err
		}
		result = value
	case "branch_restore_commit":
		var p protocol.RPCBranchRestoreCommitParams
		if err := json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		return s.app.CommitBranchRestore(ctx, p, func(value protocol.RPCBranchRestoreCommitted, messages []protocol.Message) (func() error, error) {
			start := max(0, len(messages)-protocol.RPCBranchMessagesPageMaxItems)
			history := protocol.RPCBranchMessagesPage{SessionID: value.SessionID, BranchID: value.BranchID, TipID: value.TipID, Messages: messages[start:], Start: start, Total: len(messages)}
			allTools, truncated := protocol.ProjectHistoryTools(messages)
			history.HistoryTools = make(map[string][]protocol.RPCHistoryTool)
			for _, message := range history.Messages {
				if tools, ok := allTools[message.ID]; ok {
					history.HistoryTools[message.ID] = tools
				}
			}
			history.HistoryToolsTruncated = truncated
			var err error
			value.History, err = boundVersionHistory(history, true)
			if err != nil {
				return nil, err
			}
			settings, err := s.app.RPCSettings()
			if err != nil {
				return nil, err
			}
			settings.Thinking = s.app.Agent.BranchRestoreThinking(value.Mode)
			value.Settings = rpcSettings(settings)
			response := Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: value}
			if err := checkVersionFrame(response); err != nil {
				return nil, err
			}
			return func() error { return s.write(response) }, nil
		})
	}
	response := Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result}
	if err := checkVersionFrame(response); err != nil {
		return err
	}
	return s.write(response)
}
func checkVersionFrame(value any) error {
	wire, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(wire) > branchVersionFrameBytes {
		return errBranchVersionsRejected
	}
	return nil
}
func boundVersionHistory(page protocol.RPCBranchMessagesPage, suffix bool) (protocol.RPCBranchMessagesPage, error) {
	page.Messages = publicHistoryMessages(page.Messages)
	// Reserve 64 KiB for the RPC envelope and effective settings. Reduce only
	// whole messages; never truncate an identity or silently redact public text.
	for {
		wire, err := json.Marshal(page)
		if err != nil {
			return page, err
		}
		if len(wire) <= branchVersionFrameBytes-(64<<10) {
			return page, nil
		}
		if len(page.Messages) <= 1 {
			return protocol.RPCBranchMessagesPage{}, errBranchVersionsRejected
		}
		if suffix {
			delete(page.HistoryTools, page.Messages[0].ID)
			page.Messages = page.Messages[1:]
			page.Start++
		} else {
			delete(page.HistoryTools, page.Messages[len(page.Messages)-1].ID)
			page.Messages = page.Messages[:len(page.Messages)-1]
			page.NextCursor = app.VersionMessageCursor(page.SessionID, page.BranchID, page.TipID, page.Start+len(page.Messages))
		}
	}
}
