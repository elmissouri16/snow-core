package protocol

import (
	"encoding/json"
	"slices"
)

// AgentEventType enumerates cross-surface agent events emitted by the core.
// These are the only observation channel for TUI / SDK / print / RPC.
type AgentEventType string

const (
	EvSessionUpdated    AgentEventType = "session_updated"
	EvRunStatsUpdated   AgentEventType = "run_stats_updated"
	EvTextDelta         AgentEventType = "text_delta"
	EvThinkingDelta     AgentEventType = "thinking_delta"
	EvToolStart         AgentEventType = "tool_start"
	EvToolProgress      AgentEventType = "tool_progress"
	EvToolEnd           AgentEventType = "tool_end"
	EvToolRouting       AgentEventType = "tool_routing"
	EvPermissionRequest AgentEventType = "permission_request"
	EvUserInputRequest  AgentEventType = "user_input_request"
	EvUsage             AgentEventType = "usage"
	EvProviderRetry     AgentEventType = "provider_retry"
	EvQueueUpdated      AgentEventType = "queue_updated"
	EvTurnDone          AgentEventType = "turn_done"
	EvError             AgentEventType = "error"
	EvAborted           AgentEventType = "aborted"
	EvModelChanged      AgentEventType = "model_changed"
	EvModeChanged       AgentEventType = "mode_changed"
	EvPlanStarted       AgentEventType = "plan_started"
	EvPlanDelta         AgentEventType = "plan_delta"
	EvPlanCompleted     AgentEventType = "plan_completed"
	EvPlanUpdate        AgentEventType = "plan_update"
	EvCompactionStarted AgentEventType = "compaction_started"
	EvCompactionDone    AgentEventType = "compaction_done"
	EvThreadGoalUpdated AgentEventType = "thread_goal_updated"
	EvSubagentStarted   AgentEventType = "subagent_started"
	EvSubagentStatus    AgentEventType = "subagent_status"
	EvSubagentMessage   AgentEventType = "subagent_message"
	EvSubagentActivity  AgentEventType = "subagent_activity"
)

// KnownAgentEventTypes returns every normalized event type in protocol order.
func KnownAgentEventTypes() []AgentEventType {
	return []AgentEventType{
		EvSessionUpdated,
		EvRunStatsUpdated,
		EvTextDelta,
		EvThinkingDelta,
		EvToolStart,
		EvToolProgress,
		EvToolEnd,
		EvToolRouting,
		EvPermissionRequest,
		EvUserInputRequest,
		EvUsage,
		EvProviderRetry,
		EvQueueUpdated,
		EvTurnDone,
		EvError,
		EvAborted,
		EvModelChanged,
		EvModeChanged,
		EvPlanStarted,
		EvPlanDelta,
		EvPlanCompleted,
		EvPlanUpdate,
		EvCompactionStarted,
		EvCompactionDone,
		EvThreadGoalUpdated,
		EvSubagentStarted,
		EvSubagentStatus,
		EvSubagentMessage,
		EvSubagentActivity,
	}
}

// ToolProgress is emitted by tools running through the agent.
type ToolProgress struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Message    string `json:"message,omitempty"`
	Done       bool   `json:"done"`
	IsError    bool   `json:"is_error,omitzero"`
}

// ToolRouting describes one automatic or model-requested deferred-tool search.
// ToolIDs is bounded by the agent; the raw user query is never included.
type ToolRouting struct {
	Trigger        string   `json:"trigger"`
	ToolIDs        []string `json:"tool_ids,omitempty"`
	CandidateCount int      `json:"candidate_count"`
	SelectedCount  int      `json:"selected_count"`
	ExposedCount   int      `json:"exposed_count"`
	SchemaBytes    int      `json:"schema_bytes"`
	LatencyMS      int64    `json:"latency_ms"`
	Fallback       bool     `json:"fallback,omitzero"`
}

// ProviderRetry describes a nonterminal provider recovery wait. Message text is
// deliberately excluded so raw provider diagnostics cannot leak through the
// normalized event stream.
type ProviderRetry struct {
	Provider     string `json:"provider"`
	Kind         string `json:"kind"`
	Phase        string `json:"phase"`
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"max_attempts"`
	DelayMS      int64  `json:"delay_ms"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	MaxElapsedMS int64  `json:"max_elapsed_ms"`
}

