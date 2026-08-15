package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/skills"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
	"github.com/snow-core/snow/pkg/protocol"
)

var (
	testHomeMu sync.Mutex
	testHomes  = map[*testing.T]string{}
)

// testHome returns a per-test isolated SNOW_HOME. Every TUI test must use it
// so that commands which persist state (/model → config.json, /trust, …) can
// never write to the developer's real ~/.snow directory.
func testHome(t *testing.T) string {
	t.Helper()
	testHomeMu.Lock()
	defer testHomeMu.Unlock()
	if h, ok := testHomes[t]; ok {
		return h
	}
	h := t.TempDir()
	testHomes[t] = h
	t.Setenv("SNOW_HOME", h)
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(h, "sessions"))
	return h
}

// buildAppForTest constructs the app synchronously and attaches it to the
// model so tests don't depend on the async Init path or a TTY.
func buildAppForTest(t *testing.T, m *Model) {
	t.Helper()
	testHome(t) // isolate config/auth/trust paths BEFORE the app reads them
	a, err := app.New(context.Background(), app.Options{
		Provider:   "fake",
		NoSession:  true,
		Permission: "allow",
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app = a
	t.Cleanup(func() { a.Close() })
}

func TestModelMouseToggleRestoresNativeSelection(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.app.Cfg.TUI.Mouse = false
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyF6})
	if cmd == nil || !m.app.Cfg.TUI.Mouse || !strings.Contains(m.lastStatus, "wheel scroll") {
		t.Fatalf("mouse enable: cmd=%v mouse=%v status=%q", cmd != nil, m.app.Cfg.TUI.Mouse, m.lastStatus)
	}
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyF6})
	if cmd == nil || m.app.Cfg.TUI.Mouse || !strings.Contains(m.lastStatus, "native selection") {
		t.Fatalf("mouse disable: cmd=%v mouse=%v status=%q", cmd != nil, m.app.Cfg.TUI.Mouse, m.lastStatus)
	}
}

// TestModelSlashCommands verifies command parsing without a TTY.
func TestModelSlashCommands(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/help")
	_, quit := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if quit != nil {
		t.Fatal("help should not quit")
	}
	if len(m.lines) == 0 {
		t.Fatal("expected help output")
	}

	m.editor.SetValue("/quit")
	_, quit = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if quit == nil {
		t.Fatal("quit should return a quit command")
	}
}

func TestPromptPreflightFailureReleasesBusyState(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Agent.SetModel(protocol.Model{Provider: "fake", ID: "thinking-model", SupportsThinking: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingHigh}}); err != nil {
		t.Fatal(err)
	}
	if err := m.app.Agent.SetThinking(protocol.ThinkingHigh); err != nil {
		t.Fatal(err)
	}
	if err := m.app.Agent.SetModel(protocol.Model{Provider: "openai-compatible", ID: "deepseek-v4-flash", SupportsTools: true}); err != nil {
		t.Fatal(err)
	}
	m.busy = true
	m.runStartedAt = time.Now()
	msg := m.startPrompt("hey")()
	result, ok := msg.(promptDoneMsg)
	if !ok || result.err == nil || result.admitted {
		t.Fatalf("prompt result=%T %+v", msg, msg)
	}
	_, _ = m.Update(result)
	if m.busy || !m.runStartedAt.IsZero() || m.cancelRun != nil {
		t.Fatalf("preflight error left run active: busy=%t started=%v cancel=%v", m.busy, m.runStartedAt, m.cancelRun != nil)
	}
	before := len(m.lines)
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.lines) != before {
		t.Fatalf("idle escape emitted abort output: %v", m.lines[before:])
	}
}

