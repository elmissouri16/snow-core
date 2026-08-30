package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
)

func newTranscriptSelectionTestModel(t *testing.T, lines []string) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Cfg.TUI.Mouse = true
	m.width, m.height = 32, 12
	m.layout()
	m.lines = slices.Clone(lines)
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscriptForced()
	return m
}

func TestTranscriptSelectionUsesHostClipboardByDefault(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	if m.copySelectionToClipboard == nil {
		t.Fatal("runtime transcript selection has no host clipboard writer")
	}
}

func TestTranscriptMouseDragSelectsHighlightsAndCopies(t *testing.T) {
	lines := []string{"zero", "one", "alpha beta", "gamma delta", "four", "five", "six", "seven"}
	m := newTranscriptSelectionTestModel(t, lines)
	m.transcript.SetYOffset(2)
	var copied string
	m.copySelectionToClipboard = func(text string) error {
		copied = text
		return nil
	}
	top := m.transcriptSelectionTop()

	_, _ = m.Update(tea.MouseMsg{X: 0, Y: top, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	_, _ = m.Update(tea.MouseMsg{X: 4, Y: top + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	view := m.renderTranscriptView()
	if !strings.Contains(view, "\x1b[7m") {
		t.Fatalf("drag selection was not highlighted: %q", view)
	}

	_, cmd := m.Update(tea.MouseMsg{X: 4, Y: top + 1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if cmd == nil {
		t.Fatal("selection release did not schedule clipboard copy")
	}
	_, _ = m.Update(cmd())
	if copied != "alpha beta\ngamma" {
		t.Fatalf("copied selection = %q", copied)
	}
	if !strings.Contains(m.lastStatus, "copied") {
		t.Fatalf("copy status = %q", m.lastStatus)
	}
}

func TestTranscriptOSC52SequenceIsRenderedOnceThenCleared(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"copy me"})
	message := transcriptSelectionCopiedMsg{characters: 7, sequence: "OSC52-SEQUENCE"}
	_, cmd := m.Update(message)
	if cmd == nil {
		t.Fatal("OSC52 copy did not schedule one-render cleanup")
	}
	if view := m.View(); !strings.Contains(view[:min(len(view), 64)], message.sequence) {
		t.Fatalf("OSC52 sequence missing from render prefix: %q", view)
	}
	_, _ = m.Update(cmd())
	if view := m.View(); strings.Contains(view, message.sequence) {
		t.Fatalf("OSC52 sequence repeated after cleanup: %q", view)
	}
}

func TestTranscriptBlankViewportPaddingDoesNotSelectFinalLine(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"only actual row"})
	top := m.transcriptSelectionTop()
	_, _ = m.Update(tea.MouseMsg{X: 2, Y: top + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.transcriptSelection.pressActive {
		t.Fatal("blank viewport padding started a selection on the final source row")
	}
}

func TestTranscriptSelectionPreservesANSIAndWideGraphemes(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"\x1b[31mA界B\x1b[0m"})
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 0, col: 1}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 0, col: 2}

	if got := m.selectedTranscriptText(); got != "界" {
		t.Fatalf("wide grapheme selection = %q", got)
	}
	highlighted := m.renderTranscriptView()
	if !strings.Contains(highlighted, "\x1b[7m") || xansi.Strip(highlighted) == "" {
		t.Fatalf("ANSI selection highlight missing or corrupted: %q", highlighted)
	}
}

