package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------------

func TestMarkdownRendererBasics(t *testing.T) {
	r := newMarkdownRenderer()
	out := r.render("# Title\n\nSome **bold** and `inline` code.\n\n```go\npackage main\n```\n", 60)
	plain := stripANSI(out)
	for _, want := range []string{"Title", "bold", "inline", "package main"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("markdown output missing %q: %q", want, plain)
		}
	}
	// ANSI styling should be present (header bold / code styling).
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("no ANSI styling produced: %q", out)
	}
}

// stripANSI removes terminal escape sequences for plain-text assertions.
func stripANSI(s string) string {
	return ansiStripRe.ReplaceAllString(s, "")
}

var ansiStripRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\[[0-9;]*m`)

func TestMarkdownRendererCacheAndWidth(t *testing.T) {
	r := newMarkdownRenderer()
	md := "# Hello\n\nworld"
	a := r.render(md, 40)
	b := r.render(md, 40)
	if a != b {
		t.Fatal("identical input at same width must hit the cache")
	}
	c := r.render(md, 80)
	if c == b {
		t.Fatal("width change must re-render")
	}
	// Renderer must survive render failures gracefully (empty input).
	if got := r.render("", 40); got != "" {
		t.Fatalf("empty render = %q, want empty", got)
	}
}

func TestModelAssistantMarkdownRendered(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "# Plan\n\n- step one\n- step two\n\n```bash\nls -la\n```\n"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})

	view := m.View()
	plain := stripANSI(view)
	for _, want := range []string{"Plan", "step one", "step two", "ls -la"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("view missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "assistant:") {
		t.Fatalf("view should not add an assistant role label: %q", plain)
	}
}

func TestTranscriptRefreshPreservesScrollIntent(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 60
	m.height = 12
	m.layout()
	for i := range 30 {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.transcriptBaseDirty = true
	m.refreshTranscript()
	m.transcript.SetYOffset(2)
	m.lines = append(m.lines, "new output")
	m.transcriptBaseDirty = true
	m.refreshTranscript()
	if m.transcript.YOffset != 2 {
		t.Fatalf("stream refresh moved scrolled viewport to %d", m.transcript.YOffset)
	}
	m.transcript.GotoBottom()
	m.lines = append(m.lines, "tail output")
	m.transcriptBaseDirty = true
	m.refreshTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("viewport should follow output when already at bottom")
	}
}

func TestTranscriptWrapsLongLinesToWindow(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 40
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{
		Type: protocol.EvTextDelta,
		Text: "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt.",
	})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})

	plain := stripANSI(m.transcript.View())
	if !strings.Contains(plain, "\n") {
		t.Fatalf("long transcript line did not wrap: %q", plain)
	}
	for line := range strings.SplitSeq(plain, "\n") {
		if len([]rune(line)) > m.transcript.Width {
			t.Fatalf("wrapped transcript line exceeds width %d: %q", m.transcript.Width, line)
		}
	}
}

func assertExactFrame(t *testing.T, m *Model) {
	t.Helper()
	view := m.View()
	if got := lipgloss.Height(view); got != m.height {
		t.Fatalf("frame height = %d, want %d", got, m.height)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("frame line %d width = %d, want <= %d", i, got, m.width)
		}
	}
}

func TestModelFrameAlwaysFitsWindow(t *testing.T) {
	t.Run("startup 210x55", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		m.width, m.height = 210, 55
		m.layout()
		assertExactFrame(t, m)
	})

	t.Run("loaded with thinking picker", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		buildAppForTest(t, m)
		m.width, m.height = 80, 20
		m.pickThinking = true
		m.thinkingList = []protocol.ThinkingLevel{
			protocol.ThinkingOff,
			protocol.ThinkingMinimal,
			protocol.ThinkingLow,
			protocol.ThinkingMedium,
			protocol.ThinkingHigh,
		}
		m.layout()
		assertExactFrame(t, m)
		plain := stripANSI(m.View())
		if !strings.Contains(plain, "Thinking effort") || !strings.Contains(plain, "high") {
			t.Fatalf("thinking picker clipped unexpectedly: %q", plain)
		}
	})

	t.Run("active run status", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		buildAppForTest(t, m)
		started := time.Unix(100, 0)
		m.now = func() time.Time { return started.Add(76 * time.Second) }
		m.runStartedAt = started
		m.busy = true
		m.width, m.height = 210, 55
		m.layout()
		assertExactFrame(t, m)
		plain := stripANSI(m.View())
		for _, want := range []string{"Working", "1m 16s", "esc to interrupt"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("active run status missing %q: %q", want, plain)
			}
		}

		m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
		m.layout()
		if strings.Contains(stripANSI(m.View()), "esc to interrupt") {
			t.Fatal("run status remained visible after turn completion")
		}
		assertExactFrame(t, m)
	})

	t.Run("tiny fallback", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		m.width, m.height = 3, 2
		m.layout()
		assertExactFrame(t, m)
	})
}

func TestRunStatusGeometryKeepsSingleStickyFooter(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 120, 35
	m.layout()
	assertFooter := func(stage string) {
		t.Helper()
		lines := strings.Split(stripANSI(m.View()), "\n")
		matches := 0
		index := -1
		for i, line := range lines {
			if strings.Contains(line, "permission: allow") {
				matches++
				index = i
			}
		}
		if matches != 1 || index != m.height-1 {
			t.Fatalf("%s footer matches=%d row=%d want one at %d", stage, matches, index, m.height-1)
		}
	}
	assertFooter("idle")
	m.editor.SetValue("hello")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.showRunStatus() {
		t.Fatal("prompt did not enter visible run status")
	}
	m.layout()
	assertFooter("running")
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	m.layout()
	assertFooter("settled")
}

func TestComposerHardWrapBoundStopsAtRequestedHeight(t *testing.T) {
	if composerHardWrapReaches("abc", 3, 2) {
		t.Fatal("exactly full single line was treated as two lines")
	}
	if !composerHardWrapReaches("abcd", 3, 2) {
		t.Fatal("hard-wrapped second line was not detected")
	}
	if !composerHardWrapReaches("one\ntwo\nthree", 80, 3) {
		t.Fatal("explicit newlines did not reach target height")
	}
	if !composerHardWrapReaches(strings.Repeat("界", 20), 10, 4) {
		t.Fatal("wide graphemes did not reach target height")
	}
}

func TestComposerAutoGrowsAndShrinks(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 48, 30
	m.layout()
	if got := m.editor.Height(); got != 3 {
		t.Fatalf("empty composer height = %d, want 3", got)
	}

	m.editor.SetValue("one\ntwo\nthree")
	m.layout()
	if got := m.editor.Height(); got != 3 {
		t.Fatalf("three-line composer height = %d, want 3", got)
	}

	m.editor.SetValue("one\ntwo\nthree\nfour\nfive\nsix\nseven")
	m.layout()
	if got := m.editor.Height(); got != 6 {
		t.Fatalf("composer cap = %d, want 6", got)
	}

	m.editor.SetValue(strings.Repeat("wrapped words ", 12))
	m.layout()
	wideHeight := m.editor.Height()
	if wideHeight <= 3 {
		t.Fatalf("soft-wrapped composer did not grow: %d", wideHeight)
	}
	m.width = 28
	m.layout()
	if got := m.editor.Height(); got < wideHeight || got > 6 {
		t.Fatalf("narrow composer height = %d, wide = %d", got, wideHeight)
	}

	m.editor.Reset()
	m.layout()
	if got := m.editor.Height(); got != 3 {
		t.Fatalf("reset composer height = %d, want 3", got)
	}
	assertExactFrame(t, m)
}

func TestComposerBackspaceUsesOrdinaryEditingFastPath(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.editor.SetValue("/mode")
	m.editor.CursorEnd()
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.editor.Value(); got != "/mod" {
		t.Fatalf("composer after Backspace = %q, want %q", got, "/mod")
	}
	if !m.compVisible {
		t.Fatal("Backspace fast path did not refresh slash completion state")
	}
}

func TestComposerMultilineShortcutsDoNotSubmit(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyMsg
	}{
		{name: "alt enter", keys: []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}}},
		{name: "split mac option enter", keys: []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyEnter}}},
		{name: "ctrl j", keys: []tea.KeyMsg{{Type: tea.KeyCtrlJ}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), app.Options{})
			buildAppForTest(t, m)
			m.width, m.height = 48, 30
			m.layout()
			m.editor.SetValue("first line")
			m.editor.CursorEnd()

			for i, key := range tt.keys {
				_, cmd := m.handleKey(key)
				if i == len(tt.keys)-1 && cmd != nil {
					t.Fatal("multiline shortcut returned a prompt command")
				}
			}
			if m.busy {
				t.Fatal("multiline shortcut submitted the prompt")
			}
			if got := m.editor.Value(); got != "first line\n" {
				t.Fatalf("composer value = %q, want trailing newline", got)
			}
			m.layout()
			if got := m.editor.Height(); got != 3 {
				t.Fatalf("two-line composer height = %d, want minimum 3", got)
			}
		})
	}
}

func TestComposerSplitMetaPrefixExpires(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil || !m.metaEnterPending {
		t.Fatal("Escape did not open the split Meta key window")
	}
	m.Update(clearMetaEnterMsg(m.metaEnterSeq))
	if m.metaEnterPending {
		t.Fatal("split Meta key window did not expire")
	}
}

// ---------------------------------------------------------------------------
// Interactive permission picker
// ---------------------------------------------------------------------------

// permRequestEvent builds an EvPermissionRequest agent event.
func permRequestEvent(tool string) protocol.AgentEvent {
	return protocol.AgentEvent{
		Type: protocol.EvPermissionRequest,
		Permission: &protocol.Permission{
			Request: protocol.PermissionRequest{
				Tool:  tool,
				Args:  json.RawMessage(`{"cmd":"rm -rf /"}`),
				Paths: []string{"/tmp/x"},
				Risk:  "exec",
			},
		},
	}
}

func TestModelPermissionPickerShowsAndNavigates(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.inlineTranscript = true
	m.layout()

	m.handleAgentEvent(permRequestEvent("bash"))
	if !m.permPending {
		t.Fatal("permission request should open the interactive picker")
	}
	if m.permRequest == nil || m.permRequest.Tool != "bash" {
		t.Fatalf("permRequest = %+v, want bash", m.permRequest)
	}
	view := m.View()
	for _, want := range []string{"Allow once", "Allow this scope", "Deny", "bash"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker missing %q: %q", want, view)
		}
	}

	// Arrows cycle the choice; default is Allow (index 0).
	if m.permChoice != permChoiceAllow {
		t.Fatalf("default choice = %d, want allow", m.permChoice)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.permChoice != permChoiceAlways {
		t.Fatalf("down should move to always, got %d", m.permChoice)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.permChoice != permChoiceAllow {
		t.Fatalf("up should return to allow, got %d", m.permChoice)
	}
}

func TestModelPermissionPickerHidesScopedApprovalForUnknownBash(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.inlineTranscript = true
	m.layout()
	event := permRequestEvent("bash")
	event.Permission.Request.ScopeLabel = "dynamic Bash effects"
	event.Permission.Request.Unknown = true
	event.Permission.Request.Rememberable = false
	event.Permission.Request.Effects = []protocol.PermissionEffect{{Type: "unknown", Operation: "unknown", Dynamic: true}}
	event.Permission.Request.EffectsTruncated = true
	m.handleAgentEvent(event)
	view := m.View()
	for _, want := range []string{"Allow once", "Deny", "Unknown child effects", "Permission analysis was truncated", "unrestricted host process"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "Allow this scope") {
		t.Fatalf("unknown Bash request offered scoped approval: %q", view)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.permChoice != permChoiceDeny {
		t.Fatalf("down should move directly to deny, got %d", m.permChoice)
	}
}

func TestModelPermissionPickerCompactsCompoundBashEffects(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width = 72
	m.height = 30
	m.inlineTranscript = true
	m.layout()

	event := permRequestEvent("bash")
	event.Permission.Request.Args = json.RawMessage(`{"command":"tmp_file=${TMPDIR:-/tmp}/snow-test-$$.txt; printf x > $tmp_file; test -f $tmp_file; rm -- $tmp_file; test ! -e $tmp_file; echo tail-marker-that-must-not-remain-visible-after-the-command-row-budget"}`)
	event.Permission.Request.Unknown = true
	event.Permission.Request.Rememberable = false
	event.Permission.Request.ScopeLabel = "dynamic Bash effects"
	event.Permission.Request.Reason = "4 inferred effect(s)"
	event.Permission.Request.Effects = []protocol.PermissionEffect{
		{Type: "process", Capability: "process.exec", Operation: "execute", Command: "rm", Reason: "execute command", Confidence: "high"},
		{Type: "process", Capability: "process.exec", Operation: "execute", Command: "rm", Reason: "second source", Confidence: "high"},
		{Type: "unknown", Capability: "effect.unknown", Operation: "unknown", Reason: "dynamic redirection target", Confidence: "low", Dynamic: true},
		{Type: "unknown", Capability: "effect.unknown", Operation: "unknown", Reason: "dynamic path for rm", Confidence: "low", Dynamic: true},
	}
	m.handleAgentEvent(event)

	if got, limit := lipgloss.Height(m.renderOverlays()), m.availableOverlayHeight(); got > limit {
		t.Fatalf("permission overlay height=%d want<=%d", got, limit)
	}
	view := stripANSI(m.View())
	for _, want := range []string{
		"Command:",
		"Effects (4):",
		"execute rm ×2",
		"unknown ×2 — dynamic redirection target; dynamic path for rm",
		"Unknown child effects",
		"Allow once",
		"Deny",
		"Esc deny",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact picker missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"4 inferred effect(s)", "tail-marker-that-must-not-remain-visible", "  execute\n  execute", "/!"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("compact picker retained %q:\n%s", unwanted, view)
		}
	}
	for row, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > m.width {
			t.Fatalf("picker row %d width=%d want<=%d: %q", row, width, m.width, line)
		}
	}
}

func TestModelPermissionPickerKeepsDecisionsVisibleAtNarrowWidth(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width = 20
	m.height = 20
	m.inlineTranscript = true
	m.layout()

	event := permRequestEvent("bash")
	event.Permission.Request.Args = json.RawMessage(`{"command":"printf x > one; rm two"}`)
	event.Permission.Request.Unknown = true
	event.Permission.Request.Rememberable = false
	event.Permission.Request.ScopeLabel = "dynamic Bash effects"
	event.Permission.Request.Effects = []protocol.PermissionEffect{
		{Type: "filesystem", Operation: "write", Resource: "/a path/with  repeated spaces", Command: "redirection"},
		{Type: "unknown", Operation: "unknown", Reason: "line one\nline two", Dynamic: true},
	}
	m.handleAgentEvent(event)

	view := stripANSI(m.View())
	for _, want := range []string{"Allow once", "Deny"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow picker hid %q:\n%s", want, view)
		}
	}
	for row, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > m.width {
			t.Fatalf("narrow picker row %d width=%d want<=%d: %q", row, width, m.width, line)
		}
	}
}

func TestModelPermissionPickerOwnsSmallDefaultFramesSafely(t *testing.T) {
	tests := []struct {
		width, height int
		enabled       bool
		truncated     bool
	}{
		{40, 3, false, false},
		{40, 7, false, true},
		{40, 8, true, false},
		{40, 9, true, false},
		{40, 12, true, false},
		{3, 12, false, false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			m := newModel(t.Context(), app.Options{})
			buildAppForTest(t, m)
			m.width = tc.width
			m.height = tc.height
			m.inlineTranscript = false
			m.layout()

			event := permRequestEvent("bash")
			event.Permission.Request.Args = json.RawMessage(`{"command":"printf x > one; rm two"}`)
			event.Permission.Request.Unknown = true
			event.Permission.Request.EffectsTruncated = tc.truncated
			event.Permission.Request.Rememberable = false
			event.Permission.Request.ScopeLabel = "dynamic Bash effects"
			event.Permission.Request.Effects = []protocol.PermissionEffect{{Type: "unknown", Operation: "unknown", Reason: "dynamic path", Dynamic: true}}
			m.handleAgentEvent(event)

			if got := m.permissionApprovalEnabled(); got != tc.enabled {
				t.Fatalf("permissionApprovalEnabled()=%v want %v", got, tc.enabled)
			}
			view := stripANSI(m.View())
			if tc.enabled {
				for _, want := range []string{"bash", "Unknown child", "unrestricted host", "Allow once", "Deny"} {
					if !strings.Contains(view, want) {
						t.Fatalf("permission frame missing %q:\n%s", want, view)
					}
				}
			} else {
				_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
				if !m.permPending {
					t.Fatal("Enter authorized a request that could not be meaningfully displayed")
				}
				if tc.width >= permissionReviewMinWidth && !strings.Contains(view, "Approval disabled") {
					t.Fatalf("small permission frame omitted disabled state:\n%s", view)
				}
				if tc.truncated {
					for _, want := range []string{"Command:", "analysis was truncated", "Approval disabled"} {
						if !strings.Contains(view, want) {
							t.Fatalf("truncated height-7 frame missing %q:\n%s", want, view)
						}
					}
				}
			}
			for row, line := range strings.Split(view, "\n") {
				if width := lipgloss.Width(line); width > tc.width {
					t.Fatalf("row %d width=%d want<=%d: %q", row, width, tc.width, line)
				}
			}
		})
	}
}

func TestPermissionInlineTextEscapesControlsWithoutCollapsingSpaces(t *testing.T) {
	got := permissionInlineText("a  b\tc\r\n\x1b\a")
	want := `a  b\tc\r\n\u{1b}\u{7}`
	if got != want {
		t.Fatalf("permissionInlineText()=%q want %q", got, want)
	}
	if permissionInlineText("a  b") == permissionInlineText("a b") {
		t.Fatal("permission display collapsed distinct space-containing resources")
	}
}

func TestModelPermissionPickerAllowResponds(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()

	// Drive the asker from a goroutine like the agent loop does.
	got := make(chan string, 1)
	go func() {
		d, err := m.asker.Ask(context.Background(), permissionRequest())
		if err != nil {
			got <- "err:" + err.Error()
			return
		}
		got <- string(d)
	}()
	waitAskerPending(t, m.asker)

	m.handleAgentEvent(permRequestEvent("bash"))
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // Enter on Allow

	if m.permPending {
		t.Fatal("picker should clear after resolving")
	}
	if d := <-got; d != "allow" {
		t.Fatalf("asker decision = %q, want allow", d)
	}
}

func TestBlockingPermissionPreemptsTranscriptContextMenu(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()
	got := make(chan string, 1)
	go func() {
		decision, err := m.asker.Ask(context.Background(), permissionRequest())
		if err != nil {
			got <- "err:" + err.Error()
			return
		}
		got <- string(decision)
	}()
	waitAskerPending(t, m.asker)

	m.openTranscriptSelectionContextMenu(1, 1, "must not copy")
	m.handleAgentEvent(permRequestEvent("bash"))
	if m.transcriptSelectionMenu.open {
		t.Fatal("permission arrival did not close context menu")
	}
	// Even a stale/reopened menu must not steal Enter from the visible host
	// request; reducer precedence remains authoritative.
	m.openTranscriptSelectionContextMenu(1, 1, "must not copy")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.permPending || m.transcriptSelectionMenu.open {
		t.Fatalf("permission=%v menu=%v", m.permPending, m.transcriptSelectionMenu.open)
	}
	if decision := <-got; decision != "allow" {
		t.Fatalf("decision=%q want allow", decision)
	}
}

func TestModelPermissionPickerEscDenies(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	got := make(chan string, 1)
	go func() {
		d, err := m.asker.Ask(context.Background(), permissionRequest())
		if err != nil {
			got <- "err:" + err.Error()
			return
		}
		got <- string(d)
	}()
	waitAskerPending(t, m.asker)

	m.handleAgentEvent(permRequestEvent("bash"))
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // Esc denies

	if m.permPending {
		t.Fatal("picker should clear after Esc")
	}
	if d := <-got; d != "deny" {
		t.Fatalf("asker decision = %q, want deny (safe default)", d)
	}
}

func TestModelPermissionPickerAllowAlways(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()

	got := make(chan string, 1)
	go func() {
		d, _ := m.asker.Ask(context.Background(), permissionRequest())
		got <- string(d)
	}()
	waitAskerPending(t, m.asker)

	m.handleAgentEvent(permRequestEvent("bash"))
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown}) // Allow always
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if d := <-got; d != "allow_always" {
		t.Fatalf("asker decision = %q, want allow_always", d)
	}
}

// waitAskerPending blocks until the asker has registered the pending request
// (the Ask goroutine sets pending before blocking on the response channel).
func waitAskerPending(t *testing.T, a *tuiAsker) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.Lock()
		p := a.pending
		a.mu.Unlock()
		if p != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("asker never became pending")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTUIAskerAcceptsNilContext(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	got := make(chan permission.Decision, 1)
	go func() {
		decision, err := m.asker.Ask(nil, permissionRequest())
		if err != nil {
			t.Errorf("nil-context asker error: %v", err)
			return
		}
		got <- decision
	}()
	waitAskerPending(t, m.asker)
	if err := m.asker.Respond(permission.DecisionAllow); err != nil {
		t.Fatal(err)
	}
	if decision := <-got; decision != permission.DecisionAllow {
		t.Fatalf("decision=%q, want allow", decision)
	}
}

func TestTUIAskerCancellationCannotLeakDecisionToNextRequest(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() {
		_, err := m.asker.Ask(ctx, permissionRequest())
		first <- err
	}()
	waitAskerPending(t, m.asker)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first ask err=%v", err)
	}
	if err := m.asker.Respond(permission.DecisionAllow); err == nil {
		t.Fatal("response after cancellation was accepted")
	}

	second := make(chan permission.Decision, 1)
	go func() {
		decision, _ := m.asker.Ask(context.Background(), permissionRequest())
		second <- decision
	}()
	waitAskerPending(t, m.asker)
	if err := m.asker.Respond(permission.DecisionDeny); err != nil {
		t.Fatal(err)
	}
	if got := <-second; got != permission.DecisionDeny {
		t.Fatalf("next request consumed stale decision %q", got)
	}
}

func TestTUIAskerDuplicateResponseFailsWithoutBlocking(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	result := make(chan permission.Decision, 1)
	go func() {
		decision, _ := m.asker.Ask(context.Background(), permissionRequest())
		result <- decision
	}()
	waitAskerPending(t, m.asker)
	if err := m.asker.Respond(permission.DecisionAllow); err != nil {
		t.Fatal(err)
	}
	if err := m.asker.Respond(permission.DecisionDeny); err == nil {
		t.Fatal("duplicate response succeeded")
	}
	if got := <-result; got != permission.DecisionAllow {
		t.Fatalf("decision=%q", got)
	}
}

// permissionRequest is a permission.Request for the asker tests.
func permissionRequest() permission.Request {
	return permission.Request{
		Tool: "bash",
		Args: json.RawMessage(`{}`),
		Risk: permission.RiskExec,
	}
}

// ---------------------------------------------------------------------------
// Model picker + persistence
// ---------------------------------------------------------------------------

func TestModelModelPickerFlow(t *testing.T) {
	testHome(t)

	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	// /model with no args opens the picker.
	m.editor.SetValue("/model")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickModel {
		t.Fatal("/model should open the interactive picker")
	}
	if len(m.modelList) == 0 {
		t.Fatal("picker should list catalog models")
	}
	view := m.View()
	if !strings.Contains(view, "fake-1") {
		t.Fatalf("picker should list models: %q", view)
	}

	// Esc cancels without changing the model.
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickModel {
		t.Fatal("Esc should close the model picker")
	}

	// Reopen and pick the first model.
	m.editor.SetValue("/model")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.modelIndex = 0
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickModel {
		t.Fatal("picker should close after picking")
	}
	if m.app.Model.ID != "fake-1" {
		t.Fatalf("model = %q, want fake-1", m.app.Model.ID)
	}
}

func TestModelPickerDeduplicatesAndSearches(t *testing.T) {
	testHome(t)

	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	m.app.AllModels = []protocol.Model{
		{Provider: "fake", ID: "fake-1", DisplayName: "Fake One"},
		{Provider: "fake", ID: "fake-1", DisplayName: "Duplicate"},
		{Provider: "fake", ID: "fake-spark", DisplayName: "Spark Fast"},
	}

	_, _ = m.startModelPick()
	if len(m.modelList) != 2 {
		t.Fatalf("deduplicated models = %d, want 2: %+v", len(m.modelList), m.modelList)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("spark")})
	matches := m.filteredModels()
	if len(matches) != 1 || matches[0].ID != "fake-spark" {
		t.Fatalf("search matches = %+v, want fake-spark", matches)
	}
	view := stripANSI(m.renderModelPicker())
	if !strings.Contains(view, "Search: spark") || strings.Contains(view, "fake-1") {
		t.Fatalf("filtered picker = %q", view)
	}
	if strings.Contains(view, "fake/fake-spark") {
		t.Fatalf("grouped picker repeated provider prefix: %q", view)
	}

	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickModel || m.app.Model.ID != "fake-spark" {
		t.Fatalf("search selection picker=%v model=%q", m.pickModel, m.app.Model.ID)
	}
}

func TestModelPickerSearchCanShowNoMatchesAndClear(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	_, _ = m.startModelPick()
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	if got := stripANSI(m.renderModelPicker()); !strings.Contains(got, "no matching models") {
		t.Fatalf("no-match picker = %q", got)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickModel || m.modelQuery != "" {
		t.Fatalf("search clear picker=%v query=%q", m.pickModel, m.modelQuery)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickModel {
		t.Fatal("second Esc should close the model picker")
	}
}

func TestModelSetModelPersists(t *testing.T) {
	home := testHome(t)

	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	// Direct /model <id> persists only for the active project.
	m.editor.SetValue("/model some-other-model")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.app.Model.ID != "some-other-model" {
		t.Fatalf("model = %q, want some-other-model", m.app.Model.ID)
	}

	persisted, err := config.Load(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatalf("config.json not persisted: %v", err)
	}
	selection, ok := persisted.ProjectSelections[m.app.CWD()]
	if !ok || selection.Provider != "fake" || selection.Model != "some-other-model" || selection.Thinking != "off" {
		t.Fatalf("project selection = %+v found=%v", selection, ok)
	}
	if persisted.DefaultProvider != "opencode-zen" || persisted.DefaultModel != "" || persisted.Thinking != "off" {
		t.Fatalf("global defaults changed = %s/%s thinking:%s", persisted.DefaultProvider, persisted.DefaultModel, persisted.Thinking)
	}
}

var _ = protocol.AgentEvent{}