func TestModelStartupFailureRemainsResponsive(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.width, m.height = 100, 30
	m.layout()

	wantErr := errors.New(`agent: model "deepseek-v4-flash" does not advertise thinking level "high" (supported: off)`)
	_, cmd := m.Update(doneMsg{err: wantErr})
	if cmd != nil {
		t.Fatal("startup failure unexpectedly returned a command")
	}
	if !errors.Is(m.lastErr, wantErr) {
		t.Fatalf("startup error = %v, want %v", m.lastErr, wantErr)
	}
	if m.editor.Focused() {
		t.Fatal("composer remained focused after startup failure")
	}

	view := stripANSI(m.View())
	for _, want := range []string{
		"startup failed",
		"error",
		"does not advertise thinking level",
		"ctrl+c to quit",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("startup error view missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "starting snow") || strings.Contains(view, "booting") {
		t.Fatalf("startup error view still looks active: %q", view)
	}
	if got := m.editor.Placeholder; got != "Startup failed · ctrl+c to quit" {
		t.Fatalf("startup error placeholder = %q", got)
	}
}

func TestModelCanQuitBeforeAppIsReady(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyType
	}{
		{name: "ctrl c while booting", key: tea.KeyCtrlC},
		{name: "ctrl d while booting", key: tea.KeyCtrlD},
		{name: "ctrl c after startup failure", key: tea.KeyCtrlC},
		{name: "ctrl d after startup failure", key: tea.KeyCtrlD},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), app.Options{})
			if strings.Contains(tt.name, "failure") {
				_, _ = m.Update(doneMsg{err: errors.New("startup failed")})
			}
			_, quit := m.handleKey(tea.KeyMsg{Type: tt.key})
			if quit == nil {
				t.Fatal("quit key was ignored before app initialization")
			}
		})
	}
}

func TestReadOnlyMCPStatusPicker(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	if m.app.MCPManager != nil {
		_ = m.app.MCPManager.Close()
	}
	m.app.MCPManager = nil
	m.app.MCPStatuses = []publicmcp.Status{{ID: "chrome", Transport: "stdio", Connected: true, ProtocolVersion: "2025-11-25", ServerName: "chrome_devtools", ServerVersion: "1", Capabilities: []string{"tools"}, ToolCount: 29}}

	_, _ = m.runCommand("/mcp")
	if !m.pickInfo || len(m.infoItems) != 1 {
		t.Fatalf("picker state = open:%v items:%+v", m.pickInfo, m.infoItems)
	}
	view := stripANSI(m.renderInfoPicker())
	for _, want := range []string{"MCP servers", "chrome", "connected", "29 tools"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker missing %q: %q", want, view)
		}
	}
	_, _ = m.handleInfoPick(tea.KeyMsg{Type: tea.KeyEsc})
	if m.pickInfo {
		t.Fatal("Esc did not close status picker")
	}
}

func TestReadOnlySkillsStatusPicker(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code.\n---\nDo it."), 0o644); err != nil {
		t.Fatal(err)
	}
	m.app.Skills = skills.Discover(skills.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})

	_, _ = m.runCommand("/skills")
	view := stripANSI(m.renderInfoPicker())
	if !m.pickInfo || !strings.Contains(view, "Agent Skills") || !strings.Contains(view, "review") || !strings.Contains(view, "enabled") {
		t.Fatalf("skills picker = %q", view)
	}
}

// TestModelPermissionCommand verifies /permissions updates the service mode.
func TestModelPermissionCommand(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/permissions deny")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := string(m.app.Perm.Mode()); got != "deny" {
		t.Fatalf("mode = %s, want deny", got)
	}
}

