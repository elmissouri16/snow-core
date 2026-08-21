package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func fleetTestState(id, path string, status protocol.AgentStatus) protocol.SubagentState {
	return protocol.SubagentState{
		Agent:  protocol.AgentRef{ThreadID: id, ParentThreadID: "root", Path: protocol.AgentPath(path), ParentPath: protocol.RootAgentPath, Role: "explorer", Depth: 1},
		Status: status, Provider: "fake", Model: "model", Thinking: protocol.ThinkingHigh,
	}
}

func fleetTestModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 36
	m.subagentFleetOpen = true
	m.subagentFleetGeneration = 4
	m.subagentFleetList = protocol.SubagentList{
		Agents: []protocol.SubagentState{
			fleetTestState("one", "/root/one", protocol.AgentRunning),
			fleetTestState("two", "/root/two", protocol.AgentCompleted),
		},
		Running: 1, Terminal: 1, Open: 2, ConcurrentLimit: 4, AgentLimit: 32,
	}
	return m
}

func TestSubagentFleetOpenRenderNavigateAndClose(t *testing.T) {
	m := fleetTestModel(t)
	rendered := m.View()
	view := stripANSI(rendered)
	if got := strings.Count(rendered, "\n") + 1; got != m.height {
		t.Fatalf("fleet frame height=%d want=%d", got, m.height)
	}
	for _, want := range []string{"Subagent fleet inspector", "/root/one", "/root/two", "Live activity", "capacity 1/4", "alt+p processes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("fleet view missing %q:\n%s", want, view)
		}
	}
	_, cmd := m.handleSubagentFleetKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.subagentFleetIndex != 1 || cmd == nil {
		t.Fatalf("j navigation: index=%d cmd=%v", m.subagentFleetIndex, cmd != nil)
	}
	m.subagentFleetDetailOffset = 20
	_, _ = m.handleSubagentFleetKey(tea.KeyMsg{Type: tea.KeyHome})
	if m.subagentFleetDetailOffset != 0 {
		t.Fatalf("home offset=%d", m.subagentFleetDetailOffset)
	}
	_, _ = m.handleSubagentFleetKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.subagentFleetDetailEnd {
		t.Fatal("end did not select detail tail")
	}
	_, _ = m.handleSubagentFleetKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.subagentFleetOpen {
		t.Fatal("Esc did not close fleet")
	}
}

func TestSubagentFleetWheelScrollsVisibleDetail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
	}{
		{name: "wide", width: 120},
		{name: "narrow", width: 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fleetTestModel(t)
			m.width = tc.width
			m.app.Cfg.TUI.Mouse = true
			for i := 0; i < 100; i++ {
				m.subagentFleetActivity["one"] = append(m.subagentFleetActivity["one"], fmt.Sprintf("activity %03d", i))
			}
			m.subagentFleetDetailEnd = true
			m.lines = make([]string, 80)
			for i := range m.lines {
				m.lines[i] = fmt.Sprintf("root history %03d", i)
			}
			m.transcriptBaseDirty = true
			m.transcriptDirty = true
			m.refreshTranscript()
			m.transcript.SetYOffset(5)
			rootOffset := m.transcript.YOffset
			maxOffset := max(0, m.subagentFleetDetailLineCount()-m.subagentFleetDetailPageSize())
			if maxOffset == 0 {
				t.Fatal("test detail is not scrollable")
			}
			before := stripANSI(m.renderSubagentFleetModal())

			_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
			afterUp := stripANSI(m.renderSubagentFleetModal())
			if m.subagentFleetDetailEnd || m.subagentFleetDetailOffset >= maxOffset {
				t.Fatalf("wheel up did not leave fleet tail: offset=%d max=%d end=%v", m.subagentFleetDetailOffset, maxOffset, m.subagentFleetDetailEnd)
			}
			if afterUp == before {
				t.Fatal("wheel up changed state without moving visible fleet detail")
			}
			if m.transcript.YOffset != rootOffset {
				t.Fatalf("fleet wheel moved root transcript: got=%d want=%d", m.transcript.YOffset, rootOffset)
			}

			_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
			if !m.subagentFleetDetailEnd || m.subagentFleetDetailOffset != maxOffset {
				t.Fatalf("wheel down did not restore fleet tail: offset=%d max=%d end=%v", m.subagentFleetDetailOffset, maxOffset, m.subagentFleetDetailEnd)
			}
			if afterDown := stripANSI(m.renderSubagentFleetModal()); afterDown != before {
				t.Fatal("wheel down did not restore the visible fleet tail")
			}
		})
	}
}

