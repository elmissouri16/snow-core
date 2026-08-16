package protocol

import "encoding/json"

// AgentEventType enumerates cross-surface agent events emitted by the core.
// These are the only observation channel for TUI / SDK / print / RPC.
type AgentEventType string

const (
	EvSessionUpdated    AgentEventType = "session_updated"
	EvTextDelta         AgentEventType = "text_delta"
	EvThinkingDelta     AgentEventType = "thinking_delta"
	EvToolStart         AgentEventType = "tool_start"
	EvToolProgress      AgentEventType = "tool_progress"
	EvToolEnd           AgentEventType = "tool_end"
	EvToolRouting       AgentEventType = "tool_routing"
	EvPermissionRequest AgentEventType = "permission_request"
	EvUserInputRequest  AgentEventType = "user_input_request"
	EvUsage             AgentEventType = "usage"
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
		EvTextDelta,
		EvThinkingDelta,
		EvToolStart,
		EvToolProgress,
		EvToolEnd,
		EvToolRouting,
		EvPermissionRequest,
		EvUserInputRequest,
		EvUsage,
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
	IsError    bool   `json:"is_error,omitempty"`
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
	Fallback       bool     `json:"fallback,omitempty"`
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
	ToolDurationMS int64 `json:"tool_duration_ms,omitempty"`
	// ToolProgress carries structured progress emitted by a running tool.
	ToolProgress *ToolProgress           `json:"tool_progress,omitempty"`
	ToolRouting  *ToolRouting            `json:"tool_routing,omitempty"`
	Usage        *Usage                  `json:"usage,omitempty"`
	Model        *Model                  `json:"model,omitempty"`
	Mode         *CollaborationModeState `json:"mode,omitempty"`
	Plan         *PlanItem               `json:"plan,omitempty"`
	PlanUpdate   *PlanUpdate             `json:"plan_update,omitempty"`
	Compaction   *CompactionResult       `json:"compaction,omitempty"`
	Permission   *Permission             `json:"permission,omitempty"`
	UserInput    *UserInputRequest       `json:"user_input,omitempty"`
	Queue        *InputQueue             `json:"queue,omitempty"`
	ThreadGoal   *ThreadGoalUpdate       `json:"thread_goal,omitempty"`
	// Agent correlates ordinary child stream/tool/usage events. Root events keep
	// this nil for backward compatibility. Lifecycle snapshots use Subagent.
	Agent          *AgentRef      `json:"agent,omitempty"`
	Subagent       *SubagentState `json:"subagent,omitempty"`
	AgentMessage   *AgentMessage  `json:"agent_message,omitempty"`
	TurnID         string         `json:"turn_id,omitempty"`
	TurnOrigin     string         `json:"turn_origin,omitempty"`
	TurnSequence   uint64         `json:"turn_sequence,omitempty"`
	RootEpoch      uint64         `json:"root_epoch,omitempty"`
	GoalContinuing bool           `json:"goal_continuing,omitempty"`
	IsError        bool           `json:"is_error,omitempty"`
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
		v.ToolIDs = append([]string(nil), e.ToolRouting.ToolIDs...)
		out.ToolRouting = &v
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
		v.Request.Paths = append([]string(nil), e.Permission.Request.Paths...)
		out.Permission = &v
	}
	if e.UserInput != nil {
		v := *e.UserInput
		v.Questions = make([]UserInputQuestion, len(e.UserInput.Questions))
		for i, question := range e.UserInput.Questions {
			v.Questions[i] = question
			v.Questions[i].Options = append([]UserInputOption(nil), question.Options...)
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

// PermissionRequest is embedded in permission_request events.
type PermissionRequest struct {
	// ID correlates an interactive permission decision with the process-local
	// request that emitted it. It is opaque to clients and must be echoed by
	// permission_reply or permission_reject.
	ID     string          `json:"id"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Paths  []string        `json:"paths,omitempty"`
	Risk   string          `json:"risk"`
	Reason string          `json:"reason,omitempty"`
}

// Permission is the resolved view for a permission_request event.
type Permission struct {
	Request PermissionRequest `json:"request"`
}