func TestModelThinkingPickerFiltersAndPersists(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	model := m.app.Agent.Model()
	model.SupportsThinking = true
	model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow}
	if err := m.app.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}
	m.app.Model = model

	m.editor.SetValue("/thinking")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickThinking || len(m.thinkingList) != 2 || m.thinkingList[0] != protocol.ThinkingOff || m.thinkingList[1] != protocol.ThinkingLow {
		t.Fatalf("thinking picker = open=%v levels=%v", m.pickThinking, m.thinkingList)
	}
	if strings.Contains(m.renderThinkingPicker(), "high") {
		t.Fatalf("picker exposed unsupported level: %q", m.renderThinkingPicker())
	}
	_, _ = m.handleThinkingPick(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.handleThinkingPick(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.app.Agent.Thinking(); got != protocol.ThinkingLow {
		t.Fatalf("thinking = %q, want low", got)
	}
	data, err := os.ReadFile(m.app.ConfigPath)
	if err != nil || !strings.Contains(string(data), `"thinking": "low"`) {
		t.Fatalf("thinking was not persisted: err=%v data=%s", err, data)
	}

	m.editor.SetValue("/thinking high")
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.lines) == 0 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "does not advertise") {
		t.Fatalf("unsupported thinking command did not report an error: %v", m.lines)
	}

	if err := m.app.Agent.SetThinking(protocol.ThinkingLow); err != nil {
		t.Fatal(err)
	}
	previousModel := m.app.Cfg.DefaultModel
	m.setModel(protocol.Model{Provider: "fake", ID: "plain", SupportsTools: true})
	if m.app.Agent.Model().ID != "plain" || m.app.Cfg.DefaultModel != previousModel {
		t.Fatalf("invalid model switch was not kept runtime-only: model=%+v config=%q", m.app.Agent.Model(), m.app.Cfg.DefaultModel)
	}
	if !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "does not advertise") {
		t.Fatalf("model switch warning missing: %v", m.lines)
	}
}

// TestModelAgentEventUpdates verifies streaming events update the transcript.
func TestModelPickerSelectsModelThenThinkingEffort(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	thinkingModel := protocol.Model{
		Provider: "fake", ID: "reasoner", SupportsTools: true, SupportsThinking: true,
		DefaultThinking: protocol.ThinkingXHigh,
		ThinkingLevels:  []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra},
	}
	m.app.AllModels = []protocol.Model{thinkingModel}
	_, _ = m.startModelPick()
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickModel || !m.pickThinking || m.thinkingModel == nil || m.thinkingModel.ID != "reasoner" {
		t.Fatalf("nested picker model=%v thinking=%v selected=%+v", m.pickModel, m.pickThinking, m.thinkingModel)
	}
	want := []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingLow, protocol.ThinkingHigh, protocol.ThinkingXHigh, protocol.ThinkingMax, protocol.ThinkingUltra}
	if !slices.Equal(m.thinkingList, want) || m.thinkingList[m.thinkingIndex] != protocol.ThinkingXHigh {
		t.Fatalf("thinking list=%v index=%d", m.thinkingList, m.thinkingIndex)
	}
	view := stripANSI(m.renderThinkingPicker())
	for _, label := range []string{"reasoner", "xhigh", "max", "ultra"} {
		if !strings.Contains(view, label) {
			t.Fatalf("picker missing %q: %q", label, view)
		}
	}
	_, _ = m.handleThinkingPick(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.pickModel || m.pickThinking {
		t.Fatalf("Esc did not return to model picker: model=%v thinking=%v", m.pickModel, m.pickThinking)
	}
	_, _ = m.handleModelPick(tea.KeyMsg{Type: tea.KeyEnter})
	m.thinkingIndex = len(m.thinkingList) - 1
	_, _ = m.handleThinkingPick(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pickThinking || m.app.Agent.Model().ID != "reasoner" || m.app.Agent.Thinking() != protocol.ThinkingUltra {
		t.Fatalf("selection model=%q thinking=%q picker=%v", m.app.Agent.Model().ID, m.app.Agent.Thinking(), m.pickThinking)
	}
	data, err := os.ReadFile(m.app.ConfigPath)
	if err != nil || !strings.Contains(string(data), `"default_model": "reasoner"`) || !strings.Contains(string(data), `"thinking": "ultra"`) {
		t.Fatalf("model/effort were not persisted: err=%v data=%s", err, data)
	}
}

func TestModelAgentEventUpdates(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "hello"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "bash"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "bash", IsError: true, Message: "boom"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})

	view := m.View()
	if !strings.Contains(view, "hello") {
		t.Fatalf("view missing streamed text: %q", view)
	}
	if !strings.Contains(view, "bash") {
		t.Fatalf("view missing tool name: %q", view)
	}
}

