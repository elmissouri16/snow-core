// Package tui provides the interactive Bubble Tea interface: transcript,
// editor, footer, slash commands, and streaming updates.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider/chatgpt"
	"github.com/snow-core/snow/internal/provider/openaicompat"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/pkg/protocol"
)

// Run starts the interactive TUI and blocks until exit.
func Run(ctx context.Context, opts app.Options) error {
	return run(ctx, opts, false)
}

// RunWithSessionPicker starts the TUI with the current-project session picker
// open as soon as startup completes.
func RunWithSessionPicker(ctx context.Context, opts app.Options) error {
	return run(ctx, opts, true)
}

func run(ctx context.Context, opts app.Options, sessionPicker bool) error {
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
	return errors.Join(runErr, model.Close())
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

// Styles — tuned for a dark terminal, matching the look of modern agent TUIs
// (fixed header / scrollable body / fixed input, no giant outer box).
var (
	colorAccent lipgloss.TerminalColor = lipgloss.Color("39")  // blue
	colorMuted  lipgloss.TerminalColor = lipgloss.Color("245") // gray
	colorSoft   lipgloss.TerminalColor = lipgloss.Color("252") // near-white
	colorWarn   lipgloss.TerminalColor = lipgloss.Color("214") // orange
	colorErr    lipgloss.TerminalColor = lipgloss.Color("196") // red
	colorOk     lipgloss.TerminalColor = lipgloss.Color("42")  // green

	styleUser      = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleAssistant = lipgloss.NewStyle().Foreground(colorSoft)
	styleTool      = lipgloss.NewStyle().Foreground(colorWarn)
	styleError     = lipgloss.NewStyle().Foreground(colorErr)
	styleFooter    = lipgloss.NewStyle().Foreground(colorMuted)
	styleThinking  = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	styleHeader    = lipgloss.NewStyle().Foreground(colorSoft).Bold(true)
	styleHeaderDim = lipgloss.NewStyle().Foreground(colorMuted)
	styleDiffAdd   = lipgloss.NewStyle().Foreground(colorOk)
	styleDiffDel   = lipgloss.NewStyle().Foreground(colorErr)
	styleBrand     = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleSep       = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	stylePrompt    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleComposer  = lipgloss.NewStyle().Background(lipgloss.Color("236"))

	styleCompletion         = lipgloss.NewStyle().Foreground(colorMuted)
	styleCompletionSelected = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
)

// Messages
type agentEventMsg struct {
	ev protocol.AgentEvent
}
type doneMsg struct {
	err error
	app *app.App // delivered from the Init goroutine
}
type trustPromptMsg struct {
	path  string
	store *trust.Store
}
type trustDecisionMsg struct {
	app *app.App
	err error
}
type appendLineMsg struct {
	line string
}

type oauthProgressMsg struct{ progress chatgpt.LoginProgress }
type oauthDoneMsg struct {
	status chatgpt.AuthStatus
	err    error
}
type logoutDoneMsg struct {
	provider string
	err      error
}
type compatibleLoginDoneMsg struct {
	generation uint64
	endpoint   string
	err        error
}

type chatGPTAccountChoice struct {
	AccountID string
	Sources   []string
}

type compactDoneMsg struct {
	generation uint64
	result     protocol.CompactionResult
	err        error
}

type promptDoneMsg struct {
	generation  uint64
	turnID      string
	admitted    bool
	text        string
	attachments []protocol.ContentBlock
	err         error
}

type transcriptFlushMsg uint64

type contextUsageSnapshot struct {
	usage     *protocol.Usage
	tokens    int
	estimated bool
}

type contextUsageRefreshMsg struct {
	version  uint64
	snapshot contextUsageSnapshot
	err      error
}

type modeSwitchDoneMsg struct {
	target protocol.CollaborationMode
	err    error
}

type textareaTarget uint8

const (
	textareaTargetComposer textareaTarget = iota
	textareaTargetUserInput
)

type textareaResultMsg struct {
	target          textareaTarget
	requestID       string
	questionID      string
	pasteGeneration uint64
	msg             tea.Msg
}

type queueSettledMsg struct {
	epoch uint64
	err   error
}

type queueSubmitMsg struct {
	kind     protocol.QueuedInputKind
	text     string
	expanded string
	itemID   string
	epoch    uint64
	accepted bool
	fallback bool
	err      error
}

type queuedTUIAttempt struct {
	kind     protocol.QueuedInputKind
	text     string
	expanded string
	epoch    uint64
}

type inlineHistoryAckMsg struct {
	generation uint64
	end        int
}

type inlineExitMsg struct{}

// clearMetaEnterMsg expires the short Escape/terminal-fragment window used to
// recover split mouse, Shift+Tab, and Option+Return sequences.
type clearMetaEnterMsg uint64

type mentionFilesMsg struct {
	cwd        string
	generation uint64
	files      []string
	err        error
}

type sessionListMsg struct {
	generation uint64
	sessions   []session.SessionInfo
	err        error
}

type sessionRenameMsg struct {
	generation uint64
	index      int
	title      string
	err        error
}

type branchListMsg struct {
	generation uint64
	branches   []protocol.SessionBranch
	err        error
}

type modelListMsg struct {
	generation uint64
	models     []protocol.Model
	err        error
}

type sessionStoreMsg struct {
	generation uint64
	path       string
	store      session.Store
	err        error
}

type branchActionMsg struct {
	generation uint64
	branch     protocol.SessionBranch
	action     string
	err        error
}

type subagentListMsg struct {
	generation uint64
	list       protocol.SubagentList
	err        error
}

type subagentInspectMsg struct {
	generation uint64
	state      protocol.SubagentState
	messages   []protocol.Message
	messageErr error
	err        error
}

type subagentViewState struct {
	State   protocol.SubagentState
	Preview string
}

// Model is the TUI state.
type Model struct {
	ctx     context.Context
	opts    app.Options
	asyncIO bool
	app     *app.App
	width   int
	height  int

	transcript     viewport.Model
	editor         textarea.Model
	spinner        spinner.Model
	spinnerRunning bool
	help           help.Model

	lines                         []string // rendered transcript lines
	assistantBuf                  strings.Builder
	thinkingBuf                   strings.Builder
	planBuf                       strings.Builder
	currentPlanID                 string
	latestPlan                    string
	goal                          *protocol.ThreadGoal
	confirmGoalReplace            bool
	pendingGoalObjective          string
	pendingGoalBudget             *int64
	sawPlanThisTurn               bool
	completedPlanThisTurn         bool
	planPrompt                    bool
	planPromptChoice              int
	nudgeDismissed                map[string]bool
	subagentViews                 map[string]subagentViewState
	subagentOrder                 []string
	subagentFleetOpen             bool
	subagentFleetLoading          bool
	subagentFleetDetailLoading    bool
	subagentFleetGeneration       uint64
	subagentFleetDetailGeneration uint64
	subagentFleetRequested        string
	subagentFleetList             protocol.SubagentList
	subagentFleetIndex            int
	subagentFleetDetailState      protocol.SubagentState
	subagentFleetMessages         []protocol.Message
	subagentFleetError            string
	subagentFleetDetailError      string
	subagentFleetWarning          string
	subagentFleetDetailOffset     int
	subagentFleetDetailEnd        bool
	subagentFleetActivity         map[string][]string
	subagentFleetActivityKinds    map[string]protocol.AgentEventType
	subagentFleetActivitySpace    map[string]bool
	busy                          bool
	activeTurnID                  string
	runGeneration                 uint64
	compactGeneration             uint64
	runStartedAt                  time.Time
	now                           func() time.Time
	done                          bool
	closeOnce                     sync.Once
	closeErr                      error
	startupMu                     sync.Mutex
	startupWG                     sync.WaitGroup
	startupApps                   map[*app.App]struct{}
	startupClosed                 bool
	startupCloseErr               error
	lastErr                       error
	lastStatus                    string
	trustPending                  bool
	trustPath                     string
	trustStore                    *trust.Store
	trustChoice                   int // 0=continue untrusted, 1=trust project
	trustError                    string
	trustSaving                   bool
	lastErrorText                 string
	themeName                     string
	customThemes                  map[string]config.ThemeFile
	keys                          tuiKeyMap
	auxDiagnostics                []config.Diagnostic
	lastUsage                     *protocol.Usage
	lastRequestUsage              *protocol.Usage
	contextTokens                 int
	contextEstimated              bool
	turnUsageSeen                 bool
	contextRefreshNeeded          bool
	contextRefreshPending         bool
	contextRefreshVersion         uint64
	compacting                    bool
	compactStatus                 string
	events                        *agentEventMailbox
	// Agent callbacks feed a coalescing mailbox. The update loop ingests
	// bounded logical batches and renders stream deltas on a separate cadence.
	batchingEvents          bool
	transcriptDirty         bool
	transcriptFlushPending  bool
	transcriptFlushSeq      uint64
	modeSwitchReady         bool
	modeSwitching           bool
	pendingMode             *protocol.CollaborationMode
	transcriptContent       string
	transcriptBase          string
	transcriptBaseWidth     int
	transcriptBaseDirty     bool
	inlineTranscript        bool
	inlineCommitted         int
	inlinePrintEnd          int
	inlinePrintInFlight     bool
	inlinePrintGeneration   uint64
	inlineEverCommitted     bool
	inlineHistoryKey        string
	inlineCanonicalLines    []string
	inlineDurableMessageIDs []string
	inlineHeaderPending     bool
	inlineExiting           bool
	asker                   *tuiAsker
	md                      *mdRenderer
	thinkingMD              *mdRenderer
	subagentFleetMD         *mdRenderer
	toolRunning             bool
	pendingInputs           protocol.InputQueue
	queueEpoch              uint64
	queueAttempts           []queuedTUIAttempt
	queueOriginalText       map[string]string
	queueRendered           map[string]bool
	queueFallbacks          []queueSubmitMsg
	queueSettleWaiting      bool

	// Command palette state.
	compMatches []string
	compIndex   int
	compVisible bool

	// Agent Skill completion state (for leading $skill-name directives).
	skillMatches []skillCompletionItem
	skillIndex   int
	skillVisible bool

	// File mention picker state (for @path references).
	mentionMatches      []string
	mentionIndex        int
	mentionVisible      bool
	mentionFiles        []string
	mentionFilesCWD     string
	mentionFilesLoaded  bool
	mentionLoading      bool
	mentionGeneration   uint64
	pickerGeneration    uint64
	sessionOpGeneration uint64
	sessionLoading      bool
	treeLoading         bool
	modelLoading        bool
	sessionOpLoading    bool

	// Provider picker state (for /login and /logout).
	pickProvider   bool
	providerLogout bool
	providers      []string
	provIndex      int

	// ChatGPT login actions and compatible credential imports.
	pickChatGPTAuth bool
	authAccounts    []chatGPTAccountChoice
	authIndex       int
	oauthLoading    bool
	oauthProgress   chatgpt.LoginProgress
	oauthCancel     context.CancelFunc
	oauthEvents     chan tea.Msg

	// Model picker state (for /model).
	pickModel         bool
	modelList         []protocol.Model
	modelIndex        int
	modelQuery        string
	modelSearchActive bool

	// Thinking picker state (for /thinking and the second stage of /model).
	pickThinking          bool
	thinkingList          []protocol.ThinkingLevel
	thinkingIndex         int
	thinkingModel         *protocol.Model
	thinkingReturnToModel bool

	// Interactive permission picker state (allow/deny without typing).
	permPending bool
	permRequest *protocol.PermissionRequest
	permAgent   *protocol.AgentRef
	permChoice  int // 0=allow, 1=allow-always, 2=deny

	// Model-requested user input state.
	userInputPending bool
	userInputRequest *protocol.UserInputRequest
	userInputIndex   int
	userInputOption  int
	userInputEditing bool
	userInputAnswers map[string]string
	userInputDrafts  map[string]string
	userInputError   string
	userInputEditor  textarea.Model

	// Permission-mode picker state for /permissions.
	pickPermissionMode  bool
	permissionModeIndex int

	// Settings panel state. Model selection temporarily hands control to the
	// existing model picker and returns here afterward.
	pickSettings          bool
	settingsIndex         int
	settingsReturnToPanel bool
	settingsStatus        string
	settingsError         string

	// Session picker state.
	pickSession           bool
	pickSessionOnStart    bool
	startupResumeRequired bool
	sessions              []session.SessionInfo
	sessionIndex          int
	sessionRenaming       bool
	sessionRenameInput    string

	// Branch tree picker state.
	pickTree     bool
	branches     []protocol.SessionBranch
	branchIndex  int
	branchAction string // fork|rename|delete
	branchInput  string

	// Read-only /mcp and /skills inventory picker state.
	pickInfo         bool
	infoTitle        string
	infoItems        []statusInfoItem
	infoIndex        int
	infoAgentTargets []string
	infoLoading      bool

	// Masked auth capture state.
	loginMode                 bool
	loginEndpointMode         bool
	loginProvider             string
	loginEndpoint             string
	compatibleLoginGeneration uint64
	compatibleLoginPending    bool
	secretBuf                 strings.Builder

	cancelRun context.CancelFunc

	metaEnterPending bool
	metaEnterSeq     uint64
	terminalInput    terminalInputState
	replayingInput   bool

	// Tests replace clipboard commands without reading or writing the host clipboard.
	pasteCmdOverride                 tea.Cmd
	imagePasteCmdOverride            tea.Cmd
	imagePasteGeneration             uint64
	promptImages                     []protocol.ContentBlock
	copySelectionToClipboard         func(string) error
	transcriptSelection              transcriptSelectionState
	transcriptSelectionLines         []string
	transcriptSelectionView          string
	transcriptSelectionViewRow       int
	transcriptSelectionViewValid     bool
	transcriptSelectionRendered      string
	transcriptSelectionRenderedValid bool
	transcriptSelectionClipboard     string
	transcriptSelectionCopyID        uint64
}

type statusInfoItem struct {
	Label  string
	Detail string
}

func newModel(ctx context.Context, opts app.Options) *Model {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = applyTUITheme("default")
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

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
		opts:                       opts,
		themeName:                  "default",
		customThemes:               map[string]config.ThemeFile{},
		keys:                       tuiKeys,
		transcript:                 vp,
		editor:                     ta,
		spinner:                    sp,
		help:                       help.New(),
		userInputEditor:            newUserInputEditor(),
		events:                     newAgentEventMailbox(),
		md:                         newMarkdownRenderer(),
		thinkingMD:                 newThinkingMarkdownRenderer(),
		subagentFleetMD:            newMarkdownRenderer(),
		nudgeDismissed:             make(map[string]bool),
		subagentViews:              make(map[string]subagentViewState),
		subagentFleetActivity:      make(map[string][]string),
		subagentFleetActivityKinds: make(map[string]protocol.AgentEventType),
		subagentFleetActivitySpace: make(map[string]bool),
		queueOriginalText:          make(map[string]string),
		queueRendered:              make(map[string]bool),
		startupApps:                make(map[*app.App]struct{}),
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
		cmd = tea.Batch(cmd, updated.spinner.Tick)
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

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case inlineExitMsg:
		m.inlineExiting = true
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.clearTranscriptSelection()
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.refreshTranscriptForced()
	case tea.MouseMsg:
		// Native context-menu handoff takes precedence over overlays too. A fleet
		// panel may ignore app scrolling, but it must not trap right-click forever.
		if handled, cmd := m.handoffNativeMouseOnRightClick(msg); handled {
			return m, cmd
		}
		if m.subagentFleetOpen {
			return m, nil
		}
		// Application-owned drag selection and viewport wheel scrolling share
		// the same cell-motion mouse stream.
		cmd := m.applyMouse(msg)
		return m, cmd
	case transcriptSelectionAutoScrollMsg:
		return m, m.handleTranscriptSelectionAutoScroll(uint64(msg))
	case transcriptSelectionCopiedMsg:
		if msg.err != nil {
			m.lastStatus = "copy failed: " + msg.err.Error()
			return m, nil
		}
		m.lastStatus = fmt.Sprintf("copied %d characters", msg.characters)
		if msg.sequence == "" {
			return m, nil
		}
		m.transcriptSelectionCopyID++
		id := m.transcriptSelectionCopyID
		m.transcriptSelectionClipboard = msg.sequence
		return m, tea.Tick(transcriptSelectionClipboardRenderGrace, func(time.Time) tea.Msg {
			return transcriptSelectionClipboardClearMsg(id)
		})
	case transcriptSelectionClipboardClearMsg:
		if uint64(msg) == m.transcriptSelectionCopyID {
			m.transcriptSelectionClipboard = ""
		}
		return m, nil
	case tea.KeyMsg:
		if handled, cmd := m.normalizeTerminalKey(msg); handled {
			m.layout()
			return m, cmd
		}
		// PageUp/PageDown/Home/End and explicit Ctrl+arrow bindings scroll the
		// transcript when not in a picker.
		if !m.loginMode && !m.loginEndpointMode && !m.pickProvider && !m.pickChatGPTAuth && !m.pickModel && !m.permPending && !m.userInputPending && !m.subagentFleetOpen && !m.pickPermissionMode && !m.pickSession && !m.pickTree && !m.pickInfo && !m.compVisible && !m.skillVisible && !m.mentionVisible {
			switch {
			case keyMatches(msg, m.keys.PageUp):
				m.transcript.PageUp()
				return m, nil
			case keyMatches(msg, m.keys.PageDown):
				m.transcript.PageDown()
				m.catchUpTranscriptAtBottom()
				return m, nil
			case keyMatches(msg, m.keys.Top):
				m.transcript.GotoTop()
				return m, nil
			case keyMatches(msg, m.keys.Bottom):
				m.transcript.GotoBottom()
				m.catchUpTranscriptAtBottom()
				return m, nil
			case keyMatches(msg, m.keys.LineUp):
				m.transcript.ScrollUp(m.transcript.MouseWheelDelta)
				return m, nil
			case keyMatches(msg, m.keys.LineDown):
				m.transcript.ScrollDown(m.transcript.MouseWheelDelta)
				m.catchUpTranscriptAtBottom()
				return m, nil
			}
		}
		model, cmd := m.handleKey(msg)
		m.layout()
		return model, cmd
	case trustPromptMsg:
		m.trustPending = true
		m.trustPath = msg.path
		m.trustStore = msg.store
		m.trustChoice = 0
		m.trustError = ""
		m.trustSaving = false
		m.layout()
		return m, nil
	case trustDecisionMsg:
		m.releaseStartupApp(msg.app)
		m.trustSaving = false
		if msg.err != nil {
			if msg.app != nil {
				_ = msg.app.Close()
			}
			m.trustError = msg.err.Error()
			m.layout()
			return m, nil
		}
		m.trustPending = false
		m.trustPath = ""
		m.trustStore = nil
		m.trustError = ""
		return m.Update(doneMsg{app: msg.app})
	case doneMsg:
		m.releaseStartupApp(msg.app)
		if msg.err != nil {
			if msg.app != nil {
				_ = msg.app.Close()
			}
			m.lastErr = msg.err
			m.busy = false
			m.editor.Reset()
			m.editor.Placeholder = "Startup failed · ctrl+c to quit"
			m.editor.Blur()
			m.pushLine(styleError.Render("error: " + msg.err.Error()))
			m.layout()
			m.refreshTranscript()
			return m, nil
		}
		if msg.app != nil {
			m.app = msg.app
			m.loadAuxiliaryTUIConfig()
			if msg.app.Cfg.TUI.Theme != "" {
				if err := m.applyThemeSelection(msg.app.Cfg.TUI.Theme, false, false); err != nil {
					m.auxDiagnostics = append(m.auxDiagnostics, config.Diagnostic{Path: "tui.theme", Message: err.Error()})
					_ = m.applyThemeSelection("default", false, false)
				}
			}
			m.app.EnableUserInputReplies()
			m.modelList = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
			m.asker.SetPublisher(m.app.Agent.Publish)
			m.app.Perm.SetAsker(m.asker)
			m.hydrateSession()
			if err := m.subscribe(); err != nil {
				m.lastErr = err
				m.pushLine(styleError.Render("goal restore: " + err.Error()))
			}
			for _, diagnostic := range append(append([]config.Diagnostic(nil), msg.app.Diagnostics...), m.auxDiagnostics...) {
				m.pushLine(styleFooter.Render("config warning: " + diagnostic.Path + ": " + diagnostic.Message))
			}
			// The sticky header and footer already expose provider, model, cwd,
			// and commands; do not duplicate startup chrome in the transcript.
			cmds = append(cmds, waitForEvent(m.events))
			if m.pickSessionOnStart {
				m.pickSessionOnStart = false
				_, pickerCmd := m.startSessionPick()
				cmds = append(cmds, pickerCmd)
			}
		}
		m.busy = false
		return m, tea.Batch(cmds...)
	case spinner.TickMsg:
		// Standard bubbles spinner pump: advance the frame and re-arm only while
		// there is something visible to animate. Streaming responsiveness does
		// not depend on these ticks — agent events arrive via waitForEvent.
		if !m.spinnerActive() {
			m.spinnerRunning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.spinnerRunning = true
		cmds = append(cmds, cmd)
		if m.showThinkingPlaceholder() {
			m.refreshTranscript()
		}
	case agentEventMsg:
		return m.Update(agentEventBatchMsg{events: []protocol.AgentEvent{msg.ev}})
	case contextUsageRefreshMsg:
		m.contextRefreshPending = false
		if msg.err == nil && msg.version == m.contextRefreshVersion {
			m.applyContextUsageSnapshot(msg.snapshot)
		}
		if cmd := m.scheduleContextUsageRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case agentEventBatchMsg:
		// Ingest a bounded, already-coalesced logical batch. Streaming deltas
		// schedule a render separately so input is never queued behind reflow.
		m.batchingEvents = true
		immediate := false
		for _, ev := range coalesceRootSessionUpdates(msg.events) {
			m.handleAgentEvent(ev)
			immediate = immediate || eventNeedsImmediateTranscript(ev.Type)
		}
		m.batchingEvents = false
		m.layout()
		if m.transcriptDirty {
			if immediate && m.transcript.AtBottom() {
				m.flushTranscriptImmediately()
			} else if cmd := m.scheduleTranscriptFlush(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(m.queueFallbacks) > 0 && !m.busy && m.app != nil {
			if m.app.Agent.IsRunning() {
				if cmd := m.waitForQueueSettlement(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else if cmd := m.startQueueFallback(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		} else if m.modeSwitchReady {
			if cmd := m.beginPendingModeSwitch(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := m.scheduleContextUsageRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Re-arm immediately; the mailbox self-signals while more batches wait.
		cmds = append(cmds, waitForEvent(m.events))
	case compactDoneMsg:
		if msg.generation != m.compactGeneration {
			return m, nil
		}
		// EvCompactionDone is the authoritative lifecycle transition. This
		// command result only reports the manual operation's summary/error; it
		// must not unlock a newer operation or an automatic goal continuation.
		if m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() {
			m.setRunIdle()
		}
		m.refreshContextUsageFromSession()
		m.layout()
		if msg.err != nil {
			m.lastErrorText = msg.err.Error()
			m.pushLine(styleError.Render("compact: " + msg.err.Error()))
		} else if msg.result.SummarizedMessages == 0 {
			m.pushLine(styleFooter.Render("compact: nothing to compact"))
		} else {
			status := fmt.Sprintf("compact: summarized %d messages, retained %d", msg.result.SummarizedMessages, msg.result.RetainedMessages)
			if msg.result.UsedFallback {
				status += " (local fallback)"
			}
			if m.goal != nil && m.goal.Status == protocol.GoalActive && m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() {
				if deferred, err := m.app.GoalContinuationDeferred(); err == nil && deferred {
					status += " · goal paused; /goal resume to continue"
				}
			}
			m.lastStatus = status
			m.pushLine(styleFooter.Render(status))
		}
		if m.pendingMode != nil {
			m.modeSwitchReady = true
			if cmd := m.beginPendingModeSwitch(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case promptDoneMsg:
		if msg.generation != m.runGeneration || msg.err == nil {
			return m, nil
		}
		if !msg.admitted {
			if len(msg.attachments) > 0 {
				m.promptImages = append(msg.attachments, m.promptImages...)
			}
			if msg.text != "" {
				current := m.editor.Value()
				if current == "" {
					m.editor.SetValue(msg.text)
				} else {
					m.editor.SetValue(msg.text + "\n" + current)
				}
				m.editor.CursorEnd()
			}
			m.layout()
		}
		// Only errors rejected before admission lack a terminal lifecycle event.
		// An admitted failed turn remains locked until its correlated turn_done or
		// aborted event reaches this reducer.
		if !msg.admitted && m.app != nil && m.app.Agent != nil && !m.app.Agent.IsRunning() && m.activeTurnID == "" {
			m.setRunIdle()
		}
		m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: msg.turnID, Message: msg.err.Error()})
	case appendLineMsg:
		m.pushLine(msg.line)
	case oauthProgressMsg:
		if m.oauthLoading {
			m.oauthProgress = msg.progress
			return m, waitOAuthEvent(m.oauthEvents)
		}
	case oauthDoneMsg:
		if m.oauthLoading {
			m.oauthLoading = false
			m.oauthCancel = nil
			m.oauthEvents = nil
			m.pickChatGPTAuth = false
			if msg.err != nil {
				m.pushLine(styleError.Render(msg.err.Error()))
			} else {
				m.pushLine(styleFooter.Render("chatgpt: " + chatgpt.FormatStatus(msg.status)))
			}
		}
	case logoutDoneMsg:
		if msg.err != nil {
			m.pushLine(styleError.Render("logout: " + msg.err.Error()))
		} else {
			m.pushLine(styleFooter.Render("logged out " + msg.provider))
		}
	case compatibleLoginDoneMsg:
		if msg.generation != m.compatibleLoginGeneration {
			break
		}
		m.compatibleLoginPending = false
		m.modelList = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
		if msg.err != nil {
			m.pushLine(styleError.Render("openai-compatible configured; model discovery failed: " + msg.err.Error()))
		} else {
			m.pushLine(styleFooter.Render("openai-compatible configured for " + msg.endpoint + " · choose /model to switch"))
		}
	case inlineHistoryAckMsg:
		if msg.generation == m.inlinePrintGeneration && m.inlinePrintInFlight && msg.end == m.inlinePrintEnd {
			m.inlineCommitted = msg.end
			m.inlinePrintEnd = msg.end
			m.inlinePrintInFlight = false
			m.inlineEverCommitted = true
			m.transcriptBaseDirty = true
			m.transcriptDirty = true
			m.refreshTranscriptForced()
			m.layout()
		}
	case transcriptFlushMsg:
		if uint64(msg) == m.transcriptFlushSeq {
			m.transcriptFlushPending = false
			m.layout()
			m.refreshTranscript()
		}
	case modeSwitchDoneMsg:
		m.finishModeSwitch(msg)
	case clearMetaEnterMsg:
		messages := m.expireTerminalInput(uint64(msg))
		if len(messages) == 0 && uint64(msg) == m.metaEnterSeq {
			m.metaEnterPending = false
		}
		m.replayTerminalMessages(messages, &cmds)
		m.layout()
	case mentionFilesMsg:
		if msg.generation != m.mentionGeneration || m.app == nil || msg.cwd != m.app.CWD() {
			return m, nil
		}
		m.mentionLoading = false
		if msg.err != nil {
			m.mentionFiles = nil
			m.mentionFilesCWD = msg.cwd
			m.mentionFilesLoaded = true
			m.pushLine(styleError.Render("file mentions: " + msg.err.Error()))
			m.refreshMentions()
			return m, nil
		}
		m.mentionFiles = append([]string(nil), msg.files...)
		m.mentionFilesCWD = msg.cwd
		m.mentionFilesLoaded = true
		m.refreshMentions()
		m.layout()
	case sessionListMsg:
		if msg.generation != m.pickerGeneration || !m.pickSession {
			return m, nil
		}
		m.sessionLoading = false
		if msg.err != nil {
			m.pickSession = false
			m.pushLine(styleError.Render("session list: " + msg.err.Error()))
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
			return m, nil
		}
		m.sessions = msg.sessions
		if len(m.sessions) == 0 {
			m.pickSession = false
			m.pushLine(styleFooter.Render(m.noSessionsResumeMessage()))
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
			return m, nil
		}
		m.sessionIndex = 0
		for i, info := range m.sessions {
			if m.app != nil && info.ID == currentSessionID(m.app) {
				m.sessionIndex = i
				break
			}
		}
		m.layout()
	case sessionRenameMsg:
		if msg.generation != m.pickerGeneration || !m.pickSession {
			return m, nil
		}
		m.sessionLoading = false
		if msg.err != nil {
			m.pushLine(styleError.Render("session rename: " + msg.err.Error()))
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.sessions) {
			m.sessions[msg.index].Name = msg.title
		}
		m.lastStatus = "renamed session " + msg.title
		m.layout()
	case branchListMsg:
		if msg.generation != m.pickerGeneration || !m.pickTree {
			return m, nil
		}
		m.treeLoading = false
		if msg.err != nil {
			m.pickTree = false
			m.pushLine(styleError.Render("tree: " + msg.err.Error()))
			return m, nil
		}
		m.branches = orderBranches(msg.branches)
		if len(m.branches) == 0 {
			m.pickTree = false
			m.pushLine(styleFooter.Render("tree: no branches"))
			return m, nil
		}
		m.branchIndex = 0
		for i, branch := range m.branches {
			if branch.Active {
				m.branchIndex = i
				break
			}
		}
		m.layout()
	case modelListMsg:
		if msg.generation != m.pickerGeneration || !m.pickModel {
			return m, nil
		}
		m.modelLoading = false
		if msg.err != nil {
			m.pickModel = false
			m.pushLine(styleError.Render("model list: " + msg.err.Error()))
			return m, nil
		}
		m.modelList = uniquePickerModels(msg.models, m.app.ProviderID)
		m.modelQuery = ""
		m.modelSearchActive = false
		if len(m.modelList) == 0 {
			m.pickModel = false
			m.pushLine(styleError.Render("no models available"))
			return m, nil
		}
		m.modelIndex = 0
		for i, model := range m.modelList {
			if m.app != nil && model.Provider == m.app.Model.Provider && model.ID == m.app.Model.ID {
				m.modelIndex = i
				break
			}
		}
		m.layout()
	case subagentFleetListMsg:
		return m, m.applySubagentFleetList(msg)
	case subagentFleetDetailMsg:
		m.applySubagentFleetDetail(msg)
		return m, nil
	case subagentInspectMsg:
		if msg.generation != m.pickerGeneration {
			return m, nil
		}
		m.lastStatus = "idle"
		if msg.err != nil {
			m.pushLine(styleError.Render(msg.err.Error()))
			return m, nil
		}
		m.pushLine(styleFooter.Render(renderSubagentInspection(msg.state, msg.messages, msg.messageErr, m.app.Cfg.Subagents.Durable, time.Now())))
	case subagentListMsg:
		if msg.generation != m.pickerGeneration || !m.pickInfo {
			return m, nil
		}
		m.infoLoading = false
		if msg.err != nil {
			m.closeInfoPicker()
			m.pushLine(styleError.Render("agents: " + msg.err.Error()))
			return m, nil
		}
		items, targets := subagentInfoItems(msg.list, m.app.Cfg.Subagents.Durable, time.Now())
		m.infoTitle = subagentInfoTitle(msg.list)
		m.infoItems = items
		m.infoAgentTargets = targets
		if len(items) == 0 {
			m.closeInfoPicker()
			m.pushLine(styleFooter.Render("agents: none"))
		}
		m.layout()
	case branchActionMsg:
		if msg.generation != m.pickerGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.treeLoading = false
			m.pushLine(styleError.Render("tree: " + msg.err.Error()))
			return m, nil
		}
		m.treeLoading = false
		m.pickTree = false
		m.branches = nil
		m.subagentViews = make(map[string]subagentViewState)
		m.subagentOrder = nil
		m.subagentFleetActivity = make(map[string][]string)
		m.subagentFleetActivityKinds = make(map[string]protocol.AgentEventType)
		m.subagentFleetActivitySpace = make(map[string]bool)
		m.subagentFleetList = protocol.SubagentList{}
		m.subagentFleetMessages = nil
		m.subagentFleetDetailState = protocol.SubagentState{}
		m.closeSubagentFleet()
		m.hydrateSession()
		if err := m.app.ReadyGoal(); err != nil {
			m.pushLine(styleError.Render("tree goal: " + err.Error()))
			return m, nil
		}
		switch msg.action {
		case "fork":
			m.lastStatus = "forked branch " + msg.branch.Name
		case "rename":
			m.lastStatus = "renamed branch " + msg.branch.Name
		case "delete":
			m.lastStatus = "deleted branch"
		default:
			m.lastStatus = "selected branch " + msg.branch.Name
		}
	case sessionStoreMsg:
		if msg.generation != m.sessionOpGeneration {
			if msg.store != nil {
				_ = msg.store.Close()
			}
			return m, nil
		}
		m.sessionOpLoading = false
		if msg.err != nil {
			m.pushLine(styleError.Render("session: " + msg.err.Error()))
			if m.startupResumeRequired {
				return m.startSessionPick()
			}
			return m, nil
		}
		if msg.store == nil {
			m.pushLine(styleError.Render("session: empty store result"))
			if m.startupResumeRequired {
				return m.startSessionPick()
			}
			return m, nil
		}
		if err := m.switchSession(msg.store); err != nil {
			_ = msg.store.Close()
			m.pushLine(styleError.Render("session: " + err.Error()))
			return m, nil
		}
		if msg.path == "new" {
			m.lastStatus = "new session"
		} else {
			m.lastStatus = "resumed " + shortSessionID(msg.store.ID())
		}
	case queueSettledMsg:
		if msg.epoch != m.queueEpoch {
			return m, nil
		}
		m.queueSettleWaiting = false
		if msg.err != nil || len(m.queueFallbacks) == 0 || m.busy || m.app == nil || m.app.Agent.IsRunning() {
			return m, nil
		}
		return m, m.startQueueFallback()
	case queueSubmitMsg:
		m.removeQueueAttempt(msg)
		if msg.epoch != m.queueEpoch {
			return m, nil
		}
		if msg.err != nil {
			m.pushLine(styleError.Render(string(msg.kind) + ": " + msg.err.Error()))
			return m, nil
		}
		if msg.fallback {
			// Wait until the preceding turn_done has been ingested before starting
			// the fallback prompt. Otherwise that late event can mark the new run
			// idle after it has already started.
			m.queueFallbacks = append(m.queueFallbacks, msg)
			if !m.busy {
				if m.app.Agent.IsRunning() {
					return m, m.waitForQueueSettlement()
				}
				return m, m.startQueueFallback()
			}
			m.lastStatus = "waiting for current turn to settle"
			return m, nil
		}
		if msg.accepted {
			pending := false
			for _, item := range m.app.Agent.PendingInputs().Items {
				if item.ID == msg.itemID {
					pending = true
					m.queueOriginalText[item.ID] = msg.text
					m.renderQueuedInput(item, msg.text)
					break
				}
			}
			if !pending {
				// Delivery may beat this acknowledgment. Still render the accepted
				// submission, but do not retain it as abort-restorable state.
				m.renderQueuedInput(protocol.QueuedInput{ID: msg.itemID, Kind: msg.kind, Text: msg.text}, msg.text)
				delete(m.queueRendered, msg.itemID)
			}
			if m.editor.Value() == msg.text {
				m.editor.Reset()
				m.refreshInputCompletions()
			}
		}
		m.layout()
		return m, nil
	case textareaResultMsg:
		return m.applyTextareaResult(msg)
	case clipboardImageMsg:
		if msg.generation != 0 && msg.generation != m.imagePasteGeneration {
			return m, nil
		}
		if msg.err != nil {
			// A non-image clipboard is expected during ordinary text paste. Other
			// failures (timeout, oversize, malformed image) must remain visible.
			if errors.Is(msg.err, errClipboardHasNoImage) {
				return m, routeTextareaCmdGeneration(textareaTargetComposer, "", "", msg.generation, textarea.Paste)
			}
			m.lastErrorText = "paste image: " + msg.err.Error()
			m.pushLine(styleError.Render(m.lastErrorText))
			return m, nil
		}
		if len(m.promptImages) >= maxPromptImages {
			m.lastErrorText = fmt.Sprintf("paste: at most %d images per prompt", maxPromptImages)
			return m, nil
		}
		m.promptImages = append(m.promptImages, msg.block)
		m.lastStatus = fmt.Sprintf("attached image %d", len(m.promptImages))
		m.layout()
		return m, nil
	}

	if m.userInputPending && m.userInputEditing {
		previous := m.userInputEditor.Value()
		var cmd tea.Cmd
		m.userInputEditor, cmd = m.userInputEditor.Update(msg)
		if question := m.currentUserInputQuestion(); question != nil {
			m.userInputDrafts[question.ID] = m.userInputEditor.Value()
		}
		if m.userInputEditor.Value() != previous {
			m.layout()
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		previousEditorValue := m.editor.Value()
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		if m.editor.Value() != previousEditorValue {
			// Paste and other non-key textarea messages bypass handleKey but must
			// still resize the composer and refresh input-driven overlays.
			if mentionCmd := m.refreshInputCompletions(); mentionCmd != nil {
				cmds = append(cmds, mentionCmd)
			}
			m.layout()
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.transcript, _ = m.transcript.Update(msg)
	return m, tea.Batch(cmds...)
}

func (m *Model) applyTextareaResult(result textareaResultMsg) (tea.Model, tea.Cmd) {
	if result.msg == nil || result.target == textareaTargetComposer && result.pasteGeneration != 0 && result.pasteGeneration != m.imagePasteGeneration {
		return m, nil
	}
	switch result.target {
	case textareaTargetUserInput:
		question := m.currentUserInputQuestion()
		if !m.userInputPending || !m.userInputEditing || m.userInputRequest == nil || question == nil ||
			m.userInputRequest.ID != result.requestID || question.ID != result.questionID {
			return m, nil
		}
		var cmd tea.Cmd
		m.userInputEditor, cmd = m.userInputEditor.Update(result.msg)
		m.userInputDrafts[question.ID] = m.userInputEditor.Value()
		if m.userInputEditor.Err != nil {
			m.userInputError = "paste: " + m.userInputEditor.Err.Error()
		}
		m.layout()
		return m, cmd
	default:
		previous := m.editor.Value()
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(result.msg)
		if m.editor.Err != nil {
			m.lastErrorText = "paste: " + m.editor.Err.Error()
			m.pushLine(styleError.Render(m.lastErrorText))
		}
		if m.editor.Value() != previous {
			if mentionCmd := m.refreshInputCompletions(); mentionCmd != nil {
				cmd = tea.Batch(cmd, mentionCmd)
			}
			m.layout()
		}
		return m, cmd
	}
}

func (m *Model) handleSubagentEvent(ev protocol.AgentEvent) {
	m.recordSubagentFleetEvent(ev)
	id := ev.Agent.ThreadID
	view, exists := m.subagentViews[id]
	if !exists {
		m.subagentOrder = append(m.subagentOrder, id)
	}
	if ev.Subagent != nil {
		view.State = *ev.Subagent.Clone()
	} else if !exists {
		view.State.Agent = *ev.Agent.Clone()
	}
	if ev.Type == protocol.EvTextDelta || ev.Type == protocol.EvThinkingDelta {
		view.Preview = boundedUTF8Tail(view.Preview+ev.Text, 4096)
	}
	m.subagentViews[id] = view
	switch ev.Type {
	case protocol.EvSubagentStarted:
		m.pushLine(styleTool.Render(fmt.Sprintf("• agent %s started (%s)", ev.Agent.Path, ev.Agent.Role)))
	case protocol.EvSubagentStatus:
		if ev.Subagent != nil && ev.Subagent.Status.Terminal() {
			m.pushLine(styleTool.Render(fmt.Sprintf("• agent %s %s", ev.Agent.Path, ev.Subagent.Status)))
		}
	}
}

func (m *Model) renderQueuedInput(item protocol.QueuedInput, text string) {
	if m.queueRendered[item.ID] {
		return
	}
	label := "queued steer"
	if item.Kind == protocol.QueuedInputFollowUp {
		label = "queued follow-up"
	}
	m.pushLine(styleTool.Render("↳ " + label + ": " + sanitizeToolPreview(text, 300)))
	m.queueRendered[item.ID] = true
}

func (m *Model) setRunIdle() {
	m.busy = false
	m.activeTurnID = ""
	m.toolRunning = false
	m.compacting = false
	m.compactStatus = ""
	m.runStartedAt = time.Time{}
	m.cancelRun = nil
}

func (m *Model) adoptTurn(ev protocol.AgentEvent) {
	if ev.Agent != nil || ev.TurnID == "" {
		return
	}
	if ev.TurnID != m.activeTurnID {
		m.turnUsageSeen = false
	}
	m.activeTurnID = ev.TurnID
	m.busy = true
	if m.runStartedAt.IsZero() {
		m.runStartedAt = m.currentTime()
	}
}

func (m *Model) staleRootEvent(ev protocol.AgentEvent) bool {
	if ev.Agent != nil || ev.TurnID == "" {
		return false
	}
	if m.activeTurnID != "" {
		return ev.TurnID != m.activeTurnID
	}
	if m.app != nil && m.app.Agent != nil {
		_, activeID, running := m.app.Agent.ActiveTurn()
		return running && activeID != "" && ev.TurnID != activeID
	}
	return false
}

func (m *Model) handleAgentEvent(ev protocol.AgentEvent) {
	// Root stream events deliberately have no Agent attribution. Give the fleet
	// inspector an inspector-only root identity without changing the public event
	// or allowing it into child handling.
	if ev.Agent == nil && m.app != nil && m.app.Agent != nil && m.subagentFleetOpen {
		root := protocol.AgentRef{ThreadID: "root", Path: protocol.RootAgentPath, Role: "root", Depth: 0}
		for _, state := range m.subagentFleetList.Agents {
			if state.Agent.Path == protocol.RootAgentPath {
				root = state.Agent
				break
			}
		}
		fleetEvent := ev.Clone()
		fleetEvent.Agent = root.Clone()
		m.recordSubagentFleetEvent(fleetEvent)
	}
	// Child streams never reuse root scalar buffers or trigger root session
	// hydration. Bubble Tea's Update goroutine alone mutates this map.
	if ev.Agent != nil {
		m.handleSubagentEvent(ev)
		if ev.Type != protocol.EvPermissionRequest && ev.Type != protocol.EvUserInputRequest {
			return
		}
	}
	// Ignore every delayed event from an older turn, not just its terminal
	// boundary. Command results and mailbox delivery use separate Bubble Tea
	// paths, so a newer turn can be admitted before an old batch is reduced.
	if m.staleRootEvent(ev) {
		return
	}
	// Session updates describe persistence, not active provider work. In
	// particular, a delayed update after a terminal compaction event must not
	// resurrect the completed turn and restart the idle spinner.
	if ev.Type != protocol.EvTurnDone && ev.Type != protocol.EvAborted && ev.Type != protocol.EvSessionUpdated {
		m.adoptTurn(ev)
	}
	switch ev.Type {
	case protocol.EvCompactionStarted:
		m.busy = true
		m.compacting = true
		m.compactStatus = strings.TrimSpace(ev.Message)
		if m.compactStatus == "" {
			m.compactStatus = "compacting context"
		}
	case protocol.EvCompactionDone:
		m.compacting = false
		m.compactStatus = ""
		m.activeTurnID = ""
		// Automatic compaction is one phase of an admitted ordinary or goal turn.
		// Manual compaction settles only when the core confirms no admitted
		// operation is still running; never unlock based solely on event ordering.
		if ev.Compaction == nil || !ev.Compaction.Automatic {
			if m.app == nil || m.app.Agent == nil || !m.app.Agent.IsRunning() {
				m.setRunIdle()
			}
		}
		m.refreshContextUsageFromSession()
	case protocol.EvSessionUpdated:
		// Assistant/tool persistence happens before turn_done. Rehydrating or
		// rescanning the full SQLite branch here both duplicates live transcript
		// state and blocks Bubble Tea's input goroutine on every tool cycle.
		// EvUsage already carries authoritative current-context usage while busy.
		// Turn-attributed updates are represented by the live event stream even if
		// an optimistic abort has already unlocked the composer; retain idle
		// hydration only for externally initiated, unattributed session mutations.
		if ev.TurnID == "" && !m.busy && m.assistantBuf.Len() == 0 && m.thinkingBuf.Len() == 0 && m.planBuf.Len() == 0 {
			m.hydrateSession()
		}
	case protocol.EvTextDelta:
		m.finalizeThinking()
		m.finalizePlan()
		m.assistantBuf.WriteString(ev.Text)
		m.refreshTranscript()
	case protocol.EvPlanStarted:
		m.finishAssistant()
		m.planBuf.Reset()
		m.currentPlanID = ""
		if ev.Plan != nil {
			m.currentPlanID = ev.Plan.ID
		}
		m.sawPlanThisTurn = true
	case protocol.EvPlanDelta:
		m.planBuf.WriteString(ev.Text)
		m.refreshTranscript()
	case protocol.EvPlanCompleted:
		if ev.Plan != nil && strings.TrimSpace(ev.Plan.Text) != "" {
			m.planBuf.Reset()
			m.planBuf.WriteString(ev.Plan.Text)
			m.completedPlanThisTurn = true
			m.finalizePlan()
		}
	case protocol.EvPlanUpdate:
		m.finishAssistant()
		if ev.PlanUpdate != nil {
			m.pushLine(m.renderPlanUpdate(*ev.PlanUpdate))
		}
	case protocol.EvThreadGoalUpdated:
		if ev.GoalContinuing {
			if ev.TurnID != "" && ev.TurnID != m.activeTurnID {
				m.lastErrorText = ""
			}
			m.adoptTurn(ev)
		}
		if ev.ThreadGoal != nil {
			if ev.ThreadGoal.Cleared {
				m.goal = nil
			} else {
				m.goal = ev.ThreadGoal.Goal.Clone()
			}
			// Goal workers can stop at a safe boundary (pause, clear, blocked
			// auto-compaction) without another turn terminal event. A terminal goal
			// snapshot is therefore also a lifecycle boundary once core is idle.
			if (m.goal == nil || m.goal.Status != protocol.GoalActive) &&
				(m.app == nil || m.app.Agent == nil || !m.app.Agent.IsRunning()) {
				m.setRunIdle()
			}
		}
		m.refreshTranscript()
	case protocol.EvModeChanged:
		if ev.Mode != nil {
			m.lastStatus = "mode " + string(ev.Mode.Mode)
		}
	case protocol.EvThinkingDelta:
		m.thinkingBuf.WriteString(ev.Text)
		m.refreshTranscript()
	case protocol.EvToolStart:
		m.toolRunning = true
		m.finishAssistant()
		// Call IDs stay in protocol/session data for correlation, but are
		// implementation noise in the native transcript. File tools include
		// their path so edits read like a compact terminal diff card.
		label := ev.ToolName
		if path := strings.TrimSpace(ev.Message); path != "" && path != "running" {
			label += " " + sanitizeToolPreview(path, 500)
		}
		if ev.ToolName == "spawn_agent" {
			// EvSubagentStarted supplies the useful path and role a few moments
			// later; avoid three transcript rows for one successful spawn.
		} else if ev.ToolName == "edit" {
			m.pushLine(styleTool.Render(label))
		} else {
			m.pushLine(styleTool.Render("▶ " + label))
		}
		m.busy = true
	case protocol.EvToolProgress:
		m.busy = true
		message := strings.TrimSpace(ev.Message)
		if ev.ToolProgress != nil && message == "" {
			message = strings.TrimSpace(ev.ToolProgress.Message)
		}
		if message != "" {
			m.pushLine(styleHeaderDim.Render("  ↳ " + sanitizeToolPreview(message, 500)))
		}
	case protocol.EvToolEnd:
		if ev.ToolName == "ask_user" && m.userInputPending {
			m.clearUserInput()
		}
		m.toolRunning = false
		label := ev.ToolName
		if ev.ToolDurationMS > 0 {
			label += fmt.Sprintf("  (%dms)", ev.ToolDurationMS)
		}
		if ev.IsError {
			message := strings.TrimSpace(ev.Message)
			if message == "" {
				message = "tool failed"
			}
			m.pushLine(styleError.Render("✖ " + label + ": " + sanitizeToolPreview(message, 700)))
		} else if ev.ToolName != "spawn_agent" && !toolHasDiffPreview(ev.ToolName, ev.ToolOutput) {
			m.pushLine(styleTool.Render("✔ " + label))
		}
		if !ev.IsError {
			if preview := renderToolOutput(ev.ToolName, ev.ToolOutput, m.width); preview != "" {
				m.pushLine(preview)
			}
		}
		// Keep the composer locked until turn_done. Tool calls are serial but
		// their end/start events can be separated by scheduling, so unlocking
		// here permits a second Prompt to race the current agent turn.
		m.busy = true
	case protocol.EvUsage:
		if ev.Usage != nil {
			m.lastUsage = ev.Usage.Clone()
			m.lastRequestUsage = ev.Usage.Clone()
			m.contextTokens = contextTokensFromUsage(*ev.Usage)
			m.contextEstimated = false
			m.turnUsageSeen = true
			m.contextRefreshNeeded = false
			m.contextRefreshVersion++
			// Per-request token accounting remains available in debug mode;
			// the compact aggregate stays in the sticky footer.
			if os.Getenv("SNOW_DEBUG") != "" {
				m.finishAssistant()
				m.pushLine(styleFooter.Render(fmt.Sprintf("tokens %d in · %d out · %d cached",
					ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheRead)))
			}
		}
	case protocol.EvQueueUpdated:
		previous := make(map[string]bool, len(m.pendingInputs.Items))
		for _, item := range m.pendingInputs.Items {
			previous[item.ID] = true
		}
		if ev.Queue == nil {
			m.pendingInputs = protocol.InputQueue{}
		} else {
			m.pendingInputs = *ev.Queue.Clone()
		}
		current := make(map[string]bool, len(m.pendingInputs.Items))
		for _, item := range m.pendingInputs.Items {
			current[item.ID] = true
			if previous[item.ID] {
				continue
			}
			if original := m.queueOriginalText[item.ID]; original != "" {
				m.renderQueuedInput(item, original)
			} else if !m.hasQueueAttempt(item) {
				// Inputs admitted by another programmatic surface have no compact
				// TUI draft to recover, so render their model-visible text directly.
				m.renderQueuedInput(item, item.Text)
			}
		}
		for id := range m.queueOriginalText {
			if !current[id] {
				delete(m.queueOriginalText, id)
				delete(m.queueRendered, id)
			}
		}
		m.layout()
	case protocol.EvPermissionRequest:
		if ev.Permission != nil {
			if ev.Agent == nil {
				m.busy = true
			}
			req := ev.Permission.Request
			m.permPending = true
			m.permRequest = &req
			m.permAgent = ev.Agent.Clone()
			m.permChoice = 0
			m.layout()
			m.finishAssistant()
			label := "🔐 permission request: " + req.Tool
			if ev.Agent != nil {
				label += " · " + string(ev.Agent.Path)
			}
			m.pushLine(styleTool.Render(label))
		}
	case protocol.EvUserInputRequest:
		if ev.UserInput != nil {
			m.startUserInput(*ev.UserInput)
		}
	case protocol.EvError:
		// Errors are diagnostics, not lifecycle boundaries. Correlated
		// turn_done/aborted events settle admitted work; promptDoneMsg handles the
		// only no-turn case (optimistic admission/preflight failure).
		m.sawPlanThisTurn = false
		m.completedPlanThisTurn = false
		m.planPrompt = false
		message := strings.TrimSpace(ev.Message)
		for _, prefix := range []string{"agent: provider stream: ", "agent: provider chat: ", "agent: provider resolve: ", "agent: "} {
			message = strings.TrimPrefix(message, prefix)
		}
		if message != "" && message != m.lastErrorText {
			m.lastErrorText = message
			m.finishAssistant()
			m.pushLine(styleError.Render("✖ " + message))
		}
	case protocol.EvTurnDone:
		if !m.turnUsageSeen {
			m.contextRefreshVersion++
			m.contextRefreshNeeded = true
		}
		m.turnUsageSeen = false
		m.clearUserInput()
		m.toolRunning = false
		if ev.Usage != nil {
			m.lastUsage = ev.Usage.Clone()
			if os.Getenv("SNOW_DEBUG") != "" {
				line := fmt.Sprintf("turn usage %d input · %d output · %d cached · %d total",
					ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheRead, ev.Usage.Total)
				if ev.Usage.Cost != nil {
					line += fmt.Sprintf(" · %s %.6f", ev.Usage.Cost.Currency, ev.Usage.Cost.Total)
				}
				m.pushLine(styleFooter.Render(line))
			}
		}
		m.busy = ev.GoalContinuing
		m.activeTurnID = ""
		m.lastErrorText = ""
		if !ev.GoalContinuing {
			m.setRunIdle()
		} else if m.runStartedAt.IsZero() {
			m.runStartedAt = m.currentTime()
		}
		m.finishAssistant()
		m.finalizePlan()
		if m.completedPlanThisTurn && strings.TrimSpace(m.latestPlan) != "" && m.app != nil && m.app.Agent.Mode() == protocol.ModePlan {
			m.planPrompt = true
			m.planPromptChoice = 0
		}
		m.sawPlanThisTurn = false
		m.completedPlanThisTurn = false
		if m.pendingMode != nil && !m.modeSwitching {
			m.modeSwitchReady = true
		}
		m.refreshTranscript()
	case protocol.EvAborted:
		if !m.turnUsageSeen {
			m.contextRefreshVersion++
			m.contextRefreshNeeded = true
		}
		m.turnUsageSeen = false
		m.sawPlanThisTurn = false
		m.completedPlanThisTurn = false
		m.planPrompt = false
		m.clearUserInput()
		m.toolRunning = false
		m.permPending = false
		m.permRequest = nil
		m.permAgent = nil
		m.setRunIdle()
		m.finishAssistant()
		m.pushLine(styleError.Render("aborted"))
		m.refreshTranscript()
	case protocol.EvModelChanged:
		if ev.Model != nil {
			m.app.Model = *ev.Model
			if ev.Model.Provider != "" {
				m.app.ProviderID = ev.Model.Provider
			}
		}
	}
}

// finalizeThinking promotes a completed reasoning block to a permanent
// transcript line. It runs when the model starts emitting answer text or a
// tool call (thinking always precedes those), and again at turn end for
// thinking-only turns.
func (m *Model) appendTranscriptLine(line string) {
	m.lines = append(m.lines, line)
	if m.inlineTranscript && m.inlineHistoryKey != "" {
		m.inlineCanonicalLines = append(m.inlineCanonicalLines, line)
	}
}

func (m *Model) finalizeThinking() {
	if m.thinkingBuf.Len() == 0 {
		return
	}
	m.appendTranscriptLine(m.renderThinkingBody(m.thinkingBuf.String()))
	m.transcriptBaseDirty = true
	m.thinkingBuf.Reset()
	m.refreshTranscript()
}

func (m *Model) finishAssistant() {
	m.finalizeThinking()
	m.finalizeAssistant()
	m.finalizePlan()
}

// finalizeAssistant promotes the current answer segment to the durable
// transcript. Tool, permission, error, and abort events call this before
// appending their own lines so the visible transcript remains chronological.
func (m *Model) finalizeAssistant() {
	if m.assistantBuf.Len() > 0 {
		m.appendTranscriptLine(m.renderAssistantBody(m.assistantBuf.String()))
		m.transcriptBaseDirty = true
		m.assistantBuf.Reset()
		m.refreshTranscript()
	}
}

// renderAssistantBody renders the assistant response without a role label.
// The user prompt already has the blue prompt marker; the response should
// read as a clean continuation, like the pi transcript.
func (m *Model) renderAssistantBody(text string) string {
	width := m.transcript.Width - 4
	body := strings.TrimSpace(text)
	if m.md != nil && looksLikeMarkdown(body) {
		body = strings.TrimSpace(m.md.render(body, width))
	}
	return styleAssistant.Render(body)
}

func (m *Model) finalizePlan() {
	if m.planBuf.Len() == 0 {
		return
	}
	text := m.planBuf.String()
	m.latestPlan = text
	m.appendTranscriptLine(m.renderPlanBody(text))
	m.transcriptBaseDirty = true
	m.planBuf.Reset()
	m.currentPlanID = ""
	m.refreshTranscript()
}

func (m *Model) renderPlanBody(text string) string {
	body := strings.TrimSpace(text)
	width := m.transcript.Width - 4
	if m.md != nil {
		body = strings.TrimSpace(m.md.render(body, width))
	}
	return styleHeader.Render("Plan") + "\n" + body
}

func (m *Model) renderPlanUpdate(update protocol.PlanUpdate) string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("Plan update"))
	if strings.TrimSpace(update.Explanation) != "" {
		b.WriteString("\n" + styleHeaderDim.Render(strings.TrimSpace(update.Explanation)))
	}
	for _, step := range update.Plan {
		mark := "○"
		if step.Status == protocol.PlanStepCompleted {
			mark = "✓"
		} else if step.Status == protocol.PlanStepInProgress {
			mark = "→"
		}
		b.WriteString("\n" + mark + " " + step.Step)
	}
	return b.String()
}

func (m *Model) renderThinkingBody(text string) string {
	body := strings.TrimSpace(text)
	if body == "" {
		return ""
	}
	label := "think: "
	width := max(10, m.transcript.Width-lipgloss.Width(label)-4)
	if m.thinkingMD != nil {
		body = strings.TrimSpace(m.thinkingMD.render(body, width))
	} else {
		body = styleThinking.Render(body)
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 1 {
		return styleThinking.Render(label) + lines[0]
	}
	indent := strings.Repeat(" ", lipgloss.Width(label))
	return styleThinking.Render(label) + lines[0] + "\n" + indent + strings.Join(lines[1:], "\n"+indent)
}

func looksLikeMarkdown(text string) bool {
	for _, marker := range []string{"# ", "## ", "- ", "* ", "```", "`", "[", "> "} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (m *Model) pushLine(s string) {
	m.appendTranscriptLine(s)
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	if m.batchingEvents {
		m.refreshTranscript()
		return
	}
	m.flushTranscriptImmediately()
}

// liveText renders the streaming (unfinished) tail: the in-progress thinking
// block and/or the current assistant answer segment. Visible lifecycle events
// finalize this tail before appending their own durable transcript lines.
func (m *Model) liveText() string {
	var b strings.Builder
	if m.thinkingBuf.Len() > 0 {
		// Streaming deltas stay cheap; finalized thinking is rendered as Markdown
		// once in finalizeThinking, matching the assistant streaming path.
		b.WriteString(styleThinking.Render(m.thinkingBuf.String()))
	} else if m.showThinkingPlaceholder() {
		b.WriteString(styleThinking.Render(m.spinner.View() + " thinking…"))
	}
	if m.assistantBuf.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		// Keep streaming text cheap. Markdown is rendered once when finalized.
		b.WriteString(styleAssistant.Render(m.assistantBuf.String()))
	}
	if m.planBuf.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleHeader.Render("Plan") + "\n" + styleAssistant.Render(m.planBuf.String()))
	}
	return b.String()
}

func (m *Model) showThinkingPlaceholder() bool {
	return m.busy && !m.toolRunning && !m.compacting && !m.permPending && !m.userInputPending &&
		m.lastErrorText == "" && m.thinkingBuf.Len() == 0 && m.assistantBuf.Len() == 0
}

func (m *Model) refreshTranscript() {
	m.refreshTranscriptWithForce(false)
}

func (m *Model) refreshTranscriptForced() {
	m.refreshTranscriptWithForce(true)
}

func (m *Model) refreshTranscriptWithForce(force bool) {
	if m.batchingEvents {
		m.transcriptDirty = true
		return
	}
	width := m.transcript.Width
	// Selection coordinates belong to an immutable wrapped snapshot. Keep live
	// stream deltas off-screen until release so a response cannot cancel a drag
	// or move the text beneath the pointer.
	if m.transcriptSelection.pressActive && m.transcriptContent != "" {
		m.transcriptDirty = true
		return
	}
	// Freeze the current snapshot while the user reads away from the tail.
	// State and source buffers continue to update; reaching the snapshot bottom
	// or resizing performs one complete catch-up render.
	if !force && m.transcriptContent != "" && !m.transcript.AtBottom() && m.transcriptBaseWidth == width {
		m.transcriptDirty = true
		return
	}
	if m.transcriptBaseDirty || m.transcriptBaseWidth != width {
		stableLines := m.lines
		if m.inlineTranscript {
			stableLines = m.lines[m.inlineDisplayStart():]
		}
		base := strings.Join(stableLines, "\n")
		if width > 0 {
			// Viewport content is not wrapped automatically. Reflow only the
			// stable transcript base when lines or terminal width change.
			base = lipgloss.NewStyle().Width(width).Render(base)
		}
		m.transcriptBase = base
		m.transcriptBaseWidth = width
		m.transcriptBaseDirty = false
	}
	content := m.transcriptBase
	if live := m.liveText(); live != "" {
		if content != "" {
			content += "\n"
		}
		if width > 0 {
			live = lipgloss.NewStyle().Width(width).Render(live)
		}
		content += live
	}
	if !m.transcriptDirty && content == m.transcriptContent {
		return
	}
	// Selection points refer to the current wrapped transcript snapshot. A live
	// stream boundary or width-dependent reflow can replace those rows, so clear
	// selection before publishing a different source rather than copying text
	// that no longer matches the highlighted cells.
	if content != m.transcriptContent {
		m.clearTranscriptSelection()
		m.transcriptSelectionLines = splitTranscriptSelectionLines(content)
	}
	wasAtBottom := m.transcript.AtBottom()
	m.transcript.SetContent(content)
	// Follow new output only when the user was already following the tail.
	// Preserve an intentional scroll position while a stream continues.
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
	m.transcriptContent = content
	m.transcriptDirty = false
}

const (
	fixedChromeHeight       = 4 // header, two separators, and footer
	inlineFixedChromeHeight = 4 // sticky header, two separators, and footer
	inlineOverlayMaxHeight  = 10
	minComposerHeight       = 3
	maxComposerHeight       = 6
	minTranscriptHeight     = 1
	minFullFrameHeight      = fixedChromeHeight + minComposerHeight + minTranscriptHeight
)

// desiredComposerHeight grows with explicit and soft-wrapped input while
// keeping the idle composer comfortably usable. The textarea remains internally
// scrollable after reaching maxComposerHeight.
func (m *Model) desiredComposerHeight() int {
	attachmentRows := min(2, len(m.promptImages))
	if m.loginMode || m.loginEndpointMode || m.editor.Value() == "" {
		return min(maxComposerHeight, minComposerHeight+attachmentRows)
	}
	width := m.editor.Width()
	if width < 1 {
		width = max(1, m.width-4)
	}
	wrapped := xansi.Wordwrap(m.editor.Value(), width, "")
	wrapped = xansi.Hardwrap(wrapped, width, true)
	return min(maxComposerHeight, max(minComposerHeight, lipgloss.Height(wrapped))+attachmentRows)
}

func (m *Model) currentTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Model) showRunStatus() bool {
	return m.busy && !m.compacting && !m.permPending && !m.userInputPending && !m.runStartedAt.IsZero()
}

func (m *Model) runStatusHeight() int {
	if m.showRunStatus() {
		return 1
	}
	return 0
}

func formatRunElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (m *Model) renderRunStatus() string {
	if !m.showRunStatus() {
		return ""
	}
	elapsed := formatRunElapsed(m.currentTime().Sub(m.runStartedAt))
	detail := elapsed
	if pending := len(m.pendingInputs.Items); pending > 0 {
		detail += fmt.Sprintf(" · %d queued", pending)
	}
	detail += " · esc to interrupt"
	return styleThinking.Render(m.spinner.View()+" ") +
		styleHeader.Render("Working") +
		styleHeaderDim.Render(" ("+detail+")")
}

func (m *Model) managedFrameHeight() int {
	// The normal-screen renderer still owns one terminal-height live frame. Its
	// transcript viewport absorbs unused rows so the composer/footer remain
	// anchored at the bottom while finalized rows print above into native
	// scrollback. A short fixed frame would leave the composer stranded near the
	// last response and can expose stale separator rows when its geometry changes.
	return m.height
}

func (m *Model) managedFrameWidth() int {
	// Leave the terminal's final cell unused. Writing through the right margin
	// can trigger physical autowrap before Bubble Tea's logical newline, causing
	// stale rows or apparent flicker when frame geometry changes.
	return max(1, m.width-1)
}

// inlineModalOverlay reports pickers that can temporarily own the entire fixed
// inline frame. Replacing the transcript/composer tail keeps the renderer at a
// constant height (so native scrollback is untouched) while giving modal lists
// enough rows to show more than their selected item.
func (m *Model) inlineModalOverlay() bool {
	return m.inlineTranscript && (m.pickProvider || m.pickChatGPTAuth || m.pickModel ||
		m.pickThinking || m.pickSettings || m.pickSession || m.pickTree ||
		m.pickInfo || m.pickPermissionMode || m.permPending || m.userInputPending ||
		m.confirmGoalReplace || m.planPrompt)
}

// Inline completion lists still need the composer for live filtering. They own
// the rest of the same fixed frame while visible, hiding transcript/footer rows
// instead of growing the normal-screen renderer.
func (m *Model) inlineInputOverlay() bool {
	return m.inlineTranscript && (m.compVisible || m.skillVisible || m.mentionVisible || m.mentionLoading)
}

// availableOverlayHeight is the maximum picker/palette area that leaves one
// transcript row visible inside the fixed managed frame.
func (m *Model) availableOverlayHeight() int {
	if m.inlineModalOverlay() {
		return min(m.managedFrameHeight(), inlineOverlayMaxHeight)
	}
	if m.inlineInputOverlay() {
		return max(1, min(inlineOverlayMaxHeight, m.managedFrameHeight()-1-m.editor.Height())) // separator + composer
	}
	return max(0, m.managedFrameHeight()-m.fixedChromeRows()-m.editor.Height()-m.runStatusHeight()-minTranscriptHeight)
}

// chromeHeight returns the exact rows outside the transcript viewport.
func (m *Model) fixedChromeRows() int {
	if m.inlineTranscript {
		return inlineFixedChromeHeight
	}
	return fixedChromeHeight
}

func (m *Model) chromeHeight() int {
	overlayHeight := 0
	if overlay := m.renderOverlays(); overlay != "" {
		overlayHeight = lipgloss.Height(overlay)
	}
	return m.fixedChromeRows() + m.editor.Height() + m.runStatusHeight() + overlayHeight
}

func (m *Model) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// Dynamic chrome (especially the one-line run status) changes viewport
	// height at prompt start/end. Preserve tail-following across that resize;
	// otherwise shrinking a bottomed viewport makes AtBottom false and freezes
	// reasoning/tool events until the run-status row disappears at turn_done.
	wasAtBottom := m.transcript.AtBottom()
	frameWidth := m.managedFrameWidth()
	m.editor.SetWidth(max(1, frameWidth-4))
	m.userInputEditor.SetWidth(max(1, frameWidth-6))
	frameHeight := m.managedFrameHeight()
	maxEditorHeight := max(minComposerHeight, frameHeight-m.fixedChromeRows()-m.runStatusHeight()-minTranscriptHeight)
	editorH := min(m.desiredComposerHeight(), min(maxComposerHeight, maxEditorHeight))
	m.editor.SetHeight(editorH)
	bodyH := max(minTranscriptHeight, frameHeight-m.chromeHeight())
	if m.transcript.Width != frameWidth || m.transcript.Height != bodyH {
		m.transcriptDirty = true
	}
	m.transcript.Width = frameWidth
	m.transcript.Height = bodyH
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
}

func (m *Model) quitCmd() tea.Cmd {
	if m.inlineTranscript {
		m.inlineExiting = true
	}
	return tea.Quit
}

// handleKey processes key presses, the command palette, and login capture.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.trustPending {
		// Once persistence starts, keep ownership of the async result so a late
		// app cannot be constructed after Bubble Tea has already exited. Before
		// selection, Ctrl+C/Ctrl+D still quit without recording a decision.
		if m.trustSaving {
			return m, nil
		}
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD {
			return m, m.quitCmd()
		}
		switch msg.Type {
		case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
			m.trustChoice = (m.trustChoice + 1) % 2
			m.trustError = ""
		case tea.KeyDown, tea.KeyRight, tea.KeyTab:
			m.trustChoice = (m.trustChoice + 1) % 2
			m.trustError = ""
		case tea.KeyEsc:
			m.trustChoice = 0
			m.trustSaving = true
			m.trustError = ""
			return m, m.saveTrustCmd(trust.LevelDeny)
		case tea.KeyEnter:
			level := trust.LevelDeny
			if m.trustChoice == 1 {
				level = trust.LevelAllow
			}
			m.trustSaving = true
			m.trustError = ""
			return m, m.saveTrustCmd(level)
		}
		return m, nil
	}

	// Startup can fail before the app exists. Keep emergency exit keys live
	// while booting and on the terminal error screen so the alt-screen can
	// always be restored cleanly.
	if m.app == nil {
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, m.quitCmd()
		case tea.KeyCtrlD:
			if m.editor.Value() == "" {
				return m, m.quitCmd()
			}
		}
		return m, nil
	}

	// F6 toggles application mouse reporting without restarting. Native terminal
	// selection/context menus require reporting to be disabled; application mode
	// adds wheel scrolling, transcript drag-copy, and edge auto-scroll.
	if msg.Type == tea.KeyF6 {
		if m.app.Cfg.TUI.Mouse {
			m.clearTranscriptSelection()
			m.catchUpTranscriptAfterSelection()
			m.app.Cfg.TUI.Mouse = false
			m.lastStatus = "native selection + context menu · keyboard viewport scrolling"
			return m, tea.DisableMouse
		}
		m.app.Cfg.TUI.Mouse = true
		m.lastStatus = "app mouse · wheel scroll + drag copy enabled"
		return m, tea.EnableMouseCellMotion
	}

	// Emergency Ctrl+C is resolved before any configurable action so a custom
	// submit/accept binding can never shadow terminal recovery.
	if msg.Type == tea.KeyCtrlC {
		if m.busy {
			m.requestAbort()
			return m, nil
		}
		return m, m.quitCmd()
	}

	// Session creation/opening runs asynchronously in production. Keep that
	// transition modal so no prompt or command can be admitted against the old
	// (or startup placeholder) store before the new store is installed.
	if m.sessionOpLoading {
		m.lastStatus = "opening session…"
		return m, nil
	}

	// Host interaction requests preempt ordinary pickers. They may arrive from
	// an independent child while another overlay is open; keeping them first
	// guarantees the visible blocking request also owns the keyboard.
	if m.permPending {
		return m.handlePermissionPick(msg)
	}
	if m.userInputPending {
		return m.handleUserInputKey(msg)
	}

	if m.subagentFleetOpen {
		return m.handleSubagentFleetKey(msg)
	}

	if m.confirmGoalReplace {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyEsc:
			m.confirmGoalReplace = false
			return m, nil
		case tea.KeyEnter:
			objective, budget := m.pendingGoalObjective, m.pendingGoalBudget
			m.confirmGoalReplace = false
			g, err := m.app.CreateGoal(objective, budget, true)
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
			return m, nil
		}
		return m, nil
	}
	if m.planPrompt {
		return m.handlePlanImplementationKey(normalizePickerKeyWithMap(msg, m.keys))
	}

	// Bubble Tea recognizes Option+Return when the terminal sends ESC+CR in
	// one read. Some macOS terminals split those bytes into two events. Join
	// that split form back into Alt+Enter before normal Enter submission.
	if m.metaEnterPending {
		m.metaEnterPending = false
		if msg.Type == tea.KeyEnter && !m.busy {
			msg.Alt = true
		}
	}

	// --- OpenAI-compatible endpoint capture mode ---
	if m.loginEndpointMode {
		if keyMatches(msg, m.keys.Close) {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else if keyMatches(msg, m.keys.Accept) {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		} else if keyMatches(msg, m.keys.Paste) {
			msg = tea.KeyMsg{Type: tea.KeyCtrlV}
		}
		return m.handleLoginEndpointKey(msg)
	}

	// --- Masked login capture mode ---
	if m.loginMode {
		if keyMatches(msg, m.keys.Close) {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else if keyMatches(msg, m.keys.Accept) {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		} else if keyMatches(msg, m.keys.Paste) {
			msg = tea.KeyMsg{Type: tea.KeyCtrlV}
		}
		return m.handleLoginKey(msg)
	}

	// --- Provider picker (for /login) ---
	if m.pickProvider {
		return m.handleProviderPick(msg)
	}

	// --- ChatGPT auth-source picker ---
	if m.pickChatGPTAuth {
		return m.handleChatGPTAuthPick(msg)
	}

	// --- Model picker (for /model) ---
	if m.pickModel {
		return m.handleModelPick(msg)
	}

	// --- Unified settings panel ---
	if m.pickSettings {
		return m.handleSettingsKey(msg)
	}

	// --- Thinking picker (for /thinking) ---
	if m.pickThinking {
		return m.handleThinkingPick(msg)
	}

	// --- Session picker ---
	if m.pickSession {
		return m.handleSessionPick(msg)
	}

	// --- Branch tree picker ---
	if m.pickTree {
		return m.handleTreePick(msg)
	}

	// --- Read-only MCP/skills status picker ---
	if m.pickInfo {
		return m.handleInfoPick(msg)
	}

	// --- Permission mode picker (/permissions) ---
	if m.pickPermissionMode {
		return m.handlePermissionModePick(msg)
	}

	// Escape interrupts an active agent run. Modal Escape behavior above keeps
	// its existing meaning (cancel picker/login or deny a permission request).
	if msg.Type == tea.KeyEsc && m.busy && !m.compacting && !m.runStartedAt.IsZero() {
		m.requestAbort()
		return m, nil
	}

	// --- Command palette: navigation keys are consumed while open ---
	if m.compVisible {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyUp:
			if len(m.compMatches) > 0 {
				m.compIndex = (m.compIndex - 1 + len(m.compMatches)) % len(m.compMatches)
			}
			return m, nil
		case tea.KeyDown:
			if len(m.compMatches) > 0 {
				m.compIndex = (m.compIndex + 1) % len(m.compMatches)
			}
			return m, nil
		case tea.KeyTab:
			if len(m.compMatches) > 0 {
				return m.insertCompletion(m.compMatches[m.compIndex])
			}
			return m, nil
		case tea.KeyShiftTab:
			if len(m.compMatches) > 0 {
				m.compIndex = (m.compIndex - 1 + len(m.compMatches)) % len(m.compMatches)
			}
			return m, nil
		case tea.KeyEsc:
			m.compVisible = false
			return m, nil
		case tea.KeyEnter:
			if len(m.compMatches) == 0 {
				m.compVisible = false
				return m, nil
			}
			return m.pickCompletion(m.compMatches[m.compIndex])
		}
	}

	// --- Agent Skill picker: Enter/Tab insert a leading directive. ---
	if m.skillVisible {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyUp, tea.KeyShiftTab:
			if len(m.skillMatches) > 0 {
				m.skillIndex = (m.skillIndex - 1 + len(m.skillMatches)) % len(m.skillMatches)
			}
			return m, nil
		case tea.KeyDown:
			if len(m.skillMatches) > 0 {
				m.skillIndex = (m.skillIndex + 1) % len(m.skillMatches)
			}
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if len(m.skillMatches) > 0 {
				return m.insertSkillCompletion(m.skillMatches[m.skillIndex].Name)
			}
		case tea.KeyEsc:
			m.skillVisible = false
			return m, nil
		}
	}

	// --- File mention picker: Enter/Tab insert a path, never submit the prompt ---
	if m.mentionVisible {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		switch msg.Type {
		case tea.KeyUp, tea.KeyShiftTab:
			if len(m.mentionMatches) > 0 {
				m.mentionIndex = (m.mentionIndex - 1 + len(m.mentionMatches)) % len(m.mentionMatches)
			}
			return m, nil
		case tea.KeyDown:
			if len(m.mentionMatches) > 0 {
				m.mentionIndex = (m.mentionIndex + 1) % len(m.mentionMatches)
			}
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if len(m.mentionMatches) > 0 {
				return m.insertMention(m.mentionMatches[m.mentionIndex])
			}
		case tea.KeyEsc:
			m.mentionVisible = false
			return m, nil
		}
	}

	// At the top-level composer Shift+Tab cycles collaboration mode. Every
	// modal/completion path above retains its existing navigation semantics.
	if keyMatches(msg, m.keys.Mode) {
		return m, m.toggleCollaborationMode()
	}

	if !m.busy && len(m.promptImages) > 0 && strings.TrimSpace(m.editor.Value()) == "" &&
		(msg.Type == tea.KeyBackspace || msg.Type == tea.KeyEsc) {
		m.promptImages = m.promptImages[:len(m.promptImages)-1]
		m.lastStatus = "removed pasted image"
		m.layout()
		return m, nil
	}

	// Preserve a standalone Escape briefly as a possible macOS Option/Meta
	// prefix. Modal Escape and active-run interruption have already returned
	// above, so this applies only to the idle composer. Replayed terminal input
	// has already passed through the fragment timeout and must not be held again.
	if msg.Type == tea.KeyEsc && !m.busy && !m.replayingInput {
		m.metaEnterSeq++
		seq := m.metaEnterSeq
		m.metaEnterPending = true
		return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
			return clearMetaEnterMsg(seq)
		})
	}

	// --- Normal editing / sending ---
	text := m.editor.Value()
	submitKey := keyMatches(msg, m.keys.Submit)
	followUpKey := keyMatches(msg, m.keys.FollowUp)
	if submitKey && m.modeSwitching {
		m.lastStatus = "waiting for mode switch"
		return m, nil
	}
	trimmed := strings.TrimSpace(text)
	if submitKey && m.compatibleLoginPending && m.app != nil && m.app.ProviderID == openaicompat.ProviderID && trimmed != "" && !strings.HasPrefix(trimmed, "/") {
		m.lastStatus = "waiting for openai-compatible model discovery"
		return m, nil
	}
	goalControl := m.busy && (strings.HasPrefix(trimmed, "/goal pause") || strings.HasPrefix(trimmed, "/goal clear") || strings.HasPrefix(trimmed, "/goal edit"))
	if keyMatches(msg, m.keys.Abort) && m.busy {
		m.requestAbort()
		return m, nil
	}
	if (followUpKey || submitKey && m.busy && !goalControl) && len(m.promptImages) > 0 {
		m.lastErrorText = "image attachments cannot be queued; wait for the current turn"
		m.lastStatus = m.lastErrorText
		return m, nil
	}
	if followUpKey && m.busy && trimmed != "" && !strings.HasPrefix(trimmed, "/") {
		return m, m.submitQueuedInput(text, protocol.QueuedInputFollowUp)
	}
	if submitKey && m.busy && !goalControl && trimmed != "" && !strings.HasPrefix(trimmed, "/") {
		return m, m.submitQueuedInput(text, protocol.QueuedInputSteer)
	}
	if submitKey && (!m.busy || goalControl) {
		if strings.HasPrefix(text, "/") && len(m.promptImages) == 0 {
			return m.runCommand(trimmed)
		}
		if trimmed != "" || len(m.promptImages) > 0 {
			display := text
			if display == "" {
				display = fmt.Sprintf("[%d image(s)]", len(m.promptImages))
			} else if len(m.promptImages) > 0 {
				display += fmt.Sprintf(" [%d image(s)]", len(m.promptImages))
			}
			m.pushLine(styleUser.Render("› " + display))
			m.imagePasteGeneration++
			m.editor.Reset()
			return m, m.startPrompt(text)
		}
	}

	if keyMatches(msg, m.keys.Quit) && !m.busy && (msg.String() != "ctrl+d" || m.editor.Value() == "") {
		return m, m.quitCmd()
	}

	// Forward to the editor, then refresh the palette from the new text. Keep
	// the returned command: textarea uses it to read the clipboard for paste.
	if keyMatches(msg, m.keys.Paste) {
		msg = tea.KeyMsg{Type: tea.KeyCtrlV}
	}
	prev := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if msg.Type == tea.KeyEsc {
		m.compVisible = false
		return m, nil
	}
	var mentionCmd tea.Cmd
	if m.editor.Value() != prev {
		mentionCmd = m.refreshInputCompletions()
	}
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
			return m, tea.Batch(routeTextareaCmd(textareaTargetComposer, "", "", cmd), mentionCmd)
		}
		m.imagePasteGeneration++
		generation := m.imagePasteGeneration
		imageCmd := m.imagePasteCmdOverride
		if imageCmd == nil {
			imageCmd = func() tea.Msg {
				block, err := readClipboardImageFunc()
				return clipboardImageMsg{generation: generation, block: block, err: err}
			}
		}
		return m, tea.Batch(imageCmd, mentionCmd)
	}
	return m, mentionCmd
}

