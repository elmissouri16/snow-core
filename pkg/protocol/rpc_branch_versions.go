package protocol

const (
	RPCBranchesPageMaxItems           = 100
	RPCBranchMessagesPageMaxItems     = 64
	RPCBranchRestoreRejectedErrorCode = "branch_restore_rejected"
	RPCBranchRestoreUnknownErrorCode  = "branch_restore_unknown"
)

type RPCBranchesPageParams struct {
	SessionID string `json:"session_id"`
	Limit     int    `json:"limit,omitzero"`
	Cursor    string `json:"cursor,omitempty"`
}
type RPCBranchVersion struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ParentID     string `json:"parent_id"`
	ForkedFromID string `json:"forked_from_id"`
	TipID        string `json:"tip_id"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	Active       bool   `json:"active"`
}
type RPCBranchesPage struct {
	SessionID      string             `json:"session_id"`
	ActiveBranchID string             `json:"active_branch_id"`
	ActiveTipID    string             `json:"active_tip_id"`
	Branches       []RPCBranchVersion `json:"branches"`
	NextCursor     string             `json:"next_cursor,omitempty"`
}
type RPCBranchMessagesPageParams struct {
	SessionID string `json:"session_id"`
	BranchID  string `json:"branch_id"`
	TipID     string `json:"tip_id"`
	Limit     int    `json:"limit,omitzero"`
	Cursor    string `json:"cursor,omitempty"`
}
type RPCBranchMessagesPage struct {
	HistoryTools          map[string][]RPCHistoryTool `json:"history_tools,omitempty"`
	HistoryToolsTruncated bool                        `json:"history_tools_truncated,omitzero"`
	SessionID             string                      `json:"session_id"`
	BranchID              string                      `json:"branch_id"`
	TipID                 string                      `json:"tip_id"`
	Messages              []Message                   `json:"messages"`
	Start                 int                         `json:"start"`
	Total                 int                         `json:"total"`
	NextCursor            string                      `json:"next_cursor,omitempty"`
}
type RPCBranchRestorePrepareParams struct {
	SessionID      string `json:"session_id"`
	SourceBranchID string `json:"source_branch_id"`
	SourceTipID    string `json:"source_tip_id"`
	TargetBranchID string `json:"target_branch_id"`
	TargetTipID    string `json:"target_tip_id"`
}
type RPCBranchRestorePrepared struct {
	RPCBranchRestorePrepareParams
	RestoreToken string `json:"restore_token"`
	ExpiresAt    int64  `json:"expires_at"`
}
type RPCBranchRestoreCommitParams struct {
	SessionID    string `json:"session_id"`
	RestoreToken string `json:"restore_token"`
}
type RPCBranchRestoreCommitted struct {
	Mode      CollaborationMode     `json:"mode"`
	SessionID string                `json:"session_id"`
	BranchID  string                `json:"branch_id"`
	TipID     string                `json:"tip_id"`
	History   RPCBranchMessagesPage `json:"history"`
	Settings  RPCSettings           `json:"settings"`
}
