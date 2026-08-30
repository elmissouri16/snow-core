package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestInlineTranscriptCommitsStableRowsAndKeepsLiveTail(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 80, 30
	m.layout()
	m.pushLine(styleUser.Render("› committed prompt"))
	m.thinkingBuf.WriteString("live reasoning")
	m.refreshTranscriptForced()

	if cmd := m.commitInlineHistory(); cmd == nil {
		t.Fatal("stable transcript row did not produce a native scrollback command")
	}
	view := stripANSI(m.View())
	if strings.Contains(view, "committed prompt") {
		t.Fatalf("committed history remained in managed viewport: %q", view)
	}
	if !strings.Contains(view, "live reasoning") {
		t.Fatalf("live tail missing from managed viewport: %q", view)
	}
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("inline managed region height=%d, want terminal height %d", got, m.height)
	}
}

func TestInlineHydrationPrintsBranchExactlyOnce(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 80, 30
	message := protocol.NewUserMessage("u1", m.app.Session.BranchTip(), "persisted prompt")
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}

	m.hydrateSession()
	if cmd := m.commitInlineHistory(); cmd == nil {
		t.Fatal("initial hydration did not produce scrollback history")
	}
	_, _ = m.Update(inlineHistoryAckMsg{generation: m.inlinePrintGeneration, end: m.inlinePrintEnd})
	committed := m.inlineCommitted
	m.hydrateSession()
	if cmd := m.commitInlineHistory(); cmd != nil {
		t.Fatal("same-branch session refresh replayed native scrollback")
	}
	if m.inlineCommitted != committed {
		t.Fatalf("same-branch hydration changed commit cursor: %d want %d", m.inlineCommitted, committed)
	}
}

func TestInlineHistoryBatchesWaitForRendererAcknowledgment(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 80, 30
	m.pushLine("first")
	if cmd := m.commitInlineHistory(); cmd == nil || !m.inlinePrintInFlight {
		t.Fatal("first history batch was not scheduled")
	}
	firstEnd := m.inlinePrintEnd
	m.pushLine("second")
	if cmd := m.commitInlineHistory(); cmd != nil {
		t.Fatal("second history batch overtook the in-flight batch")
	}
	_, cmd := m.Update(inlineHistoryAckMsg{generation: m.inlinePrintGeneration, end: firstEnd})
	if cmd == nil || !m.inlinePrintInFlight || m.inlineCommitted != firstEnd || m.inlinePrintEnd != len(m.lines) {
		t.Fatalf("ack did not serialize next batch: committed=%d end=%d lines=%d inFlight=%v", m.inlineCommitted, m.inlinePrintEnd, len(m.lines), m.inlinePrintInFlight)
	}
}

func TestInlineSessionHeaderCommitsBeforeEmptyTranscript(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 100, 40
	m.hydrateSession()
	if !m.inlineHeaderPending {
		t.Fatal("session header was not queued during hydration")
	}
	if cmd := m.commitInlineHistory(); cmd == nil {
		t.Fatal("empty session did not commit its header")
	}
	if m.inlineHeaderPending || !m.inlinePrintInFlight || m.inlinePrintEnd != 0 {
		t.Fatalf("header commit state: pending=%v inFlight=%v end=%d", m.inlineHeaderPending, m.inlinePrintInFlight, m.inlinePrintEnd)
	}
}

func TestInlineHeaderRemainsVisible(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 100, 30
	m.layout()
	view := stripANSI(m.View())
	model := m.app.Agent.Model()
	if !strings.Contains(view, "snow") || !strings.Contains(view, model.Provider+"/"+model.ID) || !strings.Contains(view, "idle") {
		t.Fatalf("sticky inline header missing: %q", view)
	}
}

func TestInlineManagedFrameStaysBottomAnchored(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 100, 40
	m.layout()
	wantIdleBody := m.height - inlineFixedChromeHeight - minComposerHeight
	if m.transcript.Height != wantIdleBody {
		t.Fatalf("idle transcript height=%d want bottom-filling %d", m.transcript.Height, wantIdleBody)
	}
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("idle inline frame height=%d want terminal height %d", got, m.height)
	}

	m.busy = true
	m.runStartedAt = time.Now()
	m.editor.SetValue("one\ntwo\nthree\nfour\nfive")
	m.layout()
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("busy inline frame height=%d want terminal height %d", got, m.height)
	}
	wantBusyBody := m.height - inlineFixedChromeHeight - m.editor.Height() - m.runStatusHeight()
	if m.transcript.Height != wantBusyBody {
		t.Fatalf("busy transcript height=%d want bottom-filling %d", m.transcript.Height, wantBusyBody)
	}
}

func TestInlineFrameLeavesFinalColumnUnusedAndSeparatesWrappedText(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 40, 20
	m.assistantBuf.WriteString(strings.Repeat("wrapped response ", 8))
	m.refreshTranscriptForced()
	m.layout()
	view := stripANSI(m.View())
	if got := lipgloss.Width(m.View()); got != m.width-1 {
		t.Fatalf("inline frame width=%d want safe width %d", got, m.width-1)
	}
	lines := strings.Split(view, "\n")
	separator := strings.Repeat("─", m.width-1)
	found := slices.Contains(lines, separator)
	if !found {
		t.Fatalf("separator was not isolated on its own row: %q", view)
	}
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("inline frame height=%d want %d", got, m.height)
	}
}

func TestInlineHydrationCapPreservesSwitchBoundary(t *testing.T) {
	hydrated := make([]string, 2005)
	for i := range hydrated {
		hydrated[i] = fmt.Sprintf("row-%d", i)
	}
	got := boundedInlineHydration(hydrated, 0, true)
	if len(got) != 2002 {
		t.Fatalf("bounded hydration rows=%d want 2002", len(got))
	}
	if !strings.Contains(stripANSI(got[0]), "switched transcript") {
		t.Fatalf("switch boundary missing: %q", stripANSI(got[0]))
	}
	if !strings.Contains(stripANSI(got[1]), "5 older transcript segments omitted") {
		t.Fatalf("omission marker=%q", stripANSI(got[1]))
	}
	if got[2] != "row-5" || got[len(got)-1] != "row-2004" {
		t.Fatalf("bounded tail=%q…%q", got[2], got[len(got)-1])
	}
}

func TestInlineQuitClearsOnlyManagedRegion(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 80, 30
	m.layout()
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("inline quit returned no command")
	}
	if view := m.View(); view != "" {
		t.Fatalf("inline quit retained managed chrome: %q", stripANSI(view))
	}
}

func TestInlineBranchSwitchAddsTranscriptBoundary(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width, m.height = 80, 30
	message := protocol.NewUserMessage("shared", m.app.Session.BranchTip(), "shared branch history")
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	_ = m.commitInlineHistory()
	_, _ = m.Update(inlineHistoryAckMsg{generation: m.inlinePrintGeneration, end: m.inlinePrintEnd})

	branches := m.app.Session.(session.BranchStore)
	if _, err := branches.ForkBranch(message.ID); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	if len(m.lines) != 1 || !strings.Contains(stripANSI(m.lines[0]), "switched transcript") {
		t.Fatalf("branch switch replayed shared native history: %+v", m.lines)
	}
}
