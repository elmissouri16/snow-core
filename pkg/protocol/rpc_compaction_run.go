package protocol

// RPCCompactionStartParams names the exact idle branch reviewed by the caller.
// Empty ExpectedTipID asserts an empty branch; it is never a wildcard.
type RPCCompactionStartParams struct {
	SessionID     string `json:"session_id"`
	BranchID      string `json:"branch_id"`
	ExpectedTipID string `json:"expected_tip_id"`
}

type RPCCompactionAccepted struct {
	CompactionID string `json:"compaction_id"`
	SessionID    string `json:"session_id"`
	BranchID     string `json:"branch_id"`
	TurnID       string `json:"turn_id"`
	TurnOrigin   string `json:"turn_origin"`
	RootEpoch    uint64 `json:"root_epoch"`
	TurnSequence uint64 `json:"turn_sequence"`
}

const (
	RPCTypeCompactionCompleted     = "compaction_completed"
	RPCCompactionRejectedErrorCode = "compaction_rejected"
	RPCCompactionUnknownErrorCode  = "compaction_unknown"
)

// RPCCompactionCompleted is the terminal execution boundary, unlike the
// compaction_done progress event. No summary, provider errors or private state
// are included. A canceled/failed run can have incurred usage or saved a marker;
// always refresh authoritative history before a new explicit action.
type RPCCompactionCompleted struct {
	Type               string `json:"type"`
	RequestID          string `json:"request_id"`
	CompactionID       string `json:"compaction_id"`
	SessionID          string `json:"session_id"`
	BranchID           string `json:"branch_id"`
	TurnID             string `json:"turn_id"`
	TurnOrigin         string `json:"turn_origin"`
	RootEpoch          uint64 `json:"root_epoch"`
	TurnSequence       uint64 `json:"turn_sequence"`
	Status             string `json:"status"` // completed, noop, fallback, canceled, failed
	SummarizedMessages int    `json:"summarized_messages"`
	RetainedMessages   int    `json:"retained_messages"`
	UsedFallback       bool   `json:"used_fallback"`
}
