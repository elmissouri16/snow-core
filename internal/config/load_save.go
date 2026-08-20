package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	MaxConfigFileBytes   = 4 << 20
	MaxProjectSelections = 4096
)

func readConfigFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxConfigFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxConfigFileBytes {
		return nil, fmt.Errorf("configuration exceeds %d byte limit", MaxConfigFileBytes)
	}
	return data, nil
}

// TUIConfig holds TUI preferences.
// ValidateProviderProfileID keeps profile IDs safe for config/auth map keys and
// unambiguous in CLI/TUI provider selectors.
func ValidateProviderProfileID(id string) error {
	if id == "" || len(id) > 64 {
		return errors.New("config: provider profile name must be 1..64 characters")
	}
	for i, r := range id {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && (r == '-' || r == '_' || r == '.')
		if !valid {
			return fmt.Errorf("config: provider profile name %q must use lowercase letters, digits, and internal ._- characters", id)
		}
	}
	switch id {
	case "opencode-go", "chatgpt", "fake":
		return fmt.Errorf("config: provider profile name %q is reserved", id)
	}
	return nil
}

func IsOpenAICompatibleProfile(id string, provider ProviderConfig) bool {
	return id == ProviderTypeOpenAICompatible || provider.Type == ProviderTypeOpenAICompatible
}

func DefaultProcesses() ProcessConfig {
	return ProcessConfig{
		MaxRunning:          DefaultProcessMaxRunning,
		MaxRecords:          DefaultProcessMaxRecords,
		RetainedOutputBytes: DefaultProcessRetainedOutputBytes,
	}
}

func (c ProcessConfig) Validate() error {
	if c.MaxRunning < 1 || c.MaxRunning > 32 {
		return errors.New("config: processes max_running must be 1..32")
	}
	if c.MaxRecords < c.MaxRunning || c.MaxRecords > 256 {
		return errors.New("config: processes max_records must be at least max_running and at most 256")
	}
	if c.RetainedOutputBytes < 64<<10 || c.RetainedOutputBytes > 16<<20 {
		return errors.New("config: processes retained_output_bytes must be 65536..16777216")
	}
	return nil
}

