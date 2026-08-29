package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestInitCommandStartsProjectInitializationTurn(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	_, cmd := m.runCommandWithDisplay("/init", "/init")
	if cmd == nil {
		t.Fatal("/init did not start an agent turn")
	}
	transcript := stripANSI(strings.Join(m.lines, "\n"))
	if !strings.Contains(transcript, "› /init") {
		t.Fatalf("live transcript = %q", transcript)
	}
	if strings.Contains(transcript, "Follow this workflow exactly") {
		t.Fatalf("internal init prompt leaked into live transcript: %q", transcript)
	}

	result, ok := cmd().(promptDoneMsg)
	if !ok || result.err != nil || !result.admitted {
		t.Fatalf("prompt result = %T %+v", result, result)
	}
	if result.text != "/init" || result.historyText != "/init" {
		t.Fatalf("display recovery = text %q history %q", result.text, result.historyText)
	}

	messages, err := m.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || messages[0].Role != protocol.RoleUser {
		t.Fatalf("messages = %+v", messages)
	}
	prompt := sessionMessageText(messages[0])
	for _, want := range []string{
		"current working directory",
		"`AGENTS.md` and `.snow/config.json` already exist",
		"Treat the targets independently",
		"Never modify, replace, append to, rename, or delete",
		"inspect the repository",
		"Use only facts verified from the checkout",
		"200–400 words",
		"create it with exactly this content",
		"Do not copy global settings, provider configuration, credentials",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("init prompt missing %q: %q", want, prompt)
		}
	}
}

func TestInitCommandRejectsArguments(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	_, cmd := m.runCommand("/init force")
	if cmd != nil {
		t.Fatal("argument-bearing /init started work")
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "/init takes no arguments") {
		t.Fatalf("transcript = %q", got)
	}
	messages, err := m.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("argument-bearing /init persisted messages: %+v", messages)
	}
}

func TestInitCommandRejectsPlanMode(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.runCommand("/init")
	if cmd != nil {
		t.Fatal("Plan-mode /init started work")
	}
	if m.app.Agent.Mode() != protocol.ModePlan {
		t.Fatalf("mode changed to %q", m.app.Agent.Mode())
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "init: switch to Default mode first") {
		t.Fatalf("transcript = %q", got)
	}
}

func TestBusyInitCommandReportsErrorInsteadOfQueuing(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.editor.SetValue("/init")

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("busy /init returned a command")
	}
	if m.editor.Value() != "" {
		t.Fatalf("busy /init remained in editor: %q", m.editor.Value())
	}
	if got := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(got, "init: wait for the current turn to finish") {
		t.Fatalf("transcript = %q", got)
	}
}
