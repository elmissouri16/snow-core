package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RunResult reports an explicit post-shutdown action requested by the TUI.
type RunResult struct {
	RestartRequested bool
	SessionPath      string
}

// RunWithResult starts the interactive TUI and reports post-shutdown actions.
func RunWithResult(ctx context.Context, opts app.Options) (RunResult, error) {
	return run(ctx, opts, false)
}

// RunWithSessionPickerResult starts with the session picker and reports
// post-shutdown actions.
func RunWithSessionPickerResult(ctx context.Context, opts app.Options) (RunResult, error) {
	return run(ctx, opts, true)
}

func run(ctx context.Context, opts app.Options, sessionPicker bool) (RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Bubble Tea occupies stdout, so route debug logging to a file when
	// requested (docs: tea.LogToFile pattern).
	if f := os.Getenv("SNOW_DEBUG"); f != "" {
		tf, err := tea.LogToFile(f, "snow-tui")
		if err == nil {
			defer tf.Close()
		}
	}
	// Bubble Tea's supported full-window layout is an alternate-screen frame
	// composed from a sticky header, a Bubbles viewport, and a footer. Mixing a
	// terminal-height normal-screen frame with tea.Println history causes old
	// frames to enter terminal scrollback; scrolling then exposes duplicated
	// headers and stale chrome. Keep one renderer-owned frame instead.
	mouseCapture := tuiMouseCaptureEnabled(opts)
	programOptions := []tea.ProgramOption{
		// App-owned transcript selection needs pointer-rate feedback. Bubble Tea
		// coalesces buffered frames, so the 120 FPS ceiling improves drag latency
		// without making every raw cell-motion event a terminal write.
		tea.WithFPS(120),
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	}
	if mouseCapture {
		// Cell-motion mode reports wheel, press, drag, and release events. Snow
		// uses them for viewport scrolling and application-owned selection/copy.
		programOptions = append(programOptions, tea.WithMouseCellMotion())
	}
	uiCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := newModel(uiCtx, opts)
	model.inlineTranscript = false
	model.asyncIO = true
	model.pickSessionOnStart = sessionPicker
	model.startupResumeRequired = sessionPicker
	p := tea.NewProgram(model, programOptions...)
	_, runErr := p.Run()
	cancel()
	result := RunResult{RestartRequested: model.restartRequested}
	if result.RestartRequested {
		result.SessionPath = currentSessionPath(model.app)
	}
	closeErr := model.Close()
	if runErr != nil || closeErr != nil {
		result.RestartRequested = false
		result.SessionPath = ""
	}
	return result, errors.Join(runErr, closeErr)
}

func tuiMouseCaptureEnabled(opts app.Options) bool {
	path := opts.ConfigPath
	if path == "" {
		path, _, _ = config.DefaultPaths()
	}
	cfg, err := config.Load(path)
	return err == nil && cfg.TUI.Mouse
}

func routeTextareaCmd(target textareaTarget, requestID, questionID string, cmd tea.Cmd) tea.Cmd {
	return routeTextareaCmdGeneration(target, requestID, questionID, 0, cmd)
}

func routeTextareaCmdGeneration(target textareaTarget, requestID, questionID string, generation uint64, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		return textareaResultMsg{
			target: target, requestID: requestID, questionID: questionID,
			pasteGeneration: generation, msg: cmd(),
		}
	}
}

func (i fixedAuthInteraction) Prompt(context.Context, auth.Prompt) (auth.Response, error) {
	return auth.Response{Value: i.value}, nil
}

func (fixedAuthInteraction) OpenURL(context.Context, string) error { return nil }

func (fixedAuthInteraction) Progress(auth.Progress) {}

func (i tuiOAuthInteraction) Prompt(context.Context, auth.Prompt) (auth.Response, error) {
	return auth.Response{}, auth.ErrInteractionUnavailable
}

func (tuiOAuthInteraction) PromptAvailable() bool { return false }

func (i tuiOAuthInteraction) OpenURL(ctx context.Context, target string) error {
	return openOAuthBrowser(ctx, target)
}

func (i tuiOAuthInteraction) Progress(progress auth.Progress) {
	message := oauthProgressMsg{progress: chatgpt.LoginProgress{Kind: progress.Kind, Message: progress.Message, URL: progress.URL, UserCode: progress.UserCode}}
	select {
	case i.events <- message:
	default:
	}
}