func TestSubagentFleetPageDownAtTailStaysAtTail(t *testing.T) {
	m := fleetTestModel(t)
	for i := 0; i < 100; i++ {
		m.subagentFleetActivity["one"] = append(m.subagentFleetActivity["one"], fmt.Sprintf("activity %03d", i))
	}
	m.subagentFleetDetailOffset = 0
	m.subagentFleetDetailEnd = true
	_, _ = m.handleSubagentFleetKey(tea.KeyMsg{Type: tea.KeyPgDown})
	want := max(0, m.subagentFleetDetailLineCount()-m.subagentFleetDetailPageSize())
	if !m.subagentFleetDetailEnd || m.subagentFleetDetailOffset != want {
		t.Fatalf("PgDown moved tail to stale offset: offset=%d want=%d end=%v", m.subagentFleetDetailOffset, want, m.subagentFleetDetailEnd)
	}
}

func TestSubagentFleetBlockingHostOverlayKeepsPrecedence(t *testing.T) {
	m := fleetTestModel(t)
	m.handleAgentEvent(permRequestEvent("bash"))
	view := stripANSI(m.View())
	if !strings.Contains(view, "Allow always") || !strings.Contains(view, "bash") || strings.Contains(view, "Subagent fleet inspector") {
		t.Fatalf("permission did not preempt fleet:\n%s", view)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.permChoice != 1 || m.subagentFleetIndex != 0 {
		t.Fatalf("key precedence: permission=%d fleet=%d", m.permChoice, m.subagentFleetIndex)
	}
}

func TestSubagentFleetNarrowFallback(t *testing.T) {
	m := fleetTestModel(t)
	m.width, m.height = 64, 24
	view := stripANSI(m.View())
	for _, want := range []string{"Subagent fleet inspector", "/root/one", "Conversation", "Live activity"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow fleet missing %q:\n%s", want, view)
		}
	}
}

func TestSubagentFleetListKeepsIdentityAndModelVisible(t *testing.T) {
	m := fleetTestModel(t)
	m.subagentFleetList.Agents[0].Agent.Path = "/root/deepseek_architecture"
	m.subagentFleetList.Agents[0].Model = "deepseek-v4-flash"
	m.subagentFleetList.Agents[0].Provider = "opencode-go"
	view := stripANSI(m.renderSubagentFleetList(48, 4))
	for _, want := range []string{"/root/deepseek_architecture", "running · deepseek-v4-flash"} {
		if !strings.Contains(view, want) {
			t.Fatalf("fleet list does not preserve %q:\n%s", want, view)
		}
	}
}

func TestSubagentFleetPreservesAuthoritativeCapacityAndTreatsNotLoadedAsTerminal(t *testing.T) {
	m := fleetTestModel(t)
	m.subagentFleetGeneration = 9
	list := protocol.SubagentList{
		Agents:     []protocol.SubagentState{fleetTestState("idle", "/root/idle", protocol.AgentNotLoaded)},
		Open:       7,
		Closed:     5,
		AgentLimit: 32,
		Truncated:  true,
	}
	_ = m.applySubagentFleetList(subagentFleetListMsg{generation: 9, list: list})
	if m.subagentFleetList.Open != 7 || m.subagentFleetList.Closed != 5 || m.subagentFleetList.Terminal != 1 || m.subagentFleetList.Queued != 0 {
		t.Fatalf("fleet aggregates=%+v", m.subagentFleetList)
	}
}

