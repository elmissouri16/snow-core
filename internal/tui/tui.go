// Package tui provides the interactive Bubble Tea interface: transcript,
// editor, footer, slash commands, and streaming updates.
package tui

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Styles use the adaptive default palette until a model applies the selected
// built-in or custom theme.
var (
	colorAccent lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	colorMuted  lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#57606a", Dark: "#8b949e"}
	colorSoft   lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#f0f6fc"}
	colorWarn   lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#e3b341"}
	colorErr    lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff7b72"}
	colorOk     lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#7ee787"}

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
	styleSep       = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8c959f", Dark: "#6e7681"})
	stylePrompt    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleComposer  = lipgloss.NewStyle()

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
type fixedAuthInteraction struct{ value string }

type tuiOAuthInteraction struct{ events chan<- tea.Msg }

type oauthProgressMsg struct{ progress chatgpt.LoginProgress }
type oauthDoneMsg struct {
	status chatgpt.AuthStatus
	err    error
}
type logoutDoneMsg struct {
	generation uint64
	provider   string
	err        error
}
type compatibleLoginDoneMsg struct {
	generation uint64
	provider   string
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
	historyText string
	attachments []protocol.ContentBlock
	pastedTexts []pastedTextAttachment
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

type contextReportMsg struct {
	epoch  uint64
	report agent.ContextReport
	err    error
}

type modeSwitchDoneMsg struct {
	target protocol.CollaborationMode
	err    error
}

type textareaTarget uint8

const (
	textareaTargetComposer textareaTarget = iota
	textareaTargetUserInput
	textareaTargetLoginProfile
	textareaTargetLoginEndpoint
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
	fullText string
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
	fullText string
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

type clearThinkingFlashMsg uint64

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

type sessionDeleteMsg struct {
	generation uint64
	path       string
	name       string
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

type worktreeForkMsg struct {
	generation uint64
	result     protocol.SessionForkResult
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

type loginNavigationStep uint8

const (
	loginNavigationProvider loginNavigationStep = iota + 1
	loginNavigationProfile
	loginNavigationEndpoint
)

type loginNavigationEntry struct {
	step     loginNavigationStep
	provider string
	value    string
}

// Model is the TUI state.
type Model struct {
	ctx     context.Context
	opts    app.Options
	asyncIO bool
	app     *app.App
	width   int
	height  int

	transcript        viewport.Model
	editor            textarea.Model
	pastedTexts       []pastedTextAttachment
	nextPastedTextID  uint64
	inputHistory      []string
	inputHistoryIndex int
	inputHistoryDraft string
	spinner           spinner.Model
	thinkingSpinner   spinner.Model
	spinnerRunning    bool
	help              help.Model

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
	processFleetOpen              bool
	processFleetLoading           bool
	processFleetLogLoading        bool
	processFleetGeneration        uint64
	processFleetLogGeneration     uint64
	processFleetTickGeneration    uint64
	processFleetRequested         string
	processFleetList              []app.ManagedProcessState
	processFleetIndex             int
	processFleetError             string
	processFleetLogError          string
	processFleetOutputID          string
	processFleetOutput            string
	processFleetOutputVersion     uint64
	processFleetWrappedVersion    uint64
	processFleetWrappedWidth      int
	processFleetWrappedOutput     []string
	processFleetCursor            int64
	processFleetCursorSet         bool
	processFleetEOF               bool
	processFleetDetailOffset      int
	processFleetDetailEnd         bool
	busy                          bool
	activeTurnID                  string
	abortNoticePending            bool
	rootTurnSequence              uint64
	rootEventEpoch                uint64
	rootTurnFence                 bool
	runGeneration                 uint64
	compactGeneration             uint64
	runStartedAt                  time.Time
	now                           func() time.Time
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
	transcriptBaseSynced    int
	transcriptBaseAppend    bool
	transcriptDropped       int
	transcriptBytes         int
	inlineTranscript        bool
	inlineCommitted         int
	inlinePrintEnd          int
	inlinePrintInFlight     bool
	inlinePrintGeneration   uint64
	inlineEverCommitted     bool
	inlineHistoryKey        string
	inlineDurableMessageIDs []string
	inlineHeaderPending     bool
	inlineExiting           bool
	asker                   *tuiAsker
	md                      *mdRenderer
	thinkingMD              *mdRenderer
	subagentFleetMD         *mdRenderer
	toolRunning             bool
	activeToolCallID        string
	activeToolStartMessage  string
	activeToolText          strings.Builder
	activeToolRows          int
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

	// Agent Skill completion state (for $skill-name references).
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
	pickChatGPTAuth    bool
	authAccounts       []chatGPTAccountChoice
	authIndex          int
	oauthLoading       bool
	oauthProgress      chatgpt.LoginProgress
	oauthCancel        context.CancelFunc
	oauthEvents        chan tea.Msg
	oauthBackRequested bool

	// Model picker state (for /model).
	pickModel  bool
	modelList  []protocol.Model
	modelIndex int
	modelQuery string

	// Thinking picker state (for /thinking and the second stage of /model).
	pickThinking          bool
	thinkingList          []protocol.ThinkingLevel
	thinkingIndex         int
	thinkingModel         *protocol.Model
	thinkingReturnToModel bool
	thinkingFlash         bool
	thinkingFlashSeq      uint64

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

	// Centered /help viewer state.
	pickHelp   bool
	helpOffset int

	// Session picker state.
	pickSession           bool
	pickSessionOnStart    bool
	startupResumeRequired bool
	sessions              []session.SessionInfo
	sessionIndex          int
	sessionRenaming       bool
	sessionRenameInput    string
	sessionDeleting       bool
	sessionDeleteInFlight bool

	// Branch tree picker state.
	pickTree     bool
	branches     []protocol.SessionBranch
	branchIndex  int
	branchAction string // fork|rename|delete
	branchInput  string

	// Conversation fork destination picker state.
	pickFork    bool
	forkIndex   int
	forkLoading bool

	// Read-only /mcp and /skills inventory picker state.
	pickInfo         bool
	infoTitle        string
	infoItems        []statusInfoItem
	infoIndex        int
	infoAgentTargets []string
	infoLoading      bool

	// Masked auth capture state.
	loginMode                 bool
	loginProfileMode          bool
	loginEndpointMode         bool
	loginFieldGeneration      uint64
	loginProvider             string
	loginEndpoint             string
	loginError                string
	loginNavigation           []loginNavigationEntry
	compatibleLoginGeneration uint64
	compatibleLoginPending    bool
	compatibleLoginProvider   string
	logoutGeneration          uint64
	logoutPending             bool
	logoutProvider            string
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
	transcriptSelectionMenu          transcriptSelectionContextMenu
	transcriptSelectionLines         []string
	transcriptViewRevision           uint64
	transcriptViewCacheRevision      uint64
	transcriptViewCacheOffset        int
	transcriptViewCacheWidth         int
	transcriptViewCacheHeight        int
	transcriptViewCache              string
	transcriptViewCacheValid         bool
	managedFrameCacheInput           string
	managedFrameCacheOutput          string
	managedFrameCacheWidth           int
	managedFrameCacheHeight          int
	managedFrameCacheValid           bool
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

const transcriptClipboardTimeout = 2 * time.Second

const (
	fixedChromeHeight       = 4 // header, two separators, and footer
	maxTranscriptEntries    = 2000
	maxTranscriptBytes      = 4 << 20
	inlineFixedChromeHeight = 4 // sticky header, two separators, and footer
	inlineOverlayMaxHeight  = 10
	minComposerHeight       = 3
	maxComposerHeight       = 6
	minTranscriptHeight     = 1
	minFullFrameHeight      = fixedChromeHeight + minComposerHeight + minTranscriptHeight
)

// ---------------------------------------------------------------------------
// Model selection (interactive picker + persistent config)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Fork destination, session picker, and switching
// ---------------------------------------------------------------------------

var forkChoices = []string{
	"Fork branch in current session",
	"Fork to an independent session here",
	"Create a Git worktree and independent session",
}

type contextDisplayCategory struct {
	name   string
	tokens int
	items  int
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
