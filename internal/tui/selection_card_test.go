package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func openSelectionTestCard(m *Model, name string) {
	switch name {
	case "info":
		m.pickInfo, m.infoTitle, m.infoIndex = true, "MCP servers", 29
		for i := range 30 {
			m.infoItems = append(m.infoItems, statusInfoItem{Label: fmt.Sprintf("server-%02d · connected · stdio · 界面", i), Detail: "Server version 1.8.0 · 29 tools · tools, logging"})
		}
	case "sessions":
		m.pickSession, m.sessionIndex = true, 29
		for i := range 30 {
			m.sessions = append(m.sessions, session.SessionInfo{ID: fmt.Sprintf("session-%02d", i), Name: fmt.Sprintf("name-%02d 界面", i)})
		}
	case "tree":
		m.pickTree, m.branchIndex = true, 29
		for i := range 30 {
			m.branches = append(m.branches, protocol.SessionBranch{ID: fmt.Sprintf("branch-%02d", i), Name: fmt.Sprintf("branch-%02d 界面", i)})
		}
	case "fork":
		m.pickFork, m.forkIndex = true, 2
	case "permissions":
		m.pickPermissionMode, m.permissionModeIndex = true, 2
	case "plan":
		m.planPrompt, m.planPromptChoice = true, 2
	case "goal":
		m.confirmGoalReplace = true
	}
}

func assertCenteredSelectionCard(t *testing.T, m *Model, card string) {
	t.Helper()
	width, height := lipgloss.Width(card), lipgloss.Height(card)
	if width > m.managedFrameWidth() || height > m.managedFrameHeight() {
		t.Fatalf("card %dx%d exceeds frame %dx%d:\n%s", width, height, m.managedFrameWidth(), m.managedFrameHeight(), card)
	}
	frame := m.viewContent()
	if lipgloss.Height(frame) != m.managedFrameHeight() {
		t.Fatalf("frame height=%d want %d", lipgloss.Height(frame), m.managedFrameHeight())
	}
	for _, row := range strings.Split(frame, "\n") {
		if got := xansi.StringWidth(row); got != m.managedFrameWidth() {
			t.Fatalf("frame row width=%d want %d: %q", got, m.managedFrameWidth(), row)
		}
	}
	x, y := (m.managedFrameWidth()-width)/2, (m.managedFrameHeight()-height)/2
	rows := strings.Split(stripANSI(frame), "\n")
	if xansi.Cut(rows[y], x, x+1) != "╭" || xansi.Cut(rows[y+height-1], x+width-1, x+width) != "╯" {
		t.Fatalf("card is not centered at %d,%d:\n%s", x, y, frame)
	}
}

func TestNativeSelectionCardsResizeWithoutChangingTranscript(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, name := range []string{"info", "sessions", "tree", "fork", "permissions", "plan", "goal"} {
			t.Run(fmt.Sprintf("inline=%v/%s", inline, name), func(t *testing.T) {
				m := modelPickerTestModel(t, 120, 30)
				keys, err := applyKeybindingOverrides(m.keys, map[string][]string{
					"branch_fork": {"1"}, "branch_rename": {"2"}, "branch_delete": {"3"},
				})
				if err != nil {
					t.Fatal(err)
				}
				m.keys = keys
				m.inlineTranscript = inline
				m.editor.SetValue("keep this draft")
				m.layout()
				before := m.transcript.Height()
				openSelectionTestCard(m, name)
				m.layout()
				if got := m.transcript.Height(); got != before {
					t.Fatalf("opening %s changed transcript height %d -> %d", name, before, got)
				}
				for _, size := range [][2]int{{240, 70}, {80, 24}, {40, 12}, {20, 8}, {27, 10}, {60, 16}, {120, 30}} {
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					if m.renderOverlays() != "" {
						t.Fatal("modal remained in composer overlays")
					}
					card := m.renderSelectionModal()
					assertCenteredSelectionCard(t, m, card)
					plain := stripANSI(card)
					if !strings.Contains(plain, "›") || !strings.Contains(strings.ToLower(plain), "esc") {
						t.Fatalf("%dx%d lost selection/controls:\n%s", size[0], size[1], card)
					}
					if name == "info" && !strings.Contains(plain, "server-29") {
						t.Fatalf("last selected server not visible:\n%s", card)
					}
					if size[0] == 40 && size[1] == 12 {
						var management []string
						switch name {
						case "sessions":
							management = []string{"2 rename", "3 delete"}
						case "tree":
							management = []string{"1 fork", "2 rename", "3 delete"}
						}
						for _, want := range management {
							if !strings.Contains(plain, want) {
								t.Fatalf("%s compact footer omitted configured action %q:\n%s", name, want, card)
							}
						}
					}
					if m.editor.Value() != "keep this draft" {
						t.Fatal("resize changed draft")
					}
				}
			})
		}
	}
}

