package protocol

import "encoding/json"

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
	"branch_fork",
	"follow_up",
	"goal_clear",
	"goal_continue",
	"goal_create",
	"goal_edit",
	"goal_get",
	"goal_pause",
	"goal_resume",
	"goal_set",
	"models_list",
	"prompt",
	"session_fork",
	"session_info",
	"session_rename",
	"session_worktree_fork",
	"set_mode",
	"set_model",
	"set_thinking",
	"steer",
	"subagent_followup",
	"subagent_get",
	"subagent_interrupt",
	"subagent_list",
	"subagent_models",
	"subagent_ready",
	"subagent_send_message",
	"subagent_spawn",
	"subagent_wait",
	"user_input_reject",
	"user_input_reply",
}

// KnownRPCCommands returns every command accepted by protocol version 1.
func KnownRPCCommands() []string {
	return append([]string(nil), rpcCommands...)
}

var rpcCapabilities = []string{
	"active_input",
	"branch_management",
	"goals",
	"models_list",
	"prompt_completion",
	"session_forks",
	"session_info",
	"subagent_models",
	"subagents",
	"user_input",
}

// KnownRPCCapabilities returns an independent list of optional protocol
// features implemented by this version. Callers must tolerate unknown values.
func KnownRPCCapabilities() []string {
	return append([]string(nil), rpcCapabilities...)
}

// RPCRequest is one command line sent to snow --mode rpc.
type RPCRequest struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	Message  string          `json:"message,omitempty"`
	Model    string          `json:"model,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
	Mode     string          `json:"mode,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
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
	Enabled  *bool   `json:"enabled,omitempty"`
	Models   []Model `json:"models"`
}

// RPCGoalSummary is the bounded goal view included in session_info.
type RPCGoalSummary struct {
	GoalID         string           `json:"goal_id"`
	Status         ThreadGoalStatus `json:"status"`
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
	CollaborationMode CollaborationMode     `json:"collaboration_mode"`
	Goal              *RPCGoalSummary       `json:"goal,omitempty"`
	Subagents         RPCSubagentLimits     `json:"subagents"`
	PendingInputs     RPCPendingInputCounts `json:"pending_inputs"`
}
