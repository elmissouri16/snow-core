package rpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func validatePromptContent(message string, content []protocol.ContentBlock) error {
	if len(content) == 0 {
		return errors.New("prompt content must not be empty")
	}
	hasImage := false
	for _, block := range content {
		switch block.Type {
		case protocol.BlockText:
			if block.Text == "" {
				return errors.New("prompt text block cannot be empty")
			}
		case protocol.BlockImage:
			if block.MIMEType == "" {
				return errors.New("prompt image block requires mime_type")
			}
			if len(block.Data) == 0 {
				return errors.New("prompt image block cannot be empty")
			}
			hasImage = true
		default:
			return fmt.Errorf("prompt content block type %q is not allowed", block.Type)
		}
	}
	if message == "" && !hasImage {
		return errors.New("prompt requires a message or an image attachment")
	}
	return nil
}

func hasImageContent(content []protocol.ContentBlock) bool {
	for _, block := range content {
		if block.Type == protocol.BlockImage {
			return true
		}
	}
	return false
}

func (s *Server) handleImagePrompt(ctx context.Context, req Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	active := s.promptDone != nil
	s.mu.Unlock()
	if active {
		return errors.New("rpc: prompt already running; use steer, follow_up, or abort")
	}
	promptCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel = cancel
	s.promptDone = done
	s.mu.Unlock()
	s.write(Response{ID: req.ID, Type: "response", Command: "prompt", Success: true})
	s.promptWG.Add(1)
	go func() {
		defer s.promptWG.Done()
		var err error
		if req.Mode != "" {
			mode, parseErr := protocol.ParseCollaborationMode(req.Mode)
			if parseErr != nil {
				err = parseErr
			} else {
				err = s.app.Agent.PromptContentWithMode(promptCtx, req.Message, req.Content, mode)
			}
		} else {
			err = s.app.Agent.PromptContent(promptCtx, req.Message, req.Content)
		}
		canceled := promptCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		if err != nil && !canceled {
			s.write(Response{ID: req.ID, Type: "response", Command: "prompt", Success: false, Error: err.Error()})
		}
		completed := protocol.RPCPromptCompleted{
			Type:      protocol.RPCTypePromptCompleted,
			RequestID: req.ID,
			Status:    protocol.RPCPromptCompletedStatus,
		}
		if canceled {
			completed.Status = protocol.RPCPromptCanceledStatus
		} else if err != nil {
			completed.Status = protocol.RPCPromptFailedStatus
		}
		if completed.Status == protocol.RPCPromptFailedStatus {
			completed.Error = err.Error()
		}
		s.write(completed)
		close(done)
		s.mu.Lock()
		if s.promptDone == done {
			s.promptDone = nil
			s.cancel = nil
		}
		s.mu.Unlock()
	}()
	return nil
}
