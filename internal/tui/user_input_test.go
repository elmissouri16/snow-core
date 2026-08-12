package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/userinput"
	"github.com/snow-core/snow/pkg/protocol"
)

type userInputOutcome struct {
	response protocol.UserInputResponse
	err      error
}

func startPendingUserInput(t *testing.T, m *Model, request protocol.UserInputRequest) <-chan userInputOutcome {
	t.Helper()
	m.app.EnableUserInputReplies()
	published := make(chan protocol.UserInputRequest, 1)
	unsubscribe := m.app.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvUserInputRequest && event.UserInput != nil {
			published <- *event.UserInput
		}
	})
	t.Cleanup(unsubscribe)
	outcome := make(chan userInputOutcome, 1)
	go func() {
		response, err := m.app.RequestUserInput(context.Background(), request)
		outcome <- userInputOutcome{response: response, err: err}
	}()
	select {
	case req := <-published:
		m.startUserInput(req)
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

func TestInlineUserInputLongQuestionKeepsActionsVisible(t *testing.T) {
	m := newModel(context.Background(), appOptionsForUserInputTest())
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.inlineTranscript = true
	m.layout()
	request := protocol.UserInputRequest{ID: "ask-long", Questions: []protocol.UserInputQuestion{{
		ID: "choice", Header: "Long question", Question: strings.Repeat("extended context ", 55),
		Options: []protocol.UserInputOption{{Label: "Alpha"}, {Label: "Beta"}, {Label: "Gamma"}},
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
