package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
)

func processFleetTestModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 36
	m.processFleetOpen = true
	m.processFleetGeneration = 3
	exitCode := 0
	m.processFleetList = []app.ManagedProcessState{
		{ProcessID: "proc_11111111111111111111111111111111", Name: "dev-server", Status: "running", StartedAt: time.Now().Add(-4 * time.Second).UnixMilli(), Ready: true},
		{ProcessID: "proc_22222222222222222222222222222222", Name: "test-watch", Status: "exited", StartedAt: time.Now().Add(-8 * time.Second).UnixMilli(), FinishedAt: time.Now().Add(-2 * time.Second).UnixMilli(), ExitCode: &exitCode},
	}
	m.processFleetOutputID = m.processFleetList[0].ProcessID
	m.processFleetOutput = "listening on :3000\n\x1b[31mraw escape"
	m.processFleetCursorSet = true
	m.processFleetCursor = int64(len(m.processFleetOutput))
	m.processFleetDetailEnd = true
	return m
}

func TestProcessFleetRenderNavigateAndClose(t *testing.T) {
	m := processFleetTestModel(t)
	rendered := m.View()
	plain := stripANSI(rendered)
	if got := strings.Count(rendered, "\n") + 1; got != m.height {
		t.Fatalf("process fleet frame height=%d want=%d", got, m.height)
	}
	for _, want := range []string{"Process fleet inspector", "dev-server", "test-watch", "Combined stdout / stderr", "listening on :3000", `\x1b[31mraw escape`, "1 running"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("process fleet missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(rendered, "\x1b[31mraw escape") {
		t.Fatal("raw process ANSI escape reached terminal output")
	}
	_, cmd := m.handleProcessFleetKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.processFleetIndex != 1 || cmd == nil {
		t.Fatalf("j navigation: index=%d cmd=%v", m.processFleetIndex, cmd != nil)
	}
	_, _ = m.handleProcessFleetKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.processFleetOpen {
		t.Fatal("Esc did not close process fleet")
	}
}

func TestProcessesCommandOpensFleet(t *testing.T) {
	m := processFleetTestModel(t)
	m.closeProcessFleet()
	_, cmd := m.runCommand("/processes dev-server")
	if !m.processFleetOpen || m.processFleetRequested != "dev-server" || cmd == nil {
		t.Fatalf("process command: open=%v target=%q cmd=%v", m.processFleetOpen, m.processFleetRequested, cmd != nil)
	}
}

func TestProcessesCommandOpensDuringActiveTurn(t *testing.T) {
	m := processFleetTestModel(t)
	m.closeProcessFleet()
	m.busy = true
	m.editor.SetValue("/processes")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.processFleetOpen || cmd == nil {
		t.Fatalf("busy process command: open=%v cmd=%v", m.processFleetOpen, cmd != nil)
	}
}

func TestProcessFleetAppliesIncrementalLogsAndFollowsTail(t *testing.T) {
	m := processFleetTestModel(t)
	m.processFleetLogGeneration = 9
	m.processFleetOutput = "first\n"
	m.processFleetCursor = 6
	m.applyProcessFleetLogs(processFleetLogsMsg{
		generation: 9,
		target:     m.processFleetSelectedID(),
		logs:       app.ManagedProcessLogs{ProcessID: m.processFleetSelectedID(), Status: "running", Output: "second\n", NextCursor: 13},
	})
	if m.processFleetOutput != "first\nsecond\n" || m.processFleetCursor != 13 || !m.processFleetDetailEnd {
		t.Fatalf("logs output=%q cursor=%d follow=%v", m.processFleetOutput, m.processFleetCursor, m.processFleetDetailEnd)
	}
}

func TestProcessFleetLogFetchDrainsBoundedBurst(t *testing.T) {
	m := processFleetTestModel(t)
	state, err := m.app.ProcessManager.Start(context.Background(), managedprocess.StartRequest{
		Command:   "head -c 70000 /dev/zero | tr '\\000' x; printf READY; sleep 10",
		Readiness: &managedprocess.ReadinessRequest{Type: "log", Pattern: "READY", TimeoutMS: 2000},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.processFleetList = []app.ManagedProcessState{state}
	m.processFleetIndex = 0
	m.resetProcessFleetOutput()
	cmd := m.loadProcessFleetLogs()
	if cmd == nil {
		t.Fatal("log fetch command is nil")
	}
	msg, ok := cmd().(processFleetLogsMsg)
	if !ok || msg.err != nil {
		t.Fatalf("log fetch message=%T %+v", msg, msg)
	}
	if len(msg.logs.Output) <= processFleetLogReadBytes || len(msg.logs.Output) > processFleetLogReadBytes*processFleetLogBatchChunks {
		t.Fatalf("batched output bytes=%d", len(msg.logs.Output))
	}
}

func TestProcessFleetOutputWrappingIsCached(t *testing.T) {
	m := processFleetTestModel(t)
	first := m.processOutputLines(40)
	second := m.processOutputLines(40)
	if len(first) == 0 || len(second) == 0 || &first[0] != &second[0] {
		t.Fatal("same output and width did not reuse wrapped output")
	}
	version := m.processFleetOutputVersion
	m.processFleetLogGeneration = 7
	m.applyProcessFleetLogs(processFleetLogsMsg{generation: 7, target: m.processFleetSelectedID(), logs: app.ManagedProcessLogs{Output: "next", NextCursor: 4}})
	if m.processFleetOutputVersion == version || m.processFleetWrappedOutput != nil {
		t.Fatal("new output did not invalidate wrapped output cache")
	}
}

func TestProcessFleetRefreshPreservesSelectionAndInvalidatesOnClose(t *testing.T) {
	m := processFleetTestModel(t)
	m.processFleetIndex = 1
	cmd := m.applyProcessFleetList(processFleetListMsg{
		generation: 3,
		// The refresh started while the first row was selected; the user moved
		// to the second row before this stale response arrived.
		target: "proc_11111111111111111111111111111111",
		list:   []app.ManagedProcessState{m.processFleetList[1], m.processFleetList[0]},
	})
	if cmd == nil || m.processFleetSelectedID() != "proc_22222222222222222222222222222222" {
		t.Fatalf("selection was not preserved: %q", m.processFleetSelectedID())
	}
	generation := m.processFleetGeneration
	m.closeProcessFleet()
	m.applyProcessFleetList(processFleetListMsg{generation: generation, list: []app.ManagedProcessState{{ProcessID: "stale"}}})
	if len(m.processFleetList) != 2 {
		t.Fatal("closed fleet accepted stale async list")
	}
}
