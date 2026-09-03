package protocol

import (
	"encoding/json"
	"slices"
	"time"
)

const (
	// RPCProtocolVersion is the current Snow JSONL RPC wire version.
	RPCProtocolVersion = "1"
	// RPCMaxInputBytes is the maximum encoded request frame size.
	RPCMaxInputBytes = 16 * 1024 * 1024

	RPCTypeReady           = "rpc_ready"
	RPCTypePromptCompleted = "prompt_completed"
)

var rpcCommands = []string{
	"abort",
	"auth_login_cancel",
	"auth_login_start",
	"auth_login_status",
	"auth_logout",
	"auth_profile_set",
	"auth_providers",
	"branch_delete",
	"branch_fork",
	"branch_rename",
	"branch_select",
	"branches_list",
	"compact",
	"context",
	"debug_clear",
	"debug_disable",
	"debug_dump",
	"debug_enable",
	"debug_status",
	"diagnostics",
	"follow_up",
	"goal_clear",
	"goal_continue",
	"goal_create",
	"goal_edit",
	"goal_get",
	"goal_pause",
	"goal_resume",
	"goal_set",
	"keybindings_get",
	"keybindings_update",
	"mcp_servers",
	"messages_list",
	"messages_page",
	"models_list",
	"pending_inputs",
	"pending_inputs_clear",
	"permission_mode_get",
	"permission_mode_set",
	"permission_reject",
	"permission_reply",
	"process_logs",
	"processes_list",
	"project_init",
	"prompt",
	"session_create",
	"session_delete",
	"session_fork",
	"session_info",
	"session_open",
	"session_rename",
	"session_worktree_fork",
	"sessions_list",
	"set_mode",
	"set_model",
	"set_reasoning_summary",
	"set_text_verbosity",
	"set_thinking",
	"settings_get",
	"settings_update",
	"skills",
	"skills_clear",
	"steer",
	"subagent_close",
	"subagent_followup",
	"subagent_get",
	"subagent_interrupt",
	"subagent_list",
	"subagent_messages",
	"subagent_models",
	"subagent_ready",
	"subagent_resume",
	"subagent_send_message",
	"subagent_spawn",
	"subagent_wait",
	"themes_list",
	"trust_get",
	"trust_set",
	"usage",
	"user_input_reject",
	"user_input_reply",
}

// KnownRPCCommands returns every command accepted by protocol version 1.
func KnownRPCCommands() []string {
	return slices.Clone(rpcCommands)
}

var rpcCapabilities = []string{
	"active_input",
	"authentication",
	"branch_management",
	"compaction",
	"context_report",
	"debug_diagnostics",
	"diagnostics",
	"goals",
	"managed_processes",
	"mcp_servers",
	"messages_list",
	"messages_page",
	"models_list",
	"multimodal_prompts",
	"pending_inputs",
	"permission_interaction",
	"permission_mode",
	"presentation_settings",
	"project_init",
	"project_trust",
	"prompt_completion",
	"response_controls",
	"session_forks",
	"session_info",
	"session_management",
	"settings",
	"skills",
	"subagent_messages",
	"subagent_models",
	"subagents",
	"usage",
	"user_input",
}

// KnownRPCCapabilities returns an independent list of optional protocol
// features implemented by this version. Callers must tolerate unknown values.
func KnownRPCCapabilities() []string {
	return slices.Clone(rpcCapabilities)
}