// pickCompletion selects a palette entry: commands needing args are inserted
// into the editor for completion; argument-free commands run immediately.
func (m *Model) insertCompletion(name string) (tea.Model, tea.Cmd) {
	suffix := ""
	if spec, ok := commandByExact(name); ok && spec.needsArgs() {
		suffix = " "
	}
	m.editor.SetValue(name + suffix)
	m.editor.CursorEnd()
	m.compVisible = false
	m.refreshPalette()
	return m, nil
}

func (m *Model) pickCompletion(name string) (tea.Model, tea.Cmd) {
	m.compVisible = false
	if spec, ok := commandByExact(name); ok && spec.needsArgs() {
		m.editor.SetValue(name + " ")
		m.editor.CursorEnd()
		m.refreshPalette()
		return m, nil
	}
	return m.runCommand(name)
}

// refreshPalette recomputes completion candidates from the editor's first
// token, opening or closing the palette accordingly.
func (m *Model) refreshPalette() {
	text := m.editor.Value()
	if isCommandPrefix(text) {
		m.compMatches = completeCommand(text[1:])
		if len(m.compMatches) > 10 {
			m.compMatches = m.compMatches[:10]
		}
		m.compVisible = true
		if m.compIndex >= len(m.compMatches) {
			m.compIndex = 0
		}
	} else {
		m.compVisible = false
		m.compMatches = nil
		m.compIndex = 0
	}
}