func TestTranscriptStreamingFreezesDuringSelectionAndCatchesUpAfterRelease(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"stable source"})
	m.copySelectionToClipboard = func(string) error { return nil }
	top := m.transcriptSelectionTop()
	_, _ = m.Update(tea.MouseMsg{X: 0, Y: top, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	_, _ = m.Update(tea.MouseMsg{X: 5, Y: top, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	frozen := m.transcriptContent
	m.assistantBuf.WriteString("live delta")
	m.transcriptDirty = true
	m.refreshTranscriptForced()
	if m.transcriptContent != frozen || !m.transcriptSelection.pressActive {
		t.Fatal("stream update replaced the immutable active-selection snapshot")
	}
	_, cmd := m.Update(tea.MouseMsg{X: 5, Y: top, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if cmd == nil {
		t.Fatal("release did not preserve and copy frozen selection")
	}
	if m.transcriptContent == frozen || !strings.Contains(xansi.Strip(m.transcriptContent), "live delta") {
		t.Fatal("release did not catch up the frozen stream snapshot")
	}
}

func TestTranscriptRefreshClearsSelectionBeforeSourceChanges(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"old source"})
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 0, col: 0}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 0, col: 3}
	m.lines = []string{"new source"}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscriptForced()
	if _, ok := m.transcriptSelectionBounds(); ok {
		t.Fatal("transcript source replacement retained stale selection coordinates")
	}
}

func TestTranscriptSelectionAutoScrollGenerationDoesNotAliasAfterClear(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"one", "two", "three"})
	m.transcriptSelection.pressActive = true
	m.transcriptSelection.autoScroll = 1
	m.transcriptSelection.autoScrollID = 7
	staleID := m.transcriptSelection.autoScrollID
	m.clearTranscriptSelection()
	m.transcriptSelection.pressActive = true
	m.transcriptSelection.autoScroll = 1
	m.transcriptSelection.autoScrollID++
	if staleID == m.transcriptSelection.autoScrollID {
		t.Fatalf("cleared selection reused stale auto-scroll id %d", staleID)
	}
	if cmd := m.handleTranscriptSelectionAutoScroll(staleID); cmd != nil {
		t.Fatal("stale auto-scroll tick was accepted after selection reset")
	}
}

