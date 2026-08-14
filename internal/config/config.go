// Package config loads and merges global and project configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	publicmcp "github.com/snow-core/snow/pkg/mcp"
	"github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

// DefaultToolOutputBytes is the default per-tool result cap (256 KiB).
const DefaultToolOutputBytes = 262144

// DefaultBashTimeout is the default bash tool timeout.
const DefaultBashTimeout = 120 * time.Second

// DefaultContextCapBytes is the hard cap for injected project context (100 KiB).
const DefaultContextCapBytes = 100 * 1024

const (
	// MaxConcurrentSubagents bounds provider/process fan-out from one root.
	MaxConcurrentSubagents = 256
	// MaxSubagentsPerSession bounds durable identity/database growth.
	MaxSubagentsPerSession = 4096
)

// ProviderConfig holds per-provider overrides.
type ProviderConfig struct {
	BaseURL      string `json:"base_url,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	// StreamIdleTimeoutMS bounds silence between streamed response bytes.
	// Zero uses the provider default; -1 disables the watchdog.
	StreamIdleTimeoutMS int `json:"stream_idle_timeout_ms,omitempty"`
}

// TUIConfig holds TUI preferences.
type TUIConfig struct {
	Theme string `json:"theme,omitempty"`
	Mouse bool   `json:"mouse"`
}

// CompactionConfig controls manual and pressure-based automatic compaction.
// Zero RetainTokens uses a model-aware retention target. A zero automatic
// threshold disables pressure compaction and overflow recovery.
type CompactionConfig struct {
	RetainTokens         int    `json:"retain_tokens,omitempty"`
	MinRetainedTurns     int    `json:"min_retained_turns,omitempty"`
	SummaryMaxTokens     int    `json:"summary_max_tokens,omitempty"`
	Fallback             string `json:"fallback,omitempty"` // local|error
	Guidance             string `json:"guidance,omitempty"`
	AutoThresholdPercent int    `json:"auto_threshold_percent"`
	// GoalAutoThresholdPercent is a deprecated source-compatibility alias. New
	// JSON is emitted through auto_threshold_percent; legacy JSON is accepted by Load.
	GoalAutoThresholdPercent      int `json:"-"`
	ToolResultInlineBytes         int `json:"tool_result_inline_bytes,omitempty"`
	ArtifactMaxBytes              int `json:"artifact_max_bytes,omitempty"`
	HistoricalToolResultThreshold int `json:"historical_tool_result_threshold_bytes,omitempty"`
}

func DefaultCompaction() CompactionConfig {
	return CompactionConfig{
		MinRetainedTurns: 2, SummaryMaxTokens: 2000, Fallback: "local",
		AutoThresholdPercent: 80, GoalAutoThresholdPercent: 80, ToolResultInlineBytes: 16 << 10,
		ArtifactMaxBytes: 4 << 20, HistoricalToolResultThreshold: 8 << 10,
	}
}

func (c CompactionConfig) Validate() error {
	if c.AutoThresholdPercent == 0 && c.GoalAutoThresholdPercent != 0 {
		c.AutoThresholdPercent = c.GoalAutoThresholdPercent
	}
	if c.RetainTokens < 0 || c.RetainTokens > 1_000_000 {
		return errors.New("config: compaction retain_tokens must be 0..1000000")
	}
	if c.MinRetainedTurns < 1 || c.MinRetainedTurns > 100 {
		return errors.New("config: compaction min_retained_turns must be 1..100")
	}
	if c.SummaryMaxTokens < 128 || c.SummaryMaxTokens > 32_768 {
		return errors.New("config: compaction summary_max_tokens must be 128..32768")
	}
	if c.Fallback != "local" && c.Fallback != "error" {
		return errors.New("config: compaction fallback must be local or error")
	}
	if len(c.Guidance) > 16*1024 {
		return errors.New("config: compaction guidance exceeds 16 KiB")
	}
	if c.AutoThresholdPercent != 0 && (c.AutoThresholdPercent < 50 || c.AutoThresholdPercent > 99) {
		return errors.New("config: compaction auto_threshold_percent must be 0 or 50..99")
	}
	if c.ToolResultInlineBytes < 1024 || c.ToolResultInlineBytes > 1<<20 {
		return errors.New("config: compaction tool_result_inline_bytes must be 1024..1048576")
	}
	if c.ArtifactMaxBytes < c.ToolResultInlineBytes || c.ArtifactMaxBytes > 64<<20 {
		return errors.New("config: compaction artifact_max_bytes must be at least tool_result_inline_bytes and at most 67108864")
	}
	if c.HistoricalToolResultThreshold < 1024 || c.HistoricalToolResultThreshold > 1<<20 {
		return errors.New("config: compaction historical_tool_result_threshold_bytes must be 1024..1048576")
	}
	return nil
}

const defaultTUITheme = "default"

// ValidateTUITheme accepts the small built-in palette set. Keeping this in the
// config package makes persisted settings fail early instead of producing a
// partially styled terminal later.
func ValidateTUITheme(theme string) error {
	theme = strings.TrimSpace(theme)
	if theme == "" {
		return nil
	}
	if len([]rune(theme)) > 64 {
		return fmt.Errorf("config: invalid TUI theme name %q", theme)
	}
	for _, r := range theme {
		if r < 0x20 || strings.ContainsRune(`/\\`, r) {
			return fmt.Errorf("config: invalid TUI theme name %q", theme)
		}
	}
	return nil
}

// SkillsConfig controls Agent Skills discovery. Standard .snow/.agents paths
// remain enabled unless Disabled is true.
type SkillsConfig struct {
	Disabled      bool     `json:"disabled,omitempty"`
	Dirs          []string `json:"dirs,omitempty"`
	IncludeClaude bool     `json:"include_claude,omitempty"`
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
	AllowMutation bool                    `json:"allow_mutation,omitempty"`
}

// SubagentConfig controls the one V2-style subagent implementation.
type SubagentConfig struct {
	Enabled   bool `json:"enabled,omitempty"`
	Recursive bool `json:"recursive,omitempty"`
	// MaxConcurrentThreads is the compatible config key for simultaneously
	// running child agents. The root does not consume a slot.
	MaxConcurrentThreads  int                  `json:"max_concurrent_threads,omitempty"`
	MaxAgentsPerSession   int                  `json:"max_agents_per_session,omitempty"`
	MaxDepth              int                  `json:"max_depth,omitempty"`
	MinWaitTimeoutMS      int                  `json:"min_wait_timeout_ms,omitempty"`
	DefaultWaitTimeoutMS  int                  `json:"default_wait_timeout_ms,omitempty"`
	MaxWaitTimeoutMS      int                  `json:"max_wait_timeout_ms,omitempty"`
	TaskTimeoutMS         int                  `json:"task_timeout_ms,omitempty"`
	MaxResultBytes        int                  `json:"max_result_bytes,omitempty"`
	Durable               bool                 `json:"durable,omitempty"`
	AllowMutation         bool                 `json:"allow_mutation,omitempty"`
	ExposeChildToolEvents bool                 `json:"expose_child_tool_events,omitempty"`
	DefaultProvider       string               `json:"default_provider,omitempty"`
	DefaultModel          string               `json:"default_model,omitempty"`
	DefaultRole           string               `json:"default_role,omitempty"`
	Roles                 map[string]AgentRole `json:"roles,omitempty"`
}

func DefaultSubagents() SubagentConfig {
	return SubagentConfig{MaxConcurrentThreads: 4, MaxAgentsPerSession: 32, MaxDepth: 1,
		MinWaitTimeoutMS: 10_000, DefaultWaitTimeoutMS: 30_000, MaxWaitTimeoutMS: 3_600_000,
		TaskTimeoutMS: 1_800_000, MaxResultBytes: 65_536, Durable: true, ExposeChildToolEvents: true,
		DefaultRole: "general", Roles: map[string]AgentRole{
			"general":     {Description: "General shell-capable investigation (bash remains permission-gated; file mutation is disabled by default)", Tools: []string{"read", "grep", "glob", "artifact_read", "artifact_grep", "activate_skill", "read_skill_resource", "bash"}},
			"explorer":    {Description: "Narrow read-only codebase investigation", Tools: []string{"read", "grep", "glob", "artifact_read", "artifact_grep", "activate_skill", "read_skill_resource"}},
			"implementer": {Description: "Shell-capable implementation task (write/edit require explicit role and global opt-in)"},
		}}
}

// ValidateSubagents rejects unsafe or internally inconsistent limits.
func (c SubagentConfig) ValidateSubagents() error {
	if c.MaxConcurrentThreads < 1 || c.MaxConcurrentThreads > MaxConcurrentSubagents {
		return fmt.Errorf("config: subagents max_concurrent_threads must be 1..%d", MaxConcurrentSubagents)
	}
	if c.MaxAgentsPerSession < 1 || c.MaxAgentsPerSession > MaxSubagentsPerSession {
		return fmt.Errorf("config: subagents max_agents_per_session must be 1..%d", MaxSubagentsPerSession)
	}
	if c.MaxAgentsPerSession < c.MaxConcurrentThreads {
		return errors.New("config: subagents max_agents_per_session is below child concurrency")
	}
	if c.MaxDepth < 1 || c.MaxDepth > 8 {
		return errors.New("config: subagents max_depth must be 1..8")
	}
	if c.MinWaitTimeoutMS < 0 || c.DefaultWaitTimeoutMS < c.MinWaitTimeoutMS || c.MaxWaitTimeoutMS < c.DefaultWaitTimeoutMS || c.MaxWaitTimeoutMS > int((time.Hour*24)/time.Millisecond) {
		return errors.New("config: invalid subagent wait timeout range")
	}
	if c.TaskTimeoutMS <= 0 || c.TaskTimeoutMS > int((time.Hour*24)/time.Millisecond) {
		return errors.New("config: invalid subagent task timeout")
	}
	if c.MaxResultBytes < 1024 || c.MaxResultBytes > protocol.MaxAgentMessageBytes {
		return errors.New("config: subagent max_result_bytes must be 1024..65536")
	}
	if c.DefaultProvider != "" && strings.TrimSpace(c.DefaultProvider) == "" {
		return errors.New("config: subagent default_provider is blank")
	}
	if len(c.DefaultProvider) > protocol.MaxAgentMetadataBytes {
		return errors.New("config: subagent default_provider is too large")
	}
	if c.DefaultModel != "" && strings.TrimSpace(c.DefaultModel) == "" {
		return errors.New("config: subagent default_model is blank")
	}
	if len(c.DefaultModel) > protocol.MaxAgentMetadataBytes {
		return errors.New("config: subagent default_model is too large")
	}
	if c.DefaultRole == "" {
		return errors.New("config: subagent default_role is empty")
	}
	if _, ok := c.Roles[c.DefaultRole]; !ok {
		return fmt.Errorf("config: unknown subagent default role %q", c.DefaultRole)
	}
	childTools := map[string]bool{"read": true, "grep": true, "glob": true, "artifact_read": true, "artifact_grep": true, "activate_skill": true, "read_skill_resource": true, "write": true, "edit": true, "bash": true}
	for name, role := range c.Roles {
		if name == "default" || name == "worker" {
			return fmt.Errorf("config: subagent role %q was renamed; use %q", name, map[string]string{"default": "general", "worker": "implementer"}[name])
		}
		if _, err := protocol.ResolveAgentPath(protocol.RootAgentPath, name); err != nil {
			return fmt.Errorf("config: invalid subagent role %q", name)
		}
		if role.AllowMutation && !c.AllowMutation {
			return fmt.Errorf("config: role %q broadens disabled mutation authority", name)
		}
		for _, tool := range role.Tools {
			if !childTools[tool] {
				return fmt.Errorf("config: role %q references unsupported child tool %q", name, tool)
			}
			if (tool == "write" || tool == "edit") && !role.AllowMutation {
				return fmt.Errorf("config: role %q lists mutation tool %q without allow_mutation", name, tool)
			}
		}
		if role.Provider != "" && strings.TrimSpace(role.Provider) == "" {
			return fmt.Errorf("config: role %q provider is blank", name)
		}
		if len(role.Provider) > protocol.MaxAgentMetadataBytes {
			return fmt.Errorf("config: role %q provider is too large", name)
		}
		if role.Model != "" && strings.TrimSpace(role.Model) == "" {
			return fmt.Errorf("config: role %q model is blank", name)
		}
		if len(role.Model) > protocol.MaxAgentMetadataBytes {
			return fmt.Errorf("config: role %q model is too large", name)
		}
		if role.Thinking != nil {
			if _, err := protocol.ParseThinkingLevel(string(*role.Thinking)); err != nil {
				return fmt.Errorf("config: role %q: %w", name, err)
			}
		}
	}
	return nil
}

// ProjectSkillsConfig is the trust-gated project policy layer. Disabled is a
// pointer so an omitted project value does not override the global default.
type ProjectSkillsConfig struct {
	Disabled  *bool           `json:"disabled,omitempty"`
	Overrides map[string]bool `json:"overrides,omitempty"`
}

// ProjectTUIConfig is the trust-gated subset of TUI preferences.
type ProjectTUIConfig struct {
	Theme *string `json:"theme,omitempty"`
}

// ProjectCompactionConfig is a narrow trust-gated overlay. Pointer fields
// preserve omission while guidance is additive to the fixed/global contract.
type ProjectCompactionConfig struct {
	RetainTokens     *int    `json:"retain_tokens,omitempty"`
	MinRetainedTurns *int    `json:"min_retained_turns,omitempty"`
	SummaryMaxTokens *int    `json:"summary_max_tokens,omitempty"`
	Fallback         *string `json:"fallback,omitempty"`
	Guidance         string  `json:"guidance,omitempty"`
}

// Config is the global snow configuration.
type Config struct {
	DefaultProvider         string                          `json:"default_provider,omitempty"`
	DefaultModel            string                          `json:"default_model,omitempty"`
	PermissionMode          string                          `json:"permission_mode,omitempty"`            // ask|allow|deny
	DefaultProjectTrust     string                          `json:"default_project_trust,omitempty"`      // ask|allow|deny (always|never aliases)
	Thinking                string                          `json:"thinking,omitempty"`                   // off|minimal|low|medium|high|xhigh|max|ultra
	ReasoningSummary        string                          `json:"reasoning_summary,omitempty"`          // off|auto|concise|detailed
	TextVerbosity           string                          `json:"text_verbosity,omitempty"`             // low|medium|high
	CollaborationMode       string                          `json:"collaboration_mode,omitempty"`         // default|plan
	PlanModeReasoningEffort string                          `json:"plan_mode_reasoning_effort,omitempty"` // optional off|minimal|low|medium|high|xhigh|max|ultra
	ToolOutputBytes         int                             `json:"tool_output_bytes,omitempty"`
	BashTimeoutMS           int                             `json:"bash_timeout_ms,omitempty"`
	ContextCapBytes         int                             `json:"context_cap_bytes,omitempty"`
	SystemPromptFile        string                          `json:"system_prompt_file,omitempty"`
	Providers               map[string]ProviderConfig       `json:"providers,omitempty"`
	TUI                     TUIConfig                       `json:"tui,omitempty"`
	Plugins                 []plugin.PluginSpec             `json:"plugins,omitempty"`
	MCPServers              map[string]publicmcp.ServerSpec `json:"mcp_servers,omitempty"`
	Skills                  SkillsConfig                    `json:"skills,omitempty"`
	Subagents               SubagentConfig                  `json:"subagents,omitempty"`
	Compaction              CompactionConfig                `json:"compaction,omitempty"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		DefaultProvider:     "opencode-go",
		PermissionMode:      "ask",
		DefaultProjectTrust: "ask",
		Thinking:            "off",
		ReasoningSummary:    "auto",
		TextVerbosity:       "low",
		CollaborationMode:   "default",
		ToolOutputBytes:     DefaultToolOutputBytes,
		BashTimeoutMS:       int(DefaultBashTimeout / time.Millisecond),
		ContextCapBytes:     DefaultContextCapBytes,
		Providers: map[string]ProviderConfig{
			"opencode-go":       {},
			"openai-compatible": {},
			"chatgpt":           {},
		},
		MCPServers: map[string]publicmcp.ServerSpec{},
		Skills:     SkillsConfig{Overrides: map[string]bool{}},
		Subagents:  DefaultSubagents(),
		Compaction: DefaultCompaction(),
		TUI:        TUIConfig{Theme: "default", Mouse: true},
	}
}

