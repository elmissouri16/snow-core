package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/userinput"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type userInputOutcome struct {
	response protocol.UserInputResponse
	err      error
}

type tuiAskProvider struct {
	request protocol.UserInputRequest
	calls   int
	results chan string
}

func (p *tuiAskProvider) ID() string                                           { return "tui-ask" }
func (p *tuiAskProvider) ListModels(context.Context) ([]protocol.Model, error) { return nil, nil }
func (p *tuiAskProvider) Chat(_ context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	p.calls++
	if p.calls == 1 {
		arguments, err := json.Marshal(struct {
			Questions []protocol.UserInputQuestion `json:"questions"`
		}{Questions: p.request.Questions})
		if err != nil {
			return nil, err
		}
		return &tuiAskStream{events: []protocol.StreamEvent{
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
	return &tuiAskStream{events: []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}, nil
}

type tuiAskStream struct {
	events []protocol.StreamEvent
	index  int
}

func (s *tuiAskStream) Next(context.Context) (protocol.StreamEvent, error) {
	if s.index >= len(s.events) {
		return protocol.StreamEvent{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}
func (*tuiAskStream) Close() error { return nil }

func startPendingUserInput(t *testing.T, m *Model, request protocol.UserInputRequest) <-chan userInputOutcome {
	t.Helper()
	m.app.EnableUserInputReplies()
	published := make(chan protocol.UserInputRequest, 1)
	unsubscribe := m.app.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvUserInputRequest && event.UserInput != nil {
			published <- *event.UserInput
		}
	})
	defer unsubscribe()
	provider := &tuiAskProvider{request: request, results: make(chan string, 1)}
	model := m.app.Agent.Model()
	model.Provider = provider.ID()
	if err := m.app.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	outcome := make(chan userInputOutcome, 1)
	go func() {
		if err := m.app.Agent.Prompt(context.Background(), "ask the user"); err != nil {
			outcome <- userInputOutcome{err: err}
			return
		}
		result := <-provider.results
		if strings.Contains(result, "declined") {
			outcome <- userInputOutcome{err: userinput.ErrRejected}
			return
		}
		var decoded struct {
			Answers []protocol.UserInputAnswer `json:"answers"`
		}
		if err := json.Unmarshal([]byte(result), &decoded); err != nil {
			outcome <- userInputOutcome{err: err}
			return
		}
		outcome <- userInputOutcome{response: protocol.UserInputResponse{RequestID: request.ID, Answers: decoded.Answers}}
	}()
	select {
	case req := <-published:
		m.startUserInput(req)
	case result := <-outcome:
		t.Fatalf("prompt completed before user-input event: %v", result.err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for user-input event")
	}
	return outcome
}

func awaitUserInput(t *testing.T, outcome <-chan userInputOutcome) userInputOutcome {
	t.Helper()
	select {
	case result := <-outcome:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for user-input resolution")
		return userInputOutcome{}
	}
}

func TestUserInputChoiceThenFreeForm(t *testing.T) {
	m := newModel(context.Background(), appOptionsForUserInputTest())
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.inlineTranscript = true
	m.layout()
	request := protocol.UserInputRequest{ID: "ask-1", Questions: []protocol.UserInputQuestion{
		{ID: "format", Header: "Format", Question: "Which format?", Options: []protocol.UserInputOption{{Label: "JSON", Description: "Machine readable"}, {Label: "Text", Description: "Human readable"}}},
		{ID: "notes", Header: "Notes", Question: "Anything else?"},
	}}
	outcome := startPendingUserInput(t, m, request)

	if view := stripANSI(m.View()); !strings.Contains(view, "JSON") || !strings.Contains(view, "Text") || !strings.Contains(view, "Other") {
		t.Fatalf("inline overlay = %q", view)
	}
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEnter}) // JSON
	if m.userInputIndex != 1 || !m.userInputEditing {
		t.Fatalf("state after choice = index:%d editing:%v", m.userInputIndex, m.userInputEditing)
	}
	if view := stripANSI(m.renderUserInput()); !strings.Contains(view, "Ctrl+J newline") {
		t.Fatalf("free-form overlay = %q", view)
	}
	m.userInputEditor.SetValue("keep comments\nand tests")
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	result := awaitUserInput(t, outcome)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if m.userInputPending || len(result.response.Answers) != 2 || result.response.Answers[0].Answer != "JSON" || result.response.Answers[1].Answer != "keep comments\nand tests" {
		t.Fatalf("pending=%v response=%+v", m.userInputPending, result.response)
	}
}

func TestRenderUserInputSanitizesTerminalControls(t *testing.T) {
	m := newModel(context.Background(), appOptionsForUserInputTest())
	m.width, m.height = 100, 30
	m.startUserInput(protocol.UserInputRequest{ID: "unsafe", Questions: []protocol.UserInputQuestion{{
		ID:       "choice",
		Header:   "Header\x1b[2J",
		Question: "Question\x1b]52;c;Y2xpcGJvYXJk\x07\u009b31m",
		Options: []protocol.UserInputOption{{
			Label:       "Option\x1b[3J",
			Description: "Description\x1b]2;spoofed\x07",
		}},
	}}})

	rendered := m.renderUserInput()
	for _, control := range []string{"\x1b[2J", "\x1b[3J", "\x1b]52", "\x1b]2", "\x07", "\u009b"} {
		if strings.Contains(rendered, control) {
			t.Fatalf("rendered user input retained terminal control %q: %q", control, rendered)
		}
	}
}

func TestInlineUserInputLongQuestionKeepsActionsVisible(t *testing.T) {
	m := newModel(context.Background(), appOptionsForUserInputTest())
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.inlineTranscript = true
	m.layout()
	request := protocol.UserInputRequest{ID: "ask-long", Questions: []protocol.UserInputQuestion{{
		ID: "choice", Header: "Long question", Question: strings.Repeat("extended context ", 55),
		Options: []protocol.UserInputOption{{Label: "Alpha", Description: "First choice"}, {Label: "Beta", Description: "Second choice"}, {Label: "Gamma", Description: "Third choice"}},
	}}}
	outcome := startPendingUserInput(t, m, request)
	m.userInputOption = len(request.Questions[0].Options)
	view := stripANSI(m.View())
	for _, want := range []string{"Alpha", "Beta", "Gamma", "Other", "Esc decline", "…"} {
		if !strings.Contains(view, want) {
			t.Fatalf("long inline question hid %q: %q", want, view)
		}
	}
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := awaitUserInput(t, outcome); !errors.Is(got.err, userinput.ErrRejected) {
		t.Fatalf("escape outcome = %+v", got)
	}
}

func TestUserInputOtherAndEscapeReject(t *testing.T) {
	m := newModel(context.Background(), appOptionsForUserInputTest())
	buildAppForTest(t, m)
	request := protocol.UserInputRequest{ID: "ask-other", Questions: []protocol.UserInputQuestion{{
		ID: "choice", Header: "Choice", Question: "Choose?", Options: []protocol.UserInputOption{{Label: "A", Description: "First"}, {Label: "B", Description: "Second"}},
	}}}
	outcome := startPendingUserInput(t, m, request)
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.userInputEditing {
		t.Fatal("Other did not open the free-form editor")
	}
	m.userInputEditor.SetValue("custom")
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := awaitUserInput(t, outcome); got.err != nil || got.response.Answers[0].Answer != "custom" {
		t.Fatalf("outcome = %+v", got)
	}

	request.ID = "ask-reject"
	outcome = startPendingUserInput(t, m, request)
	m.handleUserInputKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := awaitUserInput(t, outcome); !errors.Is(got.err, userinput.ErrRejected) || m.userInputPending {
		t.Fatalf("outcome=%+v pending=%v", got, m.userInputPending)
	}
}

// Keep model construction terse while still making it explicit that no
// asynchronous startup path is used by these interaction tests.
func appOptionsForUserInputTest() app.Options { return app.Options{} }