// RPCRequest is one command line sent to snow --mode rpc.
type RPCRequest struct {
	ID               string         `json:"id,omitempty"`
	Type             string         `json:"type"`
	Message          string         `json:"message,omitempty"`
	Content          []ContentBlock `json:"content,omitempty"`
	Model            string         `json:"model,omitempty"`
	Thinking         string         `json:"thinking,omitempty"`
	ReasoningSummary string         `json:"reasoning_summary,omitempty"`
	TextVerbosity    string         `json:"text_verbosity,omitempty"`
	Mode             string         `json:"mode,omitempty"`
	Provider         string         `json:"provider,omitempty"`
	Method           string         `json:"method,omitempty"`
	// Secret is accepted only by dedicated authentication commands. It must
	// never be copied into responses, events, diagnostics, or logs.
	Secret string          `json:"secret,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// RPCResponse acknowledges or rejects an RPC command.
type RPCResponse struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type"`
	Command   string `json:"command,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Data      any    `json:"data,omitempty"`
}

// RPCReady is the first frame emitted by the CLI RPC surface.
type RPCReady struct {
	Type            string   `json:"type"`
	ProtocolVersion string   `json:"protocol_version"`
	SnowVersion     string   `json:"snow_version"`
	Capabilities    []string `json:"capabilities"`
	MaxInputBytes   int      `json:"max_input_bytes"`
}

// NewRPCReady returns an independent startup handshake.
func NewRPCReady(snowVersion string) RPCReady {
	if snowVersion == "" {
		snowVersion = "dev"
	}
	return RPCReady{
		Type:            RPCTypeReady,
		ProtocolVersion: RPCProtocolVersion,
		SnowVersion:     snowVersion,
		Capabilities:    KnownRPCCapabilities(),
		MaxInputBytes:   RPCMaxInputBytes,
	}
}

// RPCPromptStatus is the terminal state of an accepted RPC prompt.
type RPCPromptStatus string

const (
	RPCPromptCompletedStatus RPCPromptStatus = "completed"
	RPCPromptFailedStatus    RPCPromptStatus = "failed"
	RPCPromptCanceledStatus  RPCPromptStatus = "canceled"
)

// RPCPromptCompleted is the definitive terminal result for an accepted prompt.
// The earlier successful response means only that the prompt was admitted.
type RPCPromptCompleted struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Status    RPCPromptStatus `json:"status"`
	Error     string          `json:"error,omitempty"`
}

// RPCModelList is returned by models_list and subagent_models.
type RPCModelList struct {
	Provider string  `json:"provider,omitempty"`
	Current  string  `json:"current,omitempty"`
	Enabled  *bool   `json:"enabled,omitzero"`
	Models   []Model `json:"models"`
}

// RPCGoalSummary is the bounded goal view included in session_info.
type RPCGoalSummary struct {
	GoalID         string           `json:"goal_id"`
	Status         ThreadGoalStatus `json:"status"`
	BlockedReason  string           `json:"blocked_reason,omitempty"`
	TokensUsed     int64            `json:"tokens_used"`
	TokenBudget    *int64           `json:"token_budget"`
	EstimatedCosts []Cost           `json:"estimated_costs"`
}

// RPCSubagentLimits describes effective child-agent availability and bounds.
type RPCSubagentLimits struct {
	Enabled              bool `json:"enabled"`
	MaxConcurrentAgents  int  `json:"max_concurrent_agents"`
	MaxConcurrentThreads int  `json:"max_concurrent_threads"`
	MaxAgentsPerSession  int  `json:"max_agents_per_session"`
	MaxDepth             int  `json:"max_depth"`
	Durable              bool `json:"durable"`
	AllowMutation        bool `json:"allow_mutation"`
}

// RPCPendingInputCounts summarizes accepted root input waiting for delivery.
type RPCPendingInputCounts struct {
	Steering int `json:"steering"`
	FollowUp int `json:"follow_up"`
	Total    int `json:"total"`
}

// RPCSessionSummary is a path-free durable session inventory entry. Session
// IDs are the only selectors accepted by independent-session commands.
type RPCSessionSummary struct {
	SessionID      string `json:"session_id"`
	Name           string `json:"name"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	Messages       int    `json:"messages"`
	MessagesCapped bool   `json:"messages_capped,omitzero"`
	Active         bool   `json:"active"`
}

// RPCSessionList is returned by sessions_list.
type RPCSessionList struct {
	Sessions []RPCSessionSummary `json:"sessions"`
}

// RPCSessionDeleteResult confirms an immutable session identity was deleted.
type RPCSessionDeleteResult struct {
	SessionID string `json:"session_id"`
	Deleted   bool   `json:"deleted"`
}

// RPCSessionRenameResult reports the identity and effective title after a
// session_rename command.
type RPCSessionRenameResult struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

// RPCSessionInfo is the stable response data for session_info.
type RPCSessionInfo struct {
	SessionID         string                `json:"session_id"`
	Name              string                `json:"name"`
	Path              string                `json:"path"`
	CWD               string                `json:"cwd"`
	Provider          string                `json:"provider"`
	Model             string                `json:"model"`
	Thinking          ThinkingLevel         `json:"thinking"`
	ThinkingLevels    []ThinkingLevel       `json:"thinking_levels"`
	ReasoningSummary  ReasoningSummary      `json:"reasoning_summary"`
	TextVerbosity     TextVerbosity         `json:"text_verbosity"`
	CollaborationMode CollaborationMode     `json:"collaboration_mode"`
	Goal              *RPCGoalSummary       `json:"goal,omitempty"`
	Subagents         RPCSubagentLimits     `json:"subagents"`
	PendingInputs     RPCPendingInputCounts `json:"pending_inputs"`
	PermissionMode    string                `json:"permission_mode"`
}

