package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestBusyComposerChoosesSteerAndFollowUp(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true

	m.editor.SetValue("change direction")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("busy Enter did not submit a steer")
	}
	msg, ok := cmd().(queueSubmitMsg)
	if !ok || msg.kind != protocol.QueuedInputSteer || !msg.fallback {
		t.Fatalf("busy Enter result = %#v", msg)
	}

	m.editor.SetValue("afterwards")
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if cmd == nil {
		t.Fatal("busy Alt+Enter did not submit a follow-up")
	}
	msg, ok = cmd().(queueSubmitMsg)
	if !ok || msg.kind != protocol.QueuedInputFollowUp || !msg.fallback {
		t.Fatalf("busy Alt+Enter result = %#v", msg)
	}
}

func TestQueueSettleFallbackStartsOnlyAfterTurnDone(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	pendingMode := protocol.ModePlan
	m.pendingMode = &pendingMode
	m.editor.SetValue("boundary input")
	_, cmd := m.Update(queueSubmitMsg{kind: protocol.QueuedInputSteer, text: "boundary input", expanded: "boundary input", epoch: m.queueEpoch, fallback: true})
	if cmd != nil || len(m.queueFallbacks) != 1 || !m.busy {
		t.Fatalf("fallback started before turn_done: cmd=%v fallbacks=%d busy=%v", cmd != nil, len(m.queueFallbacks), m.busy)
	}
	_, cmd = m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{Type: protocol.EvTurnDone, TurnID: "old"}}})
	if cmd == nil || len(m.queueFallbacks) != 0 || !m.busy || m.modeSwitching || m.pendingMode == nil {
		t.Fatalf("fallback did not preserve boundary state: cmd=%v fallbacks=%d busy=%v switching=%v pending=%v", cmd != nil, len(m.queueFallbacks), m.busy, m.modeSwitching, m.pendingMode != nil)
	}
}

func TestMultipleSettleFallbacksRemainOrderedAndRestorable(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.queueEpoch = 4
	for _, text := range []string{"first", "second"} {
		_, _ = m.Update(queueSubmitMsg{kind: protocol.QueuedInputSteer, text: text, expanded: text, fallback: true, epoch: m.queueEpoch})
	}
	if len(m.queueFallbacks) != 2 {
		t.Fatalf("fallbacks=%d, want 2", len(m.queueFallbacks))
	}
	m.editor.SetValue("draft")
	m.requestAbort()
	if got, want := m.editor.Value(), "first\n\nsecond\n\ndraft"; got != want {
		t.Fatalf("restored fallback text=%q want %q", got, want)
	}
	if len(m.queueFallbacks) != 0 {
		t.Fatalf("fallbacks remain after abort: %+v", m.queueFallbacks)
	}
}

func TestAcceptedQueueAckRendersAfterImmediateDelivery(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.queueEpoch = 2
	_, _ = m.Update(queueSubmitMsg{itemID: "already-delivered", kind: protocol.QueuedInputSteer, text: "fast", expanded: "fast", accepted: true, epoch: m.queueEpoch})
	transcript := stripANSI(strings.Join(m.lines, "\n"))
	if !strings.Contains(transcript, "queued steer: fast") {
		t.Fatalf("accepted submission missing transcript row: %q", transcript)
	}
	if _, retained := m.queueOriginalText["already-delivered"]; retained {
		t.Fatal("delivered submission retained as abort-restorable")
	}
}

func TestAbortInvalidatesLateQueueSubmission(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	m.editor.SetValue("do not restart")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("missing queue command")
	}
	m.requestAbort()
	msg := cmd().(queueSubmitMsg)
	_, follow := m.Update(msg)
	if follow != nil || m.editor.Value() != "do not restart" {
		t.Fatalf("late queue result restarted/dropped text: follow=%v editor=%q", follow != nil, m.editor.Value())
	}
}

func TestQueueEventRenderingAndAbortRestoration(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.busy = true
	m.runStartedAt = m.currentTime()
	queue := &protocol.InputQueue{Items: []protocol.QueuedInput{
		{ID: "q1", Kind: protocol.QueuedInputSteer, Text: "first", Order: 1},
		{ID: "q2", Kind: protocol.QueuedInputFollowUp, Text: "second", Order: 2},
	}}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvQueueUpdated, Queue: queue})
	view := stripANSI(strings.Join(m.lines, "\n"))
	if !strings.Contains(view, "queued steer: first") || !strings.Contains(view, "queued follow-up: second") {
		t.Fatalf("queue rows = %q", view)
	}
	if got := stripANSI(m.renderRunStatus()); !strings.Contains(got, "2 queued") {
		t.Fatalf("run status = %q", got)
	}
	m.editor.SetValue("draft")
	m.restoreAbortedInputs(*queue, nil, m.editor.Value())
	if got := m.editor.Value(); got != "first\n\nsecond\n\ndraft" {
		t.Fatalf("restored composer = %q", got)
	}
}

