package protocol

// RPCMessageEditMaxTextBytes bounds both original and replacement UTF-8 text.
const RPCMessageEditMaxTextBytes = 64 * 1024

// RPCMessageEditUnknownErrorCode requires authority invalidation and a fresh
// snapshot. It never authorizes automatic commit replay or an unchanged view.
const RPCMessageEditUnknownErrorCode = "message_edit_unknown"

// RPCMessageEditRejectedErrorCode definitively means no replacement input was
// committed and any attempted branch mutation was successfully rolled back.
const RPCMessageEditRejectedErrorCode = "message_edit_rejected"

// RPCMessageEditPrepareParams selects one exact persisted user entry or turn.
// Turn IDs are MetaAgentTurn entry IDs, not display positions or message IDs.
type RPCMessageEditPrepareParams struct {
	SessionID string `json:"session_id"`
	EntryID   string `json:"entry_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
}

// RPCMessageEditPrepared is a read-only, short-lived edit authorization. Tokens
// are single-use and bound to this precise session, branch, tip and user entry.
type RPCMessageEditPrepared struct {
	EditToken      string `json:"edit_token"`
	SessionID      string `json:"session_id"`
	SourceBranchID string `json:"source_branch_id"`
	SourceTipID    string `json:"source_tip_id"`
	EntryID        string `json:"entry_id"`
	TurnID         string `json:"turn_id"`
	Text           string `json:"text"`
	ExpiresAt      int64  `json:"expires_at"`
}

type RPCMessageEditCommitParams struct {
	SessionID string `json:"session_id"`
	EditToken string `json:"edit_token"`
	Text      string `json:"text"`
}

// RPCMessageEditCommitted acknowledges durable replacement input before any
// replacement provider work. History is a bounded public suffix ending with
// UserEntryID; History.Start > 0 means older history was omitted. Consumers
// replace their projection, rather than appending the input a second time.
// Completion uses prompt_completed with the commit request ID. A transport
// failure is ambiguous: never automatically replay a commit.
type RPCMessageEditCommitted struct {
	SessionID      string          `json:"session_id"`
	SourceBranchID string          `json:"source_branch_id"`
	SourceTipID    string          `json:"source_tip_id"`
	EntryID        string          `json:"entry_id"`
	BranchID       string          `json:"branch_id"`
	TurnID         string          `json:"turn_id"`
	UserEntryID    string          `json:"user_entry_id"`
	History        RPCMessagesPage `json:"history"`
}
