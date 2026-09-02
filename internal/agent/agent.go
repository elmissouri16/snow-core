// Package agent implements the streaming turn loop: prompt → provider →
// permission gate → tools → loop until the model stops.
package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elmissouri16/snow-core/internal/artifact"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	defaultDeferredTopK              = 5
	deferredCandidateK               = 20
	maxRoutingEventTools             = 20
	maxDeferredFallbackSchemaBytes   = 64 << 10
	defaultFixedContextBudgetPercent = 25
	unknownModelFixedContextTokens   = 32 * 1024
	maxPendingRootInputs             = 64
	maxQueuedInputBytes              = 64 * 1024
	maxPendingMailboxItems           = 64
	maxPendingMailboxBytes           = 1 << 20
	automaticTurnDelay               = 25 * time.Millisecond
	skillActivationMeta              = "agent_skill_activation"
	skillDeactivationMeta            = "agent_skill_deactivation"
	skillDeactivationAll             = "*"

	repeatedToolFirstThreshold     = 3
	repeatedToolNextThreshold      = 5
	repeatedToolLastThreshold      = 8
	repeatedToolArgsPreview        = 500
	maxConsecutiveSyntheticBatches = 1
)

// ErrNotRunning is returned when an operation requires an active,
// queue-accepting agent run.
var (
	ErrNotRunning     = errors.New("agent: no running turn accepting queued input")
	ErrPromptRejected = errors.New("agent: prompt rejected before admission")
	ErrReentrantDrain = errors.New("agent: event drain requested from inside a callback")
)

type providerFailure interface {
	error
	providerFailure()
}

type providerStartError struct{ err error }

type providerTurnError struct {
	err      error
	activity bool
}

// DeferredBundle exposes a complete lifecycle surface when any member is
// selected. Sticky keeps the bundle visible while runtime state still needs it.
type DeferredBundle struct {
	Members []string
	Sticky  func() bool
}

// ToolGuidance is appended only when at least one AnyOf tool is exposed and no
// UnlessAny tool is exposed.
type ToolGuidance struct {
	AnyOf     []string
	UnlessAny []string
	Text      string
}

// Options configures an Agent.
type Options struct {
	Provider                  provider.Provider
	Registry                  tools.Registry
	Session                   session.Store
	Permission                permission.Service
	ToolHost                  tools.ToolHost
	Router                    tools.Router
	DeferredBundles           []DeferredBundle
	ToolGuidance              []ToolGuidance
	FixedContextBudgetPercent int
	SystemPrompt              string
	Model                     protocol.Model
	MaxTurns                  int // 0 = unlimited
	CallLimit                 int // max tool calls per turn (0 = unlimited)
	// Thinking level forwarded to providers that support reasoning effort.
	Thinking protocol.ThinkingLevel
	// ReasoningSummary and TextVerbosity are forwarded to adapters that support
	// the Responses API controls.
	ReasoningSummary protocol.ReasoningSummary
	TextVerbosity    protocol.TextVerbosity
	// CollaborationMode selects Default or Plan behavior. Empty restores the
	// branch state, then falls back to Default.
	CollaborationMode protocol.CollaborationMode
	// ModeTransitionGuard lets the application reject a mode transition when
	// another runtime subsystem cannot honor the requested boundary.
	ModeTransitionGuard func(from, to protocol.CollaborationMode) error
	// PlanThinking overrides reasoning effort while in Plan mode. Nil uses
	// Medium when advertised, otherwise the configured effort/Off.
	PlanThinking *protocol.ThinkingLevel
	Goal         *goalpkg.Controller
	// Identity attributes permission and host-interaction requests for child
	// agents. Root leaves it nil for backward-compatible events.
	Identity *protocol.AgentRef
	// SkillNames filters both restored and direct $name activations. Nil keeps
	// the legacy unrestricted behavior for standalone Agent embedders; an empty
	// non-nil map disables every persisted skill activation.
	SkillNames map[string]bool
	// Retry configures centralized provider recovery for ordinary and automatic
	// goal turns. Zero uses DefaultRetryOptions.
	Retry RetryOptions
	// Compaction configures manual and pressure-based automatic compaction.
	Compaction CompactionOptions
	// Artifacts preserves oversized plain-text tool results outside provider
	// context. The durable session stores only bounded previews and opaque IDs.
	Artifacts artifact.Store
}

// CompactionOptions is kept in agent to avoid coupling core runtime behavior to
// persisted configuration packages.
type CompactionOptions struct {
	RetainTokens                  int
	MinRetainedTurns              int
	SummaryMaxTokens              int
	Fallback                      string
	Guidance                      string
	AutoThresholdPercent          int
	ToolHistoryBudgetPercent      int
	ToolResultInlineBytes         int
	HistoricalToolResultThreshold int
}

// Agent drives turns against a provider and tool registry.
type repeatedToolCallState struct {
	name          string
	canonicalArgs string
	count         int
	reminders     []string
}

type toolDisplayState struct {
	startMessage string
	progress     []string
	progressSize int
}

