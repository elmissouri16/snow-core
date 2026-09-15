package protocol

// RPCSessionDeleteControlCapability grants only explicit inactive-session
// deletion in runtime-free control mode, not activation or session creation.
const RPCSessionDeleteControlCapability = "inactive_session_delete_v1"

// RPCSessionDeleteParams selects exact project membership by immutable ID.
// The worker's launch-selected CWD is the only project filesystem authority.
type RPCSessionDeleteParams struct {
	SessionID string `json:"session_id"`
}