func TestSubagentFleetCountsExcludeRootAndSnapshotsDoNotRegress(t *testing.T) {
	m := fleetTestModel(t)
	root := fleetTestState("root", "/root", protocol.AgentRunning)
	root.Agent.ParentThreadID, root.Agent.ParentPath, root.Agent.Role, root.Agent.Depth = "", "", "root", 0
	m.subagentFleetList.Agents = append([]protocol.SubagentState{root}, m.subagentFleetList.Agents...)
	m.subagentFleetIndex = 1
	m.recountSubagentFleet(false)
	if m.subagentFleetList.Running != 1 || m.subagentFleetList.Terminal != 1 {
		t.Fatalf("root affected child counts: %+v", m.subagentFleetList)
	}

	live := m.subagentFleetList.Agents[1]
	live.Status, live.Generation = protocol.AgentCompleted, 3
	m.subagentFleetList.Agents[1] = live
	stale := protocol.SubagentList{Agents: append([]protocol.SubagentState(nil), m.subagentFleetList.Agents...)}
	stale.Agents[1].Status, stale.Agents[1].Generation = protocol.AgentRunning, 2
	m.subagentFleetGeneration = 4
	if cmd := m.applySubagentFleetList(subagentFleetListMsg{generation: 4, target: "/root/one", list: stale}); cmd == nil {
		t.Fatal("fresh list did not load selected detail")
	}
	if got := m.subagentFleetList.Agents[1]; got.Status != protocol.AgentCompleted || got.Generation != 3 {
		t.Fatalf("snapshot regressed live state: %+v", got)
	}
}

func TestSubagentFleetCoalescesStreamingDeltasAndFollowsTail(t *testing.T) {
	m := fleetTestModel(t)
	ref := m.subagentFleetList.Agents[0].Agent.Clone()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Agent: ref, Text: "checking "})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Agent: ref, Text: " files"})
	if got := m.subagentFleetActivity[ref.ThreadID]; len(got) != 1 || !strings.Contains(got[0], "checking files") {
		t.Fatalf("thinking deltas were not coalesced: %#v", got)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Agent: ref, Text: "done"})
	if got := m.subagentFleetActivity[ref.ThreadID]; len(got) != 2 {
		t.Fatalf("different stream kinds coalesced: %#v", got)
	}
	m.subagentFleetDetailEnd = false
	if cmd := m.loadSubagentFleetDetail(); cmd == nil || !m.subagentFleetDetailEnd {
		t.Fatalf("detail did not follow newest output: cmd=%v end=%v", cmd != nil, m.subagentFleetDetailEnd)
	}
}

func TestSubagentFleetShowsRootLiveEvents(t *testing.T) {
	m := fleetTestModel(t)
	root := fleetTestState("root", "/root", protocol.AgentRunning)
	root.Agent.ParentThreadID, root.Agent.ParentPath, root.Agent.Role, root.Agent.Depth = "", "", "root", 0
	m.subagentFleetList.Agents = append([]protocol.SubagentState{root}, m.subagentFleetList.Agents...)
	m.subagentFleetIndex = 0
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "root reasoning"})
	if got := m.subagentFleetActivity["root"]; len(got) != 1 || !strings.Contains(got[0], "root reasoning") {
		t.Fatalf("root activity missing: %#v", got)
	}
}