// RPCAuthMethod is one provider-owned login path.
type RPCAuthMethod struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
}

// RPCAuthStatus is the secret-free local credential state for one provider.
type RPCAuthStatus struct {
	ProviderID  string `json:"provider_id"`
	State       string `json:"state"`
	Method      string `json:"method,omitempty"`
	Refreshable bool   `json:"refreshable,omitzero"`
	ExpiresAt   int64  `json:"expires_at,omitzero"`
	AccountID   string `json:"account_id,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

// RPCAuthProvider combines safe provider metadata and locally inspected status.
type RPCAuthProvider struct {
	ProviderID  string          `json:"provider_id"`
	DisplayName string          `json:"display_name"`
	Required    bool            `json:"required"`
	Kinds       []string        `json:"kinds"`
	Environment []string        `json:"environment"`
	Methods     []RPCAuthMethod `json:"methods"`
	Status      RPCAuthStatus   `json:"status"`
}

// RPCAuthProviderList is returned by auth_providers.
type RPCAuthProviderList struct {
	Providers []RPCAuthProvider `json:"providers"`
}

// RPCAuthProgress is one bounded, secret-free interactive login instruction.
type RPCAuthProgress struct {
	Kind     string `json:"kind"`
	Message  string `json:"message,omitempty"`
	URL      string `json:"url,omitempty"`
	UserCode string `json:"user_code,omitempty"`
}

// RPCAuthLoginState is the lifecycle of one asynchronous login job.
type RPCAuthLoginState string

const (
	RPCAuthLoginRunning   RPCAuthLoginState = "running"
	RPCAuthLoginCompleted RPCAuthLoginState = "completed"
	RPCAuthLoginFailed    RPCAuthLoginState = "failed"
	RPCAuthLoginCanceled  RPCAuthLoginState = "canceled"
)

// RPCAuthLoginJob is a pollable, secret-free authentication state snapshot.
type RPCAuthLoginJob struct {
	JobID      string            `json:"job_id"`
	ProviderID string            `json:"provider_id"`
	Method     string            `json:"method"`
	State      RPCAuthLoginState `json:"state"`
	Progress   []RPCAuthProgress `json:"progress"`
	Status     *RPCAuthStatus    `json:"status,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// RPCAuthLogoutResult confirms credential removal without exposing credential data.
type RPCAuthLogoutResult struct {
	ProviderID string        `json:"provider_id"`
	Status     RPCAuthStatus `json:"status"`
}

// RPCSettings is the secret-free TUI settings snapshot returned by settings_get and settings_update.
type RPCSettings struct {
	Provider                 string           `json:"provider"`
	Model                    string           `json:"model"`
	Thinking                 ThinkingLevel    `json:"thinking"`
	ReasoningSummary         ReasoningSummary `json:"reasoning_summary"`
	TextVerbosity            TextVerbosity    `json:"text_verbosity"`
	Theme                    string           `json:"theme"`
	PermissionMode           string           `json:"permission_mode"`
	DebugEnabled             bool             `json:"debug_enabled"`
	SubagentsEnabled         bool             `json:"subagents_enabled"`
	SubagentsMaxConcurrent   int              `json:"subagents_max_concurrent"`
	SubagentsMaxAgents       int              `json:"subagents_max_agents"`
	SkillsEnabled            bool             `json:"skills_enabled"`
	SubagentsRestartRequired bool             `json:"subagents_restart_required"`
	SkillsRestartRequired    bool             `json:"skills_restart_required"`
	RestartRequired          bool             `json:"restart_required"`
	UpdateCheckOnStartup     bool             `json:"update_check_on_startup"`
	AutoUpdate               bool             `json:"auto_update"`
}

// RPCSettingsUpdateParams is the bounded, secret-free partial settings
// mutation accepted by settings_update. Provider, model, and thinking form a
// project-scoped selection; response preferences and debug capture are global.
type RPCSettingsUpdateParams struct {
	Provider               *string           `json:"provider,omitempty"`
	Model                  *string           `json:"model,omitempty"`
	Thinking               *ThinkingLevel    `json:"thinking,omitempty"`
	ReasoningSummary       *ReasoningSummary `json:"reasoning_summary,omitempty"`
	TextVerbosity          *TextVerbosity    `json:"text_verbosity,omitempty"`
	Theme                  *string           `json:"theme,omitempty"`
	DebugEnabled           *bool             `json:"debug_enabled,omitempty"`
	SubagentsEnabled       *bool             `json:"subagents_enabled,omitempty"`
	SubagentsMaxConcurrent *int              `json:"subagents_max_concurrent,omitempty"`
	SkillsEnabled          *bool             `json:"skills_enabled,omitempty"`
	UpdateCheckOnStartup   *bool             `json:"update_check_on_startup,omitempty"`
	AutoUpdate             *bool             `json:"auto_update,omitempty"`
}

// RPCAdaptiveColor is one light/dark semantic color pair.
type RPCAdaptiveColor struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// RPCThemeColors contains the seven semantic presentation roles shared by Snow surfaces.
type RPCThemeColors struct {
	Accent     RPCAdaptiveColor `json:"accent"`
	Muted      RPCAdaptiveColor `json:"muted"`
	Foreground RPCAdaptiveColor `json:"foreground"`
	Warning    RPCAdaptiveColor `json:"warning"`
	Error      RPCAdaptiveColor `json:"error"`
	Success    RPCAdaptiveColor `json:"success"`
	Separator  RPCAdaptiveColor `json:"separator"`
}

// RPCThemeDescriptor is a resolved path-free palette.
type RPCThemeDescriptor struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Scope       string         `json:"scope"`
	Colors      RPCThemeColors `json:"colors"`
}

