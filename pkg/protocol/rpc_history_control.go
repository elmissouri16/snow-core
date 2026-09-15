package protocol

const (
	RPCHistoryControlMaxNameBytes      = 256
	RPCHistoryControlRejectedErrorCode = "history_control_rejected"
	RPCHistoryControlUnknownErrorCode  = "history_control_unknown"
)

// RPCHistoryControlBinding compares the active cursor and an exact saved branch
// tip. TargetTipID is never authority to fork at an arbitrary historical entry.
// The target may equal the source, unlike branch restore.
type RPCHistoryControlBinding struct {
	SessionID      string `json:"session_id"`
	SourceBranchID string `json:"source_branch_id"`
	SourceTipID    string `json:"source_tip_id"`
	TargetBranchID string `json:"target_branch_id"`
	TargetTipID    string `json:"target_tip_id"`
}

type RPCManagedBranchForkParams struct {
	RPCHistoryControlBinding
	Name string `json:"name"`
}

// RPCManagedSessionForkParams deliberately has no destination, worktree, or
// activation option. Snow allocates and closes a detached child session.
type RPCManagedSessionForkParams struct {
	RPCHistoryControlBinding
	Name string `json:"name"`
}

type RPCManagedBranchRenameParams struct {
	RPCHistoryControlBinding
	OldName string `json:"old_name"`
	Name    string `json:"name"`
}

// RPCManagedBranchForkResult is metadata only. Reload the bound history through
// branch_messages_page and its public projection; never serialize raw history.
type RPCManagedBranchForkResult struct {
	SessionID       string            `json:"session_id"`
	BranchID        string            `json:"branch_id"`
	TipID           string            `json:"tip_id"`
	Branch          RPCBranchVersion  `json:"branch"`
	Mode            CollaborationMode `json:"mode"`
	ReasoningEffort ThinkingLevel     `json:"reasoning_effort"`
	RootEpoch       uint64            `json:"root_epoch"`
}

type RPCManagedSessionForkResult struct {
	SessionID       string            `json:"session_id"`
	Name            string            `json:"name"`
	Branch          RPCBranchVersion  `json:"branch"`
	SourceSessionID string            `json:"source_session_id"`
	SourceBranchID  string            `json:"source_branch_id"`
	SourceTipID     string            `json:"source_tip_id"`
	Mode            CollaborationMode `json:"mode"`
	// RootEpoch remains the unchanged parent epoch, not a child activation.
	RootEpoch uint64 `json:"root_epoch"`
}

type RPCManagedBranchRenameResult struct {
	SessionID string           `json:"session_id"`
	BranchID  string           `json:"branch_id"`
	TipID     string           `json:"tip_id"`
	Branch    RPCBranchVersion `json:"branch"`
	RootEpoch uint64           `json:"root_epoch"`
}
