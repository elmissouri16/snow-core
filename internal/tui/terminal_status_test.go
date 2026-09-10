package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTerminalTitleBoundsAndSanitizesProjectName(t *testing.T) {
	cwd := filepath.Join("/private-parent", "project\x1b]2;spoof\a\n\t\u009b31m"+strings.Repeat("界", 300))
	title := terminalWindowTitle(cwd, terminalRunning)
	if strings.ContainsAny(title, "\x1b\a\n\t\u009b") || strings.Contains(title, "private-parent") || len([]rune(title)) > 85 {
		t.Fatalf("unsafe or unbounded terminal title: %q", title)
	}
	if !strings.HasPrefix(title, "Snow · project") || !strings.HasSuffix(title, " · Running") {
		t.Fatalf("title=%q", title)
	}
}

func TestTerminalStatusLifecycle(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	assertStatus := func(title string, progress tea.ProgressBarState) {
		t.Helper()
		view := m.View()
		if !strings.HasSuffix(view.WindowTitle, " · "+title) || view.ProgressBar == nil || view.ProgressBar.State != progress {
			t.Fatalf("title=%q progress=%+v", view.WindowTitle, view.ProgressBar)
		}
	}
	assertStatus("Ready", tea.ProgressBarNone)
	m.beginOptimisticRun()
	assertStatus("Running", tea.ProgressBarIndeterminate)
	m.compacting = true
	assertStatus("Compacting", tea.ProgressBarIndeterminate)
	m.compacting = false
	m.permPending = true
	assertStatus("Waiting for approval", tea.ProgressBarWarning)
	m.permPending, m.userInputPending = false, true
	assertStatus("Waiting for input", tea.ProgressBarWarning)
	m.userInputPending = false
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "done"})
	assertStatus("Done", tea.ProgressBarNone)
	m.Update(tea.FocusMsg{})
	assertStatus("Ready", tea.ProgressBarNone)
	m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: "failed", Message: "private error"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "failed"})
	assertStatus("Failed", tea.ProgressBarError)
	m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvAborted, TurnID: "stopped"})
	assertStatus("Stopped", tea.ProgressBarNone)
	m.terminal.unfocused = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "stopped"})
	assertStatus("Stopped", tea.ProgressBarNone)
	if terminalTestAlert(m) != "" {
		t.Fatal("canceled turn announced success at its bookkeeping boundary")
	}
	m.Update(tea.FocusMsg{})
	m.terminal.unfocused = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "stopped"})
	if terminalTestAlert(m) != "" || m.View().ProgressBar.State != tea.ProgressBarNone {
		t.Fatal("acknowledging cancellation allowed a late success notification")
	}
}

func TestTerminalHeartbeatUsesCurrentStateAndReusesFrame(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	m.beginOptimisticRun()
	if m.syncTerminalStatus() == nil {
		t.Fatal("busy state did not start a heartbeat")
	}
	tick := terminalProgressTick(m.terminal.generation)
	m.permPending = true
	before := m.View()
	msg := terminalStatusFilter(m, tick)
	raw, ok := msg.(tea.RawMsg)
	if !ok || fmt.Sprint(raw.Msg) != ansi.SetWarningProgressBar(0) {
		t.Fatalf("heartbeat used captured running state: %#v", msg)
	}
	_, cmd := m.Update(msg)
	after := m.View()
	if cmd == nil || !m.terminal.reuseView || after.Content != before.Content || after.WindowTitle != before.WindowTitle {
		t.Fatal("heartbeat did not retain the frame and continue its timer chain")
	}
	if got := testing.AllocsPerRun(100, func() { _ = m.View() }); got != 0 {
		t.Fatalf("cached heartbeat frame allocated %g times", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 16})
	if m.terminal.reuseView || m.View().Content == before.Content {
		t.Fatal("resize reused the old frame")
	}
	m.permPending = false
	m.setRunIdle()
	m.syncTerminalStatus()
	if m.terminal.heartbeat || terminalStatusFilter(m, tick) != nil {
		t.Fatal("late heartbeat resurrected idle progress")
	}
	m.beginOptimisticRun()
	m.syncTerminalStatus()
	if terminalStatusFilter(m, tick) != nil {
		t.Fatal("old timer joined a new timer chain")
	}
	m.app.Cfg.TUI.TerminalProgress = false
	if terminalStatusFilter(m, terminalProgressTick(m.terminal.generation)) != nil || m.View().ProgressBar != nil {
		t.Fatal("disabled progress continued emitting")
	}
}

func TestTerminalHeartbeatTimerCancelsWithModel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	m := newModel(ctx, app.Options{})
	cmd := m.terminalProgressTick()
	cancel()
	if got := cmd(); got != nil {
		t.Fatalf("canceled timer returned %T", got)
	}
}

func terminalTestAlert(m *Model) string {
	msg := terminalStatusFilter(m, terminalAlertMsg(m.terminal.alertID))
	if raw, ok := msg.(tea.RawMsg); ok {
		return fmt.Sprint(raw.Msg)
	}
	return ""
}