func TestPersistedSessionUpdateDoesNotDuplicateLiveAssistant(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	user := protocol.NewUserMessage("user", "", "hey")
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
		t.Fatal(err)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Hey! How can I help?"})

	assistant := protocol.NewAssistantMessage(
		"assistant",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{{Type: protocol.BlockText, Text: "Hey! How can I help?"}},
		protocol.StopStop,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if got := strings.Count(stripANSI(m.View()), "Hey! How can I help?"); got != 1 {
		t.Fatalf("stream plus persistence rendered reply %d times before turn_done", got)
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if got := strings.Count(stripANSI(m.View()), "Hey! How can I help?"); got != 1 {
		t.Fatalf("reply rendered %d times after turn_done", got)
	}
}

func TestModelToolProgressAndOutputCard(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.busy = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "grep", ToolCallID: "call-123456"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolProgress, ToolName: "grep", Message: "scanning files"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "grep", ToolDurationMS: 12, ToolOutput: "main.go:1: match\nmain.go:2: match"})
	if !m.busy {
		t.Fatal("tool_end must not unlock the composer before turn_done")
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	plain := stripANSI(m.View())
	for _, want := range []string{"grep", "scanning files", "main.go:1: match", "12ms"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("tool card missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "call-123456") {
		t.Fatalf("native tool card should hide call IDs: %q", plain)
	}
	if m.busy {
		t.Fatal("turn_done should unlock the composer")
	}
}

func TestModelBashToolUsesSingleSummaryRow(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "bash", Message: `echo "Hello from Snow!"`})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolProgress, ToolName: "bash", Message: "running command"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolProgress, ToolName: "bash", Message: "command finished"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "bash", ToolDurationMS: 374, ToolOutput: "shell output"})

	plain := stripANSI(m.transcriptContent)
	if got := strings.Count(plain, `echo "Hello from Snow!"`); got != 1 {
		t.Fatalf("bash command transcript rows = %d, want 1: %q", got, plain)
	}
	for _, want := range []string{`✓ echo "Hello from Snow!" · 374ms`, "shell output"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("bash summary missing %q: %q", want, plain)
		}
	}
	for _, unwanted := range []string{"running command", "command finished", "▶ bash"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("bash summary contains redundant status %q: %q", unwanted, plain)
		}
	}
}

func TestRenderSkillActivationSummarizesEscapedContent(t *testing.T) {
	output := `<skill_content name="caveman-commit">Write terse commits.&#xA;&lt;type&gt; must be exact.</skill_content>`
	preview := stripANSI(renderToolOutput("activate_skill", output, 100))
	if !strings.Contains(preview, "skill instructions loaded: caveman-commit") {
		t.Fatalf("skill preview missing activation summary: %q", preview)
	}
	for _, unwanted := range []string{"&#xA;", "&lt;", "Write terse commits"} {
		if strings.Contains(preview, unwanted) {
			t.Fatalf("skill preview exposed escaped body %q: %q", unwanted, preview)
		}
	}
}

func TestRenderSkillActivationHandlesMalformedOutput(t *testing.T) {
	preview := stripANSI(renderToolOutput("activate_skill", "truncated skill output", 100))
	if !strings.Contains(preview, "skill instructions loaded") || strings.Contains(preview, "truncated skill output") {
		t.Fatalf("malformed skill preview = %q", preview)
	}
}

func TestModelKeepsStreamingSegmentsChronological(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Before tool."})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "bash"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "bash", ToolOutput: "tool output"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "After tool."})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})

	plain := stripANSI(m.transcriptContent)
	before := strings.Index(plain, "Before tool.")
	tool := strings.Index(plain, "✓ bash")
	after := strings.Index(plain, "After tool.")
	if before < 0 || tool < 0 || after < 0 || !(before < tool && tool < after) {
		t.Fatalf("transcript order = %q", plain)
	}
	if strings.Count(plain, "Before tool.") != 1 || strings.Count(plain, "After tool.") != 1 {
		t.Fatalf("streaming segments duplicated: %q", plain)
	}
}

