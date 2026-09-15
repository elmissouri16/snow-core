package protocol

// Host operations are runtime-free, explicit filesystem mutations. They do not
// activate a project or authorize arbitrary commands. SSH is unavailable until
// a separately approved host profile exists; only anonymous HTTPS is supported.
const HostOperationsCapability = "host_operations"

// HostDirectoryIdentity binds a canonical host path to the directory opened at
// review time. Device and inode are decimal strings to avoid JSON/JS precision
// loss. Identity is not a secret or a substitute for operating-system isolation.
type HostDirectoryIdentity struct {
	Path   string `json:"path"`
	Device string `json:"device"`
	Inode  string `json:"inode"`
}

type RPCProjectPrepareParams struct {
	OperationID string                `json:"operation_id"`
	Parent      HostDirectoryIdentity `json:"parent"`
	Leaf        string                `json:"leaf"`
}

// RPCProjectPrepared must be durably recorded before acknowledging creation or
// requesting clone execution. Closing its host handle does not delete the child.
type RPCProjectPrepared struct {
	OperationID string                `json:"operation_id"`
	Parent      HostDirectoryIdentity `json:"parent"`
	Child       HostDirectoryIdentity `json:"child"`
	Leaf        string                `json:"leaf"`
}

// RPCProjectCloneStartParams is an operation-bound acknowledgement that the
// exact prepared child identity has been durably recorded. URL is the exact
// reviewed anonymous HTTPS URL, not a shell command or a set of Git options.
// Transport must write its start ACK before releasing the returned clone gate.
type RPCProjectCloneStartParams struct {
	OperationID string                `json:"operation_id"`
	Child       HostDirectoryIdentity `json:"child"`
	URL         string                `json:"url"`
}

type RPCProjectCancelParams struct {
	OperationID string `json:"operation_id"`
}

type HostOperationStatus string

const (
	HostOperationSucceeded     HostOperationStatus = "succeeded"
	HostOperationFailed        HostOperationStatus = "failed"
	HostOperationCanceled      HostOperationStatus = "canceled"
	HostOperationTimedOut      HostOperationStatus = "timed_out"
	HostOperationOutputLimit   HostOperationStatus = "output_limit"
	HostOperationCleanupFailed HostOperationStatus = "cleanup_failed"
)

// RPCProjectCompletion contains only fixed public status and the exact prepared
// identity. It never contains Git output, raw errors, credentials or process IDs.
// A terminal response is not proof that the original pathname remains bound to
// Child; the durable manager must reconcile identity before registration.
type RPCProjectCompletion struct {
	OperationID string                `json:"operation_id"`
	Child       HostDirectoryIdentity `json:"child"`
	Status      HostOperationStatus   `json:"status"`
}