// RPCThemeCatalog is the bounded themes_list response.
type RPCThemeCatalog struct {
	Selected string               `json:"selected"`
	Themes   []RPCThemeDescriptor `json:"themes"`
}

// RPCKeybindingAction is one stable action's layered binding projection.
type RPCKeybindingAction struct {
	Name      string   `json:"name"`
	Global    []string `json:"global"`
	Project   []string `json:"project"`
	Effective []string `json:"effective"`
	Source    string   `json:"source"`
}

// RPCKeybindings is the bounded keybindings_get/update response.
type RPCKeybindings struct {
	ProjectAllowed bool                  `json:"project_allowed"`
	Actions        []RPCKeybindingAction `json:"actions"`
}

// RPCKeybindingsUpdateParams atomically mutates one global or trusted-project scope.
type RPCKeybindingsUpdateParams struct {
	Scope    string              `json:"scope"`
	Bindings map[string][]string `json:"bindings,omitempty"`
	Reset    []string            `json:"reset,omitempty"`
}

// RPCPermissionMode is the active session permission policy.
type RPCPermissionMode struct {
	Mode string `json:"mode"`
}

// RPCProjectTrust is the effective canonical trust preflight view. A true
// RestartRequired means the persisted decision differs from the immutable
// project-input decision made during startup.
type RPCProjectTrust struct {
	Path            string `json:"path"`
	Level           string `json:"level"`
	Prompt          bool   `json:"prompt"`
	Loaded          bool   `json:"loaded"`
	RestartRequired bool   `json:"restart_required"`
}

// RPCManagedProcess is a secret-free managed-process inventory record.
type RPCManagedProcess struct {
	ProcessID  string `json:"process_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at,omitzero"`
	ExitCode   *int   `json:"exit_code,omitzero"`
	Signal     string `json:"signal,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Ready      bool   `json:"ready,omitzero"`
}

// RPCManagedProcessList is returned by processes_list.
type RPCManagedProcessList struct {
	Processes []RPCManagedProcess `json:"processes"`
}

// RPCManagedProcessLogs is one bounded cursor-based process output page.
type RPCManagedProcessLogs struct {
	ProcessID  string `json:"process_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	NextCursor int64  `json:"next_cursor"`
	Omitted    int64  `json:"omitted_bytes,omitzero"`
	EOF        bool   `json:"eof"`
}

// RPCBranchList is the response data for branches_list.
type RPCBranchList struct {
	Branches []SessionBranch `json:"branches"`
}

// RPCMessagesList is the response data for messages_list.
type RPCMessagesList struct {
	Messages []Message `json:"messages"`
}

// RPCMessagesPageParams selects one bounded branch-history page. Cursor is an
// opaque server-issued snapshot position; clients must not inspect or modify it.
type RPCMessagesPageParams struct {
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitzero"`
	MaxBytes int    `json:"max_bytes,omitzero"`
}

// RPCMessagesPage is one ordered page from a stable append-only branch
// projection. The next cursor is present exactly when HasMore is true.
type RPCMessagesPage struct {
	Messages   []Message `json:"messages"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Start      int       `json:"start"`
	Total      int       `json:"total"`
	HasMore    bool      `json:"has_more"`
}

// RPCSubagentMessagesParams selects one bounded public child-history page.
// Cursor is an opaque server-issued snapshot position bound to the child
// thread identity; clients must not inspect or modify it.
type RPCSubagentMessagesParams struct {
	Target   string `json:"target"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitzero"`
	MaxBytes int    `json:"max_bytes,omitzero"`
}

