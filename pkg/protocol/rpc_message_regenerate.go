package protocol

// RPCMessageRegeneratePrepareParams selects the exact final assistant reply of
// one user-origin root turn. EntryID is an assistant entry, not a user entry;
// TurnID is the persisted MetaAgentTurn ID, never a display position.
type RPCMessageRegeneratePrepareParams struct {
	SessionID string `json:"session_id"`
	EntryID   string `json:"entry_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
}

// RPCMessageRegeneratePrepared identifies both the owning original user entry
// (EntryID) and terminal assistant reply (ReplyEntryID). Text is informational:
// commits use the original source held by the server, never client-supplied text.
type RPCMessageRegeneratePrepared struct {
	RPCMessageEditPrepared
	ReplyEntryID string `json:"reply_entry_id"`
}

// RPCMessageRegenerateCommitParams consumes a regeneration-only authorization.
// Its ACK is RPCMessageEditCommitted; rejected/unknown errors and completion
// lifecycle are shared with historical editing. Regeneration may repeat tool
// effects; old external effects are never undone by the append-only branch fork.
type RPCMessageRegenerateCommitParams struct {
	SessionID string `json:"session_id"`
	EditToken string `json:"edit_token"`
}
