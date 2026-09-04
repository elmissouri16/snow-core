package rpc

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type rpcAskProvider struct {
	request protocol.UserInputRequest
	calls   int
	results chan string
}

func (p *rpcAskProvider) ID() string                                           { return "rpc-ask" }
func (p *rpcAskProvider) ListModels(context.Context) ([]protocol.Model, error) { return nil, nil }
func (p *rpcAskProvider) Chat(_ context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	p.calls++
	if p.calls == 1 {
		arguments, err := json.Marshal(struct {
			Questions []protocol.UserInputQuestion `json:"questions"`
		}{Questions: p.request.Questions})
		if err != nil {
			return nil, err
		}
		return &rpcEventStream{events: []protocol.StreamEvent{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: p.request.ID, ToolName: "ask_user", Arguments: arguments},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		}}, nil
	}
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == protocol.RoleTool && len(request.Messages[i].Content) != 0 {
			p.results <- request.Messages[i].Content[0].Text
			break
		}
	}
	return &rpcGateStream{}, nil
}

type rpcEventStream struct {
	events []protocol.StreamEvent
	index  int
}

func (s *rpcEventStream) Next(context.Context) (protocol.StreamEvent, error) {
	if s.index >= len(s.events) {
		return protocol.StreamEvent{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}
func (*rpcEventStream) Close() error { return nil }

func startRPCAsk(t *testing.T, app *app.App, request protocol.UserInputRequest) (*rpcAskProvider, <-chan error) {
	t.Helper()
	provider := &rpcAskProvider{request: request, results: make(chan string, 1)}
	model := app.Agent.Model()
	model.Provider = provider.ID()
	if err := app.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- app.Agent.Prompt(context.Background(), "ask the user")
	}()
	return provider, done
}