// refreshInputCompletions keeps slash commands, leading $skill directives,
// and @ file references mutually exclusive while the editor changes.
func (m *Model) refreshInputCompletions() tea.Cmd {
	m.refreshPalette()
	m.refreshSkillCompletions()
	if m.skillVisible {
		m.mentionVisible = false
		return nil
	}
	return m.refreshMentions()
}

// refreshMentions never walks the repository from Bubble Tea's Update loop.
// The first @ query schedules a bounded discovery command; subsequent edits
// only match the cached list. Generation checks in mentionFilesMsg prevent a
// slow result from reopening a picker for an obsolete editor state.
func (m *Model) refreshMentions() tea.Cmd {
	m.mentionVisible = false
	m.mentionMatches = nil
	m.mentionIndex = 0
	if m.app == nil {
		return nil
	}
	query, _, ok := mentionQuery(m.editor.Value())
	if !ok {
		return nil
	}
	cwd := m.app.CWD()
	if !m.mentionFilesLoaded || m.mentionFilesCWD != cwd {
		if m.mentionLoading {
			return nil
		}
		m.mentionLoading = true
		m.mentionGeneration++
		generation := m.mentionGeneration
		return func() tea.Msg {
			return mentionFilesMsg{
				cwd: cwd, generation: generation,
				files: discoverMentionFiles(cwd),
			}
		}
	}
	m.mentionMatches = matchMentionFiles(m.mentionFiles, query)
	m.mentionVisible = len(m.mentionMatches) > 0
	return nil
}