// GlobalDir returns the snow global config directory.
func GlobalDir() string {
	if d := os.Getenv("SNOW_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".snow"
	}
	return filepath.Join(home, ".snow")
}

// DefaultPaths returns the standard global file paths.
func DefaultPaths() (configPath, authPath, trustPath string) {
	dir := GlobalDir()
	return filepath.Join(dir, "config.json"),
		filepath.Join(dir, "auth.json"),
		filepath.Join(dir, "trust.json")
}

// Load reads the global config file if present and merges onto defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]publicmcp.ServerSpec{}
	}
	if cfg.Skills.Overrides == nil {
		cfg.Skills.Overrides = map[string]bool{}
	}
	if cfg.Subagents.Roles == nil {
		cfg.Subagents.Roles = DefaultSubagents().Roles
	}
	if cfg.TUI.Theme == "" {
		cfg.TUI.Theme = defaultTUITheme
	}
	defaults := DefaultCompaction()
	var rawConfig struct {
		Compaction map[string]json.RawMessage `json:"compaction"`
	}
	_ = json.Unmarshal(data, &rawConfig)
	if _, present := rawConfig.Compaction["auto_threshold_percent"]; !present {
		if legacy, ok := rawConfig.Compaction["goal_auto_threshold_percent"]; ok {
			_ = json.Unmarshal(legacy, &cfg.Compaction.AutoThresholdPercent)
		} else {
			cfg.Compaction.AutoThresholdPercent = defaults.AutoThresholdPercent
		}
	}
	cfg.Compaction.GoalAutoThresholdPercent = cfg.Compaction.AutoThresholdPercent
	if cfg.Compaction.MinRetainedTurns == 0 {
		cfg.Compaction.MinRetainedTurns = defaults.MinRetainedTurns
	}
	if cfg.Compaction.SummaryMaxTokens == 0 {
		cfg.Compaction.SummaryMaxTokens = defaults.SummaryMaxTokens
	}
	if cfg.Compaction.Fallback == "" {
		cfg.Compaction.Fallback = defaults.Fallback
	}
	if cfg.Compaction.ToolResultInlineBytes == 0 {
		cfg.Compaction.ToolResultInlineBytes = defaults.ToolResultInlineBytes
	}
	if cfg.Compaction.ArtifactMaxBytes == 0 {
		cfg.Compaction.ArtifactMaxBytes = defaults.ArtifactMaxBytes
	}
	if cfg.Compaction.HistoricalToolResultThreshold == 0 {
		cfg.Compaction.HistoricalToolResultThreshold = defaults.HistoricalToolResultThreshold
	}
	for providerID, providerConfig := range cfg.Providers {
		if providerConfig.StreamIdleTimeoutMS < -1 || providerConfig.StreamIdleTimeoutMS > int((24*time.Hour)/time.Millisecond) {
			return cfg, fmt.Errorf("config: provider %q stream_idle_timeout_ms must be -1, 0, or at most 86400000", providerID)
		}
	}
	if err := ValidateTUITheme(cfg.TUI.Theme); err != nil {
		return cfg, err
	}
	if err := cfg.Subagents.ValidateSubagents(); err != nil {
		return cfg, err
	}
	if err := cfg.Compaction.Validate(); err != nil {
		return cfg, err
	}
	if err := validateSystemPromptFile(cfg.SystemPromptFile, true); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func validateSystemPromptFile(path string, allowEmpty bool) error {
	if path == "" {
		if allowEmpty {
			return nil
		}
		return errors.New("config: system_prompt_file must not be empty")
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("config: system_prompt_file must not be blank")
	}
	if len(path) > 4096 {
		return errors.New("config: system_prompt_file path exceeds 4096 bytes")
	}
	return nil
}

