package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestStreamIngestionDefersRenderButKeepsInputResponsive(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 20
	m.layout()
	m.lines = []string{"seed"}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscript()
	before := m.transcriptContent

	_, cmd := m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{Type: protocol.EvTextDelta, Text: strings.Repeat("stream ", 2_000)}}})
	if cmd == nil || !m.transcriptFlushPending {
		t.Fatal("stream delta did not schedule a deferred flush")
	}
	if m.transcriptContent != before {
		t.Fatal("stream ingestion rebuilt transcript before scheduled flush")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got := m.editor.Value(); got != "x" {
		t.Fatalf("input was not handled before flush: %q", got)
	}
	_, _ = m.Update(transcriptFlushMsg(m.transcriptFlushSeq))
	if !strings.Contains(stripANSI(m.transcriptContent), "stream") {
		t.Fatal("scheduled flush did not render accumulated stream text")
	}
}

func TestRunStatusResizeKeepsLiveToolEventsFollowingTail(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 16
	m.layout()
	for i := 0; i < 40; i++ {
		m.lines = append(m.lines, fmt.Sprintf("history %02d", i))
	}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("test setup is not following the transcript tail")
	}

	m.editor.SetValue("open the webpage")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.showRunStatus() || !m.transcript.AtBottom() {
		t.Fatalf("prompt-start resize lost tail: status=%v bottom=%v", m.showRunStatus(), m.transcript.AtBottom())
	}
	_, _ = m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{
		Type: protocol.EvToolStart, ToolName: "mcp_chrome_navigate_page", ToolCallID: "mcp-1",
	}}})
	if content := stripANSI(m.transcriptContent); !strings.Contains(content, "▶ mcp_chrome_navigate_page") {
		t.Fatalf("live MCP tool event remained frozen until turn completion: %q", content)
	}
	_, _ = m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{
		Type: protocol.EvToolEnd, ToolName: "mcp_chrome_navigate_page", ToolCallID: "mcp-1",
	}}})
	_, cmd := m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{
		Type: protocol.EvThinkingDelta, Text: "Inspecting the loaded page.",
	}}})
	if cmd == nil || !m.transcriptFlushPending {
		t.Fatal("reasoning after MCP call did not schedule a live flush")
	}
	_, _ = m.Update(transcriptFlushMsg(m.transcriptFlushSeq))
	if content := stripANSI(m.transcriptContent); !strings.Contains(content, "Inspecting the loaded page") {
		t.Fatalf("reasoning remained frozen until turn completion: %q", content)
	}
}

func TestTranscriptSnapshotFreezesOffTailAndCatchesUpAtEnd(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 60, 12
	m.layout()
	for i := 0; i < 40; i++ {
		m.lines = append(m.lines, fmt.Sprintf("history %02d", i))
	}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscript()
	m.transcript.SetYOffset(3)
	beforeContent := m.transcriptContent
	beforeOffset := m.transcript.YOffset

	_, _ = m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{{Type: protocol.EvTextDelta, Text: "new live tail"}}})
	if m.transcriptContent != beforeContent || m.transcript.YOffset != beforeOffset {
		t.Fatalf("off-tail snapshot changed: offset=%d want %d", m.transcript.YOffset, beforeOffset)
	}
	if !m.transcriptDirty {
		t.Fatal("off-tail stream did not remain dirty for catch-up")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(stripANSI(m.transcriptContent), "new live tail") || !m.transcript.AtBottom() {
		t.Fatalf("End did not catch up/follow tail: bottom=%v content=%q", m.transcript.AtBottom(), stripANSI(m.transcriptContent))
	}
}

func TestWheelToFrozenSnapshotBottomCatchesUpOnce(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 60, 12
	m.layout()
	for i := 0; i < 40; i++ {
		m.lines = append(m.lines, fmt.Sprintf("history %02d", i))
	}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscript()
	m.transcript.SetYOffset(max(0, m.transcript.YOffset-1))
	m.batchingEvents = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "wheel catch-up tail"})
	m.batchingEvents = false
	if !m.transcriptDirty {
		t.Fatal("expected dirty transcript")
	}
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if !strings.Contains(stripANSI(m.transcriptContent), "wheel catch-up tail") || !m.transcript.AtBottom() {
		t.Fatalf("wheel catch-up failed: bottom=%v", m.transcript.AtBottom())
	}
}

func TestWheelBurstReachesExactViewportOffset(t *testing.T) {
	m := prepareScrollableModel(t)
	m.app.Cfg.TUI.Mouse = true
	m.transcript.GotoTop()
	const events = 100
	for range events {
		_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	want := min(events*m.transcript.MouseWheelDelta, max(0, m.transcript.TotalLineCount()-m.transcript.Height))
	if got := m.transcript.YOffset; got != want {
		t.Fatalf("wheel burst offset=%d want %d", got, want)
	}
}

func TestAdaptiveStreamFlushIntervals(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	if got := m.transcriptFlushDelay(); got != streamFlushFast {
		t.Fatalf("empty delay=%s", got)
	}
	m.assistantBuf.WriteString(strings.Repeat("x", 64*1024+1))
	if got := m.transcriptFlushDelay(); got != streamFlushMedium {
		t.Fatalf("64KiB delay=%s", got)
	}
	m.assistantBuf.WriteString(strings.Repeat("x", 192*1024))
	if got := m.transcriptFlushDelay(); got != streamFlushSlow {
		t.Fatalf("256KiB delay=%s", got)
	}
	m.assistantBuf.WriteString(strings.Repeat("x", 768*1024))
	if got := m.transcriptFlushDelay(); got != streamFlushVerySlow {
		t.Fatalf("1MiB delay=%s", got)
	}
}

func TestCtrlArrowScrollBindings(t *testing.T) {
	m := prepareScrollableModel(t)
	before := m.transcript.YOffset
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlDown})
	if got := m.transcript.YOffset; got != before+m.transcript.MouseWheelDelta {
		t.Fatalf("Ctrl+Down offset=%d want %d", got, before+m.transcript.MouseWheelDelta)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlUp})
	if got := m.transcript.YOffset; got != before {
		t.Fatalf("Ctrl+Up offset=%d want %d", got, before)
	}
}
