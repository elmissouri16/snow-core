// Package config loads and merges global and project configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

// TUIConfig holds TUI preferences.
type TUIConfig struct {
	Theme string `json:"theme,omitempty"`
	Mouse bool   `json:"mouse,omitempty"`
}

const defaultTUITheme = "default"

// ValidateTUITheme accepts the small built-in palette set. Keeping this in the
// config package makes persisted settings fail early instead of producing a
// partially styled terminal later.
func ValidateTUITheme(theme string) error {
	switch theme {
	case "", "default", "dark", "light", "high-contrast":
		return nil
	default:
		return fmt.Errorf("config: unsupported TUI theme %q (use default, dark, light, or high-contrast)", theme)
	}
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
	DefaultRole           string               `json:"default_role,omitempty"`
	Roles                 map[string]AgentRole `json:"roles,omitempty"`
}

func DefaultSubagents() SubagentConfig {
	return SubagentConfig{MaxConcurrentThreads: 4, MaxAgentsPerSession: 32, MaxDepth: 1,
		MinWaitTimeoutMS: 10_000, DefaultWaitTimeoutMS: 30_000, MaxWaitTimeoutMS: 3_600_000,
		TaskTimeoutMS: 1_800_000, MaxResultBytes: 65_536, Durable: true, ExposeChildToolEvents: true,
		DefaultRole: "default", Roles: map[string]AgentRole{
			"default":  {Description: "General shell-capable investigation (bash remains permission-gated; file mutation is disabled by default)", Tools: []string{"read", "grep", "glob", "activate_skill", "read_skill_resource", "bash"}},
			"explorer": {Description: "Narrow read-only codebase investigation", Tools: []string{"read", "grep", "glob", "activate_skill", "read_skill_resource"}},
			"worker":   {Description: "Shell-capable implementation task (write/edit require explicit role and global opt-in)"},
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
	if c.DefaultRole == "" {
		return errors.New("config: subagent default_role is empty")
	}
	if _, ok := c.Roles[c.DefaultRole]; !ok {
		return fmt.Errorf("config: unknown subagent default role %q", c.DefaultRole)
	}
	childTools := map[string]bool{"read": true, "grep": true, "glob": true, "activate_skill": true, "read_skill_resource": true, "write": true, "edit": true, "bash": true}
	for name, role := range c.Roles {
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

// Config is the global snow configuration.
type Config struct {
	DefaultProvider         string                          `json:"default_provider,omitempty"`
	DefaultModel            string                          `json:"default_model,omitempty"`
	PermissionMode          string                          `json:"permission_mode,omitempty"`            // ask|allow|deny
	DefaultProjectTrust     string                          `json:"default_project_trust,omitempty"`      // ask|always|never
	Thinking                string                          `json:"thinking,omitempty"`                   // off|minimal|low|medium|high
	ReasoningSummary        string                          `json:"reasoning_summary,omitempty"`          // off|auto|concise|detailed
	TextVerbosity           string                          `json:"text_verbosity,omitempty"`             // low|medium|high
	CollaborationMode       string                          `json:"collaboration_mode,omitempty"`         // default|plan
	PlanModeReasoningEffort string                          `json:"plan_mode_reasoning_effort,omitempty"` // optional off|minimal|low|medium|high
	ToolOutputBytes         int                             `json:"tool_output_bytes,omitempty"`
	BashTimeoutMS           int                             `json:"bash_timeout_ms,omitempty"`
	ContextCapBytes         int                             `json:"context_cap_bytes,omitempty"`
	Providers               map[string]ProviderConfig       `json:"providers,omitempty"`
	TUI                     TUIConfig                       `json:"tui,omitempty"`
	Plugins                 []plugin.PluginSpec             `json:"plugins,omitempty"`
	MCPServers              map[string]publicmcp.ServerSpec `json:"mcp_servers,omitempty"`
	Skills                  SkillsConfig                    `json:"skills,omitempty"`
	Subagents               SubagentConfig                  `json:"subagents,omitempty"`
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
			"opencode-go": {},
			"chatgpt":     {},
		},
		MCPServers: map[string]publicmcp.ServerSpec{},
		Skills:     SkillsConfig{Overrides: map[string]bool{}},
		Subagents:  DefaultSubagents(),
		TUI:        TUIConfig{Theme: "default", Mouse: false},
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
	if err := ValidateTUITheme(cfg.TUI.Theme); err != nil {
		return cfg, err
	}
	if err := cfg.Subagents.ValidateSubagents(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// ProjectExtensions are the only project configuration fields loaded after a
// trust allow. Project files cannot override global provider or permissions.
type ProjectExtensions struct {
	Plugins    []plugin.PluginSpec             `json:"plugins,omitempty"`
	MCPServers map[string]publicmcp.ServerSpec `json:"mcp_servers,omitempty"`
	Skills     ProjectSkillsConfig             `json:"skills,omitempty"`
}

// LoadProject reads only the plugin declarations from a project config. It is
// intentionally separate from Load so callers can avoid reading project input
// until the trust decision has explicitly allowed it.
func LoadProject(path string) ([]plugin.PluginSpec, error) {
	extensions, err := LoadProjectExtensions(path)
	return extensions.Plugins, err
}

// LoadProjectExtensions reads trust-gated plugin and MCP declarations.
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