func writeTranscriptClipboard(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), transcriptClipboardTimeout)
	defer cancel()

	type clipboardCommand struct {
		name string
		args []string
	}
	var candidates []clipboardCommand
	switch runtime.GOOS {
	case "darwin":
		candidates = []clipboardCommand{{name: "pbcopy"}}
	case "linux":
		candidates = []clipboardCommand{
			{name: "wl-copy"},
			{name: "xclip", args: []string{"-selection", "clipboard"}},
			{name: "xsel", args: []string{"--clipboard", "--input"}},
		}
	default:
		return fmt.Errorf("host clipboard unsupported on %s", runtime.GOOS)
	}

	failures := make([]error, 0, len(candidates))
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate.name)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", candidate.name, err))
			continue
		}
		cmd := exec.CommandContext(ctx, path, candidate.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			failures = append(failures, fmt.Errorf("%s: %w", candidate.name, err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		failures = append(failures, fmt.Errorf("clipboard deadline: %w", err))
	}
	return errors.Join(failures...)
}

func newModel(ctx context.Context, opts app.Options) *Model {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	_ = applyTUITheme("default")
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)
	thinkingSpinner := spinner.New()
	thinkingSpinner.Spinner = spinner.Points
	thinkingSpinner.Style = lipgloss.NewStyle().Foreground(colorAccent)

	ta := textarea.New()
	normalizeTextareaStyles(&ta)
	ta.Placeholder = "Type a message…"
	// The outer input renderer owns the prompt glyph; remove textarea's
	// default boxed prompt so it does not produce a second vertical rail.
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// Plain Enter is reserved for prompt submission by handleKey. Standard
	// terminal input does not preserve Shift+Enter as a distinct key in the
	// Bubble Tea v1 key model, so expose two reliable multiline bindings.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"),
		key.WithHelp("alt+enter", "insert newline"),
	)
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.Focus()

	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	m := &Model{
		ctx:                        ctx,
		cancel:                     cancel,
		opts:                       opts,
		themeName:                  "default",
		customThemes:               map[string]config.ThemeFile{},
		keys:                       tuiKeys,
		transcript:                 vp,
		editor:                     ta,
		spinner:                    sp,
		thinkingSpinner:            thinkingSpinner,
		help:                       help.New(),
		userInputEditor:            newUserInputEditor(),
		events:                     newAgentEventMailbox(),
		md:                         newMarkdownRenderer(),
		thinkingMD:                 newThinkingMarkdownRenderer(),
		subagentFleetMD:            newMarkdownRenderer(),
		nudgeDismissed:             make(map[string]bool),
		subagentFleetActivity:      make(map[string][]string),
		subagentFleetActivityKinds: make(map[string]protocol.AgentEventType),
		subagentFleetActivitySpace: make(map[string]bool),
		queueOriginalText:          make(map[string]string),
		queueRendered:              make(map[string]bool),
		startupApps:                make(map[*app.App]struct{}),
		copySelectionToClipboard:   writeTranscriptClipboard,
		transcriptBaseDirty:        true,
		now:                        time.Now,
	}
	normalizeTextareaStyles(&m.userInputEditor)
	m.asker = newTUIAsker(m.events)
	// No outer border on the transcript — a full-screen box looks broken and
	// forces the whole terminal to scroll. The viewport itself scrolls.
	return m
}

// Close releases the attached app. It is safe to call repeatedly.
func (m *Model) Close() error {
	m.closeOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		if m.oauthCancel != nil {
			m.oauthCancel()
		}
		m.oauthWG.Wait()
		m.events.Close()
		m.startupMu.Lock()
		m.startupClosed = true
		pending := make([]*app.App, 0, len(m.startupApps))
		for candidate := range m.startupApps {
			pending = append(pending, candidate)
		}
		clear(m.startupApps)
		m.startupMu.Unlock()
		for _, candidate := range pending {
			m.closeErr = errors.Join(m.closeErr, candidate.Close())
		}
		// Commands admitted before startupClosed may still be inside app.New.
		// They either retained an app above or close a late app themselves and
		// record its error before dropping their WaitGroup reference.
		m.startupWG.Wait()
		m.startupMu.Lock()
		m.closeErr = errors.Join(m.closeErr, m.startupCloseErr)
		m.startupMu.Unlock()
		if m.app != nil {
			m.closeErr = errors.Join(m.closeErr, m.app.Close())
		}
	})
	return m.closeErr
}

func (m *Model) retainStartupApp(candidate *app.App) bool {
	if candidate == nil {
		return true
	}
	m.startupMu.Lock()
	if m.startupClosed {
		m.startupMu.Unlock()
		closeErr := candidate.Close()
		m.startupMu.Lock()
		m.startupCloseErr = errors.Join(m.startupCloseErr, closeErr)
		m.startupMu.Unlock()
		return false
	}
	m.startupApps[candidate] = struct{}{}
	m.startupMu.Unlock()
	return true
}

func (m *Model) beginStartup() bool {
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	if m.startupClosed {
		return false
	}
	m.startupWG.Add(1)
	return true
}