func (m *Model) insertMention(path string) (tea.Model, tea.Cmd) {
	text := m.editor.Value()
	_, start, ok := mentionQuery(text)
	if !ok {
		return m, nil
	}
	m.editor.SetValue(replaceMentionToken(text, start, path))
	m.editor.CursorEnd()
	m.mentionVisible = false
	m.mentionMatches = nil
	m.refreshInputCompletions()
	return m, nil
}

func (m *Model) handleLoginEndpointKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.loginEndpointMode = false
		m.loginEndpoint = ""
		m.loginProvider = ""
		m.editor.Reset()
		m.editor.Placeholder = "Type a message…"
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	case tea.KeyEnter:
		endpoint := strings.TrimSpace(m.editor.Value())
		compatible, err := openaicompat.New(openaicompat.Config{BaseURL: endpoint})
		if err != nil || !compatible.Configured() {
			if err == nil {
				err = errors.New("endpoint is required")
			}
			m.pushLine(styleError.Render("login: invalid openai-compatible endpoint: " + err.Error()))
			return m, nil
		}
		m.loginEndpoint = endpoint
		m.loginEndpointMode = false
		m.editor.Reset()
		m.editor.Placeholder = "Type a message…"
		m.beginKeyCapture(openaicompat.ProviderID)
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if msg.Type == tea.KeyCtrlV {
		m.editor.Err = nil
		if m.pasteCmdOverride != nil {
			cmd = m.pasteCmdOverride
		}
		return m, routeTextareaCmd(textareaTargetComposer, "", "", cmd)
	}
	return m, cmd
}

// handleLoginKey captures a masked API key.
func (m *Model) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.loginMode = false
		m.loginEndpoint = ""
		m.loginProvider = ""
		m.secretBuf.Reset()
		m.editor.Reset()
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	case tea.KeyEnter:
		secret := m.secretBuf.String()
		m.loginMode = false
		m.secretBuf.Reset()
		m.editor.Reset()
		if m.loginProvider == openaicompat.ProviderID {
			return m.finishCompatibleLogin(secret)
		}
		if strings.TrimSpace(secret) == "" {
			m.pushLine(styleError.Render("login: empty API key"))
			return m, nil
		}
		cred := auth.Credential{Type: auth.CredentialAPIKey, Key: secret}
		if err := m.app.Auth.Put(m.loginProvider, cred); err != nil {
			m.pushLine(styleError.Render("login: " + err.Error()))
			return m, nil
		}
		m.pushLine(styleFooter.Render("stored API key for " + m.loginProvider + " (0600)"))
		return m, nil
	case tea.KeyBackspace:
		b := m.secretBuf.String()
		if len(b) > 0 {
			m.secretBuf.Reset()
			m.secretBuf.WriteString(b[:len(b)-1])
		}
		return m, nil
	case tea.KeyCtrlC:
		m.loginMode = false
		m.loginEndpoint = ""
		m.loginProvider = ""
		m.secretBuf.Reset()
		m.editor.Reset()
		m.pushLine(styleFooter.Render("login cancelled"))
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.secretBuf.WriteString(string(msg.Runes))
	} else if msg.Type == tea.KeySpace {
		m.secretBuf.WriteString(" ")
	}
	return m, nil
}

func (m *Model) finishCompatibleLogin(secret string) (tea.Model, tea.Cmd) {
	endpoint := strings.TrimSpace(m.loginEndpoint)
	m.loginEndpoint = ""
	m.loginProvider = ""
	if endpoint == "" {
		m.pushLine(styleError.Render("login: openai-compatible endpoint is required"))
		return m, nil
	}

	oldPersisted := m.app.PersistedCfg
	candidate := oldPersisted
	candidate.Providers = make(map[string]config.ProviderConfig, len(oldPersisted.Providers)+1)
	for id, providerConfig := range oldPersisted.Providers {
		candidate.Providers[id] = providerConfig
	}
	providerConfig := candidate.Providers[openaicompat.ProviderID]
	providerConfig.BaseURL = endpoint
	candidate.Providers[openaicompat.ProviderID] = providerConfig

	oldCred, hadOldCred := m.app.Auth.Get(openaicompat.ProviderID)
	credentialChanged := strings.TrimSpace(secret) != ""
	if credentialChanged {
		if err := m.app.Auth.Put(openaicompat.ProviderID, auth.Credential{Type: auth.CredentialAPIKey, Key: secret}); err != nil {
			m.pushLine(styleError.Render("login: " + err.Error()))
			return m, nil
		}
	}
	rollbackCredential := func() error {
		if !credentialChanged {
			return nil
		}
		if hadOldCred {
			return m.app.Auth.Put(openaicompat.ProviderID, oldCred)
		}
		return m.app.Auth.Delete(openaicompat.ProviderID)
	}
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			err = errors.Join(err, rollbackCredential())
			m.pushLine(styleError.Render("login: persist endpoint: " + err.Error()))
			return m, nil
		}
	}

	m.app.PersistedCfg = candidate
	m.app.Cfg.Providers = make(map[string]config.ProviderConfig, len(candidate.Providers))
	for id, configured := range candidate.Providers {
		m.app.Cfg.Providers[id] = configured
	}
	if err := m.app.ConfigureOpenAICompatible(endpoint); err != nil {
		m.app.PersistedCfg = oldPersisted
		m.app.Cfg.Providers = make(map[string]config.ProviderConfig, len(oldPersisted.Providers))
		for id, configured := range oldPersisted.Providers {
			m.app.Cfg.Providers[id] = configured
		}
		rollbackErr := rollbackCredential()
		if m.app.ConfigPath != "" {
			rollbackErr = errors.Join(rollbackErr, config.Save(m.app.ConfigPath, oldPersisted))
		}
		m.pushLine(styleError.Render("login: " + errors.Join(err, rollbackErr).Error()))
		return m, nil
	}

	m.compatibleLoginGeneration++
	generation := m.compatibleLoginGeneration
	m.compatibleLoginPending = true
	m.pushLine(styleFooter.Render("openai-compatible endpoint saved · discovering models…"))
	app := m.app
	ctx := m.ctx
	return m, func() tea.Msg {
		return compatibleLoginDoneMsg{generation: generation, endpoint: endpoint, err: app.RefreshProviderModels(ctx, openaicompat.ProviderID)}
	}
}

func (m *Model) expandedPrompt(text string) string {
	if m.app == nil {
		return text
	}
	return expandMentionPrompt(text, m.app.CWD(), m.mentionFiles)
}

func (m *Model) submitQueuedInput(text string, kind protocol.QueuedInputKind) tea.Cmd {
	expanded := m.expandedPrompt(text)
	epoch := m.queueEpoch
	m.queueAttempts = append(m.queueAttempts, queuedTUIAttempt{kind: kind, text: text, expanded: expanded, epoch: epoch})
	return func() tea.Msg {
		item, err := m.app.Agent.QueueInput(kind, expanded)
		if errors.Is(err, agent.ErrNotRunning) {
			return queueSubmitMsg{kind: kind, text: text, expanded: expanded, epoch: epoch, fallback: true}
		}
		return queueSubmitMsg{kind: kind, text: text, expanded: expanded, itemID: item.ID, epoch: epoch, accepted: err == nil, err: err}
	}
}

func (m *Model) hasQueueAttempt(item protocol.QueuedInput) bool {
	for _, attempt := range m.queueAttempts {
		if attempt.kind == item.Kind && attempt.expanded == item.Text {
			return true
		}
	}
	return false
}

func (m *Model) removeQueueAttempt(msg queueSubmitMsg) {
	for i, attempt := range m.queueAttempts {
		if attempt.epoch != msg.epoch || attempt.kind != msg.kind || attempt.text != msg.text || attempt.expanded != msg.expanded {
			continue
		}
		m.queueAttempts = append(m.queueAttempts[:i], m.queueAttempts[i+1:]...)
		return
	}
}

func (m *Model) waitForQueueSettlement() tea.Cmd {
	if m.queueSettleWaiting || m.app == nil || m.app.Agent == nil {
		return nil
	}
	m.queueSettleWaiting = true
	epoch := m.queueEpoch
	agent := m.app.Agent
	ctx := m.ctx
	return func() tea.Msg {
		return queueSettledMsg{epoch: epoch, err: agent.WaitIdle(ctx)}
	}
}

func (m *Model) startQueueFallback() tea.Cmd {
	if len(m.queueFallbacks) == 0 || m.app == nil {
		return nil
	}
	msg := m.queueFallbacks[0]
	m.queueFallbacks = m.queueFallbacks[1:]
	// The fallback is semantically the steer that narrowly missed admission;
	// defer any already queued collaboration-mode transition until this prompt's
	// own turn_done boundary.
	m.modeSwitchReady = false
	m.beginOptimisticRun()
	m.planPrompt = false
	m.pushLine(styleUser.Render("› " + msg.text))
	if m.editor.Value() == msg.text {
		m.editor.Reset()
	}
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	generation := m.runGeneration
	promptCmd := func() tea.Msg {
		_, beforeID, _ := m.app.Agent.ActiveTurn()
		err := m.app.Agent.Prompt(ctx, msg.expanded)
		_, turnID, running := m.app.Agent.ActiveTurn()
		return promptDoneMsg{generation: generation, turnID: turnID, admitted: running || (turnID != "" && turnID != beforeID), err: err}
	}
	return promptCmd
}

func (m *Model) beginOptimisticRun() uint64 {
	m.runGeneration++
	m.activeTurnID = ""
	m.turnUsageSeen = false
	m.busy = true
	m.toolRunning = false
	m.lastErrorText = ""
	m.runStartedAt = m.currentTime()
	return m.runGeneration
}

func clonePromptImages(images []protocol.ContentBlock) []protocol.ContentBlock {
	cloned := make([]protocol.ContentBlock, len(images))
	for i, image := range images {
		cloned[i] = image
		cloned[i].Data = append([]byte(nil), image.Data...)
	}
	return cloned
}

func (m *Model) takePromptImages() []protocol.ContentBlock {
	images := clonePromptImages(m.promptImages)
	m.promptImages = nil
	return images
}

func (m *Model) startPrompt(text string) tea.Cmd {
	generation := m.beginOptimisticRun()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	prompt := m.expandedPrompt(text)
	images := m.takePromptImages()
	return func() tea.Msg {
		err := m.app.Agent.PromptContent(ctx, prompt, images)
		_, turnID, _ := m.app.Agent.ActiveTurn()
		admitted := err == nil || !errors.Is(err, agent.ErrPromptRejected)
		return promptDoneMsg{generation: generation, turnID: turnID, admitted: admitted, text: text, attachments: images, err: err}
	}
}

func (m *Model) startPromptWithMode(text string, mode protocol.CollaborationMode) tea.Cmd {
	generation := m.beginOptimisticRun()
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	prompt := m.expandedPrompt(text)
	images := m.takePromptImages()
	return func() tea.Msg {
		err := m.app.Agent.PromptContentWithMode(ctx, prompt, images, mode)
		_, turnID, _ := m.app.Agent.ActiveTurn()
		admitted := err == nil || !errors.Is(err, agent.ErrPromptRejected)
		return promptDoneMsg{generation: generation, turnID: turnID, admitted: admitted, text: text, attachments: images, err: err}
	}
}

func (m *Model) startCompact() tea.Cmd {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("compact: wait for the current turn to finish"))
		return nil
	}
	if m.cancelRun != nil {
		m.cancelRun()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRun = cancel
	m.beginOptimisticRun()
	m.compactGeneration++
	generation := m.compactGeneration
	m.compacting = true
	m.compactStatus = "compacting context"
	return func() tea.Msg {
		result, err := m.app.Agent.Compact(ctx)
		return compactDoneMsg{generation: generation, result: result, err: err}
	}
}

func (m *Model) abort() {
	if m.cancelRun != nil {
		m.cancelRun()
	}
	if m.app != nil && m.app.Agent != nil {
		m.app.Agent.Abort()
	}
}

func (m *Model) requestAbort() {
	// Invalidate optimistic command completions before joining the agent. This
	// also covers the goal worker's inter-turn delay, where no EvAborted event
	// exists to release the UI projection.
	m.runGeneration++
	m.compactGeneration++
	// Close queue admission and drain accepted input before cancelling the run.
	// An enqueue racing this key press is therefore either present in the
	// returned snapshot or rejected while its unchanged draft stays visible.
	m.queueEpoch++
	m.queueSettleWaiting = false
	queue := protocol.InputQueue{}
	if m.app != nil && m.app.Agent != nil {
		queue = m.app.Agent.ClearPendingInputs()
	}
	draft := m.editor.Value()
	fallbacks := append([]queueSubmitMsg(nil), m.queueFallbacks...)
	m.queueFallbacks = nil
	m.abort()
	m.pendingInputs = protocol.InputQueue{}
	m.restoreAbortedInputs(queue, fallbacks, draft)
	m.setRunIdle()
	m.pushLine(styleError.Render("aborting…"))
}

func (m *Model) restoreAbortedInputs(queue protocol.InputQueue, fallbacks []queueSubmitMsg, draft string) {
	parts := make([]string, 0, len(queue.Items)+len(fallbacks)+1)
	for _, item := range queue.Items {
		parts = append(parts, m.originalQueuedText(item))
	}
	for _, fallback := range fallbacks {
		parts = append(parts, fallback.text)
	}
	if draft != "" {
		duplicateAcceptedDraft := len(parts) > 0 && parts[len(parts)-1] == draft
		if !duplicateAcceptedDraft {
			parts = append(parts, draft)
		}
	}
	m.queueAttempts = nil
	clear(m.queueOriginalText)
	clear(m.queueRendered)
	if len(parts) > 0 {
		m.editor.SetValue(strings.Join(parts, "\n\n"))
		m.editor.CursorEnd()
		m.refreshInputCompletions()
	}
}

func (m *Model) originalQueuedText(item protocol.QueuedInput) string {
	if original := m.queueOriginalText[item.ID]; original != "" {
		return original
	}
	for _, attempt := range m.queueAttempts {
		if attempt.kind == item.Kind && attempt.expanded == item.Text {
			return attempt.text
		}
	}
	return item.Text
}

// runCommand handles slash commands.
func (m *Model) runCommand(line string) (tea.Model, tea.Cmd) {
	m.editor.Reset()
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/quit", "/q":
		return m, m.quitCmd()
	case "/help":
		mouseHelp := "mouse: native terminal selection · F6 enables wheel + app drag-copy"
		if m.app.Cfg.TUI.Mouse {
			mouseHelp = "mouse: wheel + app drag-copy · F6 restores native terminal selection"
		}
		m.pushLine(styleFooter.Render(formatCommandListWithKeys(m.keys) + "\n(while working: submit queues steer, follow-up uses its configured binding)\n(" + mouseHelp + ")"))
	case "/goal":
		if len(args) == 0 {
			g, err := m.app.GoalState()
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else if g == nil {
				m.pushLine(styleFooter.Render("no thread goal"))
			} else {
				m.goal = g
				m.pushLine(styleFooter.Render(fmt.Sprintf("goal %s · %s · %ds\n%s", g.Status, formatGoalTokenUsage(g), g.SecondsUsed, g.Objective)))
			}
			return m, nil
		}
		sub := args[0]
		switch sub {
		case "pause":
			g, err := m.app.PauseGoal()
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.setRunIdle()
			}
		case "resume":
			g, err := m.app.ResumeGoal()
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
		case "clear":
			if err := m.app.ClearGoal(); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = nil
				m.setRunIdle()
			}
		case "replace":
			objective := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "/goal")), "replace"))
			if objective == "" {
				m.pushLine(styleError.Render("usage: /goal replace <objective>"))
				return m, nil
			}
			g, err := m.app.CreateGoal(objective, nil, true)
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
		case "edit":
			objective := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "/goal")), "edit"))
			if objective == "" {
				if g, _ := m.app.GoalState(); g != nil {
					m.editor.SetValue("/goal edit " + g.Objective)
					m.editor.CursorEnd()
				}
				return m, nil
			}
			g, err := m.app.EditGoal(objective)
			if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.goal = g
				if g != nil && g.Status == protocol.GoalActive {
					m.busy = true
					m.runStartedAt = m.currentTime()
				}
			}
		default:
			objective := strings.TrimSpace(strings.TrimPrefix(line, "/goal"))
			var budget *int64
			if strings.HasPrefix(objective, "--budget ") {
				fields := strings.Fields(objective)
				if len(fields) < 3 {
					m.pushLine(styleError.Render("usage: /goal --budget <tokens> <objective>"))
					return m, nil
				}
				v, e := strconv.ParseInt(fields[1], 10, 64)
				if e != nil || v <= 0 {
					m.pushLine(styleError.Render("goal budget must be positive"))
					return m, nil
				}
				budget = &v
				objective = strings.Join(fields[2:], " ")
			}
			g, err := m.app.CreateGoal(objective, budget, false)
			if err != nil {
				if strings.Contains(err.Error(), "unfinished goal") {
					m.confirmGoalReplace = true
					m.pendingGoalObjective = objective
					m.pendingGoalBudget = budget
				} else {
					m.pushLine(styleError.Render(err.Error()))
				}
			} else {
				m.goal = g
				m.busy = true
				m.runStartedAt = m.currentTime()
			}
		}
		return m, nil
	case "/plan":
		if m.busy || m.app.Agent.IsRunning() {
			m.pushLine(styleError.Render("plan: wait for the current turn to finish"))
			return m, nil
		}
		message := strings.TrimSpace(strings.TrimPrefix(line, "/plan"))
		m.nudgeDismissed[m.planNudgeScope()] = true
		if message == "" {
			if err := m.app.Agent.SetMode(protocol.ModePlan); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
			return m, nil
		}
		m.pushLine(styleUser.Render("› " + message))
		return m, m.startPromptWithMode(message, protocol.ModePlan)
	case "/default":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/default takes no arguments"))
			return m, nil
		}
		if err := m.app.Agent.SetMode(protocol.ModeDefault); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		}
	case "/compact":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/compact takes no arguments"))
			return m, nil
		}
		return m, m.startCompact()
	case "/login":
		return m.startLogin(args)
	case "/logout":
		return m.doLogout(args)
	case "/agent":
		if len(args) > 0 && args[0] == "concurrency" {
			if len(args) == 1 {
				m.pushLine(styleFooter.Render(fmt.Sprintf("subagent concurrency: %d (use /agent concurrency <n>; restart to apply)", m.app.Cfg.Subagents.MaxConcurrentThreads)))
				return m, nil
			}
			if len(args) != 2 {
				m.pushLine(styleError.Render("usage: /agent concurrency <positive-number>"))
				return m, nil
			}
			limit, err := strconv.Atoi(args[1])
			if err != nil || limit < 1 {
				m.pushLine(styleError.Render("agent concurrency must be a positive number"))
				return m, nil
			}
			if err := m.setSubagentConcurrency(limit); err != nil {
				m.pushLine(styleError.Render(err.Error()))
				return m, nil
			}
			m.pushLine(styleFooter.Render(fmt.Sprintf("subagent concurrency saved as %d; restart Snow to apply", limit)))
			return m, nil
		}
		if m.app.Subagents == nil {
			m.pushLine(styleFooter.Render("subagents are disabled (enable them in /settings or start with --subagents)"))
			return m, nil
		}
		if len(args) == 0 {
			return m, m.openSubagentFleet("")
		}
		if len(args) > 1 {
			m.pushLine(styleError.Render("usage: /agent [path] | /agent concurrency <n>"))
			return m, nil
		}
		return m, m.openSubagentFleet(args[0])
	case "/mcp":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/mcp takes no arguments"))
			return m, nil
		}
		return m.startMCPInfo()
	case "/new":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/new takes no arguments"))
			return m, nil
		}
		return m.startNewSession()
	case "/resume":
		if len(args) > 1 {
			m.pushLine(styleError.Render("usage: /resume [session-path]"))
			return m, nil
		}
		if len(args) == 1 {
			return m.openSession(args[0])
		}
		return m.startSessionPick()
	case "/sessions":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/sessions takes no arguments"))
			return m, nil
		}
		return m.startSessionPick()
	case "/tree":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/tree takes no arguments"))
			return m, nil
		}
		return m.startTreePick()

	case "/model":
		if m.compatibleLoginPending {
			m.lastStatus = "waiting for openai-compatible model discovery"
			m.pushLine(styleFooter.Render(m.lastStatus))
			return m, nil
		}
		if len(args) == 0 {
			// No arg: open the startup-cached interactive model picker.
			return m.startModelPick()
		}
		selected := protocol.Model{Provider: m.app.ProviderID, ID: args[0]}
		catalog := m.modelList
		if len(catalog) == 0 {
			catalog = uniquePickerModels(m.app.AllModels, m.app.ProviderID)
		}
		for _, cached := range catalog {
			if cached.Provider == selected.Provider && cached.ID == selected.ID {
				selected = cached
				break
			}
		}
		m.setModel(selected)
	case "/settings":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/settings takes no arguments"))
			return m, nil
		}
		return m.startSettings()
	case "/skills":
		if len(args) > 0 {
			m.pushLine(styleError.Render("/skills takes no arguments"))
			return m, nil
		}
		return m.startSkillsInfo()
	case "/thinking":
		if len(args) == 0 {
			return m.startThinkingPick()
		}
		if len(args) > 1 {
			m.pushLine(styleError.Render("/thinking takes one level"))
			return m, nil
		}
		level, err := protocol.ParseThinkingLevel(args[0])
		if err != nil {
			m.pushLine(styleError.Render(err.Error()))
			return m, nil
		}
		if err := m.applyThinking(level); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		}
	case "/allow", "/deny":
		// Respond to a pending permission request from the TUI asker. This is
		// also reachable while the interactive picker is open, so clear it.
		if m.asker == nil {
			m.pushLine(styleError.Render("no pending permission request"))
			return m, nil
		}
		m.permPending = false
		m.permRequest = nil
		m.permAgent = nil
		decision := permission.DecisionDeny
		if cmd == "/allow" {
			decision = permission.DecisionAllow
			if len(args) > 0 && args[0] == "always" {
				decision = permission.DecisionAllowAlways
			}
		}
		if err := m.asker.Respond(decision); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		} else {
			m.pushLine(styleFooter.Render(cmd + " granted"))
		}
	case "/permissions":
		if len(args) == 0 {
			return m.startPermissionModePick()
		}
		if len(args) > 1 {
			m.pushLine(styleError.Render("/permissions takes one mode"))
			return m, nil
		}
		switch args[0] {
		case "ask", "allow", "deny":
			if err := m.setPermissionMode(permission.Mode(args[0]), true); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
		default:
			m.pushLine(styleError.Render("invalid mode: " + args[0]))
		}
	case "/trust":
		if len(args) == 0 {
			if lvl, ok := m.app.Trust.Get(m.app.CWD()); ok {
				m.pushLine(styleFooter.Render("trust for " + m.app.CWD() + ": " + string(lvl)))
			} else {
				m.pushLine(styleFooter.Render("no trust decision for " + m.app.CWD()))
			}
			return m, nil
		}
		switch args[0] {
		case "allow", "deny":
			if err := m.app.Trust.Set(m.app.CWD(), trust.Level(args[0])); err != nil {
				m.pushLine(styleError.Render(err.Error()))
			} else {
				m.pushLine(styleFooter.Render("trust " + args[0] + " saved for next launch · " + m.app.CWD()))
			}
		default:
			m.pushLine(styleError.Render("invalid trust level: " + args[0]))
		}
	default:
		m.pushLine(styleError.Render("unknown command: " + cmd + " (try /help)"))
	}
	return m, nil
}

// startLogin handles /login. No args opens the provider picker. A direct
// openai-compatible login captures endpoint then optional key; other API-key
// providers enter masked capture directly.
func (m *Model) startLogin(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.providers = supportedProviders()
		m.provIndex = 0
		m.providerLogout = false
		m.pickProvider = true
		m.compVisible = false
		m.editor.Reset()
		m.pushLine(styleFooter.Render("select a login provider (↑/↓ navigate, Enter to pick, Esc to cancel)"))
		return m, nil
	}
	provider := args[0]
	if provider == chatgpt.ProviderID {
		return m.startChatGPTAuthPick()
	}
	if !isSupportedProvider(provider) {
		m.pushLine(styleError.Render("login: unsupported provider " + provider +
			" (supported: " + strings.Join(supportedProviders(), ", ") + ")"))
		return m, nil
	}
	if provider == openaicompat.ProviderID {
		m.beginCompatibleEndpointCapture()
	} else {
		m.beginKeyCapture(provider)
	}
	return m, nil
}

// handleProviderPick navigates the /login provider list.
func (m *Model) handleProviderPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp:
		if len(m.providers) > 0 {
			m.provIndex = (m.provIndex - 1 + len(m.providers)) % len(m.providers)
		}
	case tea.KeyDown:
		if len(m.providers) > 0 {
			m.provIndex = (m.provIndex + 1) % len(m.providers)
		}
	case tea.KeyTab:
		if len(m.providers) > 0 {
			m.provIndex = (m.provIndex + 1) % len(m.providers)
		}
	case tea.KeyEsc:
		m.pickProvider = false
		m.providerLogout = false
		m.providers = nil
	case tea.KeyEnter:
		if len(m.providers) == 0 {
			m.pickProvider = false
			return m, nil
		}
		provider := m.providers[m.provIndex]
		logout := m.providerLogout
		m.pickProvider = false
		m.providerLogout = false
		m.providers = nil
		if logout {
			return m.doLogout([]string{provider})
		}
		if provider == chatgpt.ProviderID {
			return m.startChatGPTAuthPick()
		}
		if !isSupportedProvider(provider) {
			m.pushLine(styleError.Render("login: " + provider + " is not supported yet"))
			return m, nil
		}
		if provider == openaicompat.ProviderID {
			m.beginCompatibleEndpointCapture()
		} else {
			m.beginKeyCapture(provider)
		}
	}
	return m, nil
}