func TestAbortRestoresCompactMentionTextFromAcceptedRace(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := os.WriteFile(filepath.Join(m.app.CWD(), "notes.md"), []byte("expanded file body"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.mentionFiles = []string{"notes.md"}
	original := "inspect @notes.md"
	expanded := m.expandedPrompt(original)
	if expanded == original || !strings.Contains(expanded, "expanded file body") {
		t.Fatalf("mention was not expanded: %q", expanded)
	}
	m.queueAttempts = []queuedTUIAttempt{{kind: protocol.QueuedInputSteer, text: original, expanded: expanded, epoch: m.queueEpoch}}
	queue := protocol.InputQueue{Items: []protocol.QueuedInput{{ID: "accepted", Kind: protocol.QueuedInputSteer, Text: expanded, Order: 1}}}
	m.restoreAbortedInputs(queue, nil, "draft")
	if got := m.editor.Value(); got != original+"\n\ndraft" {
		t.Fatalf("restored composer = %q", got)
	}
}

func TestProjectTrustBootstrapPromptsEveryUnknownProject(t *testing.T) {
	home := testHome(t)
	cwd := t.TempDir()
	m := newModel(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: cwd})
	msg := m.bootstrapCmd()()
	prompt, ok := msg.(trustPromptMsg)
	if !ok {
		t.Fatalf("bootstrap result = %T %#v, want trustPromptMsg", msg, msg)
	}
	if prompt.path == "" || prompt.store == nil {
		t.Fatalf("trust prompt = %+v", prompt)
	}
	_, _ = m.Update(prompt)
	if !m.trustPending || m.trustChoice != 0 {
		t.Fatalf("trust state = pending:%v choice:%d", m.trustPending, m.trustChoice)
	}
	m.width, m.height = 100, 30
	if view := stripANSI(m.View()); !strings.Contains(view, "Continue untrusted") || !strings.Contains(view, "not a sandbox") {
		t.Fatalf("trust view = %q", view)
	}

	// Safe-default Enter persists exact deny and constructs the app immediately.
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("trust Enter returned no command")
	}
	decision, ok := cmd().(trustDecisionMsg)
	if !ok || decision.err != nil || decision.app == nil {
		t.Fatalf("trust decision = %T %+v", decision, decision)
	}
	defer decision.app.Close()
	_, _ = m.Update(decision)
	if m.trustPending || m.app == nil {
		t.Fatalf("trust decision did not attach app: pending=%v app=%v", m.trustPending, m.app != nil)
	}
	_, _, trustPath := appConfigPathsForTest()
	info, err := os.Stat(trustPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("trust file under %s = %+v, %v", home, info, err)
	}
	store, err := trust.New(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if level, ok := store.Get(cwd); !ok || level != trust.LevelDeny {
		t.Fatalf("saved decision = %q %v", level, ok)
	}
}

func TestTrustPersistenceFailureKeepsPromptActive(t *testing.T) {
	testHome(t)
	cwd := t.TempDir()
	parent := t.TempDir()
	store, err := trust.New(filepath.Join(parent, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: cwd})
	_, _ = m.Update(trustPromptMsg{path: cwd, store: store})
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result := cmd().(trustDecisionMsg)
	if result.err == nil {
		t.Fatal("unwritable trust store unexpectedly succeeded")
	}
	_, _ = m.Update(result)
	if !m.trustPending || m.trustError == "" {
		t.Fatalf("persistence failure state = pending:%v error:%q", m.trustPending, m.trustError)
	}
}

func TestCloseReclaimsBackgroundConstructedApps(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	candidate := &app.App{}
	if !m.retainStartupApp(candidate) {
		t.Fatal("open model rejected startup app")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m.startupMu.Lock()
	closed, remaining := m.startupClosed, len(m.startupApps)
	m.startupMu.Unlock()
	if !closed || remaining != 0 {
		t.Fatalf("startup ownership after close: closed=%v remaining=%d", closed, remaining)
	}
	if m.retainStartupApp(&app.App{}) {
		t.Fatal("closed model accepted late startup app")
	}
}

func TestTrustPromptCannotExitAfterPersistenceStarts(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.trustPending = true
	m.trustSaving = true
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil {
		t.Fatal("Ctrl+C exited while trust persistence owned an in-flight app result")
	}
}

func TestTrustEscapeDeniesAndControlExitsWithoutDecision(t *testing.T) {
	testHome(t)
	cwd := t.TempDir()
	m := newModel(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: cwd})
	prompt := m.bootstrapCmd()().(trustPromptMsg)
	_, _ = m.Update(prompt)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C did not quit trust prompt")
	}
	if _, ok := prompt.store.Get(cwd); ok {
		t.Fatal("Ctrl+C recorded a trust decision")
	}

	m2 := newModel(context.Background(), m.opts)
	_, _ = m2.Update(prompt)
	_, cmd = m2.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	decision := cmd().(trustDecisionMsg)
	if decision.app != nil {
		defer decision.app.Close()
	}
	if decision.err != nil {
		t.Fatal(decision.err)
	}
	if level, ok := prompt.store.Get(cwd); !ok || level != trust.LevelDeny {
		t.Fatalf("Esc decision = %q %v", level, ok)
	}
}

// appConfigPathsForTest keeps this test independent of unexported config path
// helpers while SNOW_HOME is isolated by testHome.
func appConfigPathsForTest() (string, string, string) {
	home := os.Getenv("SNOW_HOME")
	return filepath.Join(home, "config.json"), filepath.Join(home, "auth.json"), filepath.Join(home, "trust.json")
}