func TestNativeSelectionCardsOwnKeyboardMouseAndPaste(t *testing.T) {
	for _, name := range []string{"info", "sessions", "tree", "fork", "permissions", "plan", "goal"} {
		t.Run(name, func(t *testing.T) {
			m := modelPickerTestModel(t, 100, 30)
			m.editor.SetValue("untouched")
			m.lines = strings.Split(strings.Repeat("background transcript\n", 100), "\n")
			m.transcriptBaseDirty = true
			m.refreshTranscriptForced()
			m.transcript.GotoBottom()
			openSelectionTestCard(m, name)
			before := m.transcript.YOffset()
			for _, msg := range []tea.Msg{
				tea.KeyPressMsg{Code: tea.KeyPgUp}, tea.KeyPressMsg{Code: tea.KeyHome},
				tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp},
				tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft},
				tea.MouseMotionMsg{X: 20, Y: 6, Button: tea.MouseLeft},
				tea.MouseReleaseMsg{X: 20, Y: 6, Button: tea.MouseLeft},
				tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseRight},
				tea.PasteMsg{Content: "must not reach composer"},
			} {
				m.Update(msg)
			}
			if m.transcript.YOffset() != before || m.editor.Value() != "untouched" {
				t.Fatal("modal input reached background transcript/composer")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.selectionModalVisible() {
				t.Fatal("Esc failed to dismiss modal")
			}
		})
	}
}

func TestNativeSelectionCardLoadingEditingAndEmptyStates(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	m.startInfoPicker("MCP servers", nil)
	if !m.pickInfo || !strings.Contains(stripANSI(m.renderInfoPicker()), "None configured") {
		t.Fatal("empty MCP inventory did not open a card")
	}
	m.infoLoading = true
	if !strings.Contains(stripANSI(m.renderInfoPicker()), "Loading") {
		t.Fatal("info loading state missing")
	}
	m.closeInfoPicker()
	openSelectionTestCard(m, "sessions")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m.sessionLoading = true
	card := stripANSI(m.renderSessionPicker())
	if !strings.Contains(card, "loading sessions") || !strings.Contains(strings.ToLower(card), "esc cancel") || strings.Contains(card, "rename") {
		t.Fatalf("compact session loading advertised unavailable actions:\n%s", card)
	}
	m.sessionLoading = false
	m.sessionRenaming = true
	m.sessionRenameInput = strings.Repeat("界", 40)
	m.Update(tea.PasteMsg{Content: "tail"})
	for _, size := range [][2]int{{80, 24}, {20, 8}, {40, 12}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		card := m.renderSessionPicker()
		assertCenteredSelectionCard(t, m, card)
		plain := stripANSI(card)
		if !strings.Contains(plain, "tail_") {
			t.Fatalf("rename cursor hidden at %v:\n%s", size, card)
		}
		if size[0] == 40 && size[1] == 12 && (!strings.Contains(plain, "Enter save") || !strings.Contains(plain, "Esc cancel") || strings.Contains(plain, "delete")) {
			t.Fatalf("compact rename footer advertised the wrong actions:\n%s", card)
		}
	}
	m.sessionRenaming, m.sessionDeleting = false, true
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	card = stripANSI(m.renderSessionPicker())
	for _, want := range []string{"Permanently delete", "cannot be undone", "Enter confirm", "Esc cancel"} {
		if !strings.Contains(card, want) {
			t.Fatalf("delete confirmation omitted %q:\n%s", want, card)
		}
	}
	m.sessionDeleting, m.sessionLoading, m.sessionDeleteInFlight = false, true, true
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	card = stripANSI(m.renderSessionPicker())
	if !strings.Contains(card, "deleting session") || !strings.Contains(card, "Deleting; please wait") || strings.Contains(card, "rename") {
		t.Fatalf("compact delete progress advertised the wrong actions:\n%s", card)
	}
}