func chatGPTAccountChoices(sources []chatgpt.AuthSource) []chatGPTAccountChoice {
	choices := make([]chatGPTAccountChoice, 0, len(sources))
	index := make(map[string]int, len(sources))
	for _, source := range sources {
		accountID := strings.TrimSpace(source.Status.AccountID)
		if accountID == "" {
			continue
		}
		if i, ok := index[accountID]; ok {
			duplicate := false
			for _, name := range choices[i].Sources {
				if name == source.Name {
					duplicate = true
					break
				}
			}
			if !duplicate {
				choices[i].Sources = append(choices[i].Sources, source.Name)
			}
			continue
		}
		index[accountID] = len(choices)
		choices = append(choices, chatGPTAccountChoice{AccountID: accountID, Sources: []string{source.Name}})
	}
	return choices
}

// startChatGPTAuthPick discovers account IDs used by OpenCode/Pi/Codex, then
// starts a fresh Snow OAuth flow constrained to the selected account. Tokens
// from other clients are never copied into Snow by this TUI flow.
func (m *Model) startChatGPTAuthPick() (tea.Model, tea.Cmd) {
	m.authAccounts = chatGPTAccountChoices(chatgpt.DiscoverAuthSources())
	m.authIndex = 0
	m.pickChatGPTAuth = true
	m.compVisible = false
	m.editor.Reset()
	m.pushLine(styleFooter.Render("select an existing ChatGPT account or sign-in method (↑/↓ navigate, Enter select, Esc cancel)"))
	return m, nil
}

// handleChatGPTAuthPick selects and imports a discovered local credential.
func (m *Model) handleChatGPTAuthPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.oauthLoading {
		if msg.Type == tea.KeyEsc && m.oauthCancel != nil {
			m.oauthCancel()
		}
		return m, nil
	}
	count := len(m.authAccounts) + 2
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.authIndex = (m.authIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyTab:
		m.authIndex = (m.authIndex + 1) % count
	case tea.KeyEsc:
		m.pickChatGPTAuth = false
		m.authAccounts = nil
	case tea.KeyEnter:
		if m.authIndex < len(m.authAccounts) {
			account := m.authAccounts[m.authIndex]
			return m, m.startChatGPTOAuth(chatgpt.LoginBrowser, []string{account.AccountID})
		}
		method := chatgpt.LoginBrowser
		if m.authIndex-len(m.authAccounts) == 1 {
			method = chatgpt.LoginDevice
		}
		return m, m.startChatGPTOAuth(method, nil)
	}
	return m, nil
}

func (m *Model) startChatGPTOAuth(method chatgpt.LoginMethod, allowedWorkspaceIDs []string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	events := make(chan tea.Msg, 8)
	m.oauthLoading, m.oauthCancel, m.oauthEvents = true, cancel, events
	go func() {
		status, err := chatgpt.Login(ctx, chatgpt.LoginOptions{Method: method, Store: m.app.Auth, AllowedWorkspaceIDs: allowedWorkspaceIDs, OpenBrowser: openOAuthBrowser, Progress: func(progress chatgpt.LoginProgress) {
			select {
			case events <- oauthProgressMsg{progress: progress}:
			case <-ctx.Done():
			}
		}})
		if err != nil && method == chatgpt.LoginBrowser && strings.Contains(err.Error(), "callback port 1455 is unavailable") && ctx.Err() == nil {
			status, err = chatgpt.Login(ctx, chatgpt.LoginOptions{Method: chatgpt.LoginDevice, Store: m.app.Auth, AllowedWorkspaceIDs: allowedWorkspaceIDs, OpenBrowser: openOAuthBrowser, Progress: func(progress chatgpt.LoginProgress) {
				select {
				case events <- oauthProgressMsg{progress: progress}:
				case <-ctx.Done():
				}
			}})
		}
		if err == nil {
			if refreshErr := m.app.RefreshProviderModels(ctx, chatgpt.ProviderID); refreshErr != nil && ctx.Err() == nil {
				events <- oauthProgressMsg{progress: chatgpt.LoginProgress{Kind: "catalog_fallback", Message: "Signed in; using offline model catalog fallback"}}
			}
			if checked, checkErr := chatgpt.CheckStore(m.app.Auth); checkErr == nil {
				status = checked
			}
		}
		// Once Login has persisted the credential, cancellation/fallback of this
		// optional catalog refresh must not turn the committed login into failure.
		events <- oauthDoneMsg{status: status, err: err}
	}()
	return waitOAuthEvent(events)
}

func (m *Model) startChatGPTCatalogRefresh(status chatgpt.AuthStatus) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	events := make(chan tea.Msg, 4)
	m.oauthLoading, m.oauthCancel, m.oauthEvents = true, cancel, events
	m.oauthProgress = chatgpt.LoginProgress{Kind: "catalog", Message: "Refreshing ChatGPT model catalog…"}
	go func() {
		if err := m.app.RefreshProviderModels(ctx, chatgpt.ProviderID); err != nil {
			select {
			case events <- oauthProgressMsg{progress: chatgpt.LoginProgress{Kind: "catalog_fallback", Message: "Using offline model catalog fallback"}}:
			case <-ctx.Done():
			}
		}
		if checked, err := chatgpt.CheckStore(m.app.Auth); err == nil {
			status = checked
		}
		events <- oauthDoneMsg{status: status}
	}()
	return waitOAuthEvent(events)
}

func waitOAuthEvent(events <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-events } }
func openOAuthBrowser(ctx context.Context, target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}
	return exec.CommandContext(ctx, name, args...).Start()
}

// renderChatGPTAuthPicker renders discovered source names and secret-free
// status metadata. Tokens are never included in this string.
func (m *Model) renderChatGPTAuthPicker() string {
	if !m.pickChatGPTAuth {
		return ""
	}
	if m.oauthLoading {
		line := m.oauthProgress.Message
		if m.oauthProgress.URL != "" {
			line += "\n" + m.oauthProgress.URL
		}
		if m.oauthProgress.UserCode != "" {
			line += "\nDevice code: " + m.oauthProgress.UserCode
		}
		return styleCompletionSelected.Render(line + "\n\nEsc cancel")
	}
	lines := make([]string, 0, len(m.authAccounts)+2)
	for _, account := range m.authAccounts {
		lines = append(lines, "Authorize account "+account.AccountID+" for Snow  (used by "+strings.Join(account.Sources, ", ")+")")
	}
	lines = append(lines, "Sign in with browser (any ChatGPT account)", "Sign in with device code")
	var b strings.Builder
	b.WriteString(styleHeader.Render("ChatGPT account"))
	b.WriteByte('\n')
	for i, line := range lines {
		if i == m.authIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteByte('\n')
	}
	b.WriteString(styleFooter.Render("(Snow obtains its own OAuth token · Enter authorize · Esc cancel)"))
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) beginCompatibleEndpointCapture() {
	m.loginEndpointMode = true
	m.loginMode = false
	m.loginProvider = openaicompat.ProviderID
	m.loginEndpoint = ""
	m.secretBuf.Reset()
	m.editor.Reset()
	if m.app != nil {
		if configured, ok := m.app.Cfg.Providers[openaicompat.ProviderID]; ok {
			m.editor.SetValue(configured.BaseURL)
			m.editor.CursorEnd()
		}
	}
	m.editor.Placeholder = "https://gateway.example/v1"
	m.compVisible = false
	m.pickProvider = false
	m.pushLine(styleFooter.Render("openai-compatible endpoint: enter API root, /responses, or /chat/completions URL · Enter continue · Esc cancel"))
}

// beginKeyCapture switches the editor into masked API-key capture mode.
func (m *Model) beginKeyCapture(provider string) {
	m.loginMode = true
	m.loginProvider = provider
	m.secretBuf.Reset()
	m.editor.Reset()
	m.editor.Placeholder = "Type a message…"
	m.compVisible = false
	m.pickProvider = false
	hint := "type key then Enter · Esc to cancel"
	if provider == openaicompat.ProviderID {
		hint = "type optional key, or press Enter to keep existing/fallback/keyless · Esc to cancel"
	}
	m.pushLine(styleFooter.Render("API key for " + provider + " (hidden): " + hint))
}

// supportedProviders lists providers shown in the /login picker. ChatGPT is
// selected to import an existing OAuth login rather than capture an API key.
func supportedProviders() []string {
	return []string{"opencode-go", openaicompat.ProviderID, "chatgpt"}
}

// isSupportedProvider reports whether the provider can take a key now.
func isSupportedProvider(p string) bool {
	switch p {
	case "opencode-go", openaicompat.ProviderID:
		return true
	default:
		return false
	}
}

// providerStatus renders a provider line for the picker, including chatgpt
// as a visible-but-unavailable entry for discoverability.
func (m *Model) providerStatus(provider string) string {
	if provider == chatgpt.ProviderID {
		status, err := chatgpt.CheckStore(m.app.Auth)
		if err != nil {
			return provider + "  (invalid OAuth: " + err.Error() + ")"
		}
		if !status.Authenticated {
			return provider + "  (OAuth not configured)"
		}
		if status.Expired {
			return provider + "  (OAuth expired)"
		}
		return provider + "  (" + chatgpt.FormatStatus(status) + ")"
	}
	if !isSupportedProvider(provider) {
		return provider + "  (not supported yet)"
	}
	keyStatus := "no key"
	if cred, ok := m.app.Auth.Get(provider); ok && cred.Valid() {
		keyStatus = "stored ✓"
	}
	if provider == openaicompat.ProviderID {
		endpointStatus := "endpoint required"
		if configured, ok := m.app.PersistedCfg.Providers[provider]; ok && strings.TrimSpace(configured.BaseURL) != "" {
			endpointStatus = "endpoint configured"
		}
		return provider + "  (" + endpointStatus + " · " + keyStatus + ")"
	}
	return provider + "  (" + keyStatus + ")"
}

// renderProviderPicker renders the /login or /logout provider list.
func (m *Model) renderProviderPicker() string {
	if !m.pickProvider || len(m.providers) == 0 {
		return ""
	}
	var b strings.Builder
	title := "login provider"
	hint := "(↑/↓ choose · Enter sign in · Esc cancel)"
	if m.providerLogout {
		title = "logout provider"
		hint = "(↑/↓ choose · Enter log out · Esc cancel)"
	}
	b.WriteString(styleHeader.Render(title))
	b.WriteString("\n")
	for i, p := range m.providers {
		line := m.providerStatus(p)
		if i == m.provIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleFooter.Render(hint))
	return strings.TrimSuffix(b.String(), "\n")
}

// doLogout opens a picker for /logout or directly removes /logout <provider>.
func (m *Model) doLogout(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.providers = m.storedCredentialProviders()
		if len(m.providers) == 0 {
			m.pushLine(styleFooter.Render("logout: no stored credentials"))
			return m, nil
		}
		m.provIndex = 0
		m.providerLogout = true
		m.pickProvider = true
		m.compVisible = false
		return m, nil
	}
	if len(args) != 1 {
		m.pushLine(styleError.Render("usage: /logout [provider]"))
		return m, nil
	}
	provider := args[0]
	app := m.app
	ctx := m.ctx
	return m, func() tea.Msg {
		err := app.Auth.Delete(provider)
		if err == nil && provider == chatgpt.ProviderID {
			_ = app.RefreshProviderModels(ctx, provider)
		}
		return logoutDoneMsg{provider: provider, err: err}
	}
}

func (m *Model) storedCredentialProviders() []string {
	providers := make([]string, 0, len(supportedProviders()))
	for _, provider := range supportedProviders() {
		if _, ok := m.app.Auth.Get(provider); ok {
			providers = append(providers, provider)
		}
	}
	return providers
}

// ---------------------------------------------------------------------------
// Model selection (interactive picker + persistent config)
// ---------------------------------------------------------------------------

func (m *Model) startThinkingPick() (tea.Model, tea.Cmd) {
	if m.app == nil {
		return m, nil
	}
	m.startThinkingPickForModel(m.app.Agent.Model(), false)
	return m, nil
}

func (m *Model) startThinkingPickForModel(model protocol.Model, returnToModel bool) {
	model = model.Clone()
	m.thinkingModel = &model
	m.thinkingReturnToModel = returnToModel
	m.thinkingList = model.SupportedThinkingLevels()
	m.thinkingIndex = 0
	current := m.app.Agent.Thinking()
	if returnToModel && model.DefaultThinking != "" && model.SupportsThinkingLevel(model.DefaultThinking) &&
		(m.app.Agent.Model().Provider != model.Provider || m.app.Agent.Model().ID != model.ID) {
		current = model.DefaultThinking
	} else if !model.SupportsThinkingLevel(current) && model.DefaultThinking != "" && model.SupportsThinkingLevel(model.DefaultThinking) {
		current = model.DefaultThinking
	}
	for i, level := range m.thinkingList {
		if level == current {
			m.thinkingIndex = i
			break
		}
	}
	m.pickThinking = true
	m.compVisible = false
}

func (m *Model) handleThinkingPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if len(m.thinkingList) == 0 {
		m.pickThinking = false
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.thinkingIndex = (m.thinkingIndex - 1 + len(m.thinkingList)) % len(m.thinkingList)
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.thinkingIndex = (m.thinkingIndex + 1) % len(m.thinkingList)
	case tea.KeyEnter:
		level := m.thinkingList[m.thinkingIndex]
		selected := m.thinkingModel
		returnToModel := m.thinkingReturnToModel
		returnToSettings := returnToModel && m.settingsReturnToPanel
		m.clearThinkingPick()
		if returnToModel && selected != nil {
			m.clearModelPick()
			m.settingsReturnToPanel = false
			err := m.applyModelAndThinking(*selected, level)
			if returnToSettings {
				m.pickSettings = true
				if err != nil {
					m.settingsError = err.Error()
					m.settingsStatus = ""
				} else {
					m.settingsError = ""
					m.settingsStatus = "model and thinking effort saved"
				}
			} else if err != nil {
				m.pushLine(styleError.Render(err.Error()))
			}
		} else if err := m.applyThinking(level); err != nil {
			m.pushLine(styleError.Render(err.Error()))
		}
	case tea.KeyEsc:
		returnToModel := m.thinkingReturnToModel
		m.clearThinkingPick()
		if returnToModel {
			m.pickModel = true
		}
	}
	return m, nil
}

func (m *Model) clearModelPick() {
	m.pickModel = false
	m.modelList = nil
	m.modelQuery = ""
	m.modelSearchActive = false
}

func (m *Model) clearThinkingPick() {
	m.pickThinking = false
	m.thinkingList = nil
	m.thinkingModel = nil
	m.thinkingReturnToModel = false
}

func (m *Model) applyThinking(level protocol.ThinkingLevel) error {
	return m.setThinking(level, true)
}

func (m *Model) setThinking(level protocol.ThinkingLevel, announce bool) error {
	if m.app == nil {
		return fmt.Errorf("thinking: app is not ready")
	}
	old := m.app.Agent.Thinking()
	if err := m.app.Agent.SetThinking(level); err != nil {
		return err
	}
	candidate := m.app.PersistedCfg
	candidate.Thinking = string(m.app.Agent.Thinking())
	// A model switch with an incompatible existing effort is runtime-only;
	// once the user selects a valid effort, persist the complete active choice.
	candidate.DefaultProvider = m.app.ProviderID
	candidate.DefaultModel = m.app.Agent.Model().ID
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			_ = m.app.Agent.SetThinking(old)
			return fmt.Errorf("persist thinking: %w", err)
		}
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Thinking = candidate.Thinking
	m.app.Cfg.DefaultProvider = candidate.DefaultProvider
	m.app.Cfg.DefaultModel = candidate.DefaultModel
	if announce {
		m.pushLine(styleFooter.Render("thinking: " + string(m.app.Agent.Thinking())))
	}
	return nil
}

func (m *Model) renderThinkingPicker() string {
	if !m.pickThinking || len(m.thinkingList) == 0 {
		return ""
	}
	var b strings.Builder
	title := "thinking effort"
	if m.thinkingModel != nil {
		title += " for " + m.thinkingModel.ID
	}
	b.WriteString(styleHeaderDim.Render(title) + "\n")
	for i, level := range m.thinkingList {
		line := string(level)
		if i == m.thinkingIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	hint := "(↑/↓ choose, Enter apply, Esc cancel)"
	if m.thinkingReturnToModel {
		hint = "(↑/↓ choose, Enter apply, Esc back)"
	}
	b.WriteString(styleFooter.Render(hint))
	return strings.TrimSuffix(b.String(), "\n")
}

// startModelPick opens the interactive model picker from the app's current
// combined provider catalog. The fallback fetch is only for tests/SDKs without
// an app catalog snapshot.
func (m *Model) startModelPick() (tea.Model, tea.Cmd) {
	if m.compatibleLoginPending {
		m.lastStatus = "waiting for openai-compatible model discovery"
		m.pushLine(styleFooter.Render(m.lastStatus))
		return m, nil
	}
	var models []protocol.Model
	if m.app != nil {
		models = append([]protocol.Model(nil), m.app.AllModels...)
		if len(models) == 0 {
			models = append([]protocol.Model(nil), m.app.Models...)
		}
	}
	if len(models) == 0 && m.app != nil {
		if m.asyncIO {
			m.pickModel = true
			m.modelLoading = true
			m.modelList = nil
			m.modelQuery = ""
			m.modelSearchActive = false
			m.pickerGeneration++
			generation := m.pickerGeneration
			return m, func() tea.Msg {
				fetched, err := m.app.Provider.ListModels(m.ctx)
				return modelListMsg{generation: generation, models: fetched, err: err}
			}
		}
		fetched, err := m.app.Provider.ListModels(m.ctx)
		if err != nil {
			m.pushLine(styleError.Render("model list: " + err.Error()))
			return m, nil
		}
		models = fetched
		m.app.Models = uniquePickerModels(models, m.app.ProviderID)
	}
	models = uniquePickerModels(models, m.app.ProviderID)
	if len(models) == 0 {
		m.pushLine(styleError.Render("no models available"))
		return m, nil
	}
	m.modelList = models
	m.modelIndex = 0
	m.modelQuery = ""
	m.modelSearchActive = false
	for i, mm := range models {
		if mm.Provider == m.app.Model.Provider && mm.ID == m.app.Model.ID {
			m.modelIndex = i
			break
		}
	}
	m.pickModel = true
	m.compVisible = false
	return m, nil
}

func uniquePickerModels(models []protocol.Model, defaultProvider string) []protocol.Model {
	out := make([]protocol.Model, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model.Provider == "" {
			model.Provider = defaultProvider
		}
		if model.ID == "" {
			continue
		}
		key := model.Provider + "\x00" + model.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model.Clone())
	}
	return out
}

func (m *Model) filteredModels() []protocol.Model {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(m.modelQuery)))
	if len(terms) == 0 {
		return m.modelList
	}
	matches := make([]protocol.Model, 0, len(m.modelList))
	for _, model := range m.modelList {
		haystack := strings.ToLower(strings.Join([]string{model.Provider, model.ID, model.DisplayName, model.Description}, " "))
		matched := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, model)
		}
	}
	return matches
}

// handleModelPick navigates and searches the /model picker. Slash enters search
// mode so the usual j/k picker bindings remain available until text entry starts.
func (m *Model) handleModelPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modelLoading {
		msg = normalizePickerKeyWithMap(msg, m.keys)
		if msg.Type == tea.KeyEsc {
			m.pickModel = false
			m.modelLoading = false
			m.pickerGeneration++
		}
		return m, nil
	}

	if m.modelSearchActive {
		switch msg.Type {
		case tea.KeyRunes:
			m.modelQuery += string(msg.Runes)
			m.modelIndex = 0
			m.layout()
			return m, nil
		case tea.KeyBackspace:
			runes := []rune(m.modelQuery)
			if len(runes) > 0 {
				m.modelQuery = string(runes[:len(runes)-1])
			}
			m.modelIndex = 0
			m.layout()
			return m, nil
		case tea.KeyCtrlU:
			m.modelQuery = ""
			m.modelIndex = 0
			m.layout()
			return m, nil
		case tea.KeyEsc:
			m.modelQuery = ""
			m.modelIndex = 0
			m.modelSearchActive = false
			m.layout()
			return m, nil
		}
	} else if msg.Type == tea.KeyRunes && string(msg.Runes) == "/" {
		m.modelSearchActive = true
		m.modelIndex = 0
		m.layout()
		return m, nil
	}

	msg = normalizePickerKeyWithMap(msg, m.keys)
	models := m.filteredModels()
	if next, handled := movePicker(m.modelIndex, len(models), pickerKeyAction(msg), m.modelPickerVisibleModels()); handled {
		m.modelIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.clearModelPick()
		if m.settingsReturnToPanel {
			m.settingsReturnToPanel = false
			m.pickSettings = true
		}
	case tea.KeyEnter:
		if len(models) == 0 {
			return m, nil
		}
		model := models[m.modelIndex]
		if len(model.SupportedThinkingLevels()) > 1 {
			m.pickModel = false
			m.startThinkingPickForModel(model, true)
			return m, nil
		}
		m.clearModelPick()
		if m.settingsReturnToPanel {
			m.settingsReturnToPanel = false
			m.pickSettings = true
			if err := m.applyModel(model); err != nil {
				m.settingsError = err.Error()
				m.settingsStatus = ""
			} else {
				m.settingsError = ""
				m.settingsStatus = "model saved"
			}
		} else {
			m.setModel(model)
		}
	}
	return m, nil
}

func (m *Model) modelPickerVisibleModels() int {
	// An inline modal has ten total rows. Two models leave worst-case room for
	// separate provider headings, search chrome, scroll markers, details, and
	// controls while keeping the selected model visible.
	if m.inlineModalOverlay() {
		return 2
	}
	// Search makes a short list more useful than filling the whole transcript.
	// Keep enough space for headings, search state, details, and controls.
	visible := m.availableOverlayHeight() - 9
	if visible < 4 {
		visible = 4
	}
	if visible > 10 {
		visible = 10
	}
	return visible
}

func (m *Model) modelWindow(models []protocol.Model) (start, end int) {
	end = len(models)
	visible := m.modelPickerVisibleModels()
	if end <= visible {
		return 0, end
	}
	start = m.modelIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > end {
		start = end - visible
	}
	return start, start + visible
}

// renderModelPicker renders a compact, searchable, provider-grouped /model list.
func (m *Model) renderModelPicker() string {
	if !m.pickModel {
		return ""
	}
	if m.modelLoading {
		return styleHeaderDim.Render("models\n  loading models…")
	}
	if len(m.modelList) == 0 {
		return ""
	}
	models := m.filteredModels()
	start, end := m.modelWindow(models)
	var b strings.Builder
	b.WriteString(styleHeader.Render("models"))
	b.WriteString(styleHeaderDim.Render(fmt.Sprintf("  ·  %d available", len(m.modelList))))
	b.WriteString("\n")
	if m.modelSearchActive || m.modelQuery != "" {
		cursor := ""
		if m.modelSearchActive {
			cursor = "_"
		}
		b.WriteString(styleHeaderDim.Render(fmt.Sprintf("  search: %s%s  ·  %d matches", m.modelQuery, cursor, len(models))))
	} else {
		b.WriteString(styleHeaderDim.Render("  press / to search"))
	}
	b.WriteString("\n")
	if len(models) == 0 {
		b.WriteString(styleFooter.Render("  no matching models"))
		b.WriteString("\n")
	} else {
		if start > 0 {
			b.WriteString(styleHeaderDim.Render("  ↑ more"))
			b.WriteString("\n")
		}
		for i := start; i < end; i++ {
			mm := models[i]
			if i == start || mm.Provider != models[i-1].Provider {
				b.WriteString(styleHeaderDim.Render("  " + mm.Provider))
				b.WriteString("\n")
			}
			line := mm.ID
			if mm.DisplayName != "" && !strings.EqualFold(mm.DisplayName, mm.ID) {
				line += "  (" + mm.DisplayName + ")"
			}
			if m.app != nil && mm.Provider == m.app.Model.Provider && mm.ID == m.app.Model.ID {
				line += "  ✓ current"
			}
			if i == m.modelIndex {
				b.WriteString(styleCompletionSelected.Render("› " + line))
			} else {
				b.WriteString(styleCompletion.Render("  " + line))
			}
			b.WriteString("\n")
		}
		if end < len(models) {
			b.WriteString(styleHeaderDim.Render("  ↓ more"))
			b.WriteString("\n")
		}
		selected := models[m.modelIndex]
		var details []string
		if selected.ContextWindow > 0 {
			details = append(details, "ctx "+formatTokenCount(int64(selected.ContextWindow)))
		}
		if levels := selected.SupportedThinkingLevels(); len(levels) > 1 {
			parts := make([]string, 0, len(levels)-1)
			for _, level := range levels[1:] {
				parts = append(parts, string(level))
			}
			details = append(details, "thinking "+strings.Join(parts, "/"))
		}
		if len(details) > 0 {
			b.WriteString(styleHeaderDim.Render("  " + strings.Join(details, "  ·  ")))
			b.WriteString("\n")
		}
	}
	b.WriteString(styleFooter.Render("(↑/↓ choose · / search · Enter apply · Esc cancel)"))
	return strings.TrimSuffix(b.String(), "\n")
}

// setModel switches the active provider/model and persists the choice so it
// survives restarts (config.json default_provider/default_model).
func (m *Model) setModel(selected protocol.Model) {
	if m.app != nil {
		if selected.Provider == "" {
			selected.Provider = m.app.ProviderID
		}
		currentThinking := m.app.Agent.Thinking()
		if currentThinking != protocol.ThinkingOff && !selected.SupportsThinkingLevel(currentThinking) {
			if selected.Provider != m.app.ProviderID {
				if err := m.app.SetProvider(selected.Provider); err != nil {
					m.pushLine(styleError.Render(err.Error()))
					return
				}
			}
			if err := m.app.SetModel(selected); err != nil {
				m.pushLine(styleError.Render(err.Error()))
				return
			}
			m.pushLine(styleTool.Render("model does not advertise thinking level " + string(currentThinking) + "; choose /thinking before the next prompt"))
			return
		}
	}
	if err := m.applyModel(selected); err != nil {
		m.pushLine(styleError.Render(err.Error()))
	}
}

func (m *Model) applyModelAndThinking(selected protocol.Model, level protocol.ThinkingLevel) error {
	if m.app == nil {
		return fmt.Errorf("model: app is not ready")
	}
	parsed, err := protocol.ParseThinkingLevel(string(level))
	if err != nil {
		return err
	}
	if selected.Provider == "" {
		selected.Provider = m.app.ProviderID
	}
	if !selected.SupportsThinkingLevel(parsed) {
		return fmt.Errorf("model %q does not advertise thinking level %q", selected.ID, parsed)
	}

	oldProvider := m.app.ProviderID
	oldModel := m.app.Agent.Model()
	oldAppModel := m.app.Model
	oldThinking := m.app.Agent.Thinking()
	oldCfg := m.app.Cfg
	oldPersistedCfg := m.app.PersistedCfg
	rollback := func() error {
		if err := m.app.SetProviderModelThinking(oldProvider, oldModel, oldThinking); err != nil {
			return err
		}
		m.app.Model = oldAppModel
		m.app.Cfg = oldCfg
		m.app.PersistedCfg = oldPersistedCfg
		return nil
	}

	if err := m.app.SetProviderModelThinking(selected.Provider, selected, parsed); err != nil {
		return err
	}

	candidate := oldPersistedCfg
	candidate.DefaultProvider = selected.Provider
	candidate.DefaultModel = selected.ID
	candidate.Thinking = string(parsed)
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return fmt.Errorf("persist model and thinking: %w (rollback failed: %v)", err, rollbackErr)
			}
			return fmt.Errorf("persist model and thinking: %w", err)
		}
	}
	m.app.Model = selected
	m.app.PersistedCfg = candidate
	m.app.Cfg.DefaultProvider = candidate.DefaultProvider
	m.app.Cfg.DefaultModel = candidate.DefaultModel
	m.app.Cfg.Thinking = candidate.Thinking
	return nil
}