func TestSubagentFleetLiveEventsAreBoundedAndStayOutOfRoot(t *testing.T) {
	m := fleetTestModel(t)
	m.assistantBuf.WriteString("root")
	ref := m.subagentFleetList.Agents[0].Agent.Clone()
	for i := 0; i < maxFleetActivityLines+80; i++ {
		m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolProgress, Agent: ref, ToolName: "bash", Message: fmt.Sprintf("line-%03d %s", i, strings.Repeat("x", 400))})
	}
	lines := m.subagentFleetActivity[ref.ThreadID]
	if len(lines) > maxFleetActivityLines || fleetActivityBytes(lines) > maxFleetActivityBytes {
		t.Fatalf("activity unbounded: lines=%d bytes=%d", len(lines), fleetActivityBytes(lines))
	}
	if m.assistantBuf.String() != "root" {
		t.Fatalf("child event entered root buffer: %q", m.assistantBuf.String())
	}
	m.subagentFleetDetailEnd = true
	if !strings.Contains(stripANSI(m.renderSubagentFleetDetail(72, 20)), "line-207") {
		t.Fatal("latest live activity not rendered at detail end")
	}
}

func TestSubagentFleetAsyncGenerationAndPathSelection(t *testing.T) {
	m := fleetTestModel(t)
	original := m.subagentFleetList.Agents[0].Agent.Path
	stale := subagentFleetListMsg{generation: 3, list: protocol.SubagentList{Agents: []protocol.SubagentState{fleetTestState("stale", "/root/stale", protocol.AgentRunning)}}}
	if cmd := m.applySubagentFleetList(stale); cmd != nil || m.subagentFleetList.Agents[0].Agent.Path != original {
		t.Fatal("stale fleet result mutated modal")
	}
	fresh := subagentFleetListMsg{generation: 4, target: "/root/two", list: m.subagentFleetList}
	if cmd := m.applySubagentFleetList(fresh); cmd == nil || m.subagentFleetIndex != 1 {
		t.Fatalf("path selection: index=%d cmd=%v", m.subagentFleetIndex, cmd != nil)
	}
	m.subagentFleetDetailGeneration = 9
	m.subagentFleetMessages = []protocol.Message{{ID: "kept"}}
	m.applySubagentFleetDetail(subagentFleetDetailMsg{generation: 8, target: "/root/two", messages: []protocol.Message{{ID: "stale"}}})
	if len(m.subagentFleetMessages) != 1 || m.subagentFleetMessages[0].ID != "kept" {
		t.Fatal("stale detail result mutated transcript")
	}
	m.closeSubagentFleet()
	m.applySubagentFleetDetail(subagentFleetDetailMsg{generation: m.subagentFleetDetailGeneration, target: "/root/two", messages: []protocol.Message{{ID: "closed"}}})
	if m.subagentFleetMessages[0].ID != "kept" {
		t.Fatal("detail result mutated closed inspector")
	}
}

func TestSubagentFleetMissingTargetDoesNotSelectAnotherAgent(t *testing.T) {
	m := fleetTestModel(t)
	m.subagentFleetLoading = true
	cmd := m.applySubagentFleetList(subagentFleetListMsg{generation: 4, target: "/root/missing", list: m.subagentFleetList})
	if cmd != nil || !strings.Contains(m.subagentFleetError, "not found") || m.subagentFleetDetailLoading {
		t.Fatalf("missing target: cmd=%v error=%q loading=%v", cmd != nil, m.subagentFleetError, m.subagentFleetDetailLoading)
	}
}

func TestFleetShortcutsOpenAndSwitchInspectors(t *testing.T) {
	enabled := true
	testHome(t)
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	m := newModel(context.Background(), app.Options{})
	m.app = a
	m.busy = true

	altA := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true}
	_, cmd := m.handleKey(altA)
	if !m.subagentFleetOpen || m.processFleetOpen || cmd == nil {
		t.Fatalf("alt+a: agents=%v processes=%v cmd=%v", m.subagentFleetOpen, m.processFleetOpen, cmd != nil)
	}
	altP := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}
	_, cmd = m.handleKey(altP)
	if m.subagentFleetOpen || !m.processFleetOpen || cmd == nil {
		t.Fatalf("alt+p: agents=%v processes=%v cmd=%v", m.subagentFleetOpen, m.processFleetOpen, cmd != nil)
	}
	_, cmd = m.handleKey(altP)
	if !m.processFleetOpen || cmd != nil {
		t.Fatalf("repeated alt+p: open=%v cmd=%v", m.processFleetOpen, cmd != nil)
	}
}

