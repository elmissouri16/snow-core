package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/supervisor"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestWorktreeSidebarCtrlBTogglesWithoutEditingComposer(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.editor.SetValue("keep this")
	m.worktreeSidebarRequested = false
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = updated.(*Model)
	if !m.worktreeInspectorOpen || !m.worktreeSidebarRequested || !m.worktreeSidebarFocus {
		t.Fatalf("toggle state open=%t requested=%t focus=%t", m.worktreeInspectorOpen, m.worktreeSidebarRequested, m.worktreeSidebarFocus)
	}
	if got := m.editor.Value(); got != "keep this" {
		t.Fatalf("composer = %q", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = updated.(*Model)
	if m.worktreeInspectorOpen || m.worktreeSidebarRequested || m.worktreeSidebarFocus {
		t.Fatalf("second toggle open=%t requested=%t focus=%t", m.worktreeInspectorOpen, m.worktreeSidebarRequested, m.worktreeSidebarFocus)
	}
}

func TestLegacyWorktreeSidebarPreferenceDoesNotAutoOpenDashboard(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.app.Cfg.TUI.WorktreeSidebar = true
	updated, _ := m.Update(doneMsg{app: m.app})
	m = updated.(*Model)
	if m.worktreeInspectorOpen || m.worktreeSidebarRequested || m.worktreeSidebarFocus {
		t.Fatal("legacy sidebar preference automatically opened worktree UI")
	}
}

func TestWorktreeInventoryDoesNotScheduleAutomaticRefresh(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.worktreeInspectorOpen = true
	m.worktreeGeneration = 7
	updated, cmd := m.Update(worktreeInventoryMsg{generation: 7, workspaces: []app.WorktreeWorkspace{{ID: "current", Current: true}}})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("inventory completion scheduled an automatic refresh")
	}
	if m.worktreeLoading {
		t.Fatal("inventory remained loading")
	}
}

