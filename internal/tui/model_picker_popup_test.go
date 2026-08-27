package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func modelPickerTestModel(t *testing.T, width, height int) *Model {
	t.Helper()
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Cfg.TUI.Mouse = true
	m.width, m.height = width, height
	m.layout()
	return m
}

func TestHeaderModelHitTargetOpensPicker(t *testing.T) {
	m := modelPickerTestModel(t, 120, 30)
	header := m.renderHeaderLayout(m.currentHeaderStatus())
	if header.modelStart <= 0 || header.modelEnd <= header.modelStart {
		t.Fatalf("model bounds = [%d,%d), header=%q", header.modelStart, header.modelEnd, stripANSI(header.view))
	}
	if got := stripANSI(header.view); !strings.Contains(got, m.app.ProviderID+"/"+m.app.Model.ID+" ▾") {
		t.Fatalf("header selector missing: %q", got)
	}

	_, cmd := m.Update(tea.MouseMsg{X: header.modelStart, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.pickModel {
		t.Fatal("header model click did not open picker")
	}
	if cmd != nil {
		t.Fatal("cached fake catalog unexpectedly required async command")
	}
	if m.modelQuery != "" {
		t.Fatalf("initial query=%q", m.modelQuery)
	}
}

func TestHeaderThinkingAndModeHitTargets(t *testing.T) {
	thinking := modelPickerTestModel(t, 120, 30)
	header := thinking.renderHeaderLayout(thinking.currentHeaderStatus())
	if header.thinkingEnd <= header.thinkingStart || header.modeEnd <= header.modeStart {
		t.Fatalf("header control bounds: thinking=[%d,%d) mode=[%d,%d) view=%q", header.thinkingStart, header.thinkingEnd, header.modeStart, header.modeEnd, stripANSI(header.view))
	}
	_, cmd := thinking.Update(tea.MouseMsg{X: header.thinkingStart, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !thinking.pickThinking || thinking.thinkingReturnToModel || cmd != nil {
		t.Fatalf("thinking click: open=%v return_to_model=%v cmd=%v", thinking.pickThinking, thinking.thinkingReturnToModel, cmd != nil)
	}

	mode := modelPickerTestModel(t, 120, 30)
	header = mode.renderHeaderLayout(mode.currentHeaderStatus())
	_, cmd = mode.Update(tea.MouseMsg{X: header.modeEnd - 1, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil || !mode.modeSwitching || mode.pendingMode == nil || *mode.pendingMode != protocol.ModePlan {
		t.Fatalf("mode click: cmd=%v switching=%v pending=%v", cmd != nil, mode.modeSwitching, mode.pendingMode)
	}
	msg := cmd()
	_, _ = mode.Update(msg)
	if got := mode.app.Agent.Mode(); got != protocol.ModePlan {
		t.Fatalf("mode after click=%q", got)
	}
}

func TestHeaderModeClickQueuesDuringActiveTurn(t *testing.T) {
	m := modelPickerTestModel(t, 120, 30)
	m.busy = true
	header := m.renderHeaderLayout(m.currentHeaderStatus())
	_, cmd := m.Update(tea.MouseMsg{X: header.modeStart, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil || m.pendingMode == nil || *m.pendingMode != protocol.ModePlan {
		t.Fatalf("busy mode click: cmd=%v pending=%v", cmd != nil, m.pendingMode)
	}
	if m.lastStatus != "mode switches after current turn" {
		t.Fatalf("busy mode status=%q", m.lastStatus)
	}
}

func TestAltMOpensModelPickerAndRespectsActiveTurn(t *testing.T) {
	altM := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true}
	m := modelPickerTestModel(t, 80, 24)
	_, cmd := m.handleKey(altM)
	if !m.pickModel || cmd != nil {
		t.Fatalf("idle Alt+M: picker=%v cmd=%v", m.pickModel, cmd != nil)
	}

	busy := modelPickerTestModel(t, 80, 24)
	busy.busy = true
	_, cmd = busy.handleKey(altM)
	if busy.pickModel || cmd != nil {
		t.Fatalf("busy Alt+M: picker=%v cmd=%v", busy.pickModel, cmd != nil)
	}
	if want := "model: wait for the current turn to finish"; busy.lastStatus != want {
		t.Fatalf("busy Alt+M status=%q want=%q", busy.lastStatus, want)
	}
}

func TestHeaderModelHitTargetMatchesRenderedModes(t *testing.T) {
	for _, width := range []int{28, 60, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			m := modelPickerTestModel(t, width, 20)
			header := m.renderHeaderLayout("idle")
			if header.modelEnd > m.managedFrameWidth() || header.modelEnd <= header.modelStart {
				t.Fatalf("width=%d bounds=[%d,%d) frame=%d header=%q", width, header.modelStart, header.modelEnd, m.managedFrameWidth(), stripANSI(header.view))
			}
			if width >= 48 && header.modeEnd <= header.modeStart {
				t.Fatalf("width=%d missing visible mode hit target: %+v", width, header)
			}
			if width >= 80 && header.thinkingEnd <= header.thinkingStart {
				t.Fatalf("width=%d missing visible thinking hit target: %+v", width, header)
			}
			_, _ = m.Update(tea.MouseMsg{X: header.modelEnd, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if m.pickModel {
				t.Fatalf("width=%d click outside rendered selector opened picker", width)
			}

			m.app.Cfg.TUI.Mouse = false
			native := m.renderHeaderLayout("idle")
			if native.modelStart != 0 || native.modelEnd != 0 || native.thinkingStart != 0 || native.thinkingEnd != 0 || native.modeStart != 0 || native.modeEnd != 0 || strings.Contains(stripANSI(native.view), "▾") {
				t.Fatalf("native header retained controls: model=[%d,%d) thinking=[%d,%d) mode=[%d,%d) view=%q", native.modelStart, native.modelEnd, native.thinkingStart, native.thinkingEnd, native.modeStart, native.modeEnd, stripANSI(native.view))
			}
			_, _ = m.Update(tea.MouseMsg{X: header.modelStart, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if m.pickModel {
				t.Fatalf("width=%d native mouse mode opened picker", width)
			}
		})
	}
}

func TestHeaderModelHitTargetHandlesWideGraphemes(t *testing.T) {
	m := modelPickerTestModel(t, 28, 20)
	model := protocol.Model{Provider: "fake", ID: strings.Repeat("界", 20) + "e\u0301"}
	if err := m.app.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}
	m.app.Model = model
	header := m.renderHeaderLayout("idle")
	if width := xansi.StringWidth(header.view); width != m.managedFrameWidth() {
		t.Fatalf("header width=%d want=%d: %q", width, m.managedFrameWidth(), stripANSI(header.view))
	}
	if header.modelStart < 0 || header.modelEnd > m.managedFrameWidth() || header.modelEnd <= header.modelStart {
		t.Fatalf("wide selector bounds=[%d,%d) frame=%d", header.modelStart, header.modelEnd, m.managedFrameWidth())
	}
	if width := xansi.StringWidth(xansi.Cut(header.view, header.modelStart, header.modelEnd)); width != header.modelEnd-header.modelStart {
		t.Fatalf("rendered selector width=%d bounds=%d", width, header.modelEnd-header.modelStart)
	}
	_, _ = m.Update(tea.MouseMsg{X: header.modelEnd - 1, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.pickModel {
		t.Fatal("wide rendered selector was not clickable at its final cell")
	}
}

func TestHeaderModelClickFailsClosedWhileBusy(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.busy = true
	header := m.renderHeaderLayout(m.currentHeaderStatus())
	_, _ = m.Update(tea.MouseMsg{X: header.modelStart, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.pickModel || !strings.Contains(m.lastStatus, "wait for the current turn") {
		t.Fatalf("busy click picker=%v status=%q", m.pickModel, m.lastStatus)
	}
}

func TestModelPickerIsCenteredWithoutChangingFrameGeometry(t *testing.T) {
	for _, size := range []struct{ width, height int }{{100, 30}, {60, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := modelPickerTestModel(t, size.width, size.height)
			beforeTranscriptHeight := m.transcript.Height
			_, _ = m.startModelPick()
			m.layout()
			if m.transcript.Height != beforeTranscriptHeight {
				t.Fatalf("transcript height changed %d -> %d", beforeTranscriptHeight, m.transcript.Height)
			}
			card := m.renderModelModal()
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
			if y >= len(lines) || xansi.StringWidth(lines[y]) < x+1 || []rune(lines[y])[x] != '╭' {
				t.Fatalf("centered border missing at (%d,%d): %q", x, y, lines[y])
			}
		})
	}
}

func TestModelPickerWindowKeepsSelectionVisibleAndBounded(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	models := make([]protocol.Model, 40)
	for i := range models {
		models[i] = protocol.Model{
			Provider:    fmt.Sprintf("provider-%d", i%3),
			ID:          fmt.Sprintf("model-%02d", i),
			DisplayName: strings.Repeat("界", 60),
			Description: strings.Repeat("A bounded model description should wrap without widening the card. ", 8),
		}
	}
	m.app.AllModels = models
	_, _ = m.startModelPick()
	wantHeight := lipgloss.Height(m.renderModelPicker())
	for _, selected := range []int{0, 20, 39} {
		m.modelIndex = selected
		card := m.renderModelPicker()
		plain := stripANSI(card)
		if !strings.Contains(plain, "› "+models[selected].ID) {
			t.Fatalf("selection %d not visible: %q", selected, plain)
		}
		if got := lipgloss.Height(card); got != wantHeight {
			t.Fatalf("selection %d changed card height %d -> %d", selected, wantHeight, got)
		}
		for row, line := range strings.Split(card, "\n") {
			if width := xansi.StringWidth(line); width > pickerCardMaxWidth {
				t.Fatalf("selection %d row %d width=%d", selected, row, width)
			}
		}
	}
}

func TestStandaloneThinkingPickerUsesCenteredFixedFrameCard(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprintf("inline_%v", inline), func(t *testing.T) {
			m := modelPickerTestModel(t, 70, 18)
			m.inlineTranscript = inline
			model := m.app.Agent.Model()
			model.SupportsThinking = true
			model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh}
			if err := m.app.Agent.SetModel(model); err != nil {
				t.Fatal(err)
			}
			m.app.Model = model
			m.layout()
			beforeTranscriptHeight := m.transcript.Height
			_, _ = m.startThinkingPick()
			m.layout()
			if !m.thinkingModalVisible() || m.modelModalVisible() {
				t.Fatalf("thinking modal=%v model modal=%v", m.thinkingModalVisible(), m.modelModalVisible())
			}
			if m.transcript.Height != beforeTranscriptHeight {
				t.Fatalf("transcript height changed %d -> %d", beforeTranscriptHeight, m.transcript.Height)
			}
			if overlay := stripANSI(m.renderOverlays()); strings.Contains(overlay, "Thinking effort") {
				t.Fatalf("thinking picker still participates in overlay layout: %q", overlay)
			}
			card := m.renderThinkingModal()
			plainCard := stripANSI(card)
			for _, want := range []string{"Thinking effort", "subsequent provider requests", "Esc close", "› off"} {
				if !strings.Contains(plainCard, want) {
					t.Fatalf("card missing %q: %q", want, plainCard)
				}
			}
			view := m.View()
			if got := lipgloss.Height(view); got != m.managedFrameHeight() {
				t.Fatalf("view height=%d want=%d", got, m.managedFrameHeight())
			}
			cardWidth, cardHeight := transcriptSelectionBlockWidth(card), lipgloss.Height(card)
			x := (m.managedFrameWidth() - cardWidth) / 2
			y := (m.managedFrameHeight() - cardHeight) / 2
			lines := strings.Split(stripANSI(view), "\n")
			if y >= len(lines) || stripANSI(xansi.Cut(lines[y], x, x+1)) != "╭" {
				t.Fatalf("centered thinking border missing at (%d,%d): %q", x, y, lines[y])
			}
		})
	}
}

func TestModelPickerRowsStayBoundedNearMinimumFrame(t *testing.T) {
	m := modelPickerTestModel(t, 8, 10)
	models := make([]protocol.Model, 10)
	for i := range models {
		models[i] = protocol.Model{Provider: "p", ID: fmt.Sprintf("m%d", i)}
	}
	m.app.AllModels = models
	_, _ = m.startModelPick()
	m.modelIndex = 5
	card := m.renderModelPicker()
	if got := stripANSI(card); !strings.Contains(got, "› m5") {
		t.Fatalf("narrow selected model disappeared: %q", got)
	}
	assertModelCardBounds(t, m, card)

	thinkingModel := protocol.Model{
		Provider: "p", ID: "m5", SupportsThinking: true,
		ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh},
	}
	m.pickModel = false
	m.startThinkingPickForModel(thinkingModel, true)
	m.thinkingIndex = 1
	thinkingCard := m.renderModelThinkingPicker()
	if got := stripANSI(thinkingCard); !strings.Contains(got, "› low") {
		t.Fatalf("narrow selected thinking effort disappeared: %q", got)
	}
	assertModelCardBounds(t, m, thinkingCard)
}

func assertModelCardBounds(t *testing.T, m *Model, card string) {
	t.Helper()
	if height := lipgloss.Height(card); height > m.managedFrameHeight() {
		t.Fatalf("card height=%d frame=%d", height, m.managedFrameHeight())
	}
	for row, line := range strings.Split(card, "\n") {
		if width := xansi.StringWidth(line); width > m.managedFrameWidth() {
			t.Fatalf("card row %d width=%d frame=%d: %q", row, width, m.managedFrameWidth(), stripANSI(line))
		}
	}
}

func TestModelPickerTypeToFilterAndRuneNavigationContract(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.app.AllModels = []protocol.Model{
		{Provider: "fake", ID: "alpha"},
		{Provider: "fake", ID: "jupiter", DisplayName: "Jupiter Prime"},
		{Provider: "other", ID: "beta"},
	}
	_, _ = m.startModelPick()
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("界")})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.modelQuery != "j" || len(m.filteredModels()) != 1 || m.filteredModels()[0].ID != "jupiter" {
		t.Fatalf("query=%q matches=%+v", m.modelQuery, m.filteredModels())
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickModel || m.modelQuery != "" {
		t.Fatalf("clear picker=%v query=%q", m.pickModel, m.modelQuery)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jupiter")})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeySpace})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("prime")})
	if m.modelQuery != "jupiter prime" || len(m.filteredModels()) != 1 {
		t.Fatalf("space query=%q matches=%+v", m.modelQuery, m.filteredModels())
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyCtrlU})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyDown})
	if m.modelIndex != 1 {
		t.Fatalf("arrow navigation index=%d", m.modelIndex)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.modelIndex != 2 {
		t.Fatalf("page navigation index=%d", m.modelIndex)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyHome})
	if m.modelIndex != 0 {
		t.Fatalf("home navigation index=%d", m.modelIndex)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnd})
	if m.modelIndex != 2 {
		t.Fatalf("end navigation index=%d", m.modelIndex)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.modelQuery != "" {
		t.Fatalf("ctrl+u query=%q", m.modelQuery)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickModel {
		t.Fatal("empty-query Esc did not close picker")
	}
}

func TestModelCatalogRefreshPreservesQueryAndSelection(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	m.pickModel = true
	m.modelLoading = true
	m.modelQuery = "spark"
	m.modelList = []protocol.Model{
		{Provider: "fake", ID: "spark-old", DisplayName: "Spark Old"},
		{Provider: "fake", ID: "spark-keep", DisplayName: "Spark Keep"},
	}
	m.modelIndex = 1
	m.pickerGeneration = 7
	_, _ = m.Update(modelListMsg{generation: 7, models: []protocol.Model{
		{Provider: "other", ID: "spark-new", DisplayName: "Spark New"},
		{Provider: "fake", ID: "spark-keep", DisplayName: "Spark Keep"},
		{Provider: "fake", ID: "plain"},
	}})
	matches := m.filteredModels()
	if m.modelQuery != "spark" || len(matches) != 2 {
		t.Fatalf("query=%q matches=%+v", m.modelQuery, matches)
	}
	if selected := matches[m.modelIndex]; selected.Provider != "fake" || selected.ID != "spark-keep" {
		t.Fatalf("selection=%+v index=%d", selected, m.modelIndex)
	}
}

func TestModelThinkingStepStaysInCenteredFlow(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	model := protocol.Model{
		Provider: "fake", ID: "reasoner", SupportsThinking: true,
		ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh},
	}
	m.app.AllModels = []protocol.Model{model}
	_, _ = m.startModelPick()
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("reason")})
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.modelModalVisible() || m.pickModel || !m.pickThinking || !m.thinkingReturnToModel {
		t.Fatalf("model=%v thinking=%v return=%v", m.pickModel, m.pickThinking, m.thinkingReturnToModel)
	}
	if got := stripANSI(m.renderModelModal()); !strings.Contains(got, "Thinking effort") || !strings.Contains(got, "reasoner") {
		t.Fatalf("thinking card=%q", got)
	}
	if overlay := stripANSI(m.renderOverlays()); strings.Contains(overlay, "Thinking effort") {
		t.Fatalf("nested thinking leaked into bottom overlay: %q", overlay)
	}
	m.modelLoading = true
	_, _ = m.Update(modelListMsg{generation: m.pickerGeneration, models: []protocol.Model{
		model,
		{Provider: "fake", ID: "plain"},
	}})
	if m.modelLoading || m.modelQuery != "reason" || !m.pickThinking {
		t.Fatalf("nested refresh loading=%v query=%q thinking=%v", m.modelLoading, m.modelQuery, m.pickThinking)
	}
	_, _ = m.handleThinkingPick(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickModel || m.pickThinking || m.modelQuery != "reason" {
		t.Fatalf("back model=%v thinking=%v query=%q", m.pickModel, m.pickThinking, m.modelQuery)
	}
}

func TestBlockingRequestPreemptsCenteredModelCard(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	_, _ = m.startModelPick()
	m.permPending = true
	m.permRequest = &protocol.PermissionRequest{Tool: "bash", Risk: "exec"}
	if status := m.currentHeaderStatus(); status != "permission" {
		t.Fatalf("preempted header status=%q", status)
	}
	view := stripANSI(m.View())
	if strings.Contains(view, "Search:") || !strings.Contains(view, "bash") {
		t.Fatalf("permission did not preempt model card: %q", view)
	}
	m.permPending = false
	if view = stripANSI(m.View()); !strings.Contains(view, "Search:") {
		t.Fatalf("model card did not resume after permission: %q", view)
	}
}

func TestOverlayFrameBlockReplacesBisectedWideGraphemes(t *testing.T) {
	for _, base := range []string{"A界BC ", "A👩‍💻BC "} {
		for _, test := range []struct {
			x    int
			want string
		}{{x: 1, want: "AX BC "}, {x: 2, want: "A XBC "}} {
			out := overlayFrameBlock(base, "X", test.x, 0, 1)
			if got := stripANSI(out); got != test.want {
				t.Fatalf("base=%q x=%d got=%q want=%q", base, test.x, got, test.want)
			}
			if width := xansi.StringWidth(out); width != 6 {
				t.Fatalf("base=%q x=%d width=%d want=6", base, test.x, width)
			}
		}
	}
}

func TestOverlayFrameBlockPreservesDimensions(t *testing.T) {
	frame := lipgloss.NewStyle().Width(24).Height(6).Render(styleCompletionSelected.Render("wide 界") + "\nsecond")
	block := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Render("popup")
	out := overlayFrameBlock(frame, block, 17, 2, 7)
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("line count=%d want=6", len(lines))
	}
	for i, line := range lines {
		if width := xansi.StringWidth(line); width != 24 {
			t.Fatalf("line %d width=%d want=24: %q", i, width, stripANSI(line))
		}
	}
}
