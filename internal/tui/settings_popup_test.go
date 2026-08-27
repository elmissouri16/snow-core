package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSettingsCardIsCenteredWithoutChangingFrameGeometry(t *testing.T) {
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
			_, _ = m.startSettings()
			m.layout()
			if m.transcript.Height != beforeTranscriptHeight {
				t.Fatalf("transcript height changed %d -> %d", beforeTranscriptHeight, m.transcript.Height)
			}
			if overlay := stripANSI(m.renderOverlays()); strings.Contains(overlay, "Changes save immediately") {
				t.Fatalf("settings remained in the layout overlay: %q", overlay)
			}

			card := m.renderSettings()
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

func TestSettingsCardWindowFollowsSelectionAndKeepsGeometry(t *testing.T) {
	m := modelPickerTestModel(t, 60, 12)
	_, _ = m.startSettings()
	baseHeight := lipgloss.Height(m.renderSettings())

	for _, test := range []struct {
		index int
		want  string
	}{
		{index: settingsModel, want: "Model"},
		{index: settingsTextVerbosity, want: "Text verbosity"},
		{index: settingsSkills, want: "Agent Skills"},
	} {
		m.settingsIndex = test.index
		m.settingsStatus = "saved without moving the card"
		card := m.renderSettings()
		plain := stripANSI(card)
		if !strings.Contains(plain, "› "+test.want) {
			t.Fatalf("selection %d not visible: %q", test.index, plain)
		}
		if got := lipgloss.Height(card); got != baseHeight {
			t.Fatalf("selection %d changed card height %d -> %d", test.index, baseHeight, got)
		}
		for row, line := range strings.Split(card, "\n") {
			if width := xansi.StringWidth(line); width > m.managedFrameWidth() {
				t.Fatalf("selection %d row %d width=%d frame=%d", test.index, row, width, m.managedFrameWidth())
			}
		}
	}

	m.settingsStatus = ""
	m.settingsError = "save failed\x1b]52;c;hidden\a\nshifted"
	card := m.renderSettings()
	if got := lipgloss.Height(card); got != baseHeight {
		t.Fatalf("error changed card height %d -> %d", baseHeight, got)
	}
	plain := stripANSI(card)
	if strings.Contains(card, "\x1b]52") || !strings.Contains(plain, "hidden shifted") {
		t.Fatalf("settings error retained controls or failed to flatten layout: %q", plain)
	}
}

func TestSettingsHorizontalArrowsChangeValueWithoutMovingRows(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startSettings()
	m.settingsIndex = settingsPermission
	before := m.app.Perm.Mode()

	_, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.settingsIndex != settingsPermission {
		t.Fatalf("Right moved selection to row %d", m.settingsIndex)
	}
	if got := m.app.Perm.Mode(); got == before {
		t.Fatalf("Right left permission unchanged at %q", got)
	}

	_, _ = m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.settingsIndex != settingsPermission {
		t.Fatalf("Left moved selection to row %d", m.settingsIndex)
	}
	if got := m.app.Perm.Mode(); got != before {
		t.Fatalf("Left did not restore permission: got %q want %q", got, before)
	}
}

func TestSettingsModelCatalogFailureReturnsToCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.asyncIO = true
	_, _ = m.startSettings()
	beforeLines := len(m.lines)
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.pickModel || !m.settingsReturnToPanel {
		t.Fatalf("model load command=%v picker=%v return=%v", cmd != nil, m.pickModel, m.settingsReturnToPanel)
	}
	m.modelList = nil
	_, _ = m.Update(modelListMsg{generation: m.pickerGeneration, err: errors.New("catalog unavailable")})
	if !m.pickSettings || m.pickModel || m.settingsReturnToPanel {
		t.Fatalf("catalog failure settings=%v model=%v return=%v", m.pickSettings, m.pickModel, m.settingsReturnToPanel)
	}
	if !strings.Contains(m.settingsError, "catalog unavailable") {
		t.Fatalf("catalog failure error=%q", m.settingsError)
	}
	if len(m.lines) != beforeLines {
		t.Fatalf("catalog failure escaped card into transcript: before=%d after=%d", beforeLines, len(m.lines))
	}
	if card := stripANSI(m.renderSettings()); !strings.Contains(card, "catalog unavailable") {
		t.Fatalf("catalog failure missing from restored card: %q", card)
	}
}