func TestWorktreeSidebarResponsiveFrameAndRemoteTarget(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.worktreeSidebarRequested = true
	m.worktreeInspectorOpen = true
	m.worktreeSidebarFocus = true
	m.worktreeWorkspaces = []app.WorktreeWorkspace{
		{ID: "current", Name: "main", Path: m.app.CWD(), Branch: "main", Current: true},
		{ID: "remote", Name: "auth", Path: "/tmp/auth", Branch: "snow/auth"},
	}
	m.layout()
	frame := m.View()
	if lipgloss.Width(frame) != m.managedFrameWidth() || lipgloss.Height(frame) != m.height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d", lipgloss.Width(frame), lipgloss.Height(frame), m.managedFrameWidth(), m.height)
	}
	plain := stripANSI(frame)
	if !strings.Contains(plain, "Worktree agents") || !strings.Contains(plain, "Agents  2") || !strings.Contains(plain, "not attached") {
		t.Fatalf("dashboard missing from frame:\n%s", plain)
	}
	if strings.Contains(plain, "refreshing") || strings.Count(plain, "Refresh") != 1 {
		t.Fatalf("dashboard has noisy refresh UI:\n%s", plain)
	}
	m.worktreeIndex = 1
	frame = stripANSI(m.View())
	if !strings.Contains(frame, "This worktree is not attached") || !strings.Contains(frame, "Enter Chat") {
		t.Fatalf("remote dashboard detail missing:\n%s", frame)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if m.worktreeInspectorOpen || m.worktreeSidebarRequested {
		t.Fatal("Enter did not leave the dashboard for chat")
	}
	if !strings.Contains(stripANSI(m.View()), "→ auth:") {
		t.Fatal("remote composer target missing after Enter")
	}
	rootOffset := m.transcript.YOffset
	beforeDetail := m.worktreeDetailOffset
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.worktreeDetailOffset <= beforeDetail || m.transcript.YOffset != rootOffset {
		t.Fatalf("remote PageUp detail=%d root=%d want root=%d", m.worktreeDetailOffset, m.transcript.YOffset, rootOffset)
	}
	m.worktreeDetailOffset = 0
	_, _ = m.Update(tea.MouseMsg{X: m.worktreeSidebarWidth() + 5, Y: 5, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.worktreeDetailOffset == 0 || m.transcript.YOffset != rootOffset {
		t.Fatalf("remote wheel detail=%d root=%d want root=%d", m.worktreeDetailOffset, m.transcript.YOffset, rootOffset)
	}

	m.width = 80
	m.layout()
	if m.worktreeSidebarVisible() {
		t.Fatal("sidebar remained visible on narrow terminal")
	}
	if cmd := m.openWorktreeSupervisor(); cmd == nil || !m.worktreeInspectorOpen {
		t.Fatalf("narrow /worktrees did not open inspector: cmd=%v open=%t", cmd != nil, m.worktreeInspectorOpen)
	}
	if !strings.Contains(stripANSI(m.View()), "Worktree agents") {
		t.Fatal("narrow inspector missing")
	}
}

func TestWorktreeInputUsesDedicatedEditorAndPreservesRootDraft(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.worktreeWorkspaces = []app.WorktreeWorkspace{{ID: "current", Current: true}, {ID: "remote", Name: "api", Path: "/tmp/api"}}
	m.worktreeIndex = 1
	m.editor.SetValue("unsent root draft")
	id := supervisor.WorkerID("worker-input")
	state := supervisor.WorkerState{
		ID: id, WorkspaceID: "remote", ProcessGeneration: 1, ProcessStatus: supervisor.ProcessReady,
		TurnStatus: supervisor.TurnInputNeeded,
		UserInput:  &protocol.UserInputRequest{ID: "ask-1", Questions: []protocol.UserInputQuestion{{ID: "answer", Header: "Answer", Question: "Value?"}}},
	}
	m.applyWorktreeSupervisorEvent(supervisor.Event{WorkerID: id, Generation: 1, State: &state})
	if handled, _ := m.handleWorktreeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("worker answer")}); !handled {
		t.Fatal("worker input key was not handled")
	}
	if got := m.editor.Value(); got != "unsent root draft" {
		t.Fatalf("root draft = %q", got)
	}
	if !strings.Contains(m.worktreeInputEditor.Value(), "worker answer") {
		t.Fatalf("dedicated worker editor = %q", m.worktreeInputEditor.Value())
	}
	m.resetWorktreeInput()
	if m.worktreeInputRequest != "" || len(m.worktreeInputAnswers) != 0 || m.worktreeInputEditor.Value() != "" {
		t.Fatal("worker input state was not reset")
	}
	if got := m.editor.Value(); got != "unsent root draft" {
		t.Fatalf("root draft after reset = %q", got)
	}
}

func TestWorktreePermissionPreemptsFrameAndStaleGenerationIsIgnored(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 30
	m.worktreeSidebarRequested = true
	m.worktreeWorkspaces = []app.WorktreeWorkspace{
		{ID: "current", Current: true, Path: m.app.CWD()},
		{ID: "remote", Name: "api", Path: "/tmp/api", Branch: "snow/api"},
	}
	m.worktreeIndex = 1
	id := supervisor.WorkerID("worker-api")
	state := supervisor.WorkerState{
		ID: id, WorkspaceID: "remote", ProcessGeneration: 2,
		ProcessStatus: supervisor.ProcessReady, TurnStatus: supervisor.TurnPermission,
		Permission: &protocol.PermissionRequest{ID: "perm-2", Tool: "edit", Args: json.RawMessage(`{"path":"x"}`), Risk: "write"},
	}
	m.applyWorktreeSupervisorEvent(supervisor.Event{WorkerID: id, Generation: 2, State: &state})
	plain := stripANSI(m.View())
	if !strings.Contains(plain, "Worker permission") || !strings.Contains(plain, "/tmp/api") || !strings.Contains(plain, "perm") && !strings.Contains(plain, "allow once") {
		t.Fatalf("permission overlay missing attribution:\n%s", plain)
	}
	stale := state
	stale.ProcessGeneration = 1
	stale.ProcessStatus = supervisor.ProcessCrashed
	m.applyWorktreeSupervisorEvent(supervisor.Event{WorkerID: id, Generation: 1, State: &stale})
	if got := m.worktreeWorkers[id].ProcessStatus; got != supervisor.ProcessReady {
		t.Fatalf("stale generation changed state to %s", got)
	}
}
