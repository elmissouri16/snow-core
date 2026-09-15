package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var errMessageEditRejected = errors.New("message edit rejected before durable replacement")

func (s *Server) handleMessageEdit(ctx context.Context, req Request) (retErr error) {
	defer func() {
		if retErr != nil && isMessageRevisionCommit(req.Type) {
			retErr = errors.Join(errMessageEditRejected, retErr)
		}
	}()
	if req.Message != "" || req.Content != nil || req.Model != "" || req.Thinking != "" || req.ReasoningSummary != "" || req.TextVerbosity != "" || req.Mode != "" || req.Provider != "" || req.Method != "" || req.Secret != "" {
		return errors.New("message edit accepts only id, type and strict params")
	}
	if err := validateMessageEditParams(req); err != nil {
		return err
	}
	if isMessageRevisionPrepare(req.Type) {
		s.mu.Lock()
		busy := s.promptDone != nil
		s.mu.Unlock()
		if busy {
			return errors.New("rpc: message edit requires an idle prompt lifecycle")
		}
		if req.Type == "message_regenerate_prepare" {
			var params protocol.RPCMessageRegeneratePrepareParams
			if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
				return err
			}
			result, err := s.app.PrepareMessageRegenerate(ctx, params)
			if err != nil {
				return err
			}
			return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
		}
		var params protocol.RPCMessageEditPrepareParams
		if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("message_edit_prepare params: %w", err)
		}
		result, err := s.app.PrepareMessageEdit(ctx, params)
		if err != nil {
			return err
		}
		return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
	}
	var params protocol.RPCMessageEditCommitParams
	if req.Type == "message_regenerate_commit" {
		var regenerate protocol.RPCMessageRegenerateCommitParams
		if err := json.Unmarshal(req.Params, &regenerate, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		params.SessionID, params.EditToken = regenerate.SessionID, regenerate.EditToken
	} else {
		if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		if err := session.ValidateMessageEditText(params.Text); err != nil {
			return err
		}
	}
	if params.SessionID == "" || params.EditToken == "" || len(params.EditToken) > 128 {
		return errors.New("message revision requires session_id and edit_token")
	}
	promptCtx, cancel := context.WithCancel(ctx)
	promptCtx, outcome := agent.CapturePromptOutcome(promptCtx)
	done := make(chan struct{})
	s.promptLifecycleMu.Lock()
	defer s.promptLifecycleMu.Unlock()
	s.mu.Lock()
	if s.promptDone != nil {
		s.mu.Unlock()
		cancel()
		return errors.New("rpc: prompt already running; cannot edit history")
	}
	s.cancel = cancel
	s.promptDone = done
	s.mu.Unlock()
	s.promptWG.Go(func() {
		defer cancel()
		acknowledged := false
		acknowledge := func(result protocol.RPCMessageEditCommitted, messages []protocol.Message) error {
			page, err := messageEditHistory(req.ID, messages)
			if err != nil {
				return err
			}
			result.History = page
			err = s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
			acknowledged = err == nil
			return err
		}
		var err error
		if req.Type == "message_regenerate_commit" {
			err = s.app.CommitMessageRegenerate(promptCtx, protocol.RPCMessageRegenerateCommitParams{SessionID: params.SessionID, EditToken: params.EditToken}, acknowledge)
		} else {
			err = s.app.CommitMessageEdit(promptCtx, params, acknowledge)
		}
		if !acknowledged && err != nil && !errors.Is(err, app.ErrMessageEditOutcomeUnknown) {
			err = errors.Join(errMessageEditRejected, err)
		}
		if !acknowledged && err != nil && !errors.Is(err, app.ErrMessageEditOutcomeUnknown) && (promptCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			_ = s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: false, Error: "message edit canceled before admission", ErrorCode: protocol.RPCMessageEditRejectedErrorCode})
		}
		s.completePrompt(req, promptCtx, done, err, outcome)
	})
	return nil
}

// A bounded public suffix includes the replacement even on long conversations.
// It is captured under agent admission, before any replacement provider output.
func messageEditHistory(requestID string, messages []protocol.Message) (protocol.RPCMessagesPage, error) {
	total := len(messages)
	if total == 0 {
		return protocol.RPCMessagesPage{}, errors.New("message edit replacement history is empty")
	}
	start := max(0, total-maxMessagesPageLimit)
	snapshot := snapshotMessagesCursor(messages, start, total)
	snapshot.PublicHistory = true
	for {
		page, err := makeMessagesPage(messages, snapshot, start, total, total)
		if err != nil {
			return page, err
		}
		size, err := messagesPageFrameSize(requestID, page)
		if err != nil {
			return page, err
		}
		if size <= defaultMessagesPageBytes {
			return page, nil
		}
		if start == total-1 {
			return page, errors.New("message edit replacement history exceeds output bound")
		}
		start++
	}
}