// AgentEvent is a single event delivered to subscribers.
type AgentEvent struct {
	Type AgentEventType `json:"type"`

	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Message    string `json:"message,omitempty"` // error / progress text / tool path
	// ToolOutput is a bounded preview of a completed tool result for UIs and
	// SDK consumers. The complete result remains in the session message.
	ToolOutput string `json:"tool_output,omitempty"`
	// ToolDurationMS is populated on tool_end when timing is available.
	ToolDurationMS int64 `json:"tool_duration_ms,omitzero"`
	// ToolProgress carries structured progress emitted by a running tool.
	ToolProgress  *ToolProgress           `json:"tool_progress,omitempty"`
	ToolRouting   *ToolRouting            `json:"tool_routing,omitempty"`
	ProviderRetry *ProviderRetry          `json:"provider_retry,omitempty"`
	Usage         *Usage                  `json:"usage,omitempty"`
	Model         *Model                  `json:"model,omitempty"`
	Mode          *CollaborationModeState `json:"mode,omitempty"`
	Plan          *PlanItem               `json:"plan,omitempty"`
	PlanUpdate    *PlanUpdate             `json:"plan_update,omitempty"`
	Compaction    *CompactionResult       `json:"compaction,omitempty"`
	Permission    *Permission             `json:"permission,omitempty"`
	UserInput     *UserInputRequest       `json:"user_input,omitempty"`
	Queue         *InputQueue             `json:"queue,omitempty"`
	ThreadGoal    *ThreadGoalUpdate       `json:"thread_goal,omitempty"`
	// Agent correlates ordinary child stream/tool/usage events. Root events keep
	// this nil for backward compatibility. Lifecycle snapshots use Subagent.
	Agent        *AgentRef      `json:"agent,omitempty"`
	Subagent     *SubagentState `json:"subagent,omitempty"`
	AgentMessage *AgentMessage  `json:"agent_message,omitempty"`
	TurnID       string         `json:"turn_id,omitempty"`
	TurnOrigin   string         `json:"turn_origin,omitempty"`
	TurnSequence uint64         `json:"turn_sequence,omitzero"`
	RootEpoch    uint64         `json:"root_epoch,omitzero"`
	// Snapshot marks state published to initialize observers after restore or a
	// session switch, rather than a lifecycle transition that just occurred.
	Snapshot       bool `json:"snapshot,omitzero"`
	GoalContinuing bool `json:"goal_continuing,omitzero"`
	IsError        bool `json:"is_error,omitzero"`
}

// Clone returns a fully independent event for one observer. Event subscribers
// are isolation boundaries: mutating one callback's payload must not affect
// later SDK, plugin, RPC, or TUI observers.
func (e AgentEvent) Clone() AgentEvent {
	out := e
	if e.ToolProgress != nil {
		v := *e.ToolProgress
		out.ToolProgress = &v
	}
	if e.ToolRouting != nil {
		v := *e.ToolRouting
		v.ToolIDs = slices.Clone(e.ToolRouting.ToolIDs)
		out.ToolRouting = &v
	}
	if e.ProviderRetry != nil {
		v := *e.ProviderRetry
		out.ProviderRetry = &v
	}
	out.Usage = e.Usage.Clone()
	if e.Model != nil {
		v := e.Model.Clone()
		out.Model = &v
	}
	if e.Mode != nil {
		v := *e.Mode
		out.Mode = &v
	}
	if e.Plan != nil {
		v := *e.Plan
		out.Plan = &v
	}
	out.PlanUpdate = e.PlanUpdate.Clone()
	if e.Compaction != nil {
		v := *e.Compaction
		out.Compaction = &v
	}
	if e.Permission != nil {
		v := *e.Permission
		v.Request.Args = append(json.RawMessage(nil), e.Permission.Request.Args...)
		v.Request.Paths = slices.Clone(e.Permission.Request.Paths)
		out.Permission = &v
	}
	if e.UserInput != nil {
		v := *e.UserInput
		v.Questions = make([]UserInputQuestion, len(e.UserInput.Questions))
		for i, question := range e.UserInput.Questions {
			v.Questions[i] = question
			v.Questions[i].Options = slices.Clone(question.Options)
		}
		out.UserInput = &v
	}
	out.Queue = e.Queue.Clone()
	out.ThreadGoal = e.ThreadGoal.Clone()
	out.Agent = e.Agent.Clone()
	out.Subagent = e.Subagent.Clone()
	out.AgentMessage = e.AgentMessage.Clone()
	return out
}

// PermissionRequest is embedded in permission_request events. ID uniquely
// identifies one interaction so a host can correlate a reply to a specific
// request even when the root and subagents ask concurrently (they are still
// serialized FIFO).
type PermissionRequest struct {
	ID     string          `json:"id"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Paths  []string        `json:"paths,omitempty"`
	Risk   string          `json:"risk"`
	Reason string          `json:"reason,omitempty"`
}

// PermissionDecision is a trusted host's response to an interactive request.
type PermissionDecision string

const (
	PermissionAllow        PermissionDecision = "allow"
	PermissionAllowSession PermissionDecision = "allow_session"
	PermissionAllowAlways  PermissionDecision = "allow_always"
	PermissionDeny         PermissionDecision = "deny"
)

// PermissionResponse correlates one trusted-host decision to its request.
type PermissionResponse struct {
	RequestID string             `json:"request_id"`
	Decision  PermissionDecision `json:"decision"`
}

// Permission is the resolved view for a permission_request event.
type Permission struct {
	Request PermissionRequest `json:"request"`
}
