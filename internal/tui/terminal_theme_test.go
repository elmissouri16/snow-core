package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTerminalThemeQueriesActualBackground(t *testing.T) {
	m := prepareScrollableModel(t)
	for _, msg := range []tea.Msg{tea.FocusMsg{}, uv.DarkColorSchemeEvent{}, uv.LightColorSchemeEvent{}} {
		_, cmd := m.Update(msg)
		if cmd == nil || cmd() == nil {
			t.Fatalf("%T did not query background", msg)
		}
		if !m.backgroundDark {
			t.Fatal("notification guessed the background")
		}
	}
	if m.terminalThemeInit() == nil {
		t.Fatal("startup did not query terminal appearance")
	}
}

func TestTerminalThemeRestoresOnlyOwnedMode(t *testing.T) {
	for _, value := range []ansi.ModeSetting{ansi.ModeNotRecognized, ansi.ModeSet, ansi.ModeReset, ansi.ModePermanentlySet, ansi.ModePermanentlyReset} {
		m := prepareScrollableModel(t)
		_, cmd := m.Update(tea.ModeReportMsg{Mode: ansi.ModeLightDark, Value: value})
		owned := value == ansi.ModeReset
		if (cmd != nil) != owned || m.themeModeOwned != owned {
			t.Fatalf("value=%d command=%v owned=%v", value, cmd != nil, m.themeModeOwned)
		}
		var output bytes.Buffer
		if err := m.restoreTerminalAppearance(&output); err != nil {
			t.Fatal(err)
		}
		want := ""
		if owned {
			want = ansi.ResetModeLightDark
		}
		if output.String() != want {
			t.Fatalf("restore=%q want=%q", output.String(), want)
		}
		m.restoreTerminalAppearance(&output)
		if output.String() != want {
			t.Fatal("restore repeated")
		}
	}
}

func TestBackgroundChangePreservesDraftDialogScrollAndLiveState(t *testing.T) {
	m := prepareScrollableModel(t)
	t.Cleanup(func() { terminalDark = true; _ = applyTUITheme("default") })
	text := strings.Repeat("# Heading\n\nParagraph with `code`.\n\n", 40)
	message := protocol.NewAssistantMessage("theme-message", m.app.Session.BranchTip(), "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: text}}, protocol.StopStop, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	m.transcript.SetYOffset(9)
	before := strings.Join(m.lines, "\n")
	m.editor.SetValue("composer draft")
	m.inputHistory, m.inputHistoryIndex, m.inputHistoryDraft = []string{"old"}, 0, "saved draft"
	m.startUserInput(protocol.UserInputRequest{ID: "question", Questions: []protocol.UserInputQuestion{{ID: "q", Question: "Answer"}}})
	m.userInputEditor.SetValue("answer draft")
	m.busy = true
	m.assistantBuf.WriteString("streaming text")
	m.latestPlan = "live plan"
	m.contextTokens, m.turnCount, m.stepCount = 123, 7, 9
	usage := &protocol.Usage{Input: 123}
	m.lastUsage, m.lastRequestUsage = usage, usage
	_, _ = m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")})
	after := strings.Join(m.lines, "\n")
	if m.backgroundDark || terminalDark || !m.backgroundKnown || before == after {
		t.Fatal("light background did not rebuild palette and Markdown")
	}
	if stripANSI(before) != stripANSI(after) {
		t.Fatal("theme changed durable transcript content")
	}
	if m.editor.Value() != "composer draft" || m.userInputEditor.Value() != "answer draft" || !m.userInputPending {
		t.Fatal("appearance changed draft or dialog")
	}
	if m.transcript.YOffset() != 9 || m.inputHistoryIndex != 0 || m.inputHistoryDraft != "saved draft" {
		t.Fatal("appearance reset scroll or history navigation")
	}
	if !m.busy || m.assistantBuf.String() != "streaming text" || m.latestPlan != "live plan" || m.contextTokens != 123 || m.turnCount != 7 || m.stepCount != 9 || m.lastUsage != usage {
		t.Fatal("appearance changed live run state")
	}
	if m.md.style.Text.Color == nil || *m.md.style.Text.Color != activeTUITheme.soft.Light {
		t.Fatal("Markdown palette did not resolve light colors")
	}
	renderer := m.md.renderer
	m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#eeeeee")})
	if m.md.renderer != renderer {
		t.Fatal("same appearance rebuilt Markdown unnecessarily")
	}
}

func TestBackgroundChangeKeepsCustomAndPluginPalettes(t *testing.T) {
	m := pluginScreenTestModel(t, 60, 16)
	t.Cleanup(func() { terminalDark = true; _ = applyTUITheme("default") })
	custom := config.ThemeFile{Version: 1, Name: "custom-test", Extends: "default", Colors: config.ThemeColors{Accent: config.AdaptiveColor{Light: "#102030", Dark: "#a0b0c0"}}}
	m.customThemes[custom.Name] = custom
	if err := m.applyThemeSelection(custom.Name, false, false); err != nil {
		t.Fatal(err)
	}
	m.plugins.screen, m.plugins.selected = "notes:main", 1
	m.renderPluginScreen()
	if len(m.plugins.cache) == 0 {
		t.Fatal("fixture did not populate plugin cache")
	}
	m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")})
	if len(m.plugins.cache) != 0 || m.themeName != custom.Name || activeTUITheme.accent.Light != "#102030" || m.plugins.screen != "notes:main" || m.plugins.selected != 1 {
		t.Fatal("custom palette or plugin state lost during light transition")
	}
	palette, _ := config.ResolveBuiltInTheme("ember")
	plugin := protocol.PluginTheme{ID: "plugin-palette", Colors: map[string]protocol.PluginColor{}}
	for name, pair := range map[string]config.AdaptiveColor{"accent": palette.Colors.Accent, "muted": palette.Colors.Muted, "foreground": palette.Colors.Foreground, "warning": palette.Colors.Warning, "error": palette.Colors.Error, "success": palette.Colors.Success, "separator": palette.Colors.Separator} {
		plugin.Colors[name] = protocol.PluginColor{Light: pair.Light, Dark: pair.Dark}
	}
	if err := m.applyPluginTheme(plugin); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#000000")})
	if activeTUITheme.accent.Dark != palette.Colors.Accent.Dark || m.themeName != custom.Name {
		t.Fatal("background transition replaced plugin palette with persisted theme")
	}
}