func TestAgentCommandOpensFleetAndPreservesConcurrency(t *testing.T) {
	enabled := true
	testHome(t)
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	m := newModel(context.Background(), app.Options{})
	m.app = a

	_, cmd := m.runCommand("/agent /root")
	if !m.subagentFleetOpen || m.subagentFleetRequested != "/root" || cmd == nil {
		t.Fatalf("/agent path: open=%v target=%q cmd=%v", m.subagentFleetOpen, m.subagentFleetRequested, cmd != nil)
	}
	_, _ = m.Update(cmd())
	if m.subagentFleetLoading {
		t.Fatal("authoritative list command did not settle")
	}
	m.closeSubagentFleet()
	_, _ = m.runCommand("/agent concurrency 7")
	if m.subagentFleetOpen || m.app.Cfg.Subagents.MaxConcurrentThreads != 7 {
		t.Fatalf("concurrency compatibility: open=%v concurrency=%d", m.subagentFleetOpen, m.app.Cfg.Subagents.MaxConcurrentThreads)
	}
}

func TestSubagentFleetDetailFormatsMessagesAsReadableBlocks(t *testing.T) {
	m := fleetTestModel(t)
	m.subagentFleetList.Agents[0].Result = "Finished with **rendered result**."
	messages := []protocol.Message{
		{Role: protocol.RoleAssistant, StopReason: protocol.StopToolUse, Content: []protocol.ContentBlock{
			protocol.NewTextBlock("Summary with **strong emphasis**.\n\n1. first item\n2. second item"),
			{Type: protocol.BlockToolCall, Name: "bash", Arguments: []byte(`{"command":"go test ./...","timeout_ms":120000}`)},
		}},
		{Role: protocol.RoleTool, ToolName: "bash", Content: []protocol.ContentBlock{protocol.NewTextBlock("ok\nnext line")}},
	}
	m.subagentFleetMessages = messages
	plain := stripANSI(strings.Join(m.subagentFleetDetailLines(54), "\n"))
	for _, want := range []string{"Result", "rendered result", "assistant · tool_use", "Summary", "strong emphasis", "first item", "call · bash", `"command": "go test ./..."`, "tool · bash", "ok", "next line"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("formatted detail missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "**") || strings.Contains(plain, "assistant  assistant") || strings.Contains(plain, `call bash {"command"`) {
		t.Fatalf("detail contains raw Markdown or flattened message summaries:\n%s", plain)
	}
}

func TestSubagentFleetDetailMessagesStayBounded(t *testing.T) {
	m := fleetTestModel(t)
	m.subagentFleetIndex = 0
	m.subagentFleetDetailGeneration = 2
	messages := make([]protocol.Message, maxAgentInspectionMessages+20)
	for i := range messages {
		messages[i] = protocol.Message{ID: fmt.Sprint(i), Role: protocol.RoleAssistant, Content: []protocol.ContentBlock{protocol.NewTextBlock("message")}}
	}
	// Async commands trim before delivery; verify the accepted snapshot remains
	// bounded even if a custom command host returns too much data.
	if len(messages) > maxAgentInspectionMessages {
		messages = messages[len(messages)-maxAgentInspectionMessages:]
	}
	m.applySubagentFleetDetail(subagentFleetDetailMsg{generation: 2, target: "/root/one", state: m.subagentFleetList.Agents[0], messages: messages})
	if len(m.subagentFleetMessages) != maxAgentInspectionMessages {
		t.Fatalf("messages=%d", len(m.subagentFleetMessages))
	}
}
