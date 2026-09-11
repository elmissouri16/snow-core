package tui

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func pluginLayoutModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.plugins = &pluginUIState{cache: map[pluginRenderKey]string{}, running: map[string]bool{}}
	return m
}

func TestPluginUpdatesResizeBeforeNextFrame(t *testing.T) {
	m := pluginLayoutModel(t)
	m.width, m.height = 120, 24
	m.plugins.views = []protocol.PluginView{{ID: "test:footer", Placement: "footer"}}
	m.layout()
	for _, value := range []string{"Workspace: ready", "Workspace: ready\nSecond row", "", "Ready"} {
		raw, err := json.Marshal(map[string]any{"name": "footer", "content": protocol.PluginNode{Type: "text", Text: value}})
		if err != nil {
			t.Fatal(err)
		}
		m.handlePluginUI(pluginUIRequest{app: m.app, ctx: t.Context(), reply: make(chan pluginUIResponse, 1), event: protocol.PluginUIEvent{
			PluginID: "test", Generation: m.app.PluginGeneration(), Operation: "ui.update", Arguments: raw,
		}})
		// Bubble Tea renders after every message, including before the coalesced refresh.
		if got := m.transcript.Height() + m.chromeHeight(); got != m.height {
			t.Fatalf("update %q needs %d rows in a %d-row terminal", value, got, m.height)
		}
		if value != "" {
			// Padding between rows is expected; inspect each label separately.
			for line := range strings.SplitSeq(value, "\n") {
				if !strings.Contains(stripANSI(m.viewContent()), line) {
					t.Fatalf("update clipped %q", line)
				}
			}
			if !strings.HasPrefix(strings.TrimRight(strings.Split(stripANSI(m.viewContent()), "\n")[m.height-1], " "), " ") {
				t.Fatal("plugin footer lost its inset")
			}
		}
	}
}

func TestPluginChromePreservesGrowingComposerAndRunStatus(t *testing.T) {
	m := pluginLayoutModel(t)
	m.width = 60
	m.busy, m.runStartedAt = true, time.Now()
	m.editor.SetValue("first\nsecond\nthird\nfourth\nfifth\nsixth")
	m.plugins.views = []protocol.PluginView{{ID: "input", Placement: "above_input", Content: &protocol.PluginNode{Type: "text", Text: strings.Repeat("Plugin row\n", 6)}}}
	for _, height := range []int{30, 14, 9, 20} {
		m.update(tea.WindowSizeMsg{Width: m.width, Height: height})
		if got := m.transcript.Height() + m.chromeHeight(); got != height {
			t.Fatalf("height %d: content needs %d rows", height, got)
		}
		frame := stripANSI(m.viewContent())
		for _, label := range []string{"Working", "permission:", "sixth"} {
			if !strings.Contains(frame, label) {
				t.Fatalf("height %d clipped %q", height, label)
			}
		}
	}
}

func TestPluginChromeFitsResizesAndOverlays(t *testing.T) {
	m := pluginLayoutModel(t)
	for _, placement := range []string{"header", "footer", "above_input", "sidebar"} {
		node := &protocol.PluginNode{Type: "text", Text: strings.Repeat(placement+" content\n", 8)}
		m.plugins.views = append(m.plugins.views, protocol.PluginView{ID: placement, Placement: placement, Content: node})
	}
	for _, inline := range []bool{false, true} {
		m.inlineTranscript = inline
		for _, height := range []int{48, 24, 12, 9, 8, 16, 32} {
			for _, width := range []int{160, 100, 99, 40, 20} {
				for _, overlay := range []bool{false, true} {
					t.Run(fmt.Sprintf("inline=%v/%dx%d/overlay=%v", inline, width, height, overlay), func(t *testing.T) {
						m.planPrompt = overlay
						m.update(tea.WindowSizeMsg{Width: width, Height: height})
						if got := m.transcript.Height() + m.chromeHeight(); got > height {
							t.Fatalf("content needs %d rows, terminal has %d", got, height)
						}
						frame := m.viewContent()
						if lipgloss.Height(frame) != height || lipgloss.Width(frame) > width-1 {
							t.Fatalf("frame geometry %dx%d", lipgloss.Width(frame), lipgloss.Height(frame))
						}
						if overlay {
							// A centered card may cover the footer on a short frame,
							// but closing it must reveal unchanged underlying chrome.
							m.planPrompt = false
							m.layout()
							frame = m.viewContent()
						}
						if !strings.Contains(stripANSI(frame), "permission:") {
							t.Fatal("plugin content clipped the core footer")
						}
					})
				}
			}
		}
	}
}

func TestPluginHeaderOffsetsTranscriptAndRunStatus(t *testing.T) {
	m := pluginLayoutModel(t)
	m.width, m.height = 120, 24
	m.busy, m.runStartedAt = true, time.Now()
	m.plugins.views = []protocol.PluginView{{ID: "header", Placement: "header", Content: &protocol.PluginNode{Type: "text", Text: "Extra header\nSecond header"}}}
	m.lines = []string{"Transcript first row"}
	m.transcriptBaseDirty = true
	m.layout()
	m.refreshTranscriptForced()
	if top := m.transcriptSelectionTop(); top != 4 {
		t.Fatalf("transcript mouse origin=%d, want 4", top)
	}
	y, _, _, ok := m.runStatusMouseBounds()
	if !ok || y != 4+m.transcript.Height() {
		t.Fatalf("run status mouse row=%d", y)
	}
}
