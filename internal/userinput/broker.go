// Package userinput coordinates model-requested questions with interactive
// hosts without coupling the agent loop to a particular UI.
package userinput

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const MaxAnswerBytes = 8 * 1024

var (
	ErrUnavailable = errors.New("interactive user input is unavailable on this surface")
	ErrRejected    = errors.New("user declined to answer")
	ErrClosed      = errors.New("user input broker is closed")
)

// Handler lets an embedding synchronously answer a request. The broker invokes
// it after publishing the normalized user_input_request event.
type Handler func(context.Context, protocol.UserInputRequest) (protocol.UserInputResponse, error)

type result struct {
	response protocol.UserInputResponse
	err      error
}

type pending struct {
	request protocol.UserInputRequest
	result  chan result
}

// Broker owns at most one pending request. Snow executes tool calls serially,
// so a second request indicates a host or lifecycle error.
type Broker struct {
	mu      sync.Mutex
	handler Handler
	manual  bool
	closed  bool
	pending *pending
}

func New(handler Handler) *Broker { return &Broker{handler: handler} }

// EnableManual allows a TUI/RPC client to resolve requests through Reply or
// Reject instead of an in-process callback.
// HasHandler reports whether Ask can resolve without waiting for a manual
// event consumer. It is used to reject dispatcher-reentrant manual waits.
func (b *Broker) HasHandler() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.handler != nil
}

func (b *Broker) EnableManual() {
	b.mu.Lock()
	if !b.closed {
		b.manual = true
	}
	b.mu.Unlock()
}

// Ask registers req, publishes it only after replies are safe, and blocks
// until a handler/client replies, rejects, or the context is cancelled.
func (b *Broker) Ask(ctx context.Context, req protocol.UserInputRequest, publish func(protocol.UserInputRequest)) (protocol.UserInputResponse, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return protocol.UserInputResponse{}, ErrClosed
	}
	if b.handler == nil && !b.manual {
		b.mu.Unlock()
		return protocol.UserInputResponse{}, ErrUnavailable
	}
	if b.pending != nil {
		b.mu.Unlock()
		return protocol.UserInputResponse{}, errors.New("another user input request is already pending")
	}
	p := &pending{request: cloneRequest(req), result: make(chan result, 1)}
	b.pending = p
	handler := b.handler
	b.mu.Unlock()

	if publish != nil {
		publish(cloneRequest(req))
	}
	if handler != nil {
		go func() {
			response, err := handler(ctx, cloneRequest(req))
			if response.RequestID == "" {
				response.RequestID = req.ID
			}
			if err == nil {
				response, err = normalizeResponse(req, response)
			}
			b.deliver(req.ID, response, err)
		}()
	}

	select {
	case resolved := <-p.result:
		return resolved.response, resolved.err
	case <-ctx.Done():
		b.clear(req.ID)
		return protocol.UserInputResponse{}, ctx.Err()
	}
}

// Reply resolves the pending request after validating and normalizing answers.
func (b *Broker) Reply(response protocol.UserInputResponse) error {
	b.mu.Lock()
	p := b.pending
	b.mu.Unlock()
	if p == nil {
		return errors.New("no user input request is pending")
	}
	normalized, err := normalizeResponse(p.request, response)
	if err != nil {
		return err
	}
	if !b.deliver(p.request.ID, normalized, nil) {
		return errors.New("user input request is no longer pending")
	}
	return nil
}

// Reject declines the pending request.
func (b *Broker) Reject(requestID string) error {
	b.mu.Lock()
	p := b.pending
	b.mu.Unlock()
	if p == nil {
		return errors.New("no user input request is pending")
	}
	if requestID != p.request.ID {
		return fmt.Errorf("user input request id %q does not match pending request %q", requestID, p.request.ID)
	}
	if !b.deliver(requestID, protocol.UserInputResponse{}, ErrRejected) {
		return errors.New("user input request is no longer pending")
	}
	return nil
}

// Close releases any blocked request and rejects future ones.
func (b *Broker) Close() {
	b.mu.Lock()
	b.closed = true
	p := b.pending
	b.pending = nil
	b.mu.Unlock()
	if p != nil {
		p.result <- result{err: ErrClosed}
	}
}

func (b *Broker) deliver(requestID string, response protocol.UserInputResponse, err error) bool {
	b.mu.Lock()
	p := b.pending
	if p == nil || p.request.ID != requestID {
		b.mu.Unlock()
		return false
	}
	b.pending = nil
	b.mu.Unlock()
	p.result <- result{response: response, err: err}
	return true
}

func (b *Broker) clear(requestID string) {
	b.mu.Lock()
	if b.pending != nil && b.pending.request.ID == requestID {
		b.pending = nil
	}
	b.mu.Unlock()
}

func normalizeResponse(req protocol.UserInputRequest, response protocol.UserInputResponse) (protocol.UserInputResponse, error) {
	if response.RequestID != req.ID {
		return protocol.UserInputResponse{}, fmt.Errorf("user input request id %q does not match pending request %q", response.RequestID, req.ID)
	}
	provided := make(map[string]string, len(response.Answers))
	for _, answer := range response.Answers {
		id := strings.TrimSpace(answer.QuestionID)
		value := strings.TrimSpace(answer.Answer)
		if id == "" || value == "" {
			return protocol.UserInputResponse{}, errors.New("every user input answer requires a non-empty id and answer")
		}
		if len(value) > MaxAnswerBytes {
			return protocol.UserInputResponse{}, fmt.Errorf("answer %q exceeds %d bytes", id, MaxAnswerBytes)
		}
		if _, duplicate := provided[id]; duplicate {
			return protocol.UserInputResponse{}, fmt.Errorf("duplicate answer id %q", id)
		}
		provided[id] = value
	}
	if len(provided) != len(req.Questions) {
		return protocol.UserInputResponse{}, fmt.Errorf("received %d answers for %d questions", len(provided), len(req.Questions))
	}
	normalized := protocol.UserInputResponse{RequestID: req.ID, Answers: make([]protocol.UserInputAnswer, 0, len(req.Questions))}
	for _, question := range req.Questions {
		value, ok := provided[question.ID]
		if !ok {
			return protocol.UserInputResponse{}, fmt.Errorf("missing answer for question %q", question.ID)
		}
		normalized.Answers = append(normalized.Answers, protocol.UserInputAnswer{QuestionID: question.ID, Answer: value})
	}
	return normalized, nil
}

func cloneRequest(req protocol.UserInputRequest) protocol.UserInputRequest {
	out := req
	out.Questions = make([]protocol.UserInputQuestion, len(req.Questions))
	for i, question := range req.Questions {
		out.Questions[i] = question
		out.Questions[i].Options = slices.Clone(question.Options)
	}
	return out
}
