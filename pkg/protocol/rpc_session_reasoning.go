package protocol

// RPCSessionReasoningCapability advertises nonpersisting, idle, exact-session
// response-preference controls. Legacy settings commands are not a fallback.
const RPCSessionReasoningCapability = "session_reasoning"

// RPCSessionReasoningGetParams binds read-only inspection to the active session.
// The response supplies the exact active branch/tip required for mutation.
type RPCSessionReasoningGetParams struct {
	SessionID string `json:"session_id"`
}

// RPCSessionReasoningState is the complete effective-value CAS, not saved host
// defaults. Empty TipID is valid for a new branch; all other fields are required.
type RPCSessionReasoningState struct {
	SessionID        string            `json:"session_id"`
	BranchID         string            `json:"branch_id"`
	TipID            string            `json:"tip_id"`
	Provider         string            `json:"provider"`
	Model            string            `json:"model"`
	Mode             CollaborationMode `json:"mode"`
	PermissionMode   string            `json:"permission_mode"`
	Thinking         ThinkingLevel     `json:"thinking"`
	ReasoningSummary ReasoningSummary  `json:"reasoning_summary"`
	TextVerbosity    TextVerbosity     `json:"text_verbosity"`
}

// RPCSessionReasoning contains current effective values and supported choices
// from the agent's already-loaded model metadata. Inspection does no discovery,
// provider request, default persistence, turn, goal continuation or session open.
type RPCSessionReasoning struct {
	RPCSessionReasoningState
	ThinkingLevels     []ThinkingLevel    `json:"thinking_levels"`
	ReasoningSummaries []ReasoningSummary `json:"reasoning_summaries"`
	TextVerbosities    []TextVerbosity    `json:"text_verbosities"`
}

// RPCSessionReasoningSetParams changes one preference. Field accepts only
// thinking, reasoning_summary, or text_verbosity; Value must be advertised by
// the current model. Expected is required in full, including explicit tip_id.
// Thinking changes the current mode's override; no host/project defaults or
// conversation history are written, and no work is started.
type RPCSessionReasoningSetParams struct {
	Expected RPCSessionReasoningState `json:"expected"`
	Field    string                   `json:"field"`
	Value    string                   `json:"value"`
}

const (
	RPCSessionReasoningRejectedErrorCode = "session_reasoning_rejected"
	RPCSessionReasoningUnknownErrorCode  = "session_reasoning_outcome_unknown"
)
