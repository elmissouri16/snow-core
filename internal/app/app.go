// Package app wires configuration, auth, session, tools, providers, and the
// agent into ready-to-run surfaces (CLI, TUI, print, SDK, RPC).
package app

import (
	"sync"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/diagnostics"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	internalmcp "github.com/elmissouri16/snow-core/internal/mcp"
	"github.com/elmissouri16/snow-core/internal/permission"
	internalplugin "github.com/elmissouri16/snow-core/internal/plugin"
	managedprocess "github.com/elmissouri16/snow-core/internal/process"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/internal/subagent"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/internal/tools/builtin"
	"github.com/elmissouri16/snow-core/internal/trust"
	"github.com/elmissouri16/snow-core/internal/userinput"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// App is the assembled runtime.
type App struct {
	// Cfg is the effective runtime configuration after trusted project overlays.
	// PersistedCfg is the global/operator configuration and must be the base for
	// every write to ConfigPath so project-only values never leak globally.
	Cfg          config.Config
	PersistedCfg config.Config
	ConfigPath   string
	AuthPath     string
	BuildVersion string

	Auth            auth.Store // compatibility handle for credential inventory
	AuthService     *auth.Service
	Registry        *tools.SimpleRegistry
	Router          tools.Router
	Provider        provider.Provider
	ProviderID      string
	Providers       map[string]provider.Provider
	ProviderModules *provider.Registry
	// Models is the active provider catalog; AllModels is the combined live
	// snapshot used by the TUI picker and replaced on catalog refresh.
	Models    []protocol.Model
	AllModels []protocol.Model
	Model     protocol.Model
	Perm      *permission.SimpleService
	// PermBroker is the trusted-host interactive permission asker. It is used
	// only in ask mode and only when an embedded handler or manual replies are
	// enabled; otherwise ask-mode requests deny without blocking.
	PermBroker       *permission.Broker
	Session          session.Store
	Agent            *agent.Agent
	Goal             *goalpkg.Controller
	Trust            *trust.Store
	PluginManager    *internalplugin.Manager
	ProcessManager   *managedprocess.Manager
	MCPManager       *internalmcp.Manager
	MCPStatuses      []publicmcp.Status
	Skills           *skills.Registry
	SkillDiagnostics []skills.Diagnostic
	Subagents        *subagent.Manager
	Diagnostics      []config.Diagnostic
	Debugger         *diagnostics.Recorder
	DebugDumpPath    string
	SearchPolicy     config.EffectiveSearchPolicy
	ProjectAllowed   bool
	ProjectInputRoot string

	stateMu              sync.Mutex
	providerTransitionMu sync.Mutex
	diagnosticsMu        sync.Mutex
	diagnosticsCacheKey  string
	diagnosticsCache     []protocol.ConfigDiagnostic
	diagnosticSecrets    []string
	permissionBaseline   permission.Mode
	permissionOverride   bool
	modelCatalog         map[string][]protocol.Model
	runtimeSelection     *liveRuntimeSelection
	cwd                  string
	userInput            *userinput.Broker
	toolGuard            *builtin.PathGuard
	sessionHistory       *builtin.SessionBinding
	sessionQuery         *session.QueryEngine
	artifacts            artifact.Store
}

// SessionDeleteCleanupError reports that the durable session was deleted but
// one or more secondary managed-state cleanup operations failed.
type SessionDeleteCleanupError struct{ Err error }

type liveRuntimeSelection struct {
	mu                sync.RWMutex
	provider          string
	model             protocol.Model
	providers         map[string]provider.Provider
	catalogs          map[string][]protocol.Model
	catalogErrors     map[string]error
	catalogLoads      map[string]*catalogLoad
	catalogGeneration map[string]uint64
}

type catalogLoad struct {
	done       chan struct{}
	generation uint64
}

// Options control app assembly.
type Options struct {
	CWD                     string
	ConfigPath              string
	BuildVersion            string
	AuthPath                string
	Provider                string
	Model                   string
	APIKey                  string
	Permission              string   // ask|allow|deny
	SessionPath             string   // empty → create new; or existing .db to resume
	RequireExistingSession  bool     // reject missing/non-Snow SessionPath instead of creating it
	Tools                   []string // subset allowlist; empty = all builtins
	SystemPrompt            string
	Thinking                string
	ReasoningSummary        string
	TextVerbosity           string
	CollaborationMode       string
	PlanModeReasoningEffort string
	// Retry overrides global retry policy for this runtime only.
	Retry *config.RetryConfig
	// Debug overrides persisted diagnostics enablement for this runtime when set.
	Debug *bool
	// DebugDumpPath creates one final diagnostic dump during App.Close.
	DebugDumpPath string
	NoSession     bool   // in-memory session (SDK ephemeral)
	BaseURL       string // active provider base URL override
	Plugins       []publicplugin.PluginSpec
	GoPlugins     []publicplugin.Plugin
	NoPlugins     bool
	MCPServers    []publicmcp.ServerSpec
	NoMCP         bool
	SkillDirs     []string
	NoSkills      bool
	// UserInputHandler answers ask_user calls for embedded/headless clients.
	// Nil keeps the tool directly visible but makes calls fail fast until an
	// interactive surface enables manual replies.
	UserInputHandler userinput.Handler
	// PermissionHandler resolves interactive ask-mode permission requests from
	// a trusted host. Only ask mode uses it, and only when a handler or manual
	// replies are enabled. Nil keeps ask-mode blocking unavailable (deny fast),
	// preserving the deny-by-default contract.
	PermissionHandler permission.Handler
	// Subagents overrides config enablement when non-nil. Enabling never implies
	// recursive spawning or mutation.
	Subagents              *bool
	SubagentProvider       string
	SubagentModel          string
	SubagentMaxConcurrency int
	SubagentMaxAgents      int
	SubagentMaxDepth       int
}

// ProjectTrustPreflight is the side-effect-free trust decision needed before
// an interactive surface constructs the runtime. Store persists the eventual
// exact-project choice.
type ProjectTrustPreflight struct {
	Resolution trust.Resolution
	Store      *trust.Store
}

const permissionMetadataKey = "permission_state"

const maxForkArtifactCopies = 1024

// toolHost adapts the tools.ToolHost contract to app state.
type toolHost struct {
	cwd             string
	roots           []string
	perm            permission.Service
	reg             *tools.SimpleRegistry
	userInput       *userinput.Broker
	emitUserInput   func(protocol.UserInputRequest)
	inEventCallback func() bool
}
