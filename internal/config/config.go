// Package config loads and merges global and project configuration.
package config

import (
	"errors"
	"sync"
	"time"

	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// DefaultToolOutputBytes is the default per-tool result cap (256 KiB).
const DefaultToolOutputBytes = 262144

// DefaultBashTimeout is the default bash tool timeout.
const DefaultBashTimeout = 120 * time.Second

// DefaultContextCapBytes is the hard cap for injected project context (100 KiB).
const DefaultContextCapBytes = 100 * 1024

// DefaultFixedContextBudgetPercent reserves most of a model window for task
// messages while bounding recurring system instructions and tool schemas.
const DefaultFixedContextBudgetPercent = 25

const (
	DefaultProcessMaxRunning          = 4
	DefaultProcessMaxRecords          = 32
	DefaultProcessRetainedOutputBytes = 1 << 20
)

var updateMu sync.Mutex

const (
	// MaxConcurrentSubagents bounds provider/process fan-out from one root.
	MaxConcurrentSubagents = 256
	// MaxSubagentsPerSession bounds durable identity/database growth.
	MaxSubagentsPerSession = 4096
)

// ProviderConfig holds per-provider overrides.
const ProviderTypeOpenAICompatible = "openai-compatible"

type ProviderConfig struct {
	// Type identifies a named provider profile. Empty retains the built-in
	// provider implied by the map key for backward compatibility.
	Type         string `json:"type,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	// StreamIdleTimeoutMS bounds silence between streamed response bytes.
	// Zero uses the provider default; -1 disables the watchdog.
	StreamIdleTimeoutMS int `json:"stream_idle_timeout_ms,omitzero"`
}

type TUIConfig struct {
	Theme string `json:"theme,omitempty"`
	Mouse bool   `json:"mouse"`
}

// DebugConfig controls the shared opt-in runtime diagnostics recorder. Dumps
// are still created only by an explicit command, SDK/RPC call, or CLI path.
type DebugConfig struct {
	Enabled bool `json:"enabled"`
}

// UpdateConfig controls opt-in interactive release checks and installation.
// It is global operator policy and is never loaded from project configuration.
type UpdateConfig struct {
	CheckOnStartup bool `json:"check_on_startup"`
	AutoUpdate     bool `json:"auto_update"`
}

// Validate ensures automatic installation cannot be enabled without startup checks.
func (c UpdateConfig) Validate() error {
	if c.AutoUpdate && !c.CheckOnStartup {
		return errors.New("config: updates auto_update requires check_on_startup")
	}
	return nil
}

// RetryProfileConfig bounds one consecutive provider outage. It is global-only
// operator policy and is never loaded from trust-gated project configuration.
type RetryProfileConfig struct {
	MaxAttempts    int `json:"max_attempts"`
	MaxElapsedMS   int `json:"max_elapsed_ms"`
	InitialDelayMS int `json:"initial_delay_ms"`
	MaxDelayMS     int `json:"max_delay_ms"`
	JitterPercent  int `json:"jitter_percent"`
}

type RetryConfig struct {
	Normal RetryProfileConfig `json:"normal"`
	Goal   RetryProfileConfig `json:"goal"`
}

// CompactionConfig controls manual and pressure-based automatic compaction.
// Zero RetainTokens uses a model-aware retention target. A zero automatic
// threshold disables pressure compaction and overflow recovery.
type CompactionConfig struct {
	RetainTokens         int    `json:"retain_tokens,omitzero"`
	MinRetainedTurns     int    `json:"min_retained_turns,omitzero"`
	SummaryMaxTokens     int    `json:"summary_max_tokens,omitzero"`
	Fallback             string `json:"fallback,omitempty"` // local|error
	Guidance             string `json:"guidance,omitempty"`
	AutoThresholdPercent int    `json:"auto_threshold_percent"`
	// ToolHistoryBudgetPercent triggers safe whole-turn compaction when completed
	// tool calls/results exceed this share of the model window. Zero disables it.
	ToolHistoryBudgetPercent      int `json:"tool_history_budget_percent"`
	ToolResultInlineBytes         int `json:"tool_result_inline_bytes,omitzero"`
	ArtifactMaxBytes              int `json:"artifact_max_bytes,omitzero"`
	HistoricalToolResultThreshold int `json:"historical_tool_result_threshold_bytes,omitzero"`
}

const defaultTUITheme = "default"

var builtInTUIThemes = [...]string{
	"default",
	"frost",
	"ember",
	"aurora",
}

// legacyTUIThemes remain reserved and valid configuration/custom-theme bases,
// but are intentionally omitted from the Settings picker.
var legacyTUIThemes = [...]string{
	"dark",
	"light",
	"high-contrast",
	"nord",
	"dracula",
	"gruvbox",
}

// SkillsConfig controls Agent Skills discovery. Standard .snow/.agents paths
// remain enabled unless Disabled is true.
type SkillsConfig struct {
	Disabled      bool     `json:"disabled,omitzero"`
	Dirs          []string `json:"dirs,omitempty"`
	IncludeClaude bool     `json:"include_claude,omitzero"`
	// Overrides enables or disables individual discovered skills by name.
	// Presence is significant: true can re-enable a skill after a broader
	// disabled policy, while false suppresses it without removing its files.
	Overrides map[string]bool `json:"overrides,omitempty"`
}

// ProjectSkillsConfig is the trust-gated project policy layer. Disabled is a
// pointer so an omitted project value does not override the global default.
// AgentRole is a constrained child overlay. Tools and mutation are always
// intersected with operator policy; roles cannot alter provider credentials,
// roots, trust, or permission mode.
type AgentRole struct {
	Description   string                  `json:"description,omitempty"`
	System        string                  `json:"system,omitempty"`
	Provider      string                  `json:"provider,omitempty"`
	Model         string                  `json:"model,omitempty"`
	Thinking      *protocol.ThinkingLevel `json:"thinking,omitempty"`
	Tools         []string                `json:"tools,omitempty"`
	AllowMutation bool                    `json:"allow_mutation,omitzero"`
}

// SubagentConfig controls the one V2-style subagent implementation.
type SubagentConfig struct {
	Enabled   bool `json:"enabled,omitzero"`
	Recursive bool `json:"recursive,omitzero"`
	// MaxConcurrentThreads is the compatible config key for simultaneously
	// running child agents. The root does not consume a slot.
	MaxConcurrentThreads  int                  `json:"max_concurrent_threads,omitzero"`
	MaxAgentsPerSession   int                  `json:"max_agents_per_session,omitzero"`
	MaxDepth              int                  `json:"max_depth,omitzero"`
	MinWaitTimeoutMS      int                  `json:"min_wait_timeout_ms,omitzero"`
	DefaultWaitTimeoutMS  int                  `json:"default_wait_timeout_ms,omitzero"`
	MaxWaitTimeoutMS      int                  `json:"max_wait_timeout_ms,omitzero"`
	TaskTimeoutMS         int                  `json:"task_timeout_ms,omitzero"`
	MaxResultBytes        int                  `json:"max_result_bytes,omitzero"`
	Durable               bool                 `json:"durable,omitzero"`
	AllowMutation         bool                 `json:"allow_mutation,omitzero"`
	ExposeChildToolEvents bool                 `json:"expose_child_tool_events,omitzero"`
	DefaultProvider       string               `json:"default_provider,omitempty"`
	DefaultModel          string               `json:"default_model,omitempty"`
	DefaultRole           string               `json:"default_role,omitempty"`
	Roles                 map[string]AgentRole `json:"roles,omitempty"`
}

// ProjectSkillsConfig is the trust-gated project policy layer. Disabled is a
// pointer so an omitted project value does not override the global default.
type ProjectSkillsConfig struct {
	Disabled  *bool           `json:"disabled,omitzero"`
	Overrides map[string]bool `json:"overrides,omitempty"`
}

// ProjectTUIConfig is the trust-gated subset of TUI preferences.
type ProjectTUIConfig struct {
	Theme *string `json:"theme,omitempty"`
}

// ProjectCompactionConfig is a narrow trust-gated overlay. Pointer fields
// preserve omission while guidance is additive to the fixed/global contract.
type ProjectCompactionConfig struct {
	RetainTokens     *int    `json:"retain_tokens,omitzero"`
	MinRetainedTurns *int    `json:"min_retained_turns,omitzero"`
	SummaryMaxTokens *int    `json:"summary_max_tokens,omitzero"`
	Fallback         *string `json:"fallback,omitempty"`
	Guidance         string  `json:"guidance,omitempty"`
}

// ProjectSelection remembers the interactive provider/model/effort choice for
// one working directory. It lives in the operator-owned global configuration;
// project input cannot alter it.
type ProjectSelection struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

// ProcessConfig bounds app-owned managed background processes.
type ProcessConfig struct {
	MaxRunning          int `json:"max_running,omitzero"`
	MaxRecords          int `json:"max_records,omitzero"`
	RetainedOutputBytes int `json:"retained_output_bytes,omitzero"`
}

// Config is the global snow configuration.
type Config struct {
	DefaultProvider           string                          `json:"default_provider,omitempty"`
	DefaultModel              string                          `json:"default_model,omitempty"`
	ProjectSelections         map[string]ProjectSelection     `json:"project_selections,omitempty"`
	DefaultProjectTrust       string                          `json:"default_project_trust,omitempty"`      // ask|allow|deny (always|never aliases)
	Thinking                  string                          `json:"thinking,omitempty"`                   // off|minimal|low|medium|high|xhigh|max|ultra
	ReasoningSummary          string                          `json:"reasoning_summary,omitempty"`          // off|auto|concise|detailed
	TextVerbosity             string                          `json:"text_verbosity,omitempty"`             // low|medium|high
	CollaborationMode         string                          `json:"collaboration_mode,omitempty"`         // default|plan
	PlanModeReasoningEffort   string                          `json:"plan_mode_reasoning_effort,omitempty"` // optional off|minimal|low|medium|high|xhigh|max|ultra
	ToolOutputBytes           int                             `json:"tool_output_bytes,omitzero"`
	BashTimeoutMS             int                             `json:"bash_timeout_ms,omitzero"`
	ContextCapBytes           int                             `json:"context_cap_bytes,omitzero"`
	FixedContextBudgetPercent int                             `json:"fixed_context_budget_percent,omitzero"`
	SystemPromptFile          string                          `json:"system_prompt_file,omitempty"`
	Providers                 map[string]ProviderConfig       `json:"providers,omitempty"`
	TUI                       TUIConfig                       `json:"tui,omitzero"`
	Debug                     DebugConfig                     `json:"debug,omitzero"`
	Updates                   UpdateConfig                    `json:"updates,omitzero"`
	Plugins                   []plugin.PluginSpec             `json:"plugins,omitempty"`
	MCPServers                map[string]publicmcp.ServerSpec `json:"mcp_servers,omitempty"`
	Skills                    SkillsConfig                    `json:"skills,omitzero"`
	Subagents                 SubagentConfig                  `json:"subagents,omitzero"`
	Processes                 ProcessConfig                   `json:"processes,omitzero"`
	Retry                     RetryConfig                     `json:"retry,omitzero"`
	Compaction                CompactionConfig                `json:"compaction,omitzero"`
}

// ProjectExtensions are the only project configuration fields loaded after a
// trust allow. Project files cannot override global provider or permissions.
type ProjectExtensions struct {
	Plugins          []plugin.PluginSpec             `json:"plugins,omitempty"`
	MCPServers       map[string]publicmcp.ServerSpec `json:"mcp_servers,omitempty"`
	Skills           ProjectSkillsConfig             `json:"skills,omitzero"`
	TUI              ProjectTUIConfig                `json:"tui,omitzero"`
	Compaction       ProjectCompactionConfig         `json:"compaction,omitzero"`
	SystemPromptFile *string                         `json:"system_prompt_file,omitempty"`
}