func TestModelFinalizesStreamingBeforeInterruptingEvents(t *testing.T) {
	tests := []struct {
		name  string
		event protocol.AgentEvent
		want  string
	}{
		{
			name: "permission",
			event: protocol.AgentEvent{Type: protocol.EvPermissionRequest, Permission: &protocol.Permission{
				Request: protocol.PermissionRequest{Tool: "bash"},
			}},
			want: "permission request",
		},
		{name: "error", event: protocol.AgentEvent{Type: protocol.EvError, Message: "provider failed"}, want: "provider failed"},
		{name: "abort", event: protocol.AgentEvent{Type: protocol.EvAborted}, want: "aborted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), app.Options{})
			buildAppForTest(t, m)
			m.width = 100
			m.height = 30
			m.layout()
			m.busy = true

			m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "First reason."})
			m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "First answer."})
			m.handleAgentEvent(tt.event)

			plain := stripANSI(m.transcriptContent)
			reason := strings.Index(plain, "First reason.")
			answer := strings.Index(plain, "First answer.")
			interrupt := strings.Index(plain, tt.want)
			if reason < 0 || answer < 0 || interrupt < 0 || !(reason < answer && answer < interrupt) {
				t.Fatalf("transcript order = %q", plain)
			}
		})
	}
}

func TestModelDeduplicatesProviderStreamError(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	const message = "chatgpt: servers overloaded"
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, Message: message})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, Message: "agent: provider stream: " + message})

	if got := strings.Count(stripANSI(strings.Join(m.lines, "\n")), message); got != 1 {
		t.Fatalf("provider stream error rendered %d times, want once", got)
	}
}

func TestModelEditDiffPreview(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "edit", Message: "docs/sessions.md"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "edit", ToolOutput: "...\n 39 transaction\n-40 old text\n+40 new text\n..."})
	plain := stripANSI(m.View())
	for _, want := range []string{"edit docs/sessions.md", "-40 old text", "+40 new text"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("edit diff preview missing %q: %q", want, plain)
		}
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "write", Message: "test.txt"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolName: "write", ToolOutput: "-1 old\n+1 new"})
	plain = stripANSI(m.View())
	for _, want := range []string{"write test.txt", "-1 old", "+1 new"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("write diff preview missing %q: %q", want, plain)
		}
	}
}

func TestModelStreamingDeliveredAsMessages(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	// Agent callbacks push into the lossless mailbox. Adjacent compatible
	// deltas coalesce before Bubble Tea receives the bounded logical batch.
	m.events.Push(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "step one "})
	m.events.Push(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "step two"})
	m.events.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Hello "})
	m.events.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "world\nline2"})

	got := waitForEvent(m.events)()
	if _, ok := got.(agentEventBatchMsg); !ok {
		t.Fatalf("waiter returned %T, want agentEventBatchMsg", got)
	}
	_, _ = m.Update(got)
	m.flushTranscriptImmediately()
	plain := stripANSI(m.View())
	if !strings.Contains(plain, "think: step one step two") {
		t.Fatalf("thinking delta not rendered: %q", plain)
	}
	if !strings.Contains(plain, "Hello world") || !strings.Contains(plain, "line2") || strings.Contains(plain, "assistant:") {
		t.Fatalf("streamed text should render without a role label: %q", plain)
	}

	// A new event remains deliverable by the next mailbox waiter.
	m.events.Push(protocol.AgentEvent{Type: protocol.EvToolStart, ToolName: "bash"})
	got = waitForEvent(m.events)()
	if _, ok := got.(agentEventBatchMsg); !ok {
		t.Fatalf("re-armed waiter returned %T, want agentEventBatchMsg", got)
	}

	// Turn end finalizes the answer as a permanent line.
	_, _ = m.Update(agentEventMsg{ev: protocol.AgentEvent{Type: protocol.EvTurnDone}})
	plain = stripANSI(m.View())
	if !strings.Contains(plain, "Hello world") || !strings.Contains(plain, "line2") || strings.Contains(plain, "assistant:") {
		t.Fatalf("finalized assistant line should be clean: %q", plain)
	}
}

