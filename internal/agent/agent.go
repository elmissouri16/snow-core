// Package agent implements the streaming turn loop: prompt → provider →
// permission gate → tools → loop until the model stops.
package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/snow-core/snow/internal/artifact"
	goalpkg "github.com/snow-core/snow/internal/goal"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	defaultDeferredTopK     = 5
	deferredCandidateK      = 20
	maxRoutingEventTools    = 20
	maxPendingRootInputs    = 64
	maxQueuedInputBytes     = 64 * 1024
	automaticTurnDelay      = 25 * time.Millisecond
	goalTransientRetryDelay = 250 * time.Millisecond
	maxGoalTransientRetries = 1
	skillActivationMeta     = "agent_skill_activation"

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

type providerFailure interface{ providerFailure() }

type providerStartError struct{ err error }

type providerTurnError struct{ err error }

// Options configures an Agent.
type Options struct {
	Provider     provider.Provider
	Registry     tools.Registry
	Session      session.Store
	Permission   permission.Service
	ToolHost     tools.ToolHost
	Router       tools.Router
	SystemPrompt string
	Model        protocol.Model
	MaxTurns     int // 0 = unlimited
	CallLimit    int // max tool calls per turn (0 = unlimited)
	// Thinking level forwarded to providers that support reasoning effort.
	Thinking protocol.ThinkingLevel
	// ReasoningSummary and TextVerbosity are forwarded to adapters that support
	// the Responses API controls.
	ReasoningSummary protocol.ReasoningSummary
	TextVerbosity    protocol.TextVerbosity
	// CollaborationMode selects Default or Plan behavior. Empty restores the
	// branch state, then falls back to Default.
	CollaborationMode protocol.CollaborationMode
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
	// Compaction configures manual and pressure-based automatic compaction.
	Compaction CompactionOptions
	// Artifacts preserves oversized plain-text tool results outside provider
	// context. The durable session stores only bounded previews and opaque IDs.
	Artifacts artifact.Store
}

// CompactionOptions is kept in agent to avoid coupling core runtime behavior to
// persisted configuration packages.
type CompactionOptions struct {
	RetainTokens         int
	MinRetainedTurns     int
	SummaryMaxTokens     int
	Fallback             string
	Guidance             string
	AutoThresholdPercent int
	// GoalAutoThresholdPercent is a deprecated source-compatibility alias.
	GoalAutoThresholdPercent      int
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
	mailboxMu        sync.Mutex
	mailboxPersistMu sync.Mutex
	mailbox          []protocol.AgentMessage
	mailboxUnread    bool
	mailboxClosed    bool
	opts             Options
	model            protocol.Model
	bus              *eventBus
	running          bool
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

const maxCompactionRetrievalReferences = 24

// ---------------------------------------------------------------------------
// Event bus
// ---------------------------------------------------------------------------

const eventBusMaxItems = 1024

type eventBus struct {
	mu           sync.Mutex
	subs         map[int]func(protocol.AgentEvent)
	next         int
	wake         chan struct{}
	space        chan struct{}
	closingCh    chan struct{}
	items        []any
	maxItems     int
	closing      bool
	inCallback   bool
	dispatcherID uint64
	closed       chan struct{}
}
type eventBarrier struct{ done chan struct{} }
type eventStop struct{}

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

// idCounter disambiguates IDs generated within the same nanosecond tick.
var idCounter uint64
