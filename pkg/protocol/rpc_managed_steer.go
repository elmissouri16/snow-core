package protocol

const (
	RPCManagedSteerMaxTextBytes      = RPCQueueMaxTextBytes
	RPCManagedSteerMaxIDBytes        = 256
	RPCManagedSteerRejectedErrorCode = "managed_steer_rejected"
	RPCManagedSteerStaleErrorCode    = "managed_steer_stale"
)

// RPCManagedSteerParams binds one explicit, literal steering submission to an
// ordinary user root. RequestID is correlation only, not a durable replay key:
// callers must not automatically retry an ambiguous response.
type RPCManagedSteerParams struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	RootEpoch uint64 `json:"root_epoch"`
	RequestID string `json:"request_id"`
	Text      string `json:"text"`
}

// RPCManagedSteerResult acknowledges admission, NOT delivery. ItemID is the
// native queue identity observed in queue_updated. Native steering may still
// be delivered after provider failure; an error is not proof it was discarded.
// The existing queue_control.change reports delivered/discarded by ItemID.
type RPCManagedSteerResult struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	RootEpoch uint64 `json:"root_epoch"`
	RequestID string `json:"request_id"`
	ItemID    string `json:"item_id"`
	Status    string `json:"status"`
}
