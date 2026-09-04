package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestHelpCardIsCenteredWithoutChangingFrameGeometry(t *testing.T) {
	for _, test := range []struct {
		width, height int
		inline        bool
	}{
		{width: 100, height: 30},
		{width: 60, height: 16},
		{width: 100, height: 30, inline: true},
		{width: 60, height: 16, inline: true},
	} {
		name := fmt.Sprintf("%dx%d_inline_%v", test.width, test.height, test.inline)
		t.Run(name, func(t *testing.T) {
			m := modelPickerTestModel(t, test.width, test.height)
			m.inlineTranscript = test.inline
			m.layout()
			beforeTranscriptHeight := m.transcript.Height
			_, _ = m.startHelp()
			m.layout()
			if m.transcript.Height != beforeTranscriptHeight {
				t.Fatalf("transcript height changed %d -> %d", beforeTranscriptHeight, m.transcript.Height)
			}
			if overlay := stripANSI(m.renderOverlays()); strings.Contains(overlay, "Commands") {
				t.Fatalf("help remained in the layout overlay: %q", overlay)
			}

			card := m.renderHelp()
			cardWidth, cardHeight := transcriptSelectionBlockWidth(card), lipgloss.Height(card)
			if cardWidth > m.managedFrameWidth() || cardHeight > m.managedFrameHeight() {
				t.Fatalf("card=%dx%d frame=%dx%d", cardWidth, cardHeight, m.managedFrameWidth(), m.managedFrameHeight())
			}
			view := m.View()
			if got := lipgloss.Height(view); got != m.managedFrameHeight() {
				t.Fatalf("view height=%d want=%d", got, m.managedFrameHeight())
			}
			lines := strings.Split(stripANSI(view), "\n")
			x := (m.managedFrameWidth() - cardWidth) / 2
			y := (m.managedFrameHeight() - cardHeight) / 2
			if y >= len(lines) || stripANSI(xansi.Cut(lines[y], x, x+1)) != "╭" {
				t.Fatalf("centered border missing at (%d,%d): %q", x, y, lines[y])
			}
		})
	}
}

func TestHelpCardScrollsWithinFixedGeometry(t *testing.T) {
	m := modelPickerTestModel(t, 60, 12)
	_, _ = m.startHelp()
	baseHeight := lipgloss.Height(m.renderHelp())
	if plain := stripANSI(m.renderHelp()); !strings.Contains(plain, "Commands") || !strings.Contains(plain, "/agent") {
		t.Fatalf("help starts without command registry: %q", plain)
	}
	help := strings.Join(m.helpLines(), "\n")
	if !strings.Contains(help, "alt+m — models") {
		t.Fatalf("help missing model shortcut: %q", help)
	}
	if !strings.Contains(help, "Ctrl+A: select only the composer draft") {
		t.Fatalf("help missing composer Select All guidance: %q", help)
	}

	_, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEnd})
	if m.helpOffset != m.helpOffsetLimit() || m.helpOffset == 0 {
		t.Fatalf("End offset=%d limit=%d", m.helpOffset, m.helpOffsetLimit())
	}
	if plain := stripANSI(m.renderHelp()); !strings.Contains(plain, "Mouse:") {
		t.Fatalf("last help page missing behavior notes: %q", plain)
	}
	if got := lipgloss.Height(m.renderHelp()); got != baseHeight {
		t.Fatalf("scroll changed card height %d -> %d", baseHeight, got)
	}

	endOffset := m.helpOffset
	_, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.helpOffset >= endOffset {
		t.Fatalf("PageUp did not move offset: %d -> %d", endOffset, m.helpOffset)
	}
	_, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyHome})
	if m.helpOffset != 0 {
		t.Fatalf("Home offset=%d", m.helpOffset)
	}
	_, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickHelp {
		t.Fatal("Esc did not close help")
	}
}

func TestHelpCardWrapsCompleteReferenceOnNarrowTerminals(t *testing.T) {
	m := modelPickerTestModel(t, 32, 12)
	m.app.Cfg.TUI.Mouse = true
	width := m.pickerCardGeometry().innerWidth
	rows := m.helpRows(width)
	wrapped := strings.Join(strings.Fields(strings.Join(rows, "\n")), "")
	var thinkingLine, mouseLine string
	for _, line := range m.helpLines() {
		switch {
		case strings.HasPrefix(line, "  /thinking"):
			thinkingLine = line
		case strings.Contains(line, "F6 restores native terminal selection"):
			mouseLine = line
		}
	}
	for label, line := range map[string]string{"thinking": thinkingLine, "mouse": mouseLine} {
		if line == "" {
			t.Fatalf("missing raw %s help line", label)
		}
		want := strings.Join(strings.Fields(line), "")
		if !strings.Contains(wrapped, want) {
			t.Fatalf("wrapped help lost %s content: want %q in %q", label, want, wrapped)
		}
	}
	for i, row := range rows {
		if got := xansi.StringWidth(row); got > width {
			t.Fatalf("wrapped row %d width=%d want<=%d: %q", i, got, width, row)
		}
	}
}

func TestBlockingRequestPreemptsCenteredHelpCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startHelp()
	m.permPending = true
	m.permRequest = &protocol.PermissionRequest{Tool: "bash", Risk: "exec"}
	if status := m.currentHeaderStatus(); status != "permission" {
		t.Fatalf("preempted header status=%q", status)
	}
	view := stripANSI(m.View())
	if strings.Contains(view, "Commands") || !strings.Contains(view, "bash") {
		t.Fatalf("permission did not preempt help card: %q", view)
	}
	m.permPending = false
	if view = stripANSI(m.View()); !strings.Contains(view, "Commands") {
		t.Fatalf("help card did not resume after permission: %q", view)
	}
}

func TestHelpCardOwnsPointerAndPagingInput(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{
		"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
	})
	m.transcriptSelectionMenu.open = true
	_, _ = m.startHelp()
	if m.transcriptSelectionMenu.open {
		t.Fatal("help card left the transcript context menu open")
	}
	m.layout()
	m.transcript.GotoTop()

	_, _ = m.Update(tea.MouseMsg{
		X: 1, Y: m.transcriptSelectionTop(),
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if m.transcriptSelection.anchor != nil || m.transcriptSelection.pressActive {
		t.Fatal("pointer selected transcript behind help card")
	}
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.transcript.YOffset != 0 {
		t.Fatalf("wheel scrolled transcript behind help card to %d", m.transcript.YOffset)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.transcript.YOffset != 0 {
		t.Fatalf("PageDown scrolled transcript behind help card to %d", m.transcript.YOffset)
	}
	if m.helpOffset == 0 {
		t.Fatal("PageDown did not scroll help content")
	}
}
