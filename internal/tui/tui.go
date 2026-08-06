// Package tui provides the interactive Bubble Tea interface: transcript,
// editor, footer, slash commands, and streaming updates.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/pkg/protocol"
)

// Run starts the interactive TUI and blocks until exit.
func Run(ctx context.Context, opts app.Options) error {
	p := tea.NewProgram(newModel(ctx, opts))
	_, err := p.Run()
	return err
}

// Styles
var (
	styleUser     = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	styleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("231"))
	styleTool     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleFooter   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleThinking = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	styleBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
)

// Messages
type tickMsg struct{}
type agentEventMsg struct {
	ev protocol.AgentEvent
}
type doneMsg struct {
	err error
}
type appendLineMsg struct {
	line string
}
type spinnerMsg struct{}

// Model is the TUI state.
type Model struct {
	ctx    context.Context
	opts   app.Options
	app    *app.App
	width  int
	height int

	transcript viewport.Model
	editor     textarea.Model
	spinner    spinner.Model

	lines           []string // rendered transcript lines
	assistantBuf    strings.Builder
	thinkingBuf     strings.Builder
	assistantFinished bool
	busy            bool
	done            bool
	lastErr         error
	lastStatus      string
	eventChan       chan protocol.AgentEvent

	cancelRun context.CancelFunc
}

func newModel(ctx context.Context, opts app.Options) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	ta := textarea.New()
	ta.Placeholder = "Type a message… /help for commands"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.Focus()

	m := &Model{
		ctx:        ctx,
		opts:       opts,
		transcript: viewport.New(80, 20),
		editor:     ta,
		spinner:    sp,
		eventChan:  make(chan protocol.AgentEvent, 512),
	}
	m.transcript.Style = styleBorder
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	// Build the app asynchronously so the UI paints immediately.
	return tea.Batch(
		spinner.Tick,
		func() tea.Msg {
			a, err := app.New(m.ctx, m.opts)
			if err != nil {
				return doneMsg{err: err}
			}
			m.app = a
			m.subscribe()
			m.pushLine(styleFooter.Render(fmt.Sprintf(
				"snow %s — cwd %s — provider %s — model %s | /help for commands",
				"0.1.0-dev", m.app.CWD(), m.app.ProviderID, m.app.Model.ID)))
			m.pushLine(styleFooter.Render("Type /quit to exit."))
			return doneMsg{}
		},
	)
}

func (m *Model) subscribe() {
	if m.app == nil {
		return
	}
	m.app.Agent.Subscribe(func(ev protocol.AgentEvent) {
		m.eventChan <- ev
	})
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.editor.SetWidth(msg.Width - 4)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case doneMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			m.pushLine(styleError.Render("error: " + msg.err.Error()))
			return m, nil
		}
		m.busy = false
		return m, nil
	case spinnerMsg:
		if m.busy {
			cmds = append(cmds, spinner.Tick)
		}
	case spinner.TickMsg:
		if m.busy {
			cmds = append(cmds, m.spinner.Tick)
		}
	case agentEventMsg:
		m.handleAgentEvent(msg.ev)
	case appendLineMsg:
		m.pushLine(msg.line)
	}

	// Drain events from the agent channel.
	for {
		select {
		case ev := <-m.eventChan:
			m.handleAgentEvent(ev)
		default:
			goto drained
		}
	}
drained:

	m.editor, _ = m.editor.Update(msg)
	m.transcript, _ = m.transcript.Update(msg)
	return m, tea.Batch(cmds...)
}

func (m *Model) handleAgentEvent(ev protocol.AgentEvent) {
	switch ev.Type {
	case protocol.EvTextDelta:
		m.assistantBuf.WriteString(ev.Text)
		m.renderAssistant()
	case protocol.EvThinkingDelta:
		m.thinkingBuf.WriteString(ev.Text)
	case protocol.EvToolStart:
		m.pushLine(styleTool.Render("▶ " + ev.ToolName))
		m.busy = true
	case protocol.EvToolEnd:
		if ev.IsError {
			m.pushLine(styleError.Render("✖ " + ev.ToolName + ": " + ev.Message))
		} else {
			m.pushLine(styleTool.Render("✔ " + ev.ToolName))
		}
		m.busy = false
	case protocol.EvUsage:
		if ev.Usage != nil {
			m.pushLine(styleFooter.Render(fmt.Sprintf("tokens: in=%d out=%d cache_r=%d cache_w=%d",
				ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheRead, ev.Usage.CacheWrite)))
		}
	case protocol.EvError:
		m.pushLine(styleError.Render("✖ " + ev.Message))
	case protocol.EvTurnDone:
		m.finishAssistant()
		m.busy = false
	case protocol.EvAborted:
		m.pushLine(styleError.Render("aborted"))
		m.finishAssistant()
		m.busy = false
	case protocol.EvModelChanged:
		if ev.Model != nil {
			m.pushLine(styleFooter.Render("model: " + ev.Model.ID))
		}
	}
}

