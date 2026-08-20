// Package subagent orchestrates independent agent.Agent runtimes. It owns
// identity, topology, limits, mailboxes, lifecycle and shutdown; reasoning and
// tools remain in the ordinary agent loop.
package subagent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var (
	ErrNotReady = errors.New("subagents: not ready")
	ErrClosed   = errors.New("subagents: closed")
)

const maxListAgents = 256

// rolePolicyFingerprintVersion changes whenever the built-in capability policy
// changes. Persisted children from an older policy must fail safe rather than
// silently receiving newly available tools during lazy restore.
const rolePolicyFingerprintVersion = "role-tools-v2-shell"

type Caller struct {
	ThreadID string
	Path     protocol.AgentPath
}

type Role struct {
	Name          string
	Description   string
	System        string
	Provider      string
	Model         string
	Thinking      *protocol.ThinkingLevel
	Tools         []string
	AllowMutation bool
}

type Limits struct {
	// MaxConcurrentThreads is retained for config compatibility and counts
	// concurrently executing children only; the root does not consume a slot.
	MaxConcurrentThreads  int
	MaxLoadedChildren     int
	MaxAgentsPerSession   int
	MaxDepth              int
	MinWait               time.Duration
	DefaultWait           time.Duration
	MaxWait               time.Duration
	TaskTimeout           time.Duration
	MaxResultBytes        int
	Recursive             bool
	Durable               bool
	AllowMutation         bool
	ExposeChildToolEvents bool
	DefaultProvider       string
	DefaultModel          string
	DefaultRole           string
	Roles                 map[string]Role
}

type ChildSpec struct {
	State          protocol.SubagentState
	Role           Role
	ForkTurns      string
	ParentMessages []protocol.Message
	SessionPath    string
	Restore        bool
}

type ChildRuntime interface {
	Prompt(context.Context, string) error
	RunMailbox(context.Context) error
	EnqueueMailbox(protocol.AgentMessage) error
	PendingMailbox() bool
	AbortContext(context.Context) error
	IsRunning() bool
	Messages() ([]protocol.Message, error)
	ContextMessages() ([]protocol.Message, error)
	Usage() (protocol.Usage, error)
	Subscribe(func(protocol.AgentEvent)) func()
	Close()
}

type ChildFactory interface {
	NewChild(context.Context, ChildSpec) (ChildRuntime, error)
}
type ChildFactoryFunc func(context.Context, ChildSpec) (ChildRuntime, error)

type runtime struct {
	mu                        sync.Mutex
	state                     protocol.SubagentState
	record                    session.SubagentRecord
	child                     ChildRuntime
	tasks                     chan childTask
	cancel                    context.CancelFunc
	skipQueued                bool
	unsubscribe               func()
	closed                    bool
	followupQueued            bool
	interruptRequested        bool
	terminalEmittedGeneration uint64
	workerStarted             bool
	workerStop                chan struct{}
	workerDone                chan struct{}
	finalizing                bool
	lastUsed                  time.Time
}
type childTask struct {
	message       string
	initial       bool
	onlyIfPending bool
	followup      bool
}

type Manager struct {
	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.RWMutex
	byID              map[string]*runtime
	byPath            map[protocol.AgentPath]*runtime
	reserved          map[protocol.AgentPath]struct{}
	order             []string
	root              *agent.Agent
	rootRef           protocol.AgentRef
	factory           ChildFactory
	store             session.SubagentTaskStore
	publish           func(protocol.AgentEvent)
	limits            Limits
	slots             chan struct{}
	activity          chan struct{}
	generation        uint64
	waitCursor        map[string]uint64
	ready             bool
	closed            bool
	evictionScheduled bool
	evictionRequested bool
	closeDone         chan struct{}
	wg                sync.WaitGroup
	modelCatalog      func() []protocol.Model
	modelSelection    func(provider, model string) (protocol.Model, error)
}

type detachedRuntime struct {
	child       ChildRuntime
	cancel      context.CancelFunc
	unsubscribe func()
	workerStop  chan struct{}
	workerDone  chan struct{}
}

// Compile-time assertion for the concrete child used by App.
var _ ChildRuntime = (*agent.Agent)(nil)
