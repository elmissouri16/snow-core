// Package app wires configuration, auth, session, tools, providers, and the
// agent into ready-to-run surfaces (CLI, TUI, print, SDK, RPC).
package app

import (
	"sync"

	"github.com/snow-core/snow/internal/agent"
	"github.com/snow-core/snow/internal/artifact"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/config"
	goalpkg "github.com/snow-core/snow/internal/goal"
	internalmcp "github.com/snow-core/snow/internal/mcp"
	"github.com/snow-core/snow/internal/permission"
	internalplugin "github.com/snow-core/snow/internal/plugin"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/sandbox"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/internal/skills"
	"github.com/snow-core/snow/internal/subagent"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	"github.com/snow-core/snow/internal/trust"
	"github.com/snow-core/snow/internal/userinput"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
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
	Models           []protocol.Model
	AllModels        []protocol.Model
	Model            protocol.Model
	Perm             *permission.SimpleService
	Session          session.Store
	Agent            *agent.Agent
	Goal             *goalpkg.Controller
	Trust            *trust.Store
	PluginManager    *internalplugin.Manager
	MCPManager       *internalmcp.Manager
	MCPStatuses      []publicmcp.Status
	Skills           *skills.Registry
	SkillDiagnostics []skills.Diagnostic
	Sandbox          *sandbox.Manager
	SandboxEnabled   bool
	Subagents        *subagent.Manager
	Diagnostics      []config.Diagnostic
	SearchPolicy     config.EffectiveSearchPolicy
	ProjectAllowed   bool
	ProjectInputRoot string

	stateMu            sync.Mutex
	permissionDefault  permission.Mode
	permissionOverride bool
	modelCatalog       map[string][]protocol.Model
	runtimeSelection   *liveRuntimeSelection
	cwd                string
	userInput          *userinput.Broker
	toolGuard          *builtin.PathGuard
	sessionHistory     *builtin.SessionBinding
	sessionQuery       *session.QueryEngine
	artifacts          artifact.Store
}

// SessionDeleteCleanupError reports that the durable session was deleted but
// one or more secondary managed-state cleanup operations failed.
type SessionDeleteCleanupError struct{ Err error }

type liveRuntimeSelection struct {
	mu        sync.RWMutex
	provider  string
	model     protocol.Model
	providers map[string]provider.Provider
	catalogs  map[string][]protocol.Model
}

// Options control app assembly.
type Options struct {
	CWD                     string
	ConfigPath              string
	AuthPath                string
	SandboxStatePath        string // optional operator-state override for tests/internal surfaces
	DisableSandbox          bool   // explicit host-shell override despite a configured association
	RequireSandbox          bool   // fail assembly unless the project has an association
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
	NoSession               bool   // in-memory session (SDK ephemeral)
	BaseURL                 string // active provider base URL override
	Plugins                 []publicplugin.PluginSpec
	GoPlugins               []publicplugin.Plugin
	NoPlugins               bool
	MCPServers              []publicmcp.ServerSpec
	NoMCP                   bool
	SkillDirs               []string
	NoSkills                bool
	// UserInputHandler answers ask_user calls for embedded/headless clients.
	// Nil keeps the tool directly visible but makes calls fail fast until an
	// interactive surface enables manual replies.
	UserInputHandler userinput.Handler
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
