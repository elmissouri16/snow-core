package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type interactiveStubHost struct {
	stubHost
	request protocol.UserInputRequest
	answer  protocol.UserInputResponse
	err     error
}

func (h *interactiveStubHost) RequestUserInput(_ context.Context, request protocol.UserInputRequest) (protocol.UserInputResponse, error) {
	h.request = request
	return h.answer, h.err
}

func TestAskUserReturnsOrderedJSONAnswers(t *testing.T) {
	tool := NewAskUser()
	if tool.Schema().Discovery != nil {
		t.Fatal("ask_user must be a direct, always-loaded tool")
	}
	host := &interactiveStubHost{answer: protocol.UserInputResponse{Answers: []protocol.UserInputAnswer{
		{QuestionID: "format", Answer: "JSON"},
		{QuestionID: "notes", Answer: "keep comments"},
	}}}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"questions":[{"id":"format","header":"Format","question":"Which format?","options":[{"label":"JSON","description":"Machine readable"},{"label":"Text","description":"Human readable"}]},{"id":"notes","header":"Notes","question":"Anything else?"}]}`), host)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if len(host.request.Questions) != 2 || host.request.Questions[0].Options[0].Label != "JSON" {
		t.Fatalf("request = %+v", host.request)
	}
	if got, want := result.Content[0].Text, `{"answers":[{"id":"format","answer":"JSON"},{"id":"notes","answer":"keep comments"}]}`; got != want {
		t.Fatalf("output = %s, want %s", got, want)
	}
}

func TestAskUserAcceptsNilContext(t *testing.T) {
	host := &interactiveStubHost{answer: protocol.UserInputResponse{Answers: []protocol.UserInputAnswer{{QuestionID: "name", Answer: "Snow"}}}}
	result, err := NewAskUser().Run(nil, json.RawMessage(`{"questions":[{"id":"name","header":"Name","question":"What name?"}]}`), host)
	if err != nil || result.IsError {
		t.Fatalf("nil-context ask_user = %+v, err=%v", result, err)
	}
}

func TestAskUserUnavailableWithoutInteractiveHost(t *testing.T) {
	result, err := NewAskUser().Run(context.Background(), json.RawMessage(`{"questions":[{"id":"name","header":"Name","question":"What name?"}]}`), stubHost{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "unavailable") {
		t.Fatalf("result = %+v", result)
	}
}

func TestAskUserValidatesQuestions(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{name: "empty", args: `{"questions":[]}`, want: "between 1 and 3"},
		{name: "id", args: `{"questions":[{"id":"Bad ID","header":"H","question":"Q"}]}`, want: "invalid id"},
		{name: "duplicate", args: `{"questions":[{"id":"same","header":"H","question":"Q"},{"id":"same","header":"H2","question":"Q2"}]}`, want: "duplicate"},
		{name: "one option", args: `{"questions":[{"id":"pick","header":"H","question":"Q","options":[{"label":"A","description":"First"}]}]}`, want: "2-3"},
		{name: "reserved other", args: `{"questions":[{"id":"pick","header":"H","question":"Q","options":[{"label":"A","description":"First"},{"label":"Other","description":"Custom"}]}]}`, want: "reserved"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewAskUser().Run(context.Background(), json.RawMessage(tc.args), &interactiveStubHost{})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(result.Content[0].Text, tc.want) {
				t.Fatalf("result = %+v, want %q", result, tc.want)
			}
		})
	}
}

var _ tools.UserInputHost = (*interactiveStubHost)(nil)