// TestModelThinkingOnlyTurn verifies a reasoning-only response (no text) still
// surfaces the thinking block at turn end.
func TestModelThinkingOnlyTurn(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "let me think"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})

	view := stripANSI(m.View())
	if !strings.Contains(view, "think: let me think") {
		t.Fatalf("thinking-only turn not rendered: %q", view)
	}
}

func TestModelThinkingStreamsCheaplyAndFinalizesMarkdown(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()
	m.busy = true

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "**Inspecting repository**"})
	first := m.renderThinkingBody(m.thinkingBuf.String())
	firstPlain := stripANSI(first)
	if !strings.Contains(firstPlain, "think: Inspecting repository") || strings.Contains(firstPlain, "**") {
		t.Fatalf("styled first reasoning delta = %q", firstPlain)
	}
	if strings.TrimRight(firstPlain, " \t\n") != firstPlain {
		t.Fatalf("reasoning renderer padded visible output: %q", firstPlain)
	}
	if !strings.Contains(first, "\x1b[") {
		t.Fatalf("reasoning markdown has no ANSI styling: %q", first)
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "\n\nChecking files."})
	secondPlain := stripANSI(m.transcript.View())
	for _, want := range []string{"Inspecting repository", "Checking files."} {
		if !strings.Contains(secondPlain, want) {
			t.Fatalf("incremental reasoning missing %q: %q", want, secondPlain)
		}
	}
	if !strings.Contains(secondPlain, "**") {
		t.Fatalf("live reasoning unexpectedly performed full Markdown rendering: %q", secondPlain)
	}
	m.finalizeThinking()
	finalized := stripANSI(strings.Join(m.lines, "\n"))
	if strings.Contains(finalized, "**") || !strings.Contains(finalized, "Inspecting repository") {
		t.Fatalf("finalized reasoning Markdown = %q", finalized)
	}
}

func TestModelHydratesPersistedThinking(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	assistant := protocol.NewAssistantMessage(
		"assistant-thinking",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "**Planning changes**"},
			{Type: protocol.BlockText, Text: "Done."},
		},
		protocol.StopStop,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	plain := stripANSI(m.transcript.View())
	for _, want := range []string{"think: Planning changes", "Done."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("hydrated transcript missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "**") {
		t.Fatalf("hydrated thinking exposed Markdown markers: %q", plain)
	}
}

// TestModelAbortOnCtrlC verifies the busy-abort path returns no quit cmd.
func TestModelAbortOnCtrlC(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	_, quit := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit != nil {
		t.Fatal("ctrl+c while busy should abort, not quit")
	}
	// busy is cleared when the EvAborted event arrives from the agent; a
	// second ctrl+c while idle should quit.
	m.busy = false
	_, quit = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit == nil {
		t.Fatal("ctrl+c while idle should quit")
	}
}

func TestModelAbortOnEscDuringActiveRun(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.busy = true
	m.runStartedAt = time.Unix(100, 0)
	cancelled, cancel := context.WithCancel(context.Background())
	m.cancelRun = cancel
	m.layout()

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc while running returned an unexpected command")
	}
	select {
	case <-cancelled.Done():
	default:
		t.Fatal("esc did not cancel the active run")
	}
	if !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "aborting…") {
		t.Fatalf("abort status missing from transcript: %q", strings.Join(m.lines, "\n"))
	}
}

func TestFormatRunElapsed(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                     "0s",
		16*time.Second + 900*time.Millisecond: "16s",
		76 * time.Second:                      "1m 16s",
		time.Hour + 2*time.Minute + 3*time.Second: "1h 2m 3s",
	}
	for elapsed, want := range tests {
		if got := formatRunElapsed(elapsed); got != want {
			t.Fatalf("formatRunElapsed(%s) = %q, want %q", elapsed, got, want)
		}
	}
}