// ProjectExtensions are the only project configuration fields loaded after a
// trust allow. Project files cannot override global provider or permissions.
type ProjectExtensions struct {
	Plugins          []plugin.PluginSpec             `json:"plugins,omitempty"`
	MCPServers       map[string]publicmcp.ServerSpec `json:"mcp_servers,omitempty"`
	Skills           ProjectSkillsConfig             `json:"skills,omitempty"`
	TUI              ProjectTUIConfig                `json:"tui,omitempty"`
	Compaction       ProjectCompactionConfig         `json:"compaction,omitempty"`
	SystemPromptFile *string                         `json:"system_prompt_file,omitempty"`
}

// ApplyProjectPreferences applies only explicitly allowed trust-gated fields.
func ApplyProjectPreferences(cfg *Config, project ProjectExtensions) error {
	if cfg == nil {
		return nil
	}
	if project.TUI.Theme != nil {
		cfg.TUI.Theme = strings.TrimSpace(*project.TUI.Theme)
	}
	if project.Compaction.RetainTokens != nil {
		cfg.Compaction.RetainTokens = *project.Compaction.RetainTokens
	}
	if project.Compaction.MinRetainedTurns != nil {
		cfg.Compaction.MinRetainedTurns = *project.Compaction.MinRetainedTurns
	}
	if project.Compaction.SummaryMaxTokens != nil {
		cfg.Compaction.SummaryMaxTokens = *project.Compaction.SummaryMaxTokens
	}
	if project.Compaction.Fallback != nil {
		cfg.Compaction.Fallback = strings.TrimSpace(*project.Compaction.Fallback)
	}
	if strings.TrimSpace(project.Compaction.Guidance) != "" {
		if cfg.Compaction.Guidance != "" {
			cfg.Compaction.Guidance += "\n"
		}
		cfg.Compaction.Guidance += project.Compaction.Guidance
	}
	if project.SystemPromptFile != nil {
		if err := validateSystemPromptFile(*project.SystemPromptFile, false); err != nil {
			return err
		}
		cfg.SystemPromptFile = strings.TrimSpace(*project.SystemPromptFile)
	}
	return cfg.Compaction.Validate()
}

