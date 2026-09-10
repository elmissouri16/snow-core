package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func startPluginDialog(t *testing.T) (*Model, <-chan pluginCommandDone) {
	t.Helper()
	testHome(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"snow-plugin.json": `{"id":"dialog","name":"Dialog","version":"1","api_version":2,"entry":"main.js","capabilities":["commands","ui"]}`,
		"main.js":          `snow.registerCommand({name:"ask",description:"Ask",uses:["ui"],async run(_,ctx){return await ctx.ui.input({title:"Write a note"});}});`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opts := app.Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoMCP: true, NoSkills: true, JavaScriptPaths: []string{dir}}
	m := newModel(t.Context(), opts)
	a, err := app.New(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	m.app = a
	t.Cleanup(func() { a.Close() })
	m.width, m.height = 100, 30
	m.plugins = &pluginUIState{infos: m.app.PluginInfos(), commands: m.app.PluginCommands(), running: map[string]bool{}}
	m.app.EnableUserInputReplies()
	published := make(chan protocol.UserInputRequest, 1)
	unsubscribe := m.app.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvUserInputRequest {
			published <- *ev.UserInput
		}
	})
	t.Cleanup(unsubscribe)
	done := make(chan pluginCommandDone, 1)
	_, cmd := m.runPluginCommand("dialog:ask", "")
	if cmd == nil {
		t.Fatal("plugin command unavailable")
	}
	go func() { done <- cmd().(pluginCommandDone) }()
	select {
	case req := <-published:
		m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUserInputRequest, UserInput: &req})
	case <-time.After(3 * time.Second):
		t.Fatal("plugin did not ask for input")
	}
	return m, done
}

func TestPluginDialogSurvivesUnrelatedTurnEvents(t *testing.T) {
	for _, event := range []protocol.AgentEvent{
		{Type: protocol.EvTurnDone},
		{Type: protocol.EvAborted},
		{Type: protocol.EvToolEnd, ToolName: "ask_user", ToolCallID: "unrelated"},
	} {
		t.Run(string(event.Type), func(t *testing.T) {
			m, _ := startPluginDialog(t)
			m.handleAgentEvent(event)
			if !m.userInputPending {
				t.Fatal("unrelated event dismissed the pending plugin dialog")
			}
		})
	}
}

func TestPluginDialogCtrlCReleasesRequest(t *testing.T) {
	m, done := startPluginDialog(t)
	wait := m.waitUserInputSettlement()
	m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	select {
	case result := <-done:
		if result.err == nil {
			t.Fatal("interrupted input succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Ctrl+C left the plugin blocked on an invisible input request")
	}
	m.Update(wait())
	if m.userInputPending {
		t.Fatal("cancelled command left a stale dialog")
	}
}

func TestPluginDialogSettlementDoesNotClearNewerRequest(t *testing.T) {
	m, done := startPluginDialog(t)
	wait := m.waitUserInputSettlement()
	request := *m.userInputRequest
	m.app.CancelPluginCommand("dialog:ask")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled command did not settle")
	}
	settled := wait()
	m.startUserInput(request) // A later request can reuse a tool-call ID.
	m.Update(settled)
	if !m.userInputPending {
		t.Fatal("delayed settlement cleared a newer request")
	}
}

func TestPluginInputOutsideCommandCtrlCDeclinesWithoutQuitting(t *testing.T) {
	m, done := startPluginDialog(t)
	// Settings and readiness dialogs can use the broker without a TUI command.
	clear(m.plugins.running)
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil || m.userInputPending {
		t.Fatal("Ctrl+C did not decline the standalone plugin input")
	}
	select {
	case result := <-done:
		if result.err == nil {
			t.Fatal("declined input succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("standalone input is still blocked")
	}
}

func TestPluginDialogDoesNotPrintGenericAnswerReceipt(t *testing.T) {
	m, done := startPluginDialog(t)
	m.userInputEditor.SetValue("A note")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case result := <-done:
		m.finishPluginCommand(result)
	case <-time.After(time.Second):
		t.Fatal("answered command did not finish")
	}
	frame := stripANSI(m.viewContent())
	if strings.Contains(frame, "question(s)") || !strings.Contains(frame, "A note") {
		t.Fatalf("unexpected plugin feedback: %s", frame)
	}
}

func TestPluginClosedChoicesDoNotOfferCustomAnswer(t *testing.T) {
	testHome(t)
	m := newModel(t.Context(), app.Options{})
	m.width, m.height = 100, 30
	m.startUserInput(protocol.UserInputRequest{ID: "plugin-select", Questions: []protocol.UserInputQuestion{{ID: "answer", Question: "Continue?", ChoicesOnly: true, Options: []protocol.UserInputOption{{Label: "No"}, {Label: "Yes"}}}}})
	if frame := stripANSI(m.renderUserInput()); strings.Contains(frame, "Other") || !strings.Contains(frame, "› No") {
		t.Fatalf("confirmation choices: %s", frame)
	}
	m.handleUserInputKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.userInputOption != 1 || m.userInputEditing {
		t.Fatal("choice navigation reached a custom entry")
	}
	m.handleUserInputKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.userInputOption != 0 {
		t.Fatal("choice navigation did not wrap to No")
	}
}

func TestPluginErrorResultIsVisibleWithoutContent(t *testing.T) {
	testHome(t)
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.plugins = &pluginUIState{running: map[string]bool{"failed:run": true}}
	m.finishPluginCommand(pluginCommandDone{app: m.app, id: "failed:run", result: plugin.ToolResult{IsError: true}})
	if !strings.Contains(stripANSI(m.viewContent()), "failed:run: command failed") || m.plugins.running["failed:run"] {
		t.Fatal("error result was presented as silent success")
	}
}

func TestPluginCompletionCannotRestorePreviousBranchUI(t *testing.T) {
	m, done := startPluginDialog(t)
	m.plugins.generation = m.app.PluginGeneration()
	m.plugins.views = []protocol.PluginView{{ID: "dialog:old", Placement: "screen", Content: &protocol.PluginNode{Type: "text", Text: "Old panel"}}}
	m.plugins.screen = "dialog:old"
	m.userInputEditor.SetValue("Old branch note")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	var result pluginCommandDone
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("answered command did not finish")
	}
	if _, err := m.app.ForkBranchWithOptions(protocol.BranchForkOptions{Name: "new branch"}); err != nil {
		t.Fatal(err)
	}
	m.finishPluginCommand(result)
	if m.plugins.screen != "" || strings.Contains(stripANSI(m.viewContent()), "Old branch note") {
		t.Fatal("late command completion retained UI from the previous branch")
	}
}
