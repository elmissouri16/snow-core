// Package snowsdk is the public, embeddable Go API for snow-core. It exposes
// the same agent loop as the CLI — no TUI, no duplicated logic.
package snowsdk

import (
	"context"
	"errors"
	"sync"

	"github.com/elmissouri16/snow-core/internal/app"
	internalsession "github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/worktree"
	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Options configures a Session.
type Options struct {
	// CWD is the working directory. Empty means the caller's cwd.
	CWD string
	// Provider is the provider id (opencode-go | openai-compatible | chatgpt | fake). Empty uses config default.
	Provider string
	// Model is the model id. Empty resolves the provider default.
	Model string
	// SessionPath opens or creates a SQLite .db session. Empty creates an indexed one.
	SessionPath string
	// NoSession uses an ephemeral in-memory conversation. Provider credentials
	// and model caches still use AuthPath/SNOW_HOME.
	NoSession bool
	// AuthPath overrides the default auth file path.
	AuthPath string
	// ConfigPath overrides the default config file path.
	ConfigPath string
	// DisableSandbox explicitly keeps Bash on the host even when the canonical
	// project has a smolvm association. The association is inherited by default.
	DisableSandbox bool
	// RequireSandbox makes Open fail unless the project has a smolvm association.
	RequireSandbox bool
	// PermissionMode is ask|allow|deny. Headless default: deny for mutating tools.
	PermissionMode string
	// AutoApprove allows all tool calls without asking. Dangerous; CI/trusted only.
	AutoApprove bool
	// Tools is a subset allowlist of tool names. Empty = all builtins.
	Tools []string
	// SystemPrompt overrides the built-in preamble.
	SystemPrompt string
	// Thinking is a thinking level (off|minimal|low|medium|high|xhigh|max|ultra).
	Thinking string
	// ReasoningSummary is off|auto|concise|detailed.
	ReasoningSummary string
	// TextVerbosity is low|medium|high.
	TextVerbosity string
	// CollaborationMode is default|plan. Empty restores persisted state or Default.
	CollaborationMode string
	// PlanModeReasoningEffort optionally overrides Plan's Medium preset.
	PlanModeReasoningEffort string
	// APIKey provides an explicit credential (overrides auth.json and env).
	APIKey string
	// BaseURL overrides the active provider base URL. OpenAI-compatible requires
	// either this value or a globally configured endpoint.
	BaseURL string
	// Plugins are explicit argv-based external runtimes.
	Plugins []publicplugin.PluginSpec
	// NoPlugins disables all external and statically supplied plugins.
	NoPlugins bool
	// GoPlugins are statically linked extensions supplied by the embedding app.
	GoPlugins []publicplugin.Plugin
	// MCPServers are explicit stdio or Streamable HTTP MCP servers.
	MCPServers []publicmcp.ServerSpec
	// NoMCP disables configured and explicit MCP servers.
	NoMCP bool
	// SkillDirs adds trusted Agent Skills discovery roots.
	SkillDirs []string
	// NoSkills disables Agent Skills discovery and activation tools.
	NoSkills bool
	// EnableSubagents opts into independent role-scoped child agents. Mutation
	// and recursion remain controlled only by config.
	EnableSubagents  bool
	DisableSubagents bool
	// SubagentProvider and SubagentModel override the configured defaults for children.
	SubagentProvider       string
	SubagentModel          string
	SubagentMaxConcurrency int
	SubagentMaxAgents      int
	SubagentMaxDepth       int
	// UserInputHandler answers direct ask_user tool calls. When nil, calls fail
	// fast with an unavailable-input tool result instead of blocking.
	UserInputHandler func(context.Context, protocol.UserInputRequest) (protocol.UserInputResponse, error)
	// PermissionHandler resolves interactive ask-mode permission requests from
	// this trusted host. Only ask mode uses it, and only when a handler or
	// manual replies are enabled; otherwise ask-mode requests deny fast,
	// preserving the deny-by-default contract.
	PermissionHandler func(context.Context, protocol.PermissionRequest) (protocol.PermissionResponse, error)
}

// Session is an opened agent session.
type Session struct {
	mu     sync.RWMutex
	app    *app.App
	ctx    context.Context
	closed bool
}

// SkillInfo is the dependency-light Agent Skills catalog exposed to SDK users.
type SkillInfo struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
	Location      string            `json:"location"`
	Scope         string            `json:"scope"`
	Source        string            `json:"source"`
	Enabled       bool              `json:"enabled"`
	DisabledBy    string            `json:"disabled_by,omitempty"`
}

// Convenience helpers

var (
	// ErrNotRunning is returned when an operation needs a running turn.
	ErrNotRunning = errors.New("snowsdk: no running turn")
	// ErrStopped is returned after Close.
	ErrStopped = errors.New("snowsdk: session closed")
	// ErrForkDestinationExists reports a destination Snow will not overwrite.
	ErrForkDestinationExists = internalsession.ErrDestinationExists
	// ErrWorktreeDestinationExists reports a Git destination Snow will not reuse.
	ErrWorktreeDestinationExists = worktree.ErrDestinationExists
	// ErrInvalidForkBoundary reports an incomplete assistant/tool boundary.
	ErrInvalidForkBoundary = internalsession.ErrInvalidForkBoundary
	// ErrNotGitRepository reports a source that cannot create a worktree.
	ErrNotGitRepository = worktree.ErrNotRepository
	// ErrGitDirty reports uncommitted tracked or untracked source state.
	ErrGitDirty = worktree.ErrDirty
	// ErrUnsafeWorktreeDestination reports an overlapping or otherwise unsafe path.
	ErrUnsafeWorktreeDestination = worktree.ErrUnsafeDestination
)