// LoadProject reads only the plugin declarations from a project config. It is
// intentionally separate from Load so callers can avoid reading project input
// until the trust decision has explicitly allowed it.
func LoadProject(path string) ([]plugin.PluginSpec, error) {
	extensions, err := LoadProjectExtensions(path)
	return extensions.Plugins, err
}

// LoadProjectExtensions reads the restricted trust-gated project configuration.
func LoadProjectExtensions(path string) (ProjectExtensions, error) {
	if path == "" {
		return ProjectExtensions{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProjectExtensions{}, nil
		}
		return ProjectExtensions{}, fmt.Errorf("config: read project %s: %w", path, err)
	}
	var raw ProjectExtensions
	if err := json.Unmarshal(data, &raw); err != nil {
		return ProjectExtensions{}, fmt.Errorf("config: parse project %s: %w", path, err)
	}
	if raw.MCPServers == nil {
		raw.MCPServers = map[string]publicmcp.ServerSpec{}
	}
	if raw.Skills.Overrides == nil {
		raw.Skills.Overrides = map[string]bool{}
	}
	if raw.SystemPromptFile != nil {
		if err := validateSystemPromptFile(*raw.SystemPromptFile, false); err != nil {
			return ProjectExtensions{}, err
		}
	}
	return raw, nil
}