func (m *Model) renderAssistant() {
	m.renderLiveLine("assistant: " + m.assistantBuf.String())
}

func (m *Model) finishAssistant() {
	if m.assistantBuf.Len() > 0 {
		m.pushLine(styleAssistant.Render("assistant: " + m.assistantBuf.String()))
		m.assistantBuf.Reset()
	}
	m.thinkingBuf.Reset()
}

func (m *Model) renderLiveLine(s string) {
	// Replace the last line in place while streaming.
	if len(m.lines) > 0 && strings.HasPrefix(m.lines[len(m.lines)-1], "assistant: ") && !m.assistantFinished {
		m.lines[len(m.lines)-1] = styleAssistant.Render(s)
		m.refreshTranscript()
	}
}

func (m *Model) pushLine(s string) {
	m.assistantFinished = false
	m.lines = append(m.lines, s)
	m.refreshTranscript()
}

func (m *Model) refreshTranscript() {
	content := strings.Join(m.lines, "\n")
	m.transcript.SetContent(content)
	m.transcript.GotoBottom()
}

func (m *Model) layout() {
	if m.height < 8 {
		return
	}
	editorH := 4
	m.editor.SetHeight(editorH - 1)
	m.transcript.Height = m.height - editorH - 2
	m.transcript.Width = m.width - 2
	m.editor.SetWidth(m.width - 4)
}

// handleKey processes key presses and slash commands.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Slash commands are processed on Enter when the editor starts with '/'.
	if msg.Type == tea.KeyEnter && !m.busy {
		text := m.editor.Value()
		if strings.HasPrefix(text, "/") {
			return m.runCommand(strings.TrimSpace(text))
		}
		if strings.TrimSpace(text) != "" {
			m.busy = true
			m.pushLine(styleUser.Render("user: " + text))
			m.editor.Reset()
			return m, m.startPrompt(text)
		}
	}

	switch msg.String() {
	case "ctrl+c":
		if m.busy {
			// First ctrl+c aborts the running turn.
			m.abort()
			m.pushLine(styleError.Render("aborting…"))
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+d":
		if m.editor.Value() == "" {
			return m, tea.Quit
		}
	}

	m.editor, _ = m.editor.Update(msg)
	return m, nil
}

func (m *Model) startPrompt(text string) tea.Cmd {
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	return func() tea.Msg {
		err := m.app.Agent.Prompt(ctx, text)
		if err != nil {
			return agentEventMsg{ev: protocol.AgentEvent{Type: protocol.EvError, Message: err.Error()}}
		}
		return nil
	}
}

func (m *Model) abort() {
	if m.cancelRun != nil {
		m.cancelRun()
	}
}

// runCommand handles slash commands.
func (m *Model) runCommand(line string) (tea.Model, tea.Cmd) {
	m.editor.Reset()
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/quit", "/q":
		return m, tea.Quit
	case "/help":
		m.pushLine(styleFooter.Render(
			"/quit exit · /new new session · /model <id> switch model · " +
				"/permission <ask|allow|deny> · /session show session"))
	case "/new":
		m.pushLine(styleFooter.Render("new session: restart snow to start fresh (in-memory sessions not persisted in this build)"))
	case "/model":
		if len(args) == 0 {
			m.pushLine(styleError.Render("/model requires a model id"))
			return m, nil
		}
		model := m.app.Model
		model.ID = args[0]
		if err := m.app.Agent.SetModel(model); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		} else {
			m.pushLine(styleFooter.Render("model: " + args[0]))
		}
	case "/permission":
		if len(args) == 0 {
			m.pushLine(styleFooter.Render("permission mode: " + string(m.app.Perm.Mode())))
			return m, nil
		}
		switch args[0] {
		case "ask", "allow", "deny":
			m.app.Perm.SetMode(permission.Mode(args[0]))
			m.pushLine(styleFooter.Render("permission mode: " + args[0]))
		default:
			m.pushLine(styleError.Render("invalid mode: " + args[0]))
		}
	case "/session":
		m.pushLine(styleFooter.Render(fmt.Sprintf(
			"session %s · path %s · messages %d",
			m.app.Session.ID(), m.app.Session.Path(), len(m.lines))))
	default:
		m.pushLine(styleError.Render("unknown command: " + cmd + " (try /help)"))
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return "loading snow…"
	}
	status := "idle"
	if m.busy {
		status = "working " + m.spinner.View()
	}
	footer := styleFooter.Render(fmt.Sprintf(
		" %s · %s · %s | %s | %s ",
		m.app.CWD(), m.app.ProviderID, m.app.Model.ID,
		status, m.lastStatus))
	return lipgloss.JoinVertical(lipgloss.Left,
		m.transcript.View(),
		m.editor.View(),
		footer,
	)
}