func TestTerminalAlertsFollowFocusPolicyAndAreGeneric(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	m.queueTerminalAlert("Snow finished")
	if terminalTestAlert(m) != "" {
		t.Fatal("default policy alerted while focused")
	}
	m.Update(tea.BlurMsg{})
	m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: "one", Message: "secret file /private/project"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "one"})
	want := "\a" + ansi.Notify("Snow encountered an error")
	if got := terminalTestAlert(m); got != want {
		t.Fatalf("alert=%q, want generic error alert", got)
	}
	if terminalTestAlert(m) != "" {
		t.Fatal("alert replayed")
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "one"})
	if terminalTestAlert(m) != "" {
		t.Fatal("duplicate completion alerted twice")
	}
	m.queueTerminalAlert("Snow finished")
	m.Update(tea.FocusMsg{})
	if terminalTestAlert(m) != "" {
		t.Fatal("queued background alert survived refocus")
	}
	m.app.Cfg.TUI.Notifications = "always"
	m.queueTerminalAlert("Snow finished")
	if terminalTestAlert(m) == "" {
		t.Fatal("always policy suppressed focused alert")
	}
	m.queueTerminalAlert("Snow finished")
	m.app.Cfg.TUI.Notifications = "off"
	if terminalTestAlert(m) != "" {
		t.Fatal("off policy emitted a queued alert")
	}
}

func TestTerminalAlertsIgnoreStaleChildSnapshotAndContinuingTurns(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	m.terminal.unfocused = true
	m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "current", TurnSequence: 2, Text: "live"})
	for _, ev := range []protocol.AgentEvent{
		{Type: protocol.EvTurnDone, TurnID: "stale", TurnSequence: 1},
		{Type: protocol.EvTurnDone, TurnID: "child", Agent: &protocol.AgentRef{ThreadID: "child", Path: "/root/child"}},
		{Type: protocol.EvTurnDone, TurnID: "snapshot", Snapshot: true},
		{Type: protocol.EvTurnDone, TurnID: "current", TurnSequence: 2, GoalContinuing: true},
	} {
		m.handleAgentEvent(ev)
		if terminalTestAlert(m) != "" {
			t.Fatalf("unexpected alert for %+v", ev)
		}
	}
	m.queueTerminalAlert("Snow finished")
	old := terminalAlertMsg(m.terminal.alertID)
	m.beginOptimisticRun()
	if terminalStatusFilter(m, old) != nil {
		t.Fatal("completion from earlier work alerted during new turn")
	}
}

func TestTerminalAttentionDeduplicatesRequestIdentity(t *testing.T) {
	m := modelPickerTestModel(t, 60, 16)
	m.terminal.unfocused = true
	m.permPending = true
	m.permRequest = &protocol.PermissionRequest{ID: "one", Tool: "private tool"}
	m.syncTerminalStatus()
	if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow: Waiting for approval") {
		t.Fatalf("permission alert=%q", got)
	}
	m.permRequest = &protocol.PermissionRequest{ID: "one"}
	m.syncTerminalStatus()
	if terminalTestAlert(m) != "" {
		t.Fatal("same permission ID alerted twice")
	}
	m.permRequest = &protocol.PermissionRequest{ID: "two"}
	m.syncTerminalStatus()
	if terminalTestAlert(m) == "" {
		t.Fatal("next serialized permission was not announced")
	}
	m.permPending = false
	m.userInputPending = true
	m.userInputRequest = &protocol.UserInputRequest{ID: "question"}
	m.syncTerminalStatus()
	if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow: Waiting for input") {
		t.Fatalf("input alert=%q", got)
	}
	m.userInputRequest = &protocol.UserInputRequest{ID: "resolved-elsewhere"}
	m.syncTerminalStatus()
	m.userInputPending = false
	m.syncTerminalStatus()
	if terminalTestAlert(m) != "" {
		t.Fatal("resolved input request sent a delayed attention alert")
	}
}

func TestTerminalSettingsRemainVisibleAndPersistAtSmallSizes(t *testing.T) {
	m := modelPickerTestModel(t, 60, 12)
	for _, row := range []int{settingsTerminalTitle, settingsTerminalProgress, settingsNotifications} {
		m.pickSettings, m.settingsIndex = true, row
		if !strings.Contains(stripANSI(m.renderSettings()), "› Terminal") {
			t.Fatalf("terminal setting %d was clipped", row)
		}
		m.cycleSetting(1)
		if m.settingsError != "" {
			t.Fatal(m.settingsError)
		}
	}
	loaded, err := config.Load(m.app.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TUI.TerminalTitle || loaded.TUI.TerminalProgress || loaded.TUI.Notifications != "always" {
		t.Fatalf("settings did not persist: %+v", loaded.TUI)
	}
	view := m.View()
	if view.WindowTitle != "" || view.ProgressBar != nil {
		t.Fatal("disabled settings still configured the terminal")
	}
}