func TestTranscriptSelectionAutoScrollExtendsOffscreen(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "row " + string(rune('A'+i%26))
	}
	m := newTranscriptSelectionTestModel(t, lines)
	m.transcript.GotoTop()
	top := m.transcriptSelectionTop()
	bottom := top + m.transcript.Height - 1
	_, _ = m.Update(tea.MouseMsg{X: 0, Y: top + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	_, cmd := m.Update(tea.MouseMsg{X: 2, Y: bottom, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if cmd == nil {
		t.Fatal("edge drag did not start auto-scroll")
	}
	before := m.transcript.YOffset
	msg := cmd()
	_, next := m.Update(msg)
	if m.transcript.YOffset <= before {
		t.Fatalf("auto-scroll offset=%d want > %d", m.transcript.YOffset, before)
	}
	if next == nil {
		t.Fatal("active edge drag did not schedule another auto-scroll tick")
	}
	_, _ = m.Update(tea.MouseMsg{X: 2, Y: bottom, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if m.transcriptSelection.autoScroll != 0 {
		t.Fatal("mouse release did not stop selection auto-scroll")
	}
}

func TestTranscriptSelectionAutoScrollAcceleratesBeyondViewport(t *testing.T) {
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = "row"
	}
	m := newTranscriptSelectionTestModel(t, lines)
	m.transcript.GotoTop()
	top := m.transcriptSelectionTop()
	bottom := top + m.transcript.Height - 1
	_, _ = m.Update(tea.MouseMsg{X: 0, Y: top, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	_, cmd := m.Update(tea.MouseMsg{X: 0, Y: bottom + 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if cmd == nil {
		t.Fatal("drag beyond viewport did not start accelerated auto-scroll")
	}
	_, _ = m.Update(cmd())
	if m.transcript.YOffset <= 1 {
		t.Fatalf("accelerated auto-scroll advanced only %d row(s)", m.transcript.YOffset)
	}
}

func TestTranscriptSelectionAutoScrollAcceleratesWhileHeldAtEdge(t *testing.T) {
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "row"
	}
	m := newTranscriptSelectionTestModel(t, lines)
	m.transcript.GotoTop()
	top := m.transcriptSelectionTop()
	bottom := top + m.transcript.Height - 1
	_, _ = m.Update(tea.MouseMsg{X: 0, Y: top, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	_, cmd := m.Update(tea.MouseMsg{X: 0, Y: bottom, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if cmd == nil {
		t.Fatal("edge drag did not start auto-scroll")
	}
	first := 0
	for i := 0; i < 12 && cmd != nil; i++ {
		before := m.transcript.YOffset
		_, cmd = m.Update(cmd())
		advanced := m.transcript.YOffset - before
		if i == 0 {
			first = advanced
		}
		if i == 11 && advanced <= first {
			t.Fatalf("held-edge scroll did not accelerate: first=%d later=%d", first, advanced)
		}
	}
}

func TestTranscriptDoubleAndTripleClickSelectWordThenLine(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"alpha beta gamma"})
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }
	top := m.transcriptSelectionTop()
	click := func() {
		_, _ = m.Update(tea.MouseMsg{X: 7, Y: top, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		_, _ = m.Update(tea.MouseMsg{X: 7, Y: top, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
		now = now.Add(100 * time.Millisecond)
	}
	click()
	click()
	if got := m.selectedTranscriptText(); got != "beta" {
		t.Fatalf("double-click selection = %q", got)
	}
	click()
	if got := m.selectedTranscriptText(); got != "alpha beta gamma" {
		t.Fatalf("triple-click selection = %q", got)
	}
}

func TestRightClickContextMenuCopiesWithoutDisablingViewportMouse(t *testing.T) {
	lines := make([]string, 40)
	lines[0] = "select me"
	for i := 1; i < len(lines); i++ {
		lines[i] = "row"
	}
	m := newTranscriptSelectionTestModel(t, lines)
	m.transcript.GotoTop()
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 0, col: 0}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 0, col: 3}
	var copied string
	m.copySelectionToClipboard = func(text string) error {
		copied = text
		return nil
	}

	_, cmd := m.Update(tea.MouseMsg{X: 2, Y: m.transcriptSelectionTop(), Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if cmd != nil || !m.transcriptSelectionMenu.open {
		t.Fatalf("right-click context menu: cmd=%v open=%v", cmd != nil, m.transcriptSelectionMenu.open)
	}
	if want := transcriptSelectionBlockWidth(transcriptSelectionContextMenuView()); m.transcriptSelectionMenu.width != want || want > 24 {
		t.Fatalf("context menu width=%d want=%d and <=24", m.transcriptSelectionMenu.width, want)
	}
	rendered := m.View()
	if !strings.Contains(stripANSI(rendered), "Copy selection") {
		t.Fatal("right-click context menu was not rendered")
	}
	for row, line := range strings.Split(rendered, "\n") {
		if width := xansi.StringWidth(line); width > m.managedFrameWidth() {
			t.Fatalf("context menu widened frame row %d to %d cells (frame=%d)", row, width, m.managedFrameWidth())
		}
	}
	if !m.app.Cfg.TUI.Mouse {
		t.Fatal("right-click disabled fixed viewport mouse scrolling")
	}
	if _, ok := m.transcriptSelectionBounds(); !ok {
		t.Fatal("right-click cleared the current transcript selection")
	}
	menu := m.transcriptSelectionMenu
	_, cmd = m.Update(tea.MouseMsg{X: menu.x + 1, Y: menu.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil || m.transcriptSelectionMenu.open {
		t.Fatalf("context-menu copy click: cmd=%v open=%v", cmd != nil, m.transcriptSelectionMenu.open)
	}
	_, _ = m.Update(cmd())
	if copied != "sele" {
		t.Fatalf("context-menu copied %q, want %q", copied, "sele")
	}
	_, _ = m.Update(tea.MouseMsg{X: 2, Y: m.transcriptSelectionTop(), Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	before := m.transcript.YOffset
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.transcriptSelectionMenu.open {
		t.Fatal("wheel did not dismiss the transcript context menu")
	}
	if m.transcript.YOffset <= before {
		t.Fatalf("wheel offset=%d want > %d after right-click copy", m.transcript.YOffset, before)
	}
}

func TestTranscriptContextMenuFitsNarrowFrame(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"select me"})
	m.width = 10
	m.layout()
	m.refreshTranscriptForced()
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 0, col: 0}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 0, col: 3}

	_, _ = m.Update(tea.MouseMsg{X: 2, Y: m.transcriptSelectionTop(), Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if !m.transcriptSelectionMenu.open {
		t.Fatal("context menu did not open")
	}
	frameWidth := m.managedFrameWidth()
	if width := m.transcriptSelectionMenu.width; width > frameWidth {
		t.Fatalf("context menu width=%d exceeds narrow frame=%d", width, frameWidth)
	}
	for row, line := range strings.Split(m.View(), "\n") {
		if width := xansi.StringWidth(line); width > frameWidth {
			t.Fatalf("context menu widened narrow frame row %d to %d cells (frame=%d)", row, width, frameWidth)
		}
	}
}

func TestTranscriptContextMenuKeyboardCopyAndDismiss(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"copy me"})
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 0, col: 0}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 0, col: 3}
	var copied string
	m.copySelectionToClipboard = func(text string) error {
		copied = text
		return nil
	}

	open := func() {
		_, _ = m.Update(tea.MouseMsg{X: 1, Y: m.transcriptSelectionTop(), Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
		if !m.transcriptSelectionMenu.open {
			t.Fatal("context menu did not open")
		}
	}
	open()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.transcriptSelectionMenu.open {
		t.Fatalf("Enter copy: cmd=%v open=%v", cmd != nil, m.transcriptSelectionMenu.open)
	}
	_, _ = m.Update(cmd())
	if copied != "copy" {
		t.Fatalf("Enter copied %q, want %q", copied, "copy")
	}

	open()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || m.transcriptSelectionMenu.open {
		t.Fatalf("Escape dismiss: cmd=%v open=%v", cmd != nil, m.transcriptSelectionMenu.open)
	}
}

func TestTranscriptCopyFallsBackToOSC52WhenHostClipboardFails(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"fallback"})
	m.copySelectionToClipboard = func(string) error { return errors.New("clipboard unavailable") }
	message, ok := m.copyTranscriptSelectionCmd("fallback")().(transcriptSelectionCopiedMsg)
	if !ok {
		t.Fatal("copy command returned the wrong message type")
	}
	if message.err != nil || message.sequence == "" {
		t.Fatalf("clipboard fallback: err=%v sequence=%q", message.err, message.sequence)
	}
}

func TestRightClickOverFleetKeepsViewportMouseEnabled(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"fleet"})
	m.subagentFleetOpen = true
	_, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if cmd != nil || !m.app.Cfg.TUI.Mouse {
		t.Fatalf("fleet right-click changed mouse mode: cmd=%v mouse=%v", cmd != nil, m.app.Cfg.TUI.Mouse)
	}
}

func TestNonRightMouseEventsKeepAppMouseEnabled(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"wheel"})
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if !m.app.Cfg.TUI.Mouse {
		t.Fatal("wheel unexpectedly disabled app mouse")
	}
}

func TestF6ClearsAppSelectionCatchesUpAndRestoresNativeMode(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"select me"})
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 0, col: 0}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 0, col: 3}
	m.transcriptSelection.pressActive = true
	frozen := m.transcriptContent
	m.assistantBuf.WriteString("caught up after F6")
	m.transcriptDirty = true
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyF6})
	if cmd == nil || m.app.Cfg.TUI.Mouse {
		t.Fatalf("F6 did not disable app mouse mode: cmd=%v mouse=%v", cmd != nil, m.app.Cfg.TUI.Mouse)
	}
	if _, ok := m.transcriptSelectionBounds(); ok {
		t.Fatal("F6 retained application selection")
	}
	if m.transcriptContent == frozen || !strings.Contains(xansi.Strip(m.transcriptContent), "caught up after F6") {
		t.Fatal("F6 left stream updates frozen behind the cleared selection")
	}
	if !strings.Contains(m.lastStatus, "native selection") {
		t.Fatalf("F6 status = %q", m.lastStatus)
	}
}