func DefaultCompaction() CompactionConfig {
	return CompactionConfig{
		MinRetainedTurns: 2, SummaryMaxTokens: 2000, Fallback: "local",
		AutoThresholdPercent: 80, ToolHistoryBudgetPercent: 20, GoalAutoThresholdPercent: 80, ToolResultInlineBytes: 16 << 10,
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
	if c.ToolHistoryBudgetPercent != 0 && (c.ToolHistoryBudgetPercent < 5 || c.ToolHistoryBudgetPercent > 50) {
		return errors.New("config: compaction tool_history_budget_percent must be 0 or 5..50")
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

// BuiltInTUIThemes returns the selectable built-in themes in display order.
func BuiltInTUIThemes() []string {
	return append([]string(nil), builtInTUIThemes[:]...)
}

// IsBuiltInTUITheme reports whether name is reserved for a built-in palette.
func IsBuiltInTUITheme(name string) bool {
	for _, builtIn := range builtInTUIThemes {
		if name == builtIn {
			return true
		}
	}
	return false
}

// ValidateTUITheme accepts built-in names and safe custom-theme names. Keeping
// this in the config package makes persisted settings fail early instead of
// producing a partially styled terminal later.
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

func DefaultSubagents() SubagentConfig {
	return SubagentConfig{MaxConcurrentThreads: 4, MaxAgentsPerSession: 32, MaxDepth: 1,
		MinWaitTimeoutMS: 10_000, DefaultWaitTimeoutMS: 30_000, MaxWaitTimeoutMS: 3_600_000,
		TaskTimeoutMS: 1_800_000, MaxResultBytes: 65_536, Durable: true, ExposeChildToolEvents: true,
		DefaultRole: "general", Roles: map[string]AgentRole{
			"general":     {Description: "General shell-capable investigation (bash remains permission-gated; file mutation is disabled by default)", Tools: []string{"read", "grep", "glob", "artifact_read", "artifact_grep", "activate_skill", "deactivate_skill", "read_skill_resource", "bash"}},
			"explorer":    {Description: "Narrow read-only codebase investigation", Tools: []string{"read", "grep", "glob", "artifact_read", "artifact_grep", "activate_skill", "deactivate_skill", "read_skill_resource"}},
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
	childTools := map[string]bool{"read": true, "grep": true, "glob": true, "artifact_read": true, "artifact_grep": true, "activate_skill": true, "deactivate_skill": true, "read_skill_resource": true, "write": true, "edit": true, "bash": true}
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
		ProjectSelections:   map[string]ProjectSelection{},
		Providers: map[string]ProviderConfig{
			"opencode-go":       {},
			"openai-compatible": {},
			"chatgpt":           {},
		},
		MCPServers: map[string]publicmcp.ServerSpec{},
		Skills:     SkillsConfig{Overrides: map[string]bool{}},
		Subagents:  DefaultSubagents(),
		Processes:  DefaultProcesses(),
		Compaction: DefaultCompaction(),
		TUI:        TUIConfig{Theme: "default", Mouse: true},
	}
}

// Clone returns a deep copy suitable for applying runtime-only overlays without
// mutating the operator configuration through shared maps or slices.
func Clone(cfg Config) (Config, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("config: clone marshal: %w", err)
	}
	var cloned Config
	if err := json.Unmarshal(data, &cloned); err != nil {
		return Config{}, fmt.Errorf("config: clone unmarshal: %w", err)
	}
	return cloned, nil
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
	data, err := readConfigFile(path)
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
	if cfg.ProjectSelections == nil {
		cfg.ProjectSelections = map[string]ProjectSelection{}
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
	_, autoThresholdPresent := rawConfig.Compaction["auto_threshold_percent"]
	_, legacyAutoThresholdPresent := rawConfig.Compaction["goal_auto_threshold_percent"]
	if !autoThresholdPresent {
		if legacy, ok := rawConfig.Compaction["goal_auto_threshold_percent"]; ok {
			_ = json.Unmarshal(legacy, &cfg.Compaction.AutoThresholdPercent)
		} else {
			cfg.Compaction.AutoThresholdPercent = defaults.AutoThresholdPercent
		}
	}
	if _, present := rawConfig.Compaction["tool_history_budget_percent"]; !present {
		// Existing configurations that explicitly disabled automatic compaction
		// must not silently gain a new automatic trigger after upgrade.
		if cfg.Compaction.AutoThresholdPercent == 0 && (autoThresholdPresent || legacyAutoThresholdPresent) {
			cfg.Compaction.ToolHistoryBudgetPercent = 0
		} else {
			cfg.Compaction.ToolHistoryBudgetPercent = defaults.ToolHistoryBudgetPercent
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
	if len(cfg.ProjectSelections) > MaxProjectSelections {
		return cfg, fmt.Errorf("config: project_selections exceeds %d entry limit", MaxProjectSelections)
	}
	for projectPath, selection := range cfg.ProjectSelections {
		if err := validateProjectSelection(projectPath, selection); err != nil {
			return cfg, err
		}
	}
	for providerID, providerConfig := range cfg.Providers {
		if providerConfig.Type != "" {
			if providerConfig.Type != ProviderTypeOpenAICompatible {
				return cfg, fmt.Errorf("config: provider %q has unsupported type %q", providerID, providerConfig.Type)
			}
			if err := ValidateProviderProfileID(providerID); err != nil {
				return cfg, err
			}
		}
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
	if err := cfg.Processes.Validate(); err != nil {
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

func projectSelectionKey(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", errors.New("config: project selection path must not be blank")
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("config: project selection path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if len(absolute) > 4096 {
		return "", errors.New("config: project selection path exceeds 4096 bytes")
	}
	return absolute, nil
}

func validateProjectSelection(projectPath string, selection ProjectSelection) error {
	key, err := projectSelectionKey(projectPath)
	if err != nil {
		return err
	}
	if key != projectPath {
		return fmt.Errorf("config: project selection path %q must be absolute and clean", projectPath)
	}
	values := []struct {
		name  string
		value string
	}{
		{name: "provider", value: selection.Provider},
		{name: "model", value: selection.Model},
	}
	for _, field := range values {
		if field.value != "" && strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("config: project selection %s for %q must not be blank", field.name, projectPath)
		}
		if len(field.value) > protocol.MaxAgentMetadataBytes {
			return fmt.Errorf("config: project selection %s for %q is too large", field.name, projectPath)
		}
	}
	if selection.Thinking != "" {
		if _, err := protocol.ParseThinkingLevel(selection.Thinking); err != nil {
			return fmt.Errorf("config: project selection for %q: %w", projectPath, err)
		}
	}
	return nil
}

// ApplyProjectSelection overlays the remembered interactive selection for cwd
// onto the effective runtime defaults. It does not mutate the stored map.
func ApplyProjectSelection(cfg *Config, cwd string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	key, err := projectSelectionKey(cwd)
	if err != nil {
		return false, err
	}
	selection, ok := cfg.ProjectSelections[key]
	if !ok {
		return false, nil
	}
	if err := validateProjectSelection(key, selection); err != nil {
		return false, err
	}
	if selection.Provider != "" {
		if selection.Provider != cfg.DefaultProvider && selection.Model == "" {
			cfg.DefaultModel = ""
		}
		cfg.DefaultProvider = selection.Provider
	}
	if selection.Model != "" {
		cfg.DefaultModel = selection.Model
	}
	if selection.Thinking != "" {
		cfg.Thinking = selection.Thinking
	}
	return true, nil
}

// WithProjectSelection returns cfg with a copied project-selection map and the
// normalized complete selection for cwd. Other project entries are preserved.
func WithProjectSelection(cfg Config, cwd string, selection ProjectSelection) (Config, error) {
	key, err := projectSelectionKey(cwd)
	if err != nil {
		return cfg, err
	}
	selection.Provider = strings.TrimSpace(selection.Provider)
	selection.Model = strings.TrimSpace(selection.Model)
	selection.Thinking = strings.TrimSpace(selection.Thinking)
	if err := validateProjectSelection(key, selection); err != nil {
		return cfg, err
	}
	if _, exists := cfg.ProjectSelections[key]; !exists && len(cfg.ProjectSelections) >= MaxProjectSelections {
		return cfg, fmt.Errorf("config: project_selections limit %d reached", MaxProjectSelections)
	}
	projectSelections := make(map[string]ProjectSelection, len(cfg.ProjectSelections)+1)
	for existingKey, existing := range cfg.ProjectSelections {
		projectSelections[existingKey] = existing
	}
	projectSelections[key] = selection
	cfg.ProjectSelections = projectSelections
	return cfg, nil
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
	data, err := readConfigFile(path)
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
	if len(data) > MaxConfigFileBytes {
		return fmt.Errorf("config: encoded configuration exceeds %d byte limit", MaxConfigFileBytes)
	}
	return atomicWrite(path, data, 0o600)
}

// Update atomically applies one bounded mutation to the latest config under a
// process- and host-wide lock. Interactive writers use this instead of saving
// stale startup snapshots, so concurrent Snow instances preserve each other's
// unrelated settings.
func Update(path string, mutate func(*Config) error) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config: empty path")
	}
	if mutate == nil {
		return Config{}, errors.New("config: update mutation is nil")
	}
	updateMu.Lock()
	defer updateMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Config{}, fmt.Errorf("config: mkdir update lock: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Config{}, fmt.Errorf("config: open update lock: %w", err)
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return Config{}, fmt.Errorf("config: chmod update lock: %w", err)
	}
	if err := lockConfigFile(lock); err != nil {
		return Config{}, fmt.Errorf("config: lock update: %w", err)
	}
	defer unlockConfigFile(lock)

	candidate, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	if err := mutate(&candidate); err != nil {
		return Config{}, err
	}
	if err := Save(path, candidate); err != nil {
		return Config{}, err
	}
	return candidate, nil
}

// SaveProjectSelection merges one working directory's selection into the
// latest config without replacing concurrent project or global changes.
func SaveProjectSelection(path, cwd string, selection ProjectSelection) (Config, error) {
	return Update(path, func(latest *Config) error {
		candidate, err := WithProjectSelection(*latest, cwd, selection)
		if err != nil {
			return err
		}
		*latest = candidate
		return nil
	})
}