func (m *Model) releaseStartupApp(candidate *app.App) {
	if candidate == nil {
		return
	}
	m.startupMu.Lock()
	delete(m.startupApps, candidate)
	m.startupMu.Unlock()
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	// Resolve trust before app.New so an allow decision can load project input
	// on this launch and a deny decision guarantees it is never read. The
	// spinner pump starts lazily when a state that actually renders it begins.
	return m.bootstrapCmd()
}

func (m *Model) bootstrapCmd() tea.Cmd {
	return func() tea.Msg {
		if !m.beginStartup() {
			return doneMsg{err: context.Canceled}
		}
		defer m.startupWG.Done()
		preflight, err := app.InspectProjectTrust(m.opts)
		if err != nil {
			return doneMsg{err: err}
		}
		if preflight.Resolution.Prompt {
			return trustPromptMsg{path: preflight.Resolution.Path, store: preflight.Store}
		}
		a, err := app.New(m.ctx, m.opts)
		if a != nil && !m.retainStartupApp(a) {
			return doneMsg{err: context.Canceled}
		}
		return doneMsg{app: a, err: err}
	}
}

func (m *Model) saveTrustCmd(level trust.Level) tea.Cmd {
	store, path := m.trustStore, m.trustPath
	return func() tea.Msg {
		if !m.beginStartup() {
			return trustDecisionMsg{err: context.Canceled}
		}
		defer m.startupWG.Done()
		if store == nil {
			return trustDecisionMsg{err: errors.New("trust store unavailable")}
		}
		if err := store.Set(path, level); err != nil {
			return trustDecisionMsg{err: err}
		}
		a, err := app.New(m.ctx, m.opts)
		if a != nil && !m.retainStartupApp(a) {
			return trustDecisionMsg{err: context.Canceled}
		}
		return trustDecisionMsg{app: a, err: err}
	}
}

func (m *Model) subscribe() error {
	if m.app == nil {
		return errors.New("tui: app unavailable")
	}
	m.app.Agent.Subscribe(m.events.Push)
	m.events.Push(m.app.Agent.StateEvent())
	if err := m.app.ReadyGoal(); err != nil {
		return err
	}
	return m.app.ReadySubagents()
}

// Update implements tea.Model and drains immutable transcript rows into native
// terminal scrollback after each state transition in inline mode.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	wasSpinnerActive := m.spinnerActive()
	model, cmd := m.update(msg)
	updated, ok := model.(*Model)
	if !ok {
		return model, cmd
	}
	// Bubble Tea renders after every message. Keep the timer chain stopped while
	// idle so long-lived sessions do not continuously rebuild unchanged frames.
	if !wasSpinnerActive && updated.spinnerActive() && !updated.spinnerRunning {
		updated.spinnerRunning = true
		cmd = tea.Batch(cmd, updated.spinner.Tick, updated.thinkingSpinner.Tick)
	}
	if historyCmd := updated.commitInlineHistory(); historyCmd != nil {
		return model, tea.Sequence(historyCmd, cmd)
	}
	return model, cmd
}

func (m *Model) spinnerActive() bool {
	if m.lastErr != nil {
		return false
	}
	return m.trustSaving || m.compacting || (m.busy && !m.permPending && !m.userInputPending)
}

func (m *Model) inlineDisplayStart() int {
	if m.inlinePrintInFlight {
		return min(m.inlinePrintEnd, len(m.lines))
	}
	return min(m.inlineCommitted, len(m.lines))
}

func (m *Model) commitInlineHistory() tea.Cmd {
	if !m.inlineTranscript || m.inlinePrintInFlight || (!m.inlineHeaderPending && m.inlineCommitted >= len(m.lines)) {
		return nil
	}
	start, end := m.inlineCommitted, len(m.lines)
	segments := make([]string, 0, end-start+2)
	if m.inlineHeaderPending {
		segments = append(segments,
			m.renderHeader("idle"),
			styleSep.Render(strings.Repeat("─", max(1, m.width))),
		)
		m.inlineHeaderPending = false
	}
	segments = append(segments, m.lines[start:end]...)
	generation := m.inlinePrintGeneration
	m.inlinePrintInFlight = true
	m.inlinePrintEnd = end
	m.inlineEverCommitted = true
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscriptForced()
	m.layout()
	// Bubble Tea executes sequence commands in order and sends each result to
	// the event loop. The renderer handles printLineMessage before this ack, so
	// the next history batch cannot overtake the current one.
	return tea.Sequence(
		tea.Println(strings.Join(segments, "\n")),
		func() tea.Msg { return inlineHistoryAckMsg{generation: generation, end: end} },
	)
}
