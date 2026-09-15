package protocol

const (
	RPCControlMaxInputBytes      = 64 * 1024
	RPCControlMaxOutputBytes     = 64 * 1024
	RPCControlCapability         = "runtime_free_control"
	RPCDefaultsControlCapability = "defaults_control"
	RPCProviderStatusCapability  = "provider_status"
	RPCAPIKeyControlCapability   = "host_api_key_control"
	// These advertise explicit handlers, not executable or network availability.
	RPCProjectCreateCapability = "host_project_create_v1"
	RPCProjectCloneCapability  = "host_project_clone_v1"
)

// NewRPCControlReady advertises only operator defaults and local provider status.
// It does not grant runtime, authentication, discovery, or credential-write access.
func NewRPCControlReady(version string) RPCReady {
	ready := NewRPCReady(version)
	ready.MaxInputBytes = RPCControlMaxInputBytes
	ready.Capabilities = []string{RPCControlCapability, RPCDefaultsControlCapability, RPCProviderStatusCapability}
	return ready
}

const RPCTypeProjectCompleted = "project_completed"

// RPCProjectCompleted is the asynchronous terminal frame after clone-start ACK.
// It carries no process output or raw filesystem/network errors.
type RPCProjectCompleted struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	RPCProjectCompletion
}