func TestCompactBranchEditingControlsReplaceBrowsingActions(t *testing.T) {
	loading := modelPickerTestModel(t, 40, 12)
	keys, err := applyKeybindingOverrides(loading.keys, map[string][]string{"close": {"q"}})
	if err != nil {
		t.Fatal(err)
	}
	loading.keys = keys
	openSelectionTestCard(loading, "tree")
	loading.treeLoading = true
	card := stripANSI(loading.renderTreePicker())
	if !strings.Contains(card, "loading branches") || !strings.Contains(strings.ToLower(card), "q cancel") || strings.Contains(card, " fork") {
		t.Fatalf("compact tree loading advertised unavailable actions:\n%s", card)
	}
	_, _ = loading.handleTreePick(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if loading.pickTree || loading.treeLoading {
		t.Fatal("configured compact tree loading close key was inert")
	}

	for _, action := range []string{"fork", "rename"} {
		t.Run(action, func(t *testing.T) {
			m := modelPickerTestModel(t, 40, 12)
			openSelectionTestCard(m, "tree")
			m.branchAction = action
			card := stripANSI(m.renderTreePicker())
			want := "Enter create"
			if action == "rename" {
				want = "Enter save"
			}
			if !strings.Contains(card, want) || !strings.Contains(card, "Esc cancel") || strings.Contains(card, " delete") {
				t.Fatalf("compact %s footer advertised the wrong actions:\n%s", action, card)
			}
		})
	}
}

func TestCompactBranchDeleteControlsMatchConfirmationKey(t *testing.T) {
	for _, size := range [][2]int{{40, 24}, {80, 12}, {20, 8}} {
		m := modelPickerTestModel(t, size[0], size[1])
		openSelectionTestCard(m, "tree")
		m.branchAction = "delete"
		card := stripANSI(m.renderTreePicker())
		if !strings.Contains(card, m.keys.Confirm.Help().Key+" confirm") || strings.Contains(card, "Enter") {
			t.Fatalf("%v displays a confirmation key the handler cannot accept:\n%s", size, card)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.branchAction != "delete" {
			t.Fatal("Enter unexpectedly confirmed branch deletion")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.branchAction != "" {
			t.Fatal("Escape did not cancel deletion")
		}
	}
}

func TestSelectionCardResumesAfterBlockingHostRequests(t *testing.T) {
	m := modelPickerTestModel(t, 100, 30)
	openSelectionTestCard(m, "info")
	m.startUserInput(protocol.UserInputRequest{ID: "panel-input", Questions: []protocol.UserInputQuestion{{ID: "q", Header: "Question", Question: "Choose a response"}}})
	if !strings.Contains(stripANSI(m.viewContent()), "Choose a response") {
		t.Fatal("question did not preempt inspector")
	}
	m.handleAgentEvent(permRequestEvent("read"))
	if m.currentHeaderStatus() != "permission" || !strings.Contains(stripANSI(m.viewContent()), "Esc deny") {
		t.Fatal("permission did not preempt question and inspector")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.userInputPending || !m.pickInfo || m.infoIndex != 29 || !strings.Contains(stripANSI(m.viewContent()), "Choose a response") {
		t.Fatal("permission dismissal lost suspended dialogs")
	}
	m.clearUserInput()
	assertCenteredSelectionCard(t, m, m.renderInfoPicker())
	if !strings.Contains(stripANSI(m.viewContent()), "server-29") {
		t.Fatal("inspector did not restore its selection")
	}
}

func TestSelectionCardComponentClipsTinyAndUnicodeContent(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	for width := 1; width <= 35; width++ {
		for height := 1; height <= 12; height++ {
			m.width, m.height = width, height
			card := m.renderSelectionCard(selectionCard{title: strings.Repeat("界", 100),
				items: []string{strings.Repeat("界👨‍👩‍👦é", 100)}, detail: strings.Repeat("description ", 20), footer: "Enter accept · Esc cancel"})
			if lipgloss.Width(card) > m.managedFrameWidth() || lipgloss.Height(card) > height {
				t.Fatalf("%dx%d component escaped bounds: %dx%d", width, height, lipgloss.Width(card), lipgloss.Height(card))
			}
		}
	}
}