// LoadWithProject loads global configuration and, only when allowProject is
// true, appends explicitly declared project plugins. Project config cannot
// override provider, permission, or other global execution policy.
func LoadWithProject(globalPath, projectPath string, allowProject bool) (Config, error) {
	cfg, err := Load(globalPath)
	if err != nil || !allowProject {
		return cfg, err
	}
	extensions, err := LoadProjectExtensions(projectPath)
	if err != nil {
		return cfg, err
	}
	cfg.Plugins = append(cfg.Plugins, extensions.Plugins...)
	if cfg.MCPServers == nil {
		cfg.MCPServers = map[string]publicmcp.ServerSpec{}
	}
	for id, spec := range extensions.MCPServers {
		cfg.MCPServers[id] = spec
	}
	return cfg, nil
}

// Save writes the config file (creating parent dirs).
func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

// BashTimeout returns the configured bash timeout as a duration.
func (c Config) BashTimeout() time.Duration {
	if c.BashTimeoutMS <= 0 {
		return DefaultBashTimeout
	}
	return time.Duration(c.BashTimeoutMS) * time.Millisecond
}

// ToolOutputLimit returns the tool output byte cap.
func (c Config) ToolOutputLimit() int {
	if c.ToolOutputBytes <= 0 {
		return DefaultToolOutputBytes
	}
	return c.ToolOutputBytes
}