func (m *Model) applyModel(selected protocol.Model) error {
	if m.app == nil {
		return fmt.Errorf("model: app is not ready")
	}
	currentThinking := m.app.Agent.Thinking()
	if selected.Provider == "" {
		selected.Provider = m.app.ProviderID
	}
	if currentThinking != protocol.ThinkingOff && !selected.SupportsThinkingLevel(currentThinking) {
		return fmt.Errorf("model %q does not advertise thinking level %q; choose a supported effort first", selected.ID, currentThinking)
	}
	oldProvider := m.app.ProviderID
	oldModel := m.app.Agent.Model()
	oldAppModel := m.app.Model
	oldCfg := m.app.Cfg
	oldPersistedCfg := m.app.PersistedCfg
	if selected.Provider != m.app.ProviderID {
		if err := m.app.SetProvider(selected.Provider); err != nil {
			return err
		}
	}
	if err := m.app.SetModel(selected); err != nil {
		if oldProvider != m.app.ProviderID {
			_ = m.app.SetProvider(oldProvider)
		}
		return err
	}
	candidate := oldPersistedCfg
	candidate.DefaultProvider = selected.Provider
	candidate.DefaultModel = selected.ID
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			if oldProvider != m.app.ProviderID {
				_ = m.app.SetProvider(oldProvider)
			}
			_ = m.app.SetModel(oldModel)
			m.app.Model = oldAppModel
			m.app.Cfg = oldCfg
			return fmt.Errorf("persist model: %w", err)
		}
	}
	m.app.Model = selected
	m.app.PersistedCfg = candidate
	m.app.Cfg.DefaultProvider = candidate.DefaultProvider
	m.app.Cfg.DefaultModel = candidate.DefaultModel
	return nil
}

// ---------------------------------------------------------------------------
// Session picker and switching
// ---------------------------------------------------------------------------

func (m *Model) currentSessions() ([]session.SessionInfo, error) {
	return session.NewFileIndex(session.DefaultSessionsRoot()).List(m.app.CWD())
}

func (m *Model) noSessionsResumeMessage() string {
	hint := "/new"
	if m.startupResumeRequired {
		hint = "snow"
	}
	return "no sessions to resume for " + m.app.CWD() + " (use " + hint + " to create one)"
}

func currentSessionID(a *app.App) string {
	if a == nil || a.Agent == nil {
		return ""
	}
	id, _, err := a.Agent.SessionIdentity()
	if err != nil {
		return ""
	}
	return id
}

func currentSessionPath(a *app.App) string {
	if a == nil || a.Agent == nil {
		return ""
	}
	_, path, err := a.Agent.SessionIdentity()
	if err != nil {
		return ""
	}
	return path
}

func (m *Model) startSessionPick() (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("session: wait for the current turn to finish"))
		return m, nil
	}
	if m.asyncIO {
		m.pickSession = true
		m.sessionLoading = true
		m.sessions = nil
		m.pickerGeneration++
		generation := m.pickerGeneration
		return m, func() tea.Msg {
			infos, err := m.currentSessions()
			return sessionListMsg{generation: generation, sessions: infos, err: err}
		}
	}
	infos, err := m.currentSessions()
	if err != nil {
		m.pushLine(styleError.Render("session list: " + err.Error()))
		if m.startupResumeRequired {
			return m, m.quitCmd()
		}
		return m, nil
	}
	if len(infos) == 0 {
		m.sessions = nil
		m.pickSession = false
		m.pushLine(styleFooter.Render(m.noSessionsResumeMessage()))
		if m.startupResumeRequired {
			return m, m.quitCmd()
		}
		return m, nil
	}
	m.sessions = infos
	m.sessionIndex = 0
	for i, info := range infos {
		if info.ID == currentSessionID(m.app) {
			m.sessionIndex = i
			break
		}
	}
	m.pickSession = true
	m.compVisible = false
	return m, nil
}

func (m *Model) handleSessionPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.sessionRenaming {
		switch {
		case keyMatches(msg, m.keys.Close):
			m.sessionRenaming, m.sessionRenameInput = false, ""
		case keyMatches(msg, m.keys.Accept):
			return m.executeSessionRename()
		case msg.Type == tea.KeyBackspace:
			r := []rune(m.sessionRenameInput)
			if len(r) > 0 {
				m.sessionRenameInput = string(r[:len(r)-1])
			}
		case msg.Type == tea.KeyRunes:
			if len([]rune(m.sessionRenameInput))+len(msg.Runes) <= 72 {
				m.sessionRenameInput += string(msg.Runes)
			}
		}
		return m, nil
	}
	if m.sessionLoading {
		if msg.Type == tea.KeyEsc {
			m.pickSession = false
			m.sessionLoading = false
			m.pickerGeneration++
			if m.startupResumeRequired {
				return m, m.quitCmd()
			}
		}
		return m, nil
	}
	count := len(m.sessions)
	if count == 0 {
		m.pickSession = false
		return m, nil
	}
	if next, handled := movePicker(m.sessionIndex, count, pickerKeyAction(msg), m.sessionPickerVisibleItems()); handled {
		m.sessionIndex = next
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.sessionIndex = (m.sessionIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.sessionIndex = (m.sessionIndex + 1) % count
	case tea.KeyPgUp:
		m.sessionIndex -= m.sessionPickerVisibleItems()
		if m.sessionIndex < 0 {
			m.sessionIndex = 0
		}
	case tea.KeyPgDown:
		m.sessionIndex += m.sessionPickerVisibleItems()
		if m.sessionIndex >= count {
			m.sessionIndex = count - 1
		}
	case tea.KeyHome:
		m.sessionIndex = 0
	case tea.KeyEnd:
		m.sessionIndex = count - 1
	case tea.KeyEsc:
		m.pickSession = false
		m.sessions = nil
		if m.startupResumeRequired {
			return m, m.quitCmd()
		}
	case tea.KeyEnter:
		return m.openSession(m.sessions[m.sessionIndex].Path)
	case tea.KeyRunes:
		if keyMatches(msg, m.keys.BranchRename) {
			m.sessionRenaming = true
			m.sessionRenameInput = m.sessions[m.sessionIndex].Name
		}
	}
	return m, nil
}

func (m *Model) executeSessionRename() (tea.Model, tea.Cmd) {
	if m.sessionIndex < 0 || m.sessionIndex >= len(m.sessions) {
		m.sessionRenaming = false
		return m, nil
	}
	title := strings.TrimSpace(m.sessionRenameInput)
	m.sessionRenaming, m.sessionRenameInput = false, ""
	selected := m.sessions[m.sessionIndex]
	index := m.sessionIndex
	m.sessionLoading = m.asyncIO
	m.pickerGeneration++
	generation := m.pickerGeneration
	run := func() tea.Msg {
		var err error
		if selected.ID == currentSessionID(m.app) {
			err = m.app.RenameSession(title)
		} else {
			err = session.NewFileIndex(session.DefaultSessionsRoot()).Rename(m.app.CWD(), selected.Path, title)
		}
		return sessionRenameMsg{generation: generation, index: index, title: title, err: err}
	}
	return m, run
}

func (m *Model) startNewSession() (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("session: wait for the current turn to finish"))
		return m, nil
	}
	if m.asyncIO {
		m.sessionOpLoading = true
		m.lastStatus = "creating session…"
		m.sessionOpGeneration++
		generation := m.sessionOpGeneration
		return m, func() tea.Msg {
			st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Create(m.app.CWD())
			return sessionStoreMsg{generation: generation, path: "new", store: st, err: err}
		}
	}
	st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Create(m.app.CWD())
	if err != nil {
		m.pushLine(styleError.Render("new session: " + err.Error()))
		return m, nil
	}
	if err := m.switchSession(st); err != nil {
		_ = st.Close()
		m.pushLine(styleError.Render("new session: " + err.Error()))
		return m, nil
	}
	m.lastStatus = "new session"
	return m, nil
}

func (m *Model) openSession(path string) (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("session: wait for the current turn to finish"))
		return m, nil
	}
	m.pickSession = false
	m.sessions = nil
	if m.asyncIO {
		m.sessionOpLoading = true
		m.lastStatus = "opening session…"
		m.sessionOpGeneration++
		generation := m.sessionOpGeneration
		return m, func() tea.Msg {
			st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(path)
			return sessionStoreMsg{generation: generation, path: path, store: st, err: err}
		}
	}
	st, err := session.NewFileIndex(session.DefaultSessionsRoot()).Open(path)
	if err != nil {
		m.pushLine(styleError.Render("resume session: " + err.Error()))
		return m, nil
	}
	if err := m.switchSession(st); err != nil {
		_ = st.Close()
		m.pushLine(styleError.Render("resume session: " + err.Error()))
		return m, nil
	}
	m.lastStatus = "resumed " + shortSessionID(st.ID())
	return m, nil
}

func (m *Model) switchSession(st session.Store) error {
	if err := m.app.SetSession(st); err != nil {
		return err
	}
	m.startupResumeRequired = false
	m.pickSession = false
	m.sessions = nil
	m.sessionIndex = 0
	// Child runtimes are scoped to the root session. The app manager detaches
	// terminal children during a successful switch; discard their old UI
	// snapshots before restored topology for the new session is delivered.
	m.subagentViews = make(map[string]subagentViewState)
	m.subagentOrder = nil
	m.subagentFleetActivity = make(map[string][]string)
	m.subagentFleetActivityKinds = make(map[string]protocol.AgentEventType)
	m.subagentFleetActivitySpace = make(map[string]bool)
	m.subagentFleetList = protocol.SubagentList{}
	m.subagentFleetMessages = nil
	m.subagentFleetDetailState = protocol.SubagentState{}
	m.closeSubagentFleet()
	m.assistantBuf.Reset()
	m.thinkingBuf.Reset()
	m.planBuf.Reset()
	m.latestPlan = ""
	goalState, goalStateErr := m.app.GoalState()
	if goalStateErr != nil {
		m.goal = nil
		m.pushLine(styleError.Render("restore goal: " + goalStateErr.Error()))
	} else {
		m.goal = goalState
	}
	m.planPrompt = false
	m.pendingMode = nil
	m.modeSwitchReady = false
	m.modeSwitching = false
	m.toolRunning = false
	m.busy = false
	m.runStartedAt = time.Time{}
	m.transcript.GotoBottom()
	m.transcriptContent = ""
	m.transcriptSelectionLines = nil
	m.transcriptSelectionView = ""
	m.transcriptSelectionViewValid = false
	m.transcriptSelectionRendered = ""
	m.transcriptSelectionRenderedValid = false
	m.transcriptBase = ""
	m.hydrateSession()
	if err := m.app.ReadyGoal(); err != nil {
		// SetSession has already committed this store across App, Agent, Goal,
		// permissions, and subagents. Readiness failures are diagnostics; returning
		// an error would make the caller close the now-active store.
		m.pushLine(styleError.Render("continue restored goal: " + err.Error()))
	}
	if m.goal != nil && (m.goal.Status == protocol.GoalPaused || m.goal.Status == protocol.GoalBlocked || m.goal.Status == protocol.GoalUsageLimited) {
		m.pushLine(styleFooter.Render(fmt.Sprintf("Resume %s goal? Use /goal resume to continue.", m.goal.Status)))
	}
	return nil
}

func (m *Model) inlineSessionKey() string {
	if m.app == nil || m.app.Session == nil {
		return ""
	}
	key := m.app.Session.ID()
	if branches, ok := m.app.Session.(session.BranchStore); ok {
		if list, err := branches.Branches(); err == nil {
			for _, branch := range list {
				if branch.Active {
					return key + "\x00" + branch.ID
				}
			}
		}
	}
	return key
}

func boundedInlineHydration(hydrated []string, common int, switched bool) []string {
	common = min(max(0, common), len(hydrated))
	rows := hydrated[common:]
	const hydrationSegmentLimit = 2000
	omitted := 0
	if len(rows) > hydrationSegmentLimit {
		omitted = len(rows) - hydrationSegmentLimit
		rows = rows[len(rows)-hydrationSegmentLimit:]
	}
	out := make([]string, 0, len(rows)+2)
	if switched {
		out = append(out, styleFooter.Render("── switched transcript ──"))
	}
	if omitted > 0 {
		out = append(out, styleFooter.Render(fmt.Sprintf("── %d older transcript segments omitted ──", omitted)))
	}
	return append(out, rows...)
}

func (m *Model) hydrateSession() {
	m.clearTranscriptSelection()
	if m.app == nil || m.app.Agent == nil {
		m.lines = nil
		m.inlineCommitted = 0
		m.inlinePrintEnd = 0
		m.inlinePrintInFlight = false
		m.inlinePrintGeneration++
		m.inlineHeaderPending = false
		m.transcriptBaseDirty = true
		m.transcriptDirty = true
		m.refreshTranscript()
		return
	}
	messages, err := m.app.Agent.Messages()
	if err != nil {
		m.pushLine(styleError.Render("session read: " + err.Error()))
		return
	}
	messageIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
	}
	key := m.inlineSessionKey()
	if m.inlineTranscript && key != "" && key == m.inlineHistoryKey {
		// Live events already committed this branch's new rows. Track durable
		// identity for a future branch switch without replaying history now.
		m.inlineDurableMessageIDs = messageIDs
		m.refreshContextUsage(messages)
		return
	}
	hadPrintedHistory := m.inlineTranscript && m.inlineEverCommitted
	m.inlineCommitted = 0
	m.inlinePrintEnd = 0
	m.inlinePrintInFlight = false
	m.inlinePrintGeneration++
	m.latestPlan = ""
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshContextUsage(messages)
	hydrated := make([]string, 0, len(messages))
	hydratedIDs := make([]string, 0, len(messages))
	appendHydrated := func(id, row string) {
		hydrated = append(hydrated, row)
		hydratedIDs = append(hydratedIDs, id)
	}
	for _, msg := range messages {
		switch msg.Role {
		case protocol.RoleUser:
			text := sessionMessageText(msg)
			images := sessionMessageImageCount(msg)
			if text != "" || images > 0 {
				if text == "" {
					text = fmt.Sprintf("[%d image(s)]", images)
				} else if images > 0 {
					text += fmt.Sprintf(" [%d image(s)]", images)
				}
				appendHydrated(msg.ID, styleUser.Render("› "+text))
			}
		case protocol.RoleAssistant:
			if thinking := sessionMessageThinking(msg); thinking != "" {
				appendHydrated(msg.ID, m.renderThinkingBody(thinking))
			}
			for _, block := range msg.Content {
				switch block.Type {
				case protocol.BlockText:
					if strings.TrimSpace(block.Text) != "" {
						appendHydrated(msg.ID, m.renderAssistantBody(block.Text))
					}
				case protocol.BlockPlan:
					if strings.TrimSpace(block.Text) != "" {
						m.latestPlan = block.Text
						appendHydrated(msg.ID, m.renderPlanBody(block.Text))
					}
				}
			}
		case protocol.RoleTool:
			// Live tool cards include transient timing/progress/output previews
			// that cannot be reconstructed exactly after persistence. Excluding a
			// synthetic replacement keeps shared branch prefix detection semantic
			// and prevents the remaining history from replaying after a tool fork.
		}
	}
	m.lines = hydrated
	if m.inlineTranscript {
		commonRows := 0
		if hadPrintedHistory {
			commonMessages := 0
			limit := min(len(m.inlineDurableMessageIDs), len(messageIDs))
			for commonMessages < limit && m.inlineDurableMessageIDs[commonMessages] == messageIDs[commonMessages] {
				commonMessages++
			}
			shared := make(map[string]struct{}, commonMessages)
			for _, id := range messageIDs[:commonMessages] {
				shared[id] = struct{}{}
			}
			for commonRows < len(hydratedIDs) {
				if _, ok := shared[hydratedIDs[commonRows]]; !ok {
					break
				}
				commonRows++
			}
		}
		m.inlineHistoryKey = key
		m.inlineCanonicalLines = append([]string(nil), hydrated...)
		m.inlineDurableMessageIDs = messageIDs
		m.inlineHeaderPending = true
		m.lines = boundedInlineHydration(hydrated, commonRows, hadPrintedHistory)
	}
	m.refreshTranscript()
}

func sessionMessageText(msg protocol.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == protocol.BlockText {
			b.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func sessionMessageImageCount(msg protocol.Message) int {
	count := 0
	for _, block := range msg.Content {
		if block.Type == protocol.BlockImage {
			count++
		}
	}
	return count
}

func sessionMessageThinking(msg protocol.Message) string {
	var parts []string
	for _, block := range msg.Content {
		if block.Type == protocol.BlockThinking && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (m *Model) refreshContextUsageFromSession() {
	if m.app == nil || m.app.Agent == nil {
		m.applyContextUsageSnapshot(contextUsageSnapshot{})
		return
	}
	projected, err := m.app.Agent.ContextMessages()
	if err != nil {
		return
	}
	compacted := len(projected) > 0 && projected[0].Role == protocol.RoleCustom
	m.refreshProjectedContextUsage(projected, compacted)
}

func (m *Model) scheduleContextUsageRefresh() tea.Cmd {
	if !m.contextRefreshNeeded || m.contextRefreshPending || m.app == nil || m.app.Agent == nil {
		return nil
	}
	m.contextRefreshNeeded = false
	m.contextRefreshPending = true
	version := m.contextRefreshVersion
	a := m.app.Agent
	return func() tea.Msg {
		projected, err := a.ContextMessages()
		compacted := len(projected) > 0 && projected[0].Role == protocol.RoleCustom
		return contextUsageRefreshMsg{version: version, snapshot: projectedContextUsage(projected, compacted), err: err}
	}
}

func (m *Model) refreshContextUsage(messages []protocol.Message) {
	projected := messages
	compacted := false
	if contextStore, ok := m.app.Session.(session.ContextStore); ok {
		if contextMessages, err := contextStore.ContextMessages(); err == nil {
			projected = contextMessages
			compacted = len(contextMessages) != len(messages) ||
				(len(contextMessages) > 0 && contextMessages[0].Role == protocol.RoleCustom)
		}
	}
	m.refreshProjectedContextUsage(projected, compacted)
}

func (m *Model) refreshProjectedContextUsage(projected []protocol.Message, compacted bool) {
	m.applyContextUsageSnapshot(projectedContextUsage(projected, compacted))
}

func (m *Model) applyContextUsageSnapshot(snapshot contextUsageSnapshot) {
	m.lastUsage = snapshot.usage.Clone()
	m.lastRequestUsage = snapshot.usage.Clone()
	m.contextTokens = snapshot.tokens
	m.contextEstimated = snapshot.estimated
	m.contextRefreshVersion++
}

func projectedContextUsage(projected []protocol.Message, compacted bool) contextUsageSnapshot {
	if compacted {
		tokens := estimateContextTokens(projected)
		return contextUsageSnapshot{tokens: tokens, estimated: tokens > 0}
	}
	for i := len(projected) - 1; i >= 0; i-- {
		usage := projected[i].Usage
		if usage == nil {
			continue
		}
		snapshot := contextUsageSnapshot{usage: usage.Clone(), tokens: contextTokensFromUsage(*usage)}
		if i+1 < len(projected) {
			snapshot.tokens += estimateContextTokens(projected[i+1:])
			snapshot.estimated = true
		}
		return snapshot
	}
	tokens := estimateContextTokens(projected)
	return contextUsageSnapshot{tokens: tokens, estimated: tokens > 0}
}

func contextTokensFromUsage(usage protocol.Usage) int {
	if usage.Total > 0 {
		return usage.Total
	}
	return usage.Input + usage.Output
}

func estimateContextTokens(messages []protocol.Message) int {
	chars := 0
	for _, message := range messages {
		chars += len(message.Role) + 8
		for _, block := range message.Content {
			chars += len(block.Type) + len(block.Text) + len(block.Name) +
				len(block.ToolCallID) + len(block.Arguments) + 8
		}
	}
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}

func shortSessionID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func renderToolOutput(toolName, output string, width int) string {
	if summary, handled := renderSubagentToolSummary(toolName, output); handled {
		return summary
	}
	if toolHasDiffPreview(toolName, output) {
		return renderEditDiff(output, width)
	}
	return renderToolOutputPreview(output, width)
}

func renderSubagentToolSummary(toolName, output string) (string, bool) {
	switch toolName {
	case "spawn_agent", "send_message", "followup_task", "interrupt_agent":
		return "", true
	case "wait_agent":
		var result protocol.WaitSubagentsResult
		if json.Unmarshal([]byte(output), &result) != nil {
			return "", false
		}
		status := formatSubagentCounts(result.Running, result.Queued, result.Terminal)
		switch {
		case result.TimedOut:
			status += " · timed out"
		case result.AllTerminal:
			status += " · all finished"
		default:
			status += " · activity received"
		}
		return styleHeaderDim.Render("  ↳ " + status), true
	case "list_agents":
		var result struct {
			Running         int `json:"running"`
			Queued          int `json:"queued"`
			Terminal        int `json:"terminal"`
			ConcurrentLimit int `json:"concurrent_limit"`
			AgentLimit      int `json:"agent_limit"`
		}
		if json.Unmarshal([]byte(output), &result) != nil {
			return "", false
		}
		status := formatSubagentCounts(result.Running, result.Queued, result.Terminal)
		status += fmt.Sprintf(" · capacity %d/%d · identities %d/%d", result.Running, result.ConcurrentLimit, result.Running+result.Queued+result.Terminal, result.AgentLimit)
		return styleHeaderDim.Render("  ↳ " + status), true
	default:
		return "", false
	}
}

func formatSubagentCounts(running, queued, terminal int) string {
	return fmt.Sprintf("agents: %d running · %d queued · %d finished", running, queued, terminal)
}

func toolHasDiffPreview(toolName, output string) bool {
	return (toolName == "edit" || toolName == "write") && looksLikeEditDiff(output)
}

func looksLikeEditDiff(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			return true
		}
	}
	return false
}

// renderEditDiff colors the line-oriented edit preview so additions and
// deletions are immediately visible without making the model-facing result
// noisy. Context stays muted, matching the surrounding transcript.
func renderEditDiff(output string, width int) string {
	// Keep the leading marker on context lines; only remove framing newlines.
	output = strings.Trim(output, "\n")
	if output == "" {
		return ""
	}
	output = sanitizeToolPreview(output, 8*1024)
	lines := strings.Split(output, "\n")
	if len(lines) > 80 {
		lines = append(lines[:80], "... [diff preview truncated]")
	}
	maxWidth := width - 2
	if maxWidth < 20 {
		maxWidth = 20
	}
	for i, line := range lines {
		line = truncateRunes(line, maxWidth)
		switch {
		case strings.HasPrefix(line, "-"):
			lines[i] = styleDiffDel.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = styleDiffAdd.Render(line)
		default:
			lines[i] = styleHeaderDim.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// renderToolOutputPreview keeps tool cards useful without dumping a whole
// read/grep result into the transcript. The complete output remains available
// to the model through the session and to SDK/RPC subscribers.
func renderToolOutputPreview(output string, width int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	output = sanitizeToolPreview(output, 2400)
	lines := strings.Split(output, "\n")
	maxLines := 6
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "… [preview truncated]")
	}
	maxWidth := width - 8
	if maxWidth < 20 {
		maxWidth = 20
	}
	for i, line := range lines {
		lines[i] = "  │ " + truncateRunes(line, maxWidth)
	}
	return styleHeaderDim.Render(strings.Join(lines, "\n"))
}

// sanitizeToolPreview removes terminal controls before tool output is rendered
// in the TUI. Tool output is untrusted repository/process data.
func sanitizeToolPreview(value string, maxBytes int) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
		if b.Len() >= maxBytes {
			break
		}
	}
	return b.String()
}

func (m *Model) startMCPInfo() (tea.Model, tea.Cmd) {
	statuses := m.app.MCPStatuses
	if m.app.MCPManager != nil {
		statuses = m.app.MCPManager.Statuses()
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	items := make([]statusInfoItem, 0, len(statuses))
	for _, status := range statuses {
		state := "failed"
		if status.Connected {
			state = "connected"
		} else if status.Message == "disabled" || strings.HasPrefix(status.Message, "disabled by") {
			state = "disabled"
		}
		label := fmt.Sprintf("%s  ·  %s  ·  %s", status.ID, state, status.Transport)
		if status.ProtocolVersion != "" {
			label += "  ·  " + status.ProtocolVersion
		}
		detail := strings.TrimSpace(status.ServerName + " " + status.ServerVersion)
		if status.ToolCount > 0 {
			detail += fmt.Sprintf(" · %d tools", status.ToolCount)
		}
		if len(status.Capabilities) > 0 {
			detail += " · " + strings.Join(status.Capabilities, ", ")
		}
		if status.Message != "" {
			detail += " · " + status.Message
		}
		items = append(items, statusInfoItem{Label: label, Detail: strings.Trim(detail, " ·")})
	}
	return m.startInfoPicker("MCP servers", items)
}

func (m *Model) startSkillsInfo() (tea.Model, tea.Cmd) {
	var items []statusInfoItem
	if m.app.Skills != nil {
		for _, skill := range m.app.Skills.Inventory() {
			state := "enabled"
			if !skill.Enabled {
				state = "disabled"
			}
			label := fmt.Sprintf("%s  ·  %s  ·  %s/%s", skill.Name, state, skill.Scope, skill.Source)
			detail := skill.Description + " · " + skill.Location
			if skill.DisabledBy != "" {
				detail += " · " + skill.DisabledBy
			}
			items = append(items, statusInfoItem{Label: label, Detail: detail})
		}
	}
	return m.startInfoPicker("Agent Skills", items)
}

func (m *Model) startInfoPicker(title string, items []statusInfoItem) (tea.Model, tea.Cmd) {
	if len(items) == 0 {
		m.pushLine(styleFooter.Render(strings.ToLower(title) + ": none configured or discovered"))
		return m, nil
	}
	m.pickInfo, m.infoTitle, m.infoItems, m.infoIndex = true, title, items, 0
	m.infoLoading = false
	m.compVisible = false
	return m, nil
}

func (m *Model) handleInfoPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	if m.infoLoading {
		if msg.Type == tea.KeyEsc {
			m.closeInfoPicker()
			m.pickerGeneration++
		}
		return m, nil
	}
	count := len(m.infoItems)
	if count == 0 {
		m.closeInfoPicker()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.infoIndex = (m.infoIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.infoIndex = (m.infoIndex + 1) % count
	case tea.KeyPgUp:
		m.infoIndex -= m.infoPickerVisibleItems()
		if m.infoIndex < 0 {
			m.infoIndex = 0
		}
	case tea.KeyPgDown:
		m.infoIndex += m.infoPickerVisibleItems()
		if m.infoIndex >= count {
			m.infoIndex = count - 1
		}
	case tea.KeyHome:
		m.infoIndex = 0
	case tea.KeyEnd:
		m.infoIndex = count - 1
	case tea.KeyEsc:
		m.closeInfoPicker()
	case tea.KeyEnter:
		if strings.HasPrefix(m.infoTitle, "Agents") && m.infoIndex < len(m.infoAgentTargets) {
			target := m.infoAgentTargets[m.infoIndex]
			m.closeInfoPicker()
			return m, m.inspectAgent(target)
		} else {
			m.closeInfoPicker()
		}
	}
	return m, nil
}

func (m *Model) closeInfoPicker() {
	m.pickInfo = false
	m.infoLoading = false
	m.infoTitle = ""
	m.infoItems = nil
	m.infoIndex = 0
	m.infoAgentTargets = nil
}

func (m *Model) inspectAgent(target string) tea.Cmd {
	if m.asyncIO {
		m.pickerGeneration++
		generation := m.pickerGeneration
		m.lastStatus = "inspecting"
		return func() tea.Msg {
			state, err := m.app.Subagent(m.ctx, target)
			if err != nil {
				return subagentInspectMsg{generation: generation, err: err}
			}
			var messages []protocol.Message
			var messageErr error
			if state.Agent.Path == protocol.RootAgentPath {
				messages, messageErr = m.app.Agent.Messages()
			} else {
				messages, messageErr = m.app.SubagentMessages(m.ctx, target)
			}
			return subagentInspectMsg{generation: generation, state: state, messages: messages, messageErr: messageErr}
		}
	}
	state, err := m.app.Subagent(m.ctx, target)
	if err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return nil
	}
	var messages []protocol.Message
	var messageErr error
	if state.Agent.Path == protocol.RootAgentPath {
		messages, messageErr = m.app.Agent.Messages()
	} else {
		messages, messageErr = m.app.SubagentMessages(m.ctx, target)
	}
	m.pushLine(styleFooter.Render(renderSubagentInspection(state, messages, messageErr, m.app.Cfg.Subagents.Durable, time.Now())))
	return nil
}

func (m *Model) infoPickerVisibleItems() int {
	visible := m.height - 14
	if m.inlineModalOverlay() {
		visible = m.availableOverlayHeight() - 3 // title, selected detail, hint
	}
	if visible < 1 {
		visible = 1
	}
	if visible > len(m.infoItems) {
		visible = len(m.infoItems)
	}
	return visible
}

func (m *Model) infoWindow() (start, end int) {
	visible := m.infoPickerVisibleItems()
	if len(m.infoItems) <= visible {
		return 0, len(m.infoItems)
	}
	start = m.infoIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(m.infoItems) {
		start = len(m.infoItems) - visible
	}
	return start, start + visible
}

func (m *Model) infoPickerRows() int {
	if !m.pickInfo {
		return 0
	}
	if m.infoLoading {
		return 2
	}
	start, end := m.infoWindow()
	return 3 + end - start
}

func (m *Model) renderInfoPicker() string {
	if !m.pickInfo {
		return ""
	}
	if m.infoLoading {
		return styleHeaderDim.Render(m.infoTitle + "\n  loading…")
	}
	if len(m.infoItems) == 0 {
		return ""
	}
	start, end := m.infoWindow()
	width := max(1, m.width-2)
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render(truncateRunes(fmt.Sprintf("%s (%d)", m.infoTitle, len(m.infoItems)), width)) + "\n")
	for i := start; i < end; i++ {
		line := truncateRunes(m.infoItems[i].Label, max(8, m.width-4))
		if i == m.infoIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleHeaderDim.Render(truncateRunes("  "+m.infoItems[m.infoIndex].Detail, width)) + "\n")
	b.WriteString(styleFooter.Render(truncateRunes("(↑/↓ inspect · Enter/Esc close)", width)))
	return b.String()
}

