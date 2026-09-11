package protocol

// PluginSessionChanged is an observation of a committed active transition.
// Workflow data is deliberately excluded from this cross-plugin event.
type PluginSessionChanged struct {
	OldSessionID string `json:"old_session_id"`
	NewSessionID string `json:"new_session_id"`
	OldBranchID  string `json:"old_branch_id"`
	NewBranchID  string `json:"new_branch_id"`
	Reason       string `json:"reason"`
	Generation   uint64 `json:"generation"`
}