func TestSynchronousModelCatalogFailureReturnsToSettingsCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.pickModel = true
	m.settingsReturnToPanel = true
	beforeLines := len(m.lines)
	m.failModelPick("no models available")
	if !m.pickSettings || m.pickModel || m.settingsReturnToPanel || m.settingsError != "no models available" {
		t.Fatalf("sync failure settings=%v model=%v return=%v error=%q", m.pickSettings, m.pickModel, m.settingsReturnToPanel, m.settingsError)
	}
	if len(m.lines) != beforeLines {
		t.Fatalf("sync failure escaped card into transcript: before=%d after=%d", beforeLines, len(m.lines))
	}

	direct := modelPickerTestModel(t, 100, 30)
	direct.pickModel = true
	directLines := len(direct.lines)
	direct.failModelPick("no models available")
	if direct.pickSettings || direct.pickModel || len(direct.lines) != directLines+1 {
		t.Fatalf("direct failure settings=%v model=%v lines=%d want=%d", direct.pickSettings, direct.pickModel, len(direct.lines), directLines+1)
	}
	if got := stripANSI(direct.lines[len(direct.lines)-1]); !strings.Contains(got, "no models available") {
		t.Fatalf("direct failure transcript=%q", got)
	}
}

func TestThinkingPickersBlockBackgroundTranscriptPaging(t *testing.T) {
	for _, returnToModel := range []bool{false, true} {
		t.Run(fmt.Sprintf("return_to_model_%v", returnToModel), func(t *testing.T) {
			m := newTranscriptSelectionTestModel(t, []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"})
			m.pickThinking = true
			m.thinkingReturnToModel = returnToModel
			m.thinkingList = []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingLow}
			m.transcript.GotoTop()
			_, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
			if m.transcript.YOffset != 0 {
				t.Fatalf("PageDown scrolled transcript behind thinking picker to %d", m.transcript.YOffset)
			}
		})
	}
}

func TestBlockingRequestPreemptsCenteredSettingsCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startSettings()
	m.permPending = true
	m.permRequest = &protocol.PermissionRequest{Tool: "bash", Risk: "exec"}
	if status := m.currentHeaderStatus(); status != "permission" {
		t.Fatalf("preempted header status=%q", status)
	}
	view := stripANSI(m.View())
	if strings.Contains(view, "Changes save immediately") || !strings.Contains(view, "bash") {
		t.Fatalf("permission did not preempt settings card: %q", view)
	}
	m.permPending = false
	if view = stripANSI(m.View()); !strings.Contains(view, "Changes save immediately") {
		t.Fatalf("settings card did not resume after permission: %q", view)
	}
}

func TestSettingsCardBlocksBackgroundPointerAndTranscriptPaging(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"})
	m.transcriptSelectionMenu.open = true
	_, _ = m.startSettings()
	if m.transcriptSelectionMenu.open {
		t.Fatal("settings card left the transcript context menu open")
	}
	m.layout()
	m.transcript.GotoTop()

	_, _ = m.Update(tea.MouseMsg{X: 1, Y: m.transcriptSelectionTop(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.transcriptSelection.anchor != nil || m.transcriptSelection.pressActive {
		t.Fatal("pointer selected transcript behind settings card")
	}
	_, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.transcript.YOffset != 0 {
		t.Fatalf("wheel scrolled transcript behind settings card to %d", m.transcript.YOffset)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.transcript.YOffset != 0 {
		t.Fatalf("PageDown scrolled transcript behind settings card to %d", m.transcript.YOffset)
	}
}