func (m *Model) sessionPickerBodyMin() int {
	// Below this size the session selector takes priority over transcript
	// history so the whole picker still fits in the terminal.
	if m.height < 14 {
		return 1
	}
	return 3
}

func (m *Model) sessionPickerMaxRows() int {
	if m.inlineModalOverlay() {
		return max(3, m.availableOverlayHeight())
	}
	rows := m.height - 8 - m.sessionPickerBodyMin()
	if rows < 3 {
		return 3
	}
	return rows
}

func (m *Model) sessionPickerVisibleItems() int {
	total := len(m.sessions)
	if total == 0 {
		return 0
	}
	// Keep rows for the title and hint. Reserve two more rows for scroll
	// markers when the terminal is tall enough to show them.
	visible := m.sessionPickerMaxRows() - 2
	if m.sessionPickerMaxRows() >= 5 && total > visible {
		visible -= 2
	}
	if visible < 1 {
		visible = 1
	}
	if visible > total {
		visible = total
	}
	return visible
}

func (m *Model) sessionWindow() (start, end int) {
	total := len(m.sessions)
	visible := m.sessionPickerVisibleItems()
	if total == 0 || total <= visible {
		return 0, total
	}
	start = m.sessionIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m *Model) sessionPickerRows() int {
	if !m.pickSession {
		return 0
	}
	if m.sessionLoading {
		return 2
	}
	start, end := m.sessionWindow()
	rows := 2 + end - start // title + entries + hint
	if m.sessionPickerMaxRows() >= 5 {
		if start > 0 {
			rows++
		}
		if end < len(m.sessions) {
			rows++
		}
	}
	return rows
}

func (m *Model) renderSessionPicker() string {
	if !m.pickSession {
		return ""
	}
	if m.sessionLoading {
		return styleHeaderDim.Render("sessions\n  loading sessions…")
	}
	start, end := m.sessionWindow()
	var b strings.Builder
	pickerWidth := max(1, m.width-2)
	title := truncateRunes(fmt.Sprintf("sessions (%d)", len(m.sessions)), pickerWidth)
	b.WriteString(styleHeaderDim.Render(title) + "\n")
	showMarkers := m.sessionPickerMaxRows() >= 5
	if showMarkers && start > 0 {
		b.WriteString(styleHeaderDim.Render(truncateRunes("  ↑ more sessions", pickerWidth)) + "\n")
	}
	for i := start; i < end; i++ {
		line := formatSessionPickerInfo(m.sessions[i], currentSessionID(m.app))
		line = truncateRunes(line, max(8, m.width-4))
		if i == m.sessionIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	if showMarkers && end < len(m.sessions) {
		b.WriteString(styleHeaderDim.Render(truncateRunes("  ↓ more sessions", pickerWidth)) + "\n")
	}
	hint := "(↑/↓ choose · PgUp/PgDn scroll · Enter resume · r rename · Esc cancel)"
	if m.sessionRenaming {
		hint = "Rename session: " + m.sessionRenameInput + "_"
	}
	b.WriteString(styleFooter.Render(truncateRunes(hint, pickerWidth)))
	return strings.TrimSuffix(b.String(), "\n")
}

func formatSessionPickerInfo(info session.SessionInfo, activeID string) string {
	label := shortSessionID(info.ID)
	if info.Name != "" {
		label = info.Name + "  ·  " + label
	}
	label += fmt.Sprintf("  ·  %d messages", info.Messages)
	if info.ID == activeID {
		label += "  ✓ active"
	}
	return label
}

func (m *Model) startTreePick() (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("tree: wait for the current turn to finish"))
		return m, nil
	}
	if m.asyncIO {
		m.pickTree = true
		m.treeLoading = true
		m.branches = nil
		m.pickerGeneration++
		generation := m.pickerGeneration
		return m, func() tea.Msg {
			branches, err := m.app.Agent.Branches()
			return branchListMsg{generation: generation, branches: branches, err: err}
		}
	}
	branches, err := m.app.Agent.Branches()
	if err != nil {
		m.pushLine(styleError.Render("tree: " + err.Error()))
		return m, nil
	}
	if len(branches) == 0 {
		m.pushLine(styleFooter.Render("tree: no branches"))
		return m, nil
	}
	m.branches = orderBranches(branches)
	m.branchIndex = 0
	for i, branch := range m.branches {
		if branch.Active {
			m.branchIndex = i
			break
		}
	}
	m.pickTree = true
	m.compVisible = false
	return m, nil
}

func (m *Model) handleTreePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.treeLoading {
		if msg.Type == tea.KeyEsc {
			m.pickTree = false
			m.treeLoading = false
			m.pickerGeneration++
		}
		return m, nil
	}
	count := len(m.branches)
	if count == 0 {
		m.pickTree = false
		return m, nil
	}
	if m.branchAction != "" {
		if m.branchAction == "delete" {
			if keyMatches(msg, m.keys.Close) || (msg.Type == tea.KeyRunes && strings.EqualFold(string(msg.Runes), "n")) {
				m.branchAction = ""
				return m, nil
			}
			if keyMatches(msg, m.keys.Confirm) {
				return m.executeTreeAction()
			}
			return m, nil
		}
		switch {
		case keyMatches(msg, m.keys.Close):
			m.branchAction, m.branchInput = "", ""
		case keyMatches(msg, m.keys.Accept):
			return m.executeTreeAction()
		case msg.Type == tea.KeyBackspace:
			r := []rune(m.branchInput)
			if len(r) > 0 {
				m.branchInput = string(r[:len(r)-1])
			}
		case msg.Type == tea.KeyRunes:
			if len([]rune(m.branchInput))+len(msg.Runes) <= 64 {
				m.branchInput += string(msg.Runes)
			}
		}
		return m, nil
	}
	action := pickerKeyActionWithMap(msg, m.keys)
	if next, handled := movePicker(m.branchIndex, count, action, m.treePickerVisibleItems()); handled && action != pickerAccept && action != pickerClose {
		m.branchIndex = next
		return m, nil
	}
	if action == pickerClose {
		m.pickTree = false
		m.branches = nil
		return m, nil
	}
	if action == pickerAccept {
		branch := m.branches[m.branchIndex]
		if m.asyncIO {
			m.treeLoading = true
			m.pickerGeneration++
			gen := m.pickerGeneration
			return m, func() tea.Msg {
				return branchActionMsg{generation: gen, branch: branch, action: "select", err: m.app.SelectBranch(branch.ID)}
			}
		}
		if err := m.app.SelectBranch(branch.ID); err != nil {
			m.pushLine(styleError.Render("tree: " + err.Error()))
			return m, nil
		}
		m.pickTree = false
		m.branches = nil
		m.hydrateSession()
		m.lastStatus = "selected branch " + branch.Name
		return m, nil
	}
	switch {
	case keyMatches(msg, m.keys.BranchFork):
		m.branchAction = "fork"
		m.branchInput = ""
	case keyMatches(msg, m.keys.BranchRename):
		m.branchAction = "rename"
		m.branchInput = m.branches[m.branchIndex].Name
	case keyMatches(msg, m.keys.BranchDelete):
		m.branchAction = "delete"
	}
	return m, nil
}

func (m *Model) executeTreeAction() (tea.Model, tea.Cmd) {
	selected := m.branches[m.branchIndex]
	action, input := m.branchAction, strings.TrimSpace(m.branchInput)
	m.branchAction, m.branchInput = "", ""
	m.treeLoading = m.asyncIO
	m.pickerGeneration++
	gen := m.pickerGeneration
	run := func() branchActionMsg {
		switch action {
		case "fork":
			branch, err := m.app.ForkBranchWithOptions(protocol.BranchForkOptions{SourceBranchID: selected.ID, FromEntryID: selected.TipID, Name: input})
			return branchActionMsg{generation: gen, branch: branch, action: action, err: err}
		case "rename":
			branch, err := m.app.RenameBranch(selected.ID, input)
			return branchActionMsg{generation: gen, branch: branch, action: action, err: err}
		case "delete":
			err := m.app.DeleteBranch(selected.ID)
			return branchActionMsg{generation: gen, action: action, err: err}
		default:
			return branchActionMsg{generation: gen, err: errors.New("unknown branch action")}
		}
	}
	if m.asyncIO {
		return m, func() tea.Msg { return run() }
	}
	return m, func() tea.Msg { return run() }
}

func orderBranches(branches []protocol.SessionBranch) []protocol.SessionBranch {
	byParent := map[string][]protocol.SessionBranch{}
	for _, branch := range branches {
		byParent[branch.ParentID] = append(byParent[branch.ParentID], branch)
	}
	for parent := range byParent {
		sort.SliceStable(byParent[parent], func(i, j int) bool {
			if byParent[parent][i].CreatedAt == byParent[parent][j].CreatedAt {
				return byParent[parent][i].ID < byParent[parent][j].ID
			}
			return byParent[parent][i].CreatedAt < byParent[parent][j].CreatedAt
		})
	}
	var out []protocol.SessionBranch
	seen := map[string]bool{}
	var visit func(string)
	visit = func(parent string) {
		for _, branch := range byParent[parent] {
			if seen[branch.ID] {
				continue
			}
			seen[branch.ID] = true
			out = append(out, branch)
			visit(branch.ID)
		}
	}
	visit("")
	for _, branch := range branches {
		if !seen[branch.ID] {
			out = append(out, branch)
		}
	}
	return out
}

func branchDepth(branches []protocol.SessionBranch, branch protocol.SessionBranch) int {
	parents := map[string]string{}
	for _, b := range branches {
		parents[b.ID] = b.ParentID
	}
	depth := 0
	seen := map[string]bool{}
	for parent := branch.ParentID; parent != "" && !seen[parent]; parent = parents[parent] {
		seen[parent] = true
		depth++
		if depth > 8 {
			break
		}
	}
	return depth
}

func (m *Model) treePickerVisibleItems() int {
	total := len(m.branches)
	if total == 0 {
		return 0
	}
	visible := m.height - 12
	if m.inlineModalOverlay() {
		visible = m.availableOverlayHeight() - 4 // title, two scroll markers, hint
	}
	if visible < 1 {
		visible = 1
	}
	if visible > total {
		visible = total
	}
	return visible
}

func (m *Model) treeWindow() (start, end int) {
	total := len(m.branches)
	visible := m.treePickerVisibleItems()
	if total == 0 || total <= visible {
		return 0, total
	}
	start = m.branchIndex - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m *Model) treePickerRows() int {
	if !m.pickTree {
		return 0
	}
	if m.treeLoading {
		return 2
	}
	start, end := m.treeWindow()
	rows := 2 + end - start
	if start > 0 {
		rows++
	}
	if end < len(m.branches) {
		rows++
	}
	return rows
}

func (m *Model) renderTreePicker() string {
	if !m.pickTree {
		return ""
	}
	if m.treeLoading {
		return styleHeaderDim.Render("branches\n  loading branches…")
	}
	start, end := m.treeWindow()
	width := max(1, m.width-2)
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render(truncateRunes(fmt.Sprintf("branches (%d)", len(m.branches)), width)) + "\n")
	if start > 0 {
		b.WriteString(styleHeaderDim.Render("  ↑ more branches") + "\n")
	}
	for i := start; i < end; i++ {
		branch := m.branches[i]
		marker := "  "
		if branch.Active {
			marker = "✓ "
		}
		name := branch.Name
		if name == "" {
			name = branch.ID
		}
		indent := strings.Repeat("  ", branchDepth(m.branches, branch))
		connector := "└─ "
		if branch.ParentID == "" {
			connector = ""
		}
		line := fmt.Sprintf("%s%s%s%s  ·  %s  ·  %d messages", marker, indent, connector, name, shortSessionID(branch.ID), branch.Messages)
		if branch.Preview != "" {
			line += "  ·  " + branch.Preview
		}
		line = truncateRunes(line, max(8, m.width-4))
		if i == m.branchIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	if end < len(m.branches) {
		b.WriteString(styleHeaderDim.Render("  ↓ more branches") + "\n")
	}
	hint := fmt.Sprintf("(%s choose · %s switch · %s fork · %s rename · %s delete · %s cancel)", m.keys.PickerDown.Help().Key, m.keys.Accept.Help().Key, m.keys.BranchFork.Help().Key, m.keys.BranchRename.Help().Key, m.keys.BranchDelete.Help().Key, m.keys.Close.Help().Key)
	if m.branchAction == "fork" {
		hint = "Fork name (blank = automatic): " + m.branchInput + "_"
	}
	if m.branchAction == "rename" {
		hint = "Rename: " + m.branchInput + "_"
	}
	if m.branchAction == "delete" {
		hint = "Delete selected leaf branch? " + m.keys.Confirm.Help().Key + "/" + m.keys.Close.Help().Key
	}
	b.WriteString(styleFooter.Render(truncateRunes(hint, width)))
	return strings.TrimSuffix(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// Interactive permission picker (allow/deny without typing)
// ---------------------------------------------------------------------------

// permChoice values for the permission picker.
const (
	permChoiceAllow = iota
	permChoiceAlways
	permChoiceDeny
	permChoices
)

// handlePermissionPick resolves an interactive permission request with
// arrows + Enter. Esc denies (safe default).
func (m *Model) handlePermissionPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.permChoice = (m.permChoice - 1 + permChoices) % permChoices
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.permChoice = (m.permChoice + 1) % permChoices
	case tea.KeyEnter:
		m.resolvePermission()
	case tea.KeyEsc:
		m.permChoice = permChoiceDeny
		m.resolvePermission()
	}
	return m, nil
}

// resolvePermission delivers the selected decision to the blocked asker and
// clears the picker.
func (m *Model) resolvePermission() {
	d := permission.DecisionDeny
	switch m.permChoice {
	case permChoiceAllow:
		d = permission.DecisionAllow
	case permChoiceAlways:
		d = permission.DecisionAllowAlways
	}
	m.permPending = false
	m.permRequest = nil
	m.permAgent = nil
	if m.asker != nil {
		_ = m.asker.Respond(d)
	}
	m.pushLine(styleFooter.Render("permission: " + string(d)))
}

// renderPermissionPicker renders the allow/deny selector.
func (m *Model) renderPermissionPicker() string {
	if !m.permPending || m.permRequest == nil {
		return ""
	}
	req := m.permRequest
	label := "🔐 " + req.Tool + " · " + string(req.Risk)
	if m.permAgent != nil {
		label += " · " + string(m.permAgent.Path)
	}
	if len(req.Paths) > 0 {
		label += " · " + strings.Join(req.Paths, ", ")
	}
	if req.Reason != "" {
		label += " · " + req.Reason
	}
	var b strings.Builder
	b.WriteString(styleTool.Render(label) + "\n")
	options := []struct {
		id   int
		name string
		hint string
	}{
		{permChoiceAllow, "Allow", "this request"},
		{permChoiceAlways, "Allow always", "all matching requests this session"},
		{permChoiceDeny, "Deny", "this request"},
	}
	for _, o := range options {
		line := o.name
		if o.hint != "" {
			line += "  (" + o.hint + ")"
		}
		if o.id == m.permChoice {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleFooter.Render("(↑/↓ choose, Enter confirm, Esc deny)"))
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) startPermissionModePick() (tea.Model, tea.Cmd) {
	m.pickPermissionMode = true
	m.permissionModeIndex = 0
	if m.app != nil {
		switch m.app.Perm.Mode() {
		case permission.ModeAllow:
			m.permissionModeIndex = 1
		case permission.ModeDeny:
			m.permissionModeIndex = 2
		}
	}
	m.compVisible = false
	return m, nil
}

func (m *Model) handlePermissionModePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	const count = 3
	switch msg.Type {
	case tea.KeyUp, tea.KeyLeft, tea.KeyShiftTab:
		m.permissionModeIndex = (m.permissionModeIndex - 1 + count) % count
	case tea.KeyDown, tea.KeyRight, tea.KeyTab:
		m.permissionModeIndex = (m.permissionModeIndex + 1) % count
	case tea.KeyEnter:
		m.applyPermissionMode()
	case tea.KeyEsc:
		m.pickPermissionMode = false
	}
	return m, nil
}

func (m *Model) applyPermissionMode() {
	modes := []permission.Mode{permission.ModeAsk, permission.ModeAllow, permission.ModeDeny}
	mode := modes[m.permissionModeIndex]
	if err := m.setPermissionMode(mode, true); err != nil {
		m.pushLine(styleError.Render(err.Error()))
		return
	}
	m.pickPermissionMode = false
}

func (m *Model) setPermissionMode(mode permission.Mode, announce bool) error {
	if m.app == nil {
		return fmt.Errorf("permissions: app is not ready")
	}
	if mode != permission.ModeAsk && mode != permission.ModeAllow && mode != permission.ModeDeny {
		return fmt.Errorf("invalid permission mode %q", mode)
	}
	candidate := m.app.PersistedCfg
	candidate.PermissionMode = string(mode)
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			return fmt.Errorf("persist permissions: %w", err)
		}
	}
	if err := m.app.SetPermissionDefault(mode); err != nil {
		return err
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.PermissionMode = candidate.PermissionMode
	if announce {
		m.pushLine(styleFooter.Render("permission mode: " + string(mode)))
	}
	return nil
}

func (m *Model) renderPermissionModePicker() string {
	if !m.pickPermissionMode {
		return ""
	}
	modes := []permission.Mode{permission.ModeAsk, permission.ModeAllow, permission.ModeDeny}
	var b strings.Builder
	b.WriteString(styleHeaderDim.Render("permissions") + "\n")
	for i, mode := range modes {
		line := string(mode)
		if i == m.permissionModeIndex {
			b.WriteString(styleCompletionSelected.Render("› " + line))
		} else {
			b.WriteString(styleCompletion.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleFooter.Render("(↑/↓ choose, Enter apply, Esc cancel)"))
	return strings.TrimSuffix(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// Unified settings panel
// ---------------------------------------------------------------------------

const (
	settingsModel = iota
	settingsTheme
	settingsThinking
	settingsReasoningSummary
	settingsTextVerbosity
	settingsPermission
	settingsSubagents
	settingsSubagentConcurrency
	settingsSkills
	settingsCount
)

func (m *Model) startSettings() (tea.Model, tea.Cmd) {
	if m.app == nil {
		return m, nil
	}
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("settings: wait for the current turn to finish"))
		return m, nil
	}
	m.pickSettings = true
	m.settingsIndex = 0
	m.settingsStatus = ""
	m.settingsError = ""
	m.compVisible = false
	return m, nil
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = normalizePickerKeyWithMap(msg, m.keys)
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.settingsIndex = (m.settingsIndex - 1 + settingsCount) % settingsCount
	case tea.KeyDown, tea.KeyTab:
		m.settingsIndex = (m.settingsIndex + 1) % settingsCount
	case tea.KeyEsc:
		m.pickSettings = false
		m.settingsError = ""
		m.settingsStatus = ""
	case tea.KeyEnter:
		if m.settingsIndex == settingsModel {
			if m.compatibleLoginPending {
				m.settingsStatus = "waiting for openai-compatible model discovery"
				return m, nil
			}
			m.pickSettings = false
			m.settingsReturnToPanel = true
			return m.startModelPick()
		}
		m.cycleSetting(1)
	case tea.KeyLeft:
		if m.settingsIndex != settingsModel {
			m.cycleSetting(-1)
		}
	case tea.KeyRight:
		if m.settingsIndex != settingsModel {
			m.cycleSetting(1)
		}
	}
	return m, nil
}

func (m *Model) cycleSetting(direction int) {
	m.settingsError = ""
	m.settingsStatus = ""
	var err error
	switch m.settingsIndex {
	case settingsTheme:
		values := m.themeChoices()
		next := cycleValue(values, m.themeName, direction)
		err = m.setTheme(next, false)
		if err == nil {
			m.settingsStatus = "theme saved"
		}
	case settingsThinking:
		levels := m.app.Agent.Model().SupportedThinkingLevels()
		current := m.app.Agent.Thinking()
		next := cycleValue(levels, current, direction)
		err = m.setThinking(next, false)
		if err == nil {
			m.settingsStatus = "thinking effort saved"
		}
	case settingsReasoningSummary:
		if !m.chatGPTSettingsEnabled() {
			m.settingsStatus = "reasoning summary is available for ChatGPT only"
			return
		}
		values := protocol.KnownReasoningSummaries()
		next := cycleValue(values, m.app.Agent.ReasoningSummary(), direction)
		err = m.setReasoningSummary(next)
		if err == nil {
			m.settingsStatus = "reasoning summary saved"
		}
	case settingsTextVerbosity:
		if !m.chatGPTSettingsEnabled() {
			m.settingsStatus = "text verbosity is available for ChatGPT only"
			return
		}
		values := protocol.KnownTextVerbosities()
		next := cycleValue(values, m.app.Agent.TextVerbosity(), direction)
		err = m.setTextVerbosity(next)
		if err == nil {
			m.settingsStatus = "text verbosity saved"
		}
	case settingsPermission:
		values := []permission.Mode{permission.ModeAsk, permission.ModeAllow, permission.ModeDeny}
		next := cycleValue(values, m.app.Perm.Mode(), direction)
		err = m.setPermissionMode(next, false)
		if err == nil {
			m.settingsStatus = "permission mode saved"
		}
	case settingsSubagents:
		next := cycleValue([]bool{false, true}, m.app.Cfg.Subagents.Enabled, direction)
		err = m.setSubagentsEnabled(next)
		if err == nil {
			m.settingsStatus = "subagent setting saved; restart Snow to apply"
		}
	case settingsSubagentConcurrency:
		next := m.app.Cfg.Subagents.MaxConcurrentThreads + direction
		if next < 1 {
			next = 1
		}
		err = m.setSubagentConcurrency(next)
		if err == nil {
			m.settingsStatus = "subagent concurrency saved; restart Snow to apply"
		}
	case settingsSkills:
		next := cycleValue([]bool{true, false}, !m.app.Cfg.Skills.Disabled, direction)
		err = m.setSkillsEnabled(next)
		if err == nil {
			m.settingsStatus = "skills setting saved; restart Snow to apply"
		}
	}
	if err != nil {
		m.settingsError = err.Error()
		m.settingsStatus = ""
	}
}

func onOff(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func cycleValue[T comparable](values []T, current T, direction int) T {
	if len(values) == 0 {
		return current
	}
	index := 0
	for i, value := range values {
		if value == current {
			index = i
			break
		}
	}
	if direction < 0 {
		index = (index - 1 + len(values)) % len(values)
	} else {
		index = (index + 1) % len(values)
	}
	return values[index]
}

func (m *Model) chatGPTSettingsEnabled() bool {
	return m.app != nil && m.app.ProviderID == "chatgpt"
}

func (m *Model) loadAuxiliaryTUIConfig() {
	if m.app == nil {
		return
	}
	themes, themeDiagnostics := config.LoadThemes(config.GlobalDir(), m.app.ProjectInputRoot, m.app.ProjectAllowed)
	scopes, keyDiagnostics := config.LoadKeybindingScopes(config.GlobalDir(), m.app.ProjectInputRoot, m.app.ProjectAllowed)
	m.customThemes = themes
	m.auxDiagnostics = append(themeDiagnostics, keyDiagnostics...)
	m.keys = tuiKeys
	for _, scope := range scopes {
		keys, err := applyKeybindingOverrides(m.keys, scope.File.Bindings)
		if err != nil {
			m.auxDiagnostics = append(m.auxDiagnostics, config.Diagnostic{Path: scope.Path, Message: err.Error()})
			continue
		}
		m.keys = keys
	}
	m.editor.KeyMap.InsertNewline = m.keys.Newline
	m.userInputEditor.KeyMap.InsertNewline = m.keys.Newline
}

func (m *Model) themeChoices() []string {
	values := themeChoices()
	var custom []string
	for name := range m.customThemes {
		custom = append(custom, name)
	}
	sort.Strings(custom)
	return append(values, custom...)
}

func (m *Model) setTheme(name string, announce bool) error {
	return m.applyThemeSelection(name, announce, true)
}

func (m *Model) applyThemeSelection(name string, announce, persist bool) error {
	if _, custom := m.customThemes[name]; !custom {
		if err := config.ValidateTUITheme(name); err != nil {
			return err
		}
	}
	if name == "" {
		name = "default"
	}
	old := m.themeName
	var applyErr error
	if custom, ok := m.customThemes[name]; ok {
		applyErr = applyCustomTUITheme(custom)
	} else {
		applyErr = applyTUITheme(name)
	}
	if applyErr != nil {
		return applyErr
	}
	normalizeTextareaStyles(&m.editor)
	normalizeTextareaStyles(&m.userInputEditor)
	if m.app != nil && persist {
		candidate := m.app.PersistedCfg
		candidate.TUI.Theme = name
		if m.app.ConfigPath != "" {
			if err := config.Save(m.app.ConfigPath, candidate); err != nil {
				if custom, ok := m.customThemes[old]; ok {
					_ = applyCustomTUITheme(custom)
				} else {
					_ = applyTUITheme(old)
				}
				return fmt.Errorf("persist theme: %w", err)
			}
		}
		m.app.PersistedCfg = candidate
		m.app.Cfg.TUI.Theme = name
	}
	m.themeName = name
	if announce {
		m.pushLine(styleFooter.Render("theme: " + name))
	}
	return nil
}

func (m *Model) setReasoningSummary(summary protocol.ReasoningSummary) error {
	old := m.app.Agent.ReasoningSummary()
	if err := m.app.Agent.SetReasoningSummary(summary); err != nil {
		return err
	}
	candidate := m.app.PersistedCfg
	candidate.ReasoningSummary = string(m.app.Agent.ReasoningSummary())
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			_ = m.app.Agent.SetReasoningSummary(old)
			return fmt.Errorf("persist reasoning summary: %w", err)
		}
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.ReasoningSummary = candidate.ReasoningSummary
	return nil
}

func (m *Model) setTextVerbosity(verbosity protocol.TextVerbosity) error {
	old := m.app.Agent.TextVerbosity()
	if err := m.app.Agent.SetTextVerbosity(verbosity); err != nil {
		return err
	}
	candidate := m.app.PersistedCfg
	candidate.TextVerbosity = string(m.app.Agent.TextVerbosity())
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			_ = m.app.Agent.SetTextVerbosity(old)
			return fmt.Errorf("persist text verbosity: %w", err)
		}
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.TextVerbosity = candidate.TextVerbosity
	return nil
}