// RPCSubagentMessagesPage is one ordered page from a stable append-only child
// history projection. State result/error text and provider-private continuity
// are intentionally excluded. Generation identifies the selected lifecycle
// snapshot; Agent provides stable path and thread identity.
type RPCSubagentMessagesPage struct {
	Agent      AgentRef  `json:"agent"`
	Generation uint64    `json:"generation"`
	Messages   []Message `json:"messages"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Start      int       `json:"start"`
	Total      int       `json:"total"`
	HasMore    bool      `json:"has_more"`
}

// RPCDiagnosticsList is the response data for diagnostics.
type RPCDiagnosticsList struct {
	Diagnostics []ConfigDiagnostic `json:"diagnostics"`
}

// RPCDebugStatus describes process-local shared diagnostic capture.
type RPCDebugStatus struct {
	Enabled       bool      `json:"enabled"`
	StartedAt     time.Time `json:"started_at,omitzero"`
	EventCount    int       `json:"event_count"`
	RetainedBytes int       `json:"retained_bytes"`
	DroppedEvents uint64    `json:"dropped_events"`
	MaxEvents     int       `json:"max_events"`
	MaxBytes      int       `json:"max_bytes"`
}

// RPCDebugDumpParams selects a diagnostic dump destination. A blank path asks
// Snow to create a unique file under its private diagnostics directory.
type RPCDebugDumpParams struct {
	Path string `json:"path,omitempty"`
}

// RPCDebugDumpResult reports the resolved dump path and sharing warning.
type RPCDebugDumpResult struct {
	Path    string `json:"path"`
	Warning string `json:"warning"`
}

// RPCMCPServer is a secret-free snapshot of a negotiated MCP connection.
type RPCMCPServer struct {
	ID              string    `json:"id"`
	Transport       string    `json:"transport"`
	Connected       bool      `json:"connected"`
	ProtocolVersion string    `json:"protocol_version,omitempty"`
	ServerName      string    `json:"server_name,omitempty"`
	ServerVersion   string    `json:"server_version,omitempty"`
	Capabilities    []string  `json:"capabilities,omitempty"`
	ToolCount       int       `json:"tool_count,omitzero"`
	Message         string    `json:"message,omitempty"`
	State           string    `json:"state,omitempty"`
	Cached          bool      `json:"cached,omitzero"`
	CachedAt        time.Time `json:"cached_at,omitzero"`
	LastUsedAt      time.Time `json:"last_used_at,omitzero"`
}

// RPCSkill is the metadata catalog entry exposed to clients.
type RPCSkill struct {
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

// RPCSkillDiagnostic records a malformed or shadowed skill entry.
type RPCSkillDiagnostic struct {
	Path    string `json:"path,omitempty"`
	Skill   string `json:"skill,omitempty"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// RPCContextCategory is one secret-free estimated contributor to provider input.
type RPCContextCategory struct {
	Name            string `json:"name"`
	Bytes           int    `json:"bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Items           int    `json:"items"`
}

// RPCContextReport describes the latest or next projected provider request
// using counts and estimates only. It never includes prompt or tool contents.
type RPCContextReport struct {
	LatestRequest            bool                 `json:"latest_request"`
	Categories               []RPCContextCategory `json:"categories"`
	EstimatedInputTokens     int                  `json:"estimated_input_tokens"`
	FixedContextTokens       int                  `json:"fixed_context_tokens"`
	FixedContextBudgetTokens int                  `json:"fixed_context_budget_tokens"`
	FixedContextOverBudget   bool                 `json:"fixed_context_over_budget"`
	MessageCount             int                  `json:"message_count"`
	ToolCount                int                  `json:"tool_count"`
	ContextWindow            int                  `json:"context_window"`
	Usage                    *Usage               `json:"usage,omitempty"`
}

// RPCSkillsClearResult confirms branch-active skill deactivation and includes
// the current public catalog. It does not delete skill files or change config.
type RPCSkillsClearResult struct {
	Cleared int           `json:"cleared"`
	Catalog RPCSkillsList `json:"catalog"`
}

// RPCMCPServersList is the response data for mcp_servers.
type RPCMCPServersList struct {
	Servers []RPCMCPServer `json:"servers"`
}

// RPCSkillsList is the response data for skills. Diagnostics accompany the
// full inventory so clients can surface policy-disabled or malformed entries.
type RPCSkillsList struct {
	Skills      []RPCSkill           `json:"skills"`
	Diagnostics []RPCSkillDiagnostic `json:"diagnostics,omitempty"`
}
