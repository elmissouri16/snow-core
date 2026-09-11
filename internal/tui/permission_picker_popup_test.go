package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func permissionPopupModel(t *testing.T, width, height int, inline bool) *Model {
	t.Helper()
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height, m.inlineTranscript = width, height, inline
	m.layout()
	event := permRequestEvent("bash")
	event.Permission.Request.Args = json.RawMessage(`{"command":"printf review-marker"}`)
	event.Permission.Request.Unknown = true
	event.Permission.Request.EffectsTruncated = true
	event.Permission.Request.Rememberable = false
	event.Permission.Request.Effects = []protocol.PermissionEffect{{Type: "process", Operation: "execute", Command: "printf"}}
	m.handleAgentEvent(event)
	return m
}

func assertPermissionPopupBounds(t *testing.T, m *Model, card string) {
	t.Helper()
	geometry := m.permissionCardGeometry()
	if got := lipgloss.Height(card); got > geometry.outerHeight || got > m.managedFrameHeight() {
		t.Fatalf("card height=%d, geometry=%d, frame=%d:\n%s", got, geometry.outerHeight, m.managedFrameHeight(), card)
	}
	for row, line := range strings.Split(card, "\n") {
		if got := lipgloss.Width(line); got != geometry.outerWidth || got > m.managedFrameWidth() || got > permissionCardMaxWidth {
			t.Fatalf("row %d width=%d, geometry=%d, frame=%d: %q", row, got, geometry.outerWidth, m.managedFrameWidth(), line)
		}
	}
}

func permissionPopupText(card string) string {
	var rows []string
	for line := range strings.SplitSeq(stripANSI(card), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "│") {
			rows = append(rows, strings.Trim(line, "│ "))
		}
	}
	return strings.Join(strings.Fields(strings.Join(rows, " ")), " ")
}

func TestPermissionPopupBoundsAndSafety(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, width := range []int{24, 27, 40, 72, 100, 160} {
			t.Run(fmt.Sprintf("inline=%v/width=%d", inline, width), func(t *testing.T) {
				m := permissionPopupModel(t, width, 30, inline)
				if !m.permissionApprovalEnabled() {
					t.Fatal("reviewable card unexpectedly disabled approval")
				}
				card := m.renderPermissionPicker()
				assertPermissionPopupBounds(t, m, card)
				if !strings.HasPrefix(stripANSI(card), "╭") {
					t.Fatal("renderer returned a placed frame instead of a card block")
				}
				text := permissionPopupText(card)
				for _, want := range []string{
					"bash", "Command:", "Allow once", "Deny", "Esc deny",
					"Permission analysis was truncated; review the command directly.",
					"Unknown child effects cannot be determined statically.",
					"Execution: unrestricted host process",
				} {
					if !strings.Contains(text, want) {
						t.Fatalf("card omitted %q:\n%s", want, card)
					}
				}
				if strings.Contains(text, "Allow this scope") {
					t.Fatal("unknown effects offered remembered approval")
				}
			})
		}
	}
}

func TestPermissionPopupPreservesEveryChoice(t *testing.T) {
	m := permissionPopupModel(t, 27, 30, false)
	m.permRequest.Unknown = false
	m.permRequest.Rememberable = true
	m.permRequest.ScopeLabel = "matching printf calls"
	m.permChoice = permChoiceAlways
	card := m.renderPermissionPicker()
	assertPermissionPopupBounds(t, m, card)
	for _, want := range []string{"Allow once", "› Allow this scope", "Deny", "Esc deny"} {
		if !strings.Contains(stripANSI(card), want) {
			t.Fatalf("narrow card omitted %q:\n%s", want, card)
		}
	}
}

func TestPermissionPopupTinyWindowsFailClosed(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, size := range [][2]int{{1, 1}, {2, 2}, {3, 12}, {20, 30}, {40, 3}, {40, 7}, {40, 12}, {100, 8}} {
			t.Run(fmt.Sprintf("inline=%v/%dx%d", inline, size[0], size[1]), func(t *testing.T) {
				m := permissionPopupModel(t, size[0], size[1], inline)
				card := m.renderPermissionPicker()
				assertPermissionPopupBounds(t, m, card)
				if m.permissionApprovalEnabled() {
					t.Fatal("tiny card enabled approval without visible review context")
				}
				geometry := m.permissionCardGeometry()
				if geometry.innerWidth >= 17 && !strings.Contains(stripANSI(card), "Approval disabled") {
					t.Fatalf("card omitted disabled status:\n%s", card)
				}
				_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				if !m.permPending {
					t.Fatal("Enter resolved an unreviewable request")
				}
				_, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.permPending || m.permChoice != permChoiceDeny {
					t.Fatal("Escape failed to deny an unreviewable request")
				}
			})
		}
	}
}

func TestPermissionPopupResizeRecomputesReviewBudget(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprintf("inline=%v", inline), func(t *testing.T) {
			m := permissionPopupModel(t, 120, 30, inline)
			request := m.permRequest
			m.permChoice = permChoiceDeny
			for _, size := range []struct {
				width, height int
				enabled       bool
			}{{120, 30, true}, {40, 16, false}, {40, 18, true}, {20, 30, false}, {120, 30, true}} {
				_, _ = m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				card := m.renderPermissionPicker()
				assertPermissionPopupBounds(t, m, card)
				frame := stripANSI(m.viewContent())
				if lipgloss.Height(frame) != m.managedFrameHeight() || lipgloss.Width(frame) != m.managedFrameWidth() {
					t.Fatalf("resize produced an out-of-bounds frame:\n%s", frame)
				}
				x := (m.managedFrameWidth() - lipgloss.Width(card)) / 2
				y := (m.managedFrameHeight() - lipgloss.Height(card)) / 2
				// The transcript can retain a wide glyph before the card. Check
				// terminal cells, not rune indices, when locating its border.
				line := strings.Split(frame, "\n")[y]
				if xansi.Cut(line, x, x+1) != "╭" {
					t.Fatalf("card not centered at (%d,%d):\n%s", x, y, frame)
				}
				if got := m.permissionApprovalEnabled(); got != size.enabled {
					t.Fatalf("%dx%d enabled=%v, want %v:\n%s", size.width, size.height, got, size.enabled, card)
				}
				if m.permRequest != request || !m.permPending || m.permChoice != permChoiceDeny {
					t.Fatal("resize changed the pending request or decision")
				}
			}
		})
	}
}

func TestPermissionPopupRequiresShellReviewDetail(t *testing.T) {
	m := permissionPopupModel(t, 120, 30, false)
	m.permRequest.Args = json.RawMessage(`{}`)
	m.permRequest.Effects, m.permRequest.Paths = nil, nil
	if m.permissionApprovalEnabled() {
		t.Fatal("shell request with no review detail enabled approval")
	}
	if !strings.Contains(stripANSI(m.renderPermissionPicker()), "Approval disabled") {
		t.Fatal("missing disabled status")
	}
}