type Agent struct {
	mu          sync.RWMutex
	admissionMu sync.Mutex
	// queuePublishMu serializes queue mutation with snapshot publication. Queue
	// callbacks never run under mu, while observers still see snapshots in the
	// exact order the underlying queue changed.
	queuePublishMu sync.Mutex
	// mailboxMu protects attributed collaboration messages. Producers only
	// enqueue; the admitted agent loop is the sole writer that drains them into
	// the mutable session cursor at safe boundaries.
	mailboxMu          sync.Mutex
	mailboxPersistMu   sync.Mutex
	mailbox            []protocol.AgentMessage
	mailboxBytes       int
	mailboxUnreadItems int
	mailboxUnreadBytes int
	mailboxUnread      bool
	mailboxClosed      bool
	opts               Options
	model              protocol.Model
	bus                *eventBus
	running            bool
	// tool results retained between the tool_use assistant message and the
	// continuation provider call. pendingToolError forces synthetic results for
	// an unsafe batch, such as length-truncated tool arguments.
	pending          map[string]protocol.ContentBlock
	pendingOrder     []string
	pendingToolError string
	// toolStarts and toolDisplays retain bounded, surface-safe presentation data
	// until the corresponding tool result can persist it for transcript resume.
	toolStarts   map[string]time.Time
	toolDisplays map[string]toolDisplayState
	// repeatedTool tracks identical consecutive calls across provider steps in
	// one admitted run. It is advisory only and resets for each fresh user turn.
	repeatedTool          repeatedToolCallState
	turnToolCalls         int
	turnUsage             protocol.Usage
	usageSet              bool
	turnProgress          bool
	latestContextTokens   int
	latestRequestEstimate int
	latestContextReport   *ContextReport
	// Deferred schemas selected for the current user turn. The base selection
	// is sticky; the latest search_tools result may add at most five more.
	baseDeferred     []string
	searchedDeferred []string
	// queuedInputs are in-memory until a safe provider boundary persists them as
	// ordinary user messages. queueAccepting closes atomically with the final
	// empty check so an accepted input can never become stranded.
	queuedInputs   []protocol.QueuedInput
	queueSequence  uint64
	queueAccepting bool
	// activeSkills are re-appended to the system instructions on every provider
	// request so manual compaction cannot silently discard activated guidance.
	activeSkills       map[string]string
	artifactRefs       map[string]string
	mode               protocol.CollaborationMode
	turnMode           protocol.CollaborationMode
	turnPlanSeen       bool
	activeCancel       context.CancelFunc
	activeDone         chan struct{}
	autoRunning        bool
	autoStop           bool
	autoPending        bool
	autoEmpty          int
	autoEmptyGoal      string
	autoConflictCount  int
	autoConflictGoal   string
	autoConflictKey    string
	turnGoalConflict   *goalpkg.ConflictDetails
	autoDone           chan struct{}
	autoWG             sync.WaitGroup
	turnWG             sync.WaitGroup
	closed             bool
	turnOrigin         string
	turnID             string
	latestTurnOrigin   string
	latestTurnID       string
	turnSequence       uint64
	activeTurnSequence uint64
	latestTurnSequence uint64
	rootEpoch          uint64
	goalAtTurn         *protocol.ThreadGoal
	turnStarted        time.Time
	goalTurn           int
	goalTurnID         string
	budgetWrap         bool
}

type compactionTrigger string

const (
	compactionManual      compactionTrigger = "manual"
	compactionPressure    compactionTrigger = "pressure"
	compactionToolHistory compactionTrigger = "tool-history"
	compactionOverflow    compactionTrigger = "context-overflow"
)

const (
	maxUserImageBytes      = 20 << 20
	maxUserImageTotalBytes = 40 << 20
	maxUserImages          = 8
)

type toolBatchResult struct {
	Calls      int
	Dispatched int
}

// progressHost preserves the host contract while making tool progress visible
// to every surface that subscribes to AgentEvent.
type progressHost struct {
	tools.ToolHost
	agent  *Agent
	callID string
	name   string
}

const (
	maxPersistedToolProgressRows  = 2000
	maxPersistedToolProgressBytes = 1 << 20
)

const (
	maxCompactionRetrievalReferences = 24
	maxCachedArtifactReferences      = 256
)

// ---------------------------------------------------------------------------
// Event bus
// ---------------------------------------------------------------------------

const (
	eventBusMaxItems       = 1024
	eventSubscriberTimeout = time.Second
)

type eventBus struct {
	mu           sync.Mutex
	subs         map[int]*eventSubscriber
	next         int
	wake         chan struct{}
	space        chan struct{}
	closingCh    chan struct{}
	items        []any
	maxItems     int
	closing      bool
	callbackIDs  map[uint64]struct{}
	dispatcherID atomic.Uint64
	closed       chan struct{}
}
type eventBarrier struct{ done chan struct{} }
type eventStop struct{}

type eventSubscriber struct {
	id       int
	fn       func(protocol.AgentEvent)
	tasks    chan eventSubscriberTask
	stop     chan struct{}
	stopOnce sync.Once
}

type eventSubscriberTask struct {
	event      protocol.AgentEvent
	generation uint64
	completed  chan<- eventSubscriberCompletion
}

type eventSubscriberCompletion struct {
	id         int
	generation uint64
}

func (s *eventSubscriber) close() {
	if s != nil {
		s.stopOnce.Do(func() { close(s.stop) })
	}
}

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

// idCounter disambiguates IDs generated within the same nanosecond tick.
var idCounter atomic.Uint64
