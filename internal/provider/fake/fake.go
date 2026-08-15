// Package fake provides a deterministic provider for tests, examples, and
// headless demos. It replays a scripted step sequence as stream events and
// can optionally record every ChatRequest for assertions.
package fake

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/snow-core/snow/pkg/protocol"
)

// StepKind enumerates scripted stream steps.
type StepKind string

const (
	StepText     StepKind = "text"
	StepThinking StepKind = "thinking"
	StepToolCall StepKind = "tool_call"
	StepUsage    StepKind = "usage"
	StepDone     StepKind = "done"
	StepError    StepKind = "error"
)

// Step is one scripted provider event.
type Step struct {
	Kind       StepKind
	Text       string
	Thinking   string
	ToolCallID string
	ToolName   string
	Arguments  json.RawMessage
	Usage      *protocol.Usage
	Stop       protocol.StopReason
	Err        error
}

// Provider is a deterministic fake implementing provider.Provider.
type Provider struct {
	mu     sync.Mutex
	models []protocol.Model
	script []Step // replayed on every Chat call
	calls  []protocol.ChatRequest
	count  int
	record bool
}

// New returns a fake that replays script on every Chat call.
func New(script []Step) *Provider {
	return &Provider{
		models: defaultModels(),
		script: script,
	}
}

// NewWithModels returns a fake with a custom model catalog and an empty
// script (Chat yields done immediately).
func NewWithModels(models []protocol.Model) *Provider {
	if models == nil {
		models = defaultModels()
	}
	return &Provider{models: models}
}

// NewRecorded returns a fake that records every ChatRequest and replays an
// empty script. Use RecordedCalls to inspect captured requests.
func NewRecorded() *Provider {
	return &Provider{
		models: defaultModels(),
		record: true,
	}
}

func defaultModels() []protocol.Model {
	return []protocol.Model{
		{
			Provider:      "fake",
			ID:            "fake-1",
			DisplayName:   "Fake Model 1",
			ContextWindow: 128000,
			SupportsTools: true,
		},
	}
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return "fake" }

// ListModels implements provider.Provider.
func (p *Provider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]protocol.Model, len(p.models))
	copy(out, p.models)
	return out, nil
}

// Chat implements provider.Provider. It replays the script for this call
// index (all calls share the script from New) as an EventStream, and records
// the request when the provider is in recorded mode.
func (p *Provider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	p.count++
	if p.record {
		p.calls = append(p.calls, req)
	}
	script := p.script
	p.mu.Unlock()
	return &stream{ctx: ctx, steps: script}, nil
}

// RecordedCalls returns the ChatRequests captured in recorded mode,
// in call order. The returned slice is a copy.
func (p *Provider) RecordedCalls() []protocol.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]protocol.ChatRequest, len(p.calls))
	copy(out, p.calls)
	return out
}

// CallCount returns the number of Chat invocations.
func (p *Provider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

// stream replays a script as protocol.StreamEvent values.
type stream struct {
	ctx   context.Context
	steps []Step
	pos   int
	done  bool
}

// Next implements protocol.EventStream. Scripts that omit an explicit done
// step receive one deterministic normal terminal event before io.EOF.
func (s *stream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.ctx != nil && s.ctx.Err() != nil {
		return protocol.StreamEvent{}, s.ctx.Err()
	}
	if ctx != nil && ctx.Err() != nil {
		return protocol.StreamEvent{}, ctx.Err()
	}
	if s.done {
		return protocol.StreamEvent{}, io.EOF
	}
	if s.pos >= len(s.steps) {
		if !s.done {
			s.done = true
			return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}, nil
		}
		return protocol.StreamEvent{}, io.EOF
	}
	step := s.steps[s.pos]
	s.pos++
	ev := stepToEvent(step)
	if ev.Type == protocol.EvStreamDone {
		s.done = true
	}
	return ev, nil
}

// Close implements protocol.EventStream.
func (s *stream) Close() error {
	s.done = true
	return nil
}

func stepToEvent(step Step) protocol.StreamEvent {
	ev := protocol.StreamEvent{
		ToolCallID: step.ToolCallID,
		ToolName:   step.ToolName,
		Arguments:  step.Arguments,
		Usage:      step.Usage,
		StopReason: step.Stop,
		Err:        step.Err,
	}
	switch step.Kind {
	case StepText:
		ev.Type = protocol.EvStreamTextDelta
		ev.Text = step.Text
	case StepThinking:
		ev.Type = protocol.EvStreamThinkingDelta
		ev.Text = step.Thinking
	case StepToolCall:
		ev.Type = protocol.EvStreamToolCallDone
		if ev.ToolCallID == "" {
			ev.ToolCallID = "tool_0"
		}
	case StepUsage:
		ev.Type = protocol.EvStreamUsage
	case StepDone:
		ev.Type = protocol.EvStreamDone
		if ev.StopReason == "" {
			ev.StopReason = protocol.StopStop
		}
	case StepError:
		ev.Type = protocol.EvStreamError
		if ev.Err == nil {
			ev.Err = errors.New("fake: scripted error")
		}
	}
	return ev
}