func (m *Model) setSubagentsEnabled(enabled bool) error {
	candidate := m.app.PersistedCfg
	candidate.Subagents.Enabled = enabled
	if err := candidate.Subagents.ValidateSubagents(); err != nil {
		return err
	}
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			return fmt.Errorf("persist subagent setting: %w", err)
		}
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Subagents = candidate.Subagents
	return nil
}

func (m *Model) setSubagentConcurrency(limit int) error {
	if limit < 1 {
		return errors.New("subagent concurrency must be positive")
	}
	candidate := m.app.PersistedCfg
	candidate.Subagents.MaxConcurrentThreads = limit
	if candidate.Subagents.MaxAgentsPerSession < limit {
		candidate.Subagents.MaxAgentsPerSession = limit
	}
	if err := candidate.Subagents.ValidateSubagents(); err != nil {
		return err
	}
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			return fmt.Errorf("persist subagent concurrency: %w", err)
		}
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Subagents = candidate.Subagents
	return nil
}

func (m *Model) setSkillsEnabled(enabled bool) error {
	candidate := m.app.PersistedCfg
	candidate.Skills.Disabled = !enabled
	if m.app.ConfigPath != "" {
		if err := config.Save(m.app.ConfigPath, candidate); err != nil {
			return fmt.Errorf("persist skills setting: %w", err)
		}
	}
	m.app.PersistedCfg = candidate
	m.app.Cfg.Skills.Disabled = candidate.Skills.Disabled
	return nil
}

func (m *Model) settingsRows() int {
	rows := settingsCount + 2 // title + settings + key hint
	if m.settingsStatus != "" || m.settingsError != "" {
		rows++
	}
	return rows
}

func (m *Model) renderSettings() string {
	if !m.pickSettings || m.app == nil {
		return ""
	}
	model := m.app.Agent.Model()
	rows := []string{
		"Model  " + model.Provider + "/" + model.ID,
		"Theme  " + m.themeName,
		"Thinking effort  " + string(m.app.Agent.Thinking()),
		"Reasoning summary  " + string(m.app.Agent.ReasoningSummary()),
		"Text verbosity  " + string(m.app.Agent.TextVerbosity()),
		"Permission mode  " + string(m.app.Perm.Mode()),
		"Subagents  " + onOff(m.app.Cfg.Subagents.Enabled) + " (restart to apply)",
		fmt.Sprintf("Concurrent subagents  %d (restart to apply)", m.app.Cfg.Subagents.MaxConcurrentThreads),
		"Agent Skills  " + onOff(!m.app.Cfg.Skills.Disabled) + " (restart to apply)",
	}
	if !m.chatGPTSettingsEnabled() {
		rows[settingsReasoningSummary] += "  (ChatGPT only)"
		rows[settingsTextVerbosity] += "  (ChatGPT only)"
	}
	var b strings.Builder
	header := styleHeaderDim.Render("settings")
	if m.inlineTranscript {
		header = styleHeaderDim.Render("settings  (↑/↓ row · ←/→ change · Enter select · Esc close)")
		if m.settingsError != "" {
			header = styleError.Render("settings: " + m.settingsError)
		} else if m.settingsStatus != "" {
			header = styleFooter.Render("settings: " + m.settingsStatus)
		}
	}
	b.WriteString(header + "\n")
	start, end := 0, len(rows)
	if m.inlineTranscript {
		// Header consumes one row; keep a selected-row-centered window for short
		// terminals rather than clipping the bottom of the fixed list.
		visible := max(1, m.availableOverlayHeight()-1)
		if end > visible {
			start = m.settingsIndex - visible/2
			if start < 0 {
				start = 0
			}
			if start+visible > end {
				start = end - visible
			}
			end = start + visible
		}
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := "  "
		style := styleCompletion
		if i == m.settingsIndex {
			prefix = "› "
			style = styleCompletionSelected
		} else if !m.chatGPTSettingsEnabled() && (i == settingsReasoningSummary || i == settingsTextVerbosity) {
			style = styleHeaderDim
		}
		b.WriteString(style.Render(prefix + row))
		b.WriteString("\n")
	}
	if !m.inlineTranscript {
		b.WriteString(styleFooter.Render("(↑/↓ row, ←/→ change, Enter select, Esc close)"))
		if m.settingsError != "" {
			b.WriteString("\n" + styleError.Render(m.settingsError))
		} else if m.settingsStatus != "" {
			b.WriteString("\n" + styleFooter.Render(m.settingsStatus))
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// renderOverlays returns the picker/palette area between the transcript and
// composer. Its height is bounded so no overlay can push the frame into the
// terminal's scrollback buffer.
func (m *Model) renderOverlays() string {
	// Blocking host requests are exclusive overlays and mirror keyboard
	// precedence. Do not let an unrelated picker hide a request that is holding
	// a root or child agent.
	if m.permPending {
		return m.limitOverlay(m.renderPermissionPicker())
	}
	if m.userInputPending {
		return m.limitOverlay(m.renderUserInput())
	}
	var overlays []string
	if m.confirmGoalReplace {
		overlays = append(overlays, styleHeader.Render("Replace unfinished goal?")+"\n"+styleCompletionSelected.Render("› Enter to replace")+"\n"+styleCompletion.Render("  Esc to cancel"))
	} else if m.planPrompt {
		overlays = append(overlays, m.renderPlanImplementationPrompt())
	} else if m.planNudgeVisible() {
		overlays = append(overlays, styleHeaderDim.Render("Tip: use /plan to explore and produce a decision-complete plan"))
	}
	if m.compVisible {
		matches := m.compMatches
		selected := m.compIndex
		if limit := m.availableOverlayHeight(); len(matches) > limit {
			start := selected - limit/2
			if start < 0 {
				start = 0
			}
			if start+limit > len(matches) {
				start = len(matches) - limit
			}
			matches = matches[start : start+limit]
			selected -= start
		}
		overlays = append(overlays, renderCompletions(matches, selected, m.width))
	}
	if m.skillVisible {
		if r := m.renderSkillCompletionPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if !m.skillVisible && (m.mentionVisible || m.mentionLoading) {
		if r := m.renderMentionPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickProvider {
		if r := m.renderProviderPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickChatGPTAuth {
		if r := m.renderChatGPTAuthPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickModel {
		if r := m.renderModelPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickThinking {
		if r := m.renderThinkingPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickSettings {
		if r := m.renderSettings(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickSession {
		if r := m.renderSessionPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickTree {
		if r := m.renderTreePicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.pickInfo {
		if r := m.renderInfoPicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if m.compacting {
		overlays = append(overlays, m.renderCompactionProgress())
	}
	if m.pickPermissionMode {
		if r := m.renderPermissionModePicker(); r != "" {
			overlays = append(overlays, r)
		}
	}
	if len(overlays) == 0 {
		return ""
	}
	return m.limitOverlay(strings.Join(overlays, "\n"))
}

func (m *Model) limitOverlay(overlay string) string {
	maxHeight := m.availableOverlayHeight()
	if maxHeight <= 0 || overlay == "" {
		return ""
	}
	lines := strings.Split(overlay, "\n")
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) planNudgeScope() string {
	if m.app == nil || m.app.Session == nil {
		return ""
	}
	branchID := "main"
	// Branches returns rich picker metadata and SQLite computes it by walking
	// and decoding every branch history. Rendering the composer only needs the
	// already-cached active identity; never put a history scan on View/layout.
	if active, ok := m.app.Session.(session.ActiveBranchStore); ok {
		if id := strings.TrimSpace(active.ActiveBranchID()); id != "" {
			branchID = id
		}
	}
	return currentSessionID(m.app) + ":" + branchID
}

func (m *Model) planNudgeVisible() bool {
	if m.app == nil || m.busy || m.app.Agent.Mode() != protocol.ModeDefault || strings.HasPrefix(strings.TrimSpace(m.editor.Value()), "/") {
		return false
	}
	containsPlan := false
	for _, word := range strings.FieldsFunc(strings.ToLower(m.editor.Value()), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') }) {
		if word == "plan" {
			containsPlan = true
			break
		}
	}
	return containsPlan && !m.nudgeDismissed[m.planNudgeScope()]
}

func (m *Model) renderPlanImplementationPrompt() string {
	items := []string{"Yes, implement this plan", "Yes, clear context and implement", "No, stay in Plan mode"}
	var b strings.Builder
	b.WriteString(styleHeader.Render("Implement this plan?") + "\n")
	for i, item := range items {
		prefix, style := "  ", styleCompletion
		if i == m.planPromptChoice {
			prefix, style = "› ", styleCompletionSelected
		}
		b.WriteString(style.Render(prefix + item))
		if i < len(items)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *Model) handlePlanImplementationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyShiftTab:
		m.planPromptChoice = (m.planPromptChoice + 2) % 3
	case tea.KeyDown, tea.KeyTab:
		m.planPromptChoice = (m.planPromptChoice + 1) % 3
	case tea.KeyEsc:
		m.planPrompt = false
	case tea.KeyEnter:
		choice := m.planPromptChoice
		m.planPrompt = false
		if choice == 2 {
			return m, nil
		}
		message := "Implement the plan."
		planText := m.latestPlan
		if choice == 1 {
			var st session.Store
			var err error
			if currentSessionPath(m.app) == "" {
				st = session.NewMemoryStore(session.Options{CWD: m.app.CWD()})
			} else {
				st, err = session.NewFileIndex(session.DefaultSessionsRoot()).Create(m.app.CWD())
			}
			if err != nil {
				m.pushLine(styleError.Render("new session: " + err.Error()))
				return m, nil
			}
			if err := m.switchSession(st); err != nil {
				_ = st.Close()
				m.pushLine(styleError.Render("new session: " + err.Error()))
				return m, nil
			}
			message = "A previous agent produced the plan below. Implement it in a fresh context, re-read files as needed, and carry the work through implementation and verification.\n\n" + planText
		}
		m.pushLine(styleUser.Render("› " + message))
		return m, m.startPromptWithMode(message, protocol.ModeDefault)
	}
	return m, nil
}

// View implements tea.Model as one full-window frame: sticky header, scrollable
// transcript viewport, overlays/run status, composer, and footer.
func (m *Model) View() string {
	if m.inlineTranscript && m.inlineExiting {
		return ""
	}
	if m.width <= 0 || m.height <= 0 {
		return "loading snow…"
	}
	clipboardSequence := m.transcriptSelectionClipboard
	if m.height < minFullFrameHeight+m.runStatusHeight() || m.width < 4 {
		return fitFrame(styleBrand.Render(" snow ")+styleHeaderDim.Render("terminal too small"), m.width, m.height)
	}
	if m.trustPending {
		return m.renderTrustPrompt()
	}
	// The fleet inspector owns the frame, except when a blocking host request
	// must preempt it. Its renderer consumes only bounded in-memory snapshots.
	if m.subagentFleetOpen && !m.permPending && !m.userInputPending {
		return clipboardSequence + fitFrame(m.renderSubagentFleetModal(), m.managedFrameWidth(), m.managedFrameHeight())
	}

	status := "starting…"
	if m.lastErr != nil {
		status = "error"
	} else if m.app != nil {
		status = "idle"
		if m.busy {
			if m.showRunStatus() {
				status = ""
			} else {
				status = m.spinner.View() + " working"
			}
		}
		if m.compacting {
			status = m.spinner.View() + " compacting"
		}
		if m.permPending {
			status = "permission"
		}
		if m.userInputPending {
			status = "input needed"
		}
		if m.sessionOpLoading {
			status = "session…"
		}
		if m.compatibleLoginPending {
			status = "models…"
		}
		if m.loginMode || m.loginEndpointMode {
			status = "login"
		}
		if m.pickChatGPTAuth {
			status = "import auth"
		}
		if m.pickThinking {
			status = "thinking"
		}
		if m.pickSettings || m.settingsReturnToPanel {
			status = "settings"
		}
		if m.pickInfo {
			status = "inspect"
		}
	}

	header := m.renderHeader(status)
	frameWidth := m.managedFrameWidth()
	sep := styleSep.Render(strings.Repeat("─", frameWidth))
	overlay := m.renderOverlays()
	if m.inlineModalOverlay() && overlay != "" {
		// Modal pickers replace the live tail but remain bottom-anchored inside the
		// same terminal-height frame, so closing one restores the composer without
		// moving terminal-owned history.
		return clipboardSequence + fitFrameBottom(overlay, frameWidth, m.managedFrameHeight())
	}
	if m.inlineInputOverlay() && overlay != "" {
		frame := lipgloss.JoinVertical(lipgloss.Left, overlay, sep, m.renderEditor())
		return clipboardSequence + fitFrameBottom(frame, frameWidth, m.managedFrameHeight())
	}
	runStatus := m.renderRunStatus()

	editorView := m.renderEditor()
	footer := styleFooter.Render(" starting snow…")
	if m.lastErr != nil {
		footer = styleError.Render(" startup failed") + styleFooter.Render(" · ctrl+c to quit")
	} else if m.app != nil {
		footer = m.renderFooter()
	}

	parts := make([]string, 0, 8)
	// Keep the active provider/model/mode visible in both render modes. Inline
	// session headers also remain in native scrollback as historical boundaries,
	// but the current selection must not disappear above the visible window.
	parts = append(parts, header, sep, m.renderTranscriptView())
	if overlay != "" {
		parts = append(parts, overlay)
	}
	if runStatus != "" {
		parts = append(parts, runStatus)
	}
	parts = append(parts, sep, editorView, footer)
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.inlineTranscript {
		// Keep a constant logical row count. Growing a normal-screen Bubble Tea
		// frame at the terminal bottom scrolls old chrome into native history;
		// only tea.Println transcript commits are allowed to move scrollback.
		return clipboardSequence + fitFrame(frame, frameWidth, m.managedFrameHeight())
	}
	return clipboardSequence + fitFrame(frame, frameWidth, m.height)
}

func (m *Model) renderTrustPrompt() string {
	width := max(20, m.width-8)
	choice := func(index int, label string) string {
		prefix := "  "
		style := styleHeaderDim
		if m.trustChoice == index {
			prefix = "› "
			style = styleHeader
		}
		return prefix + style.Render(label)
	}
	status := "Choose with arrows/Tab · Enter confirms · Esc continues untrusted"
	if m.trustSaving {
		status = m.spinner.View() + " saving trust decision…"
	}
	body := []string{
		styleBrand.Render(" snow ") + styleHeader.Render("Project trust"),
		"",
		styleHeaderDim.Render("Project:"),
		lipgloss.NewStyle().Width(width).Render(m.trustPath),
		"",
		"Snow always reads AGENTS.md as project context. Trust additionally permits",
		"project config, plugins, MCP declarations, and project skills to load.",
		styleError.Render("Trust is not a sandbox; loaded code runs with your OS privileges."),
		"",
		choice(0, "Continue untrusted"),
		choice(1, "Trust project"),
		"",
		styleFooter.Render(status),
		styleFooter.Render("Ctrl+C/Ctrl+D exits without recording a decision."),
	}
	if m.trustError != "" {
		body = append(body, styleError.Render("trust: "+m.trustError))
	}
	return fitFrame(lipgloss.NewStyle().Padding(1, 3).Render(strings.Join(body, "\n")), m.width, m.height)
}

func fitFrame(frame string, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(frame)
}

func fitFrameBottom(frame string, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		AlignVertical(lipgloss.Bottom).
		Render(frame)
}

// renderHeader is the sticky top bar: brand · provider/model · cwd · status.
func (m *Model) renderHeader(status string) string {
	w := m.managedFrameWidth()
	brand := styleBrand.Render(" snow ")
	midText := "booting"
	if m.lastErr != nil {
		midText = "startup failed"
	} else if m.app != nil {
		model := m.app.Agent.Model()
		goalText := ""
		if m.goal != nil {
			goalText = fmt.Sprintf("  ·  goal:%s %s", m.goal.Status, formatGoalTokenUsage(m.goal))
		}
		midText = m.app.ProviderID + "/" + model.ID
		if w >= 80 {
			midText += "  ·  mode:" + m.collaborationModeLabel() + goalText +
				"  ·  thinking:" + string(m.app.Agent.Thinking()) +
				"  ·  " + shortPath(m.app.CWD(), max(12, w/3))
		} else if w >= 48 {
			midText += "  ·  mode:" + m.collaborationModeLabel() + "  ·  " + shortPath(m.app.CWD(), max(10, w/4))
		}
	}
	right := styleHeaderDim.Render(status + " ")
	// Fill the middle so brand sticks left and status sticks right.
	maxMid := w - lipgloss.Width(brand) - lipgloss.Width(right) - 1
	if maxMid < 4 {
		maxMid = 4
	}
	midText = truncateRunes(midText, maxMid)
	mid := styleHeaderDim.Render(midText)
	used := lipgloss.Width(brand) + lipgloss.Width(mid) + lipgloss.Width(right)
	pad := max(1, w-used)
	return brand + mid + strings.Repeat(" ", pad) + right
}

// renderEditor draws a composer that grows from three to six rows.
func (m *Model) renderEditor() string {
	var input string
	if m.loginEndpointMode {
		input = stylePrompt.Render("URL ") + m.editor.View()
	} else if m.loginMode {
		n := m.secretBuf.Len()
		masked := strings.Repeat("•", n)
		if n == 0 {
			hint := "type API key, Enter to save, Esc to cancel"
			if m.loginProvider == openaicompat.ProviderID {
				hint = "optional API key; Enter keeps existing/fallback or uses keyless"
			}
			masked = styleHeaderDim.Render("(" + hint + ")")
		} else {
			masked = styleAssistant.Render(masked)
		}
		input = stylePrompt.Render("🔑 ") + styleHeaderDim.Render(m.loginProvider+": ") + masked
	} else {
		if len(m.promptImages) > 0 {
			labels := make([]string, 0, len(m.promptImages))
			for i, image := range m.promptImages {
				labels = append(labels, imageAttachmentLabel(image, i))
			}
			barWidth := max(1, m.managedFrameWidth()-4)
			bar := xansi.Wordwrap(strings.Join(labels, " "), barWidth, "")
			bar = xansi.Truncate(bar, barWidth*2, "…")
			input = styleHeaderDim.Render(bar+"  · Backspace removes last") + "\n"
		}
		input += stylePrompt.Render("› ") + m.editor.View()
	}
	height := max(minComposerHeight, m.editor.Height())
	width := m.managedFrameWidth()
	return styleComposer.
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		PaddingLeft(1).
		Render(input)
}

func (m *Model) permissionStatus() string {
	if m.app == nil || m.app.Perm == nil {
		return "permission: unavailable"
	}
	return "permission: " + string(m.app.Perm.Mode())
}

// renderFooter is the sticky bottom status bar.
func (m *Model) permissionStatusStyle() lipgloss.Style {
	if m.app == nil || m.app.Perm == nil {
		return styleFooter
	}
	switch m.app.Perm.Mode() {
	case permission.ModeAllow:
		return lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	case permission.ModeDeny:
		return lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	case permission.ModeAsk:
		return lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	default:
		return styleFooter
	}
}

func (m *Model) renderFooter() string {
	permissionText := m.permissionStatus()
	// Reserve a fixed field so the permission label does not jump when
	// ask/allow/deny changes (the labels have different lengths).
	permissionWidth := lipgloss.Width("permission: unavailable")
	permissionField := lipgloss.PlaceHorizontal(permissionWidth, lipgloss.Left, permissionText)
	available := max(8, m.managedFrameWidth()-2)
	mode := string(protocol.ModeDefault)
	if m.app != nil && m.app.Agent != nil {
		mode = m.collaborationModeLabel()
	}
	goalText := ""
	if m.goal != nil {
		goalText = fmt.Sprintf(" · goal:%s %s", m.goal.Status, formatGoalTokenUsage(m.goal))
	}
	contextUsage := m.renderContextUsage()
	cacheHit := m.renderCacheHit()
	runtimeText := "mode:" + mode
	if m.inlineTranscript && m.app != nil && m.app.Agent != nil && available >= 72 {
		model := m.app.Agent.Model()
		runtimeText = model.Provider + "/" + model.ID + " · " + runtimeText + " · thinking:" + string(m.app.Agent.Thinking())
	}
	rightPrefix := "· " + runtimeText + goalText + " · "
	if cacheHit != "" {
		rightPrefix += cacheHit + " · "
	}
	right := rightPrefix + contextUsage
	// Add width-aware help only when it fits beside the persistent context
	// indicator. Narrow terminals keep the footer quiet and leave shortcuts in
	// /help rather than forcing the usage counter off-screen.
	m.help.Width = available
	helpText := m.help.ShortHelpView(m.keys.ShortHelp())
	maxRight := available - lipgloss.Width(" "+permissionField)
	if lipgloss.Width(helpText)+lipgloss.Width(" · ")+lipgloss.Width(right) <= maxRight {
		rightPrefix = helpText + " · " + rightPrefix
		right = rightPrefix + contextUsage
	}
	// Keep the whole footer inside the terminal: shrink the usage side before
	// the fixed permission field when the terminal is narrow.
	if maxRight < 4 {
		maxRight = 4
	}
	if lipgloss.Width(right) > maxRight {
		compactRightPrefix := ""
		if m.inlineTranscript && m.app != nil && m.app.Agent != nil && maxRight >= 18 {
			model := m.app.Agent.Model()
			runtimeText = model.ID + " · " + mode + "/" + string(m.app.Agent.Thinking())
			compactRightPrefix = "· " + runtimeText + " · "
			rightPrefix = compactRightPrefix
			if cacheHit != "" {
				rightPrefix += cacheHit + " · "
			}
			right = rightPrefix + contextUsage
		}
		if lipgloss.Width(right) > maxRight && cacheHit != "" {
			cacheHit = ""
			if compactRightPrefix != "" {
				rightPrefix = compactRightPrefix
			} else {
				rightPrefix = "· " + runtimeText + goalText + " · "
			}
			right = rightPrefix + contextUsage
		}
		if lipgloss.Width(right) > maxRight {
			rightPrefix = "· "
			contextUsage = truncateRunes(contextUsage, maxRight-2)
			right = rightPrefix + contextUsage
		}
	}
	line := m.permissionStatusStyle().Render(permissionField)
	pad := available - lipgloss.Width(" "+permissionField) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	styledRight := styleFooter.Render(rightPrefix) + m.contextUsageStyle().Render(contextUsage)
	line += strings.Repeat(" ", pad) + styledRight
	return styleFooter.Render(" ") + line
}

func contextUsageBand(current, total int) string {
	if total <= 0 || current < 0 {
		return "unknown"
	}
	ratio := float64(current) / float64(total)
	switch {
	case ratio >= 0.9:
		return "critical"
	case ratio >= 0.7:
		return "warning"
	case ratio >= 0.5:
		return "notice"
	default:
		return "healthy"
	}
}

func (m *Model) contextUsageStyle() lipgloss.Style {
	total := 0
	if m.app != nil {
		total = m.app.Model.ContextWindow
	}
	switch contextUsageBand(m.contextTokens, total) {
	case "healthy":
		return lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	case "notice":
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	case "warning":
		return lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	case "critical":
		return lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	default:
		return styleFooter
	}
}

func (m *Model) renderCacheHit() string {
	usage := m.lastRequestUsage
	if usage == nil || (!usage.CacheReadKnown && usage.CacheRead <= 0) || usage.Input <= 0 {
		return ""
	}
	percent := 100 * float64(usage.CacheRead) / float64(usage.Input)
	percent = min(100, max(0, percent))
	return fmt.Sprintf("CH%.1f%%", percent)
}

func (m *Model) renderContextUsage() string {
	current := formatTokenCount(int64(m.contextTokens))
	if m.contextEstimated && m.contextTokens > 0 {
		current = "~" + current
	}
	total := "?"
	if m.app != nil && m.app.Model.ContextWindow > 0 {
		total = formatTokenCount(int64(m.app.Model.ContextWindow))
	}
	return fmt.Sprintf("context: %s/%s", current, total)
}

func (m *Model) renderCompactionProgress() string {
	status := strings.TrimSpace(m.compactStatus)
	if status == "" {
		status = "compacting context"
	}
	return m.spinner.View() + " " + styleHeaderDim.Render(status+"…")
}

func formatGoalTokenUsage(goal *protocol.ThreadGoal) string {
	if goal == nil {
		return "0 tks"
	}
	usage := formatTokenCount(goal.TokensUsed)
	if goal.TokenBudget != nil {
		usage += "/" + formatTokenCount(*goal.TokenBudget)
	}
	usage += " tks"
	if len(goal.EstimatedCosts) == 0 {
		return usage
	}
	costs := append([]protocol.Cost(nil), goal.EstimatedCosts...)
	sort.Slice(costs, func(i, j int) bool { return costs[i].Currency < costs[j].Currency })
	formatted := make([]string, 0, len(costs))
	for _, cost := range costs {
		formatted = append(formatted, formatEstimatedCost(cost))
	}
	return usage + " · est. " + strings.Join(formatted, " + ")
}

func formatEstimatedCost(cost protocol.Cost) string {
	currency := strings.ToUpper(strings.TrimSpace(cost.Currency))
	prefix := currency + " "
	if currency == "USD" {
		prefix = "$"
	}
	if cost.Total > 0 && cost.Total < 0.0001 {
		return "<" + prefix + "0.0001"
	}
	precision := 2
	if cost.Total < 1 {
		precision = 4
	}
	value := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.*f", precision, cost.Total), "0"), ".")
	if !strings.Contains(value, ".") {
		value += ".00"
	}
	return prefix + value
}

func formatTokenCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		value := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0")
		return value + "k"
	}
	if n < 1_000_000_000 {
		value := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0")
		return value + "m"
	}
	value := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000_000), ".0")
	return value + "b"
}

// shortPath collapses the home prefix to ~ and truncates the middle of long paths.
func shortPath(p string, maxLen int) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	return truncateRunes(p, maxLen)
}

// truncateRunes trims s to at most n display runes, adding an ellipsis when cut.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
