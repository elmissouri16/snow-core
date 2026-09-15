package protocol

import "strings"

// Managed process controls address runtime-only opaque handles, never OS PIDs.
// Every request must name the current durable session. No command-launch
// operation exists in this protocol; inventory intentionally omits commands/env.
const (
	ProcessControlCapability = "process_control"
	ProcessControlMaxBytes   = 32 << 10
	ProcessControlMaxRecords = 128
	ProcessControlMaxGraceMS = 5000
)

type RPCProcessControlListParams struct {
	SessionID string `json:"session_id"`
}
type RPCProcessControlLogsParams struct {
	SessionID string `json:"session_id"`
	ProcessID string `json:"process_id"`
	Cursor    *int64 `json:"cursor,omitzero"`
	MaxBytes  int    `json:"max_bytes,omitzero"`
}
type RPCProcessControlStopParams struct {
	SessionID string `json:"session_id"`
	ProcessID string `json:"process_id"`
	GraceMS   int    `json:"grace_ms,omitzero"`
}
type RPCProcessControlList struct {
	SessionID string              `json:"session_id"`
	Processes []RPCManagedProcess `json:"processes"`
	Truncated bool                `json:"truncated"`
}
type RPCProcessControlLogs struct {
	SessionID  string `json:"session_id"`
	ProcessID  string `json:"process_id"`
	Status     string `json:"status"`
	Output     string `json:"output"`
	NextCursor int64  `json:"next_cursor"`
	Omitted    int64  `json:"omitted_bytes"`
	EOF        bool   `json:"eof"`
}
type RPCProcessControlStop struct {
	SessionID string            `json:"session_id"`
	Process   RPCManagedProcess `json:"process"`
}

func ValidProcessControlSession(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 || c == '/' || c == '\\' {
			return false
		}
	}
	return true
}
func ValidManagedProcessID(id string) bool {
	suffix, ok := strings.CutPrefix(id, "proc_")
	if !ok || len(suffix) != 32 {
		return false
	}
	for _, c := range suffix {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (p RPCProcessControlLogsParams) Valid() bool {
	return ValidProcessControlSession(p.SessionID) && ValidManagedProcessID(p.ProcessID) && (p.Cursor == nil || *p.Cursor >= 0) && (p.MaxBytes == 0 || p.MaxBytes >= 4 && p.MaxBytes <= ProcessControlMaxBytes)
}
func (p RPCProcessControlStopParams) Valid() bool {
	return ValidProcessControlSession(p.SessionID) && ValidManagedProcessID(p.ProcessID) && p.GraceMS >= 0 && p.GraceMS <= ProcessControlMaxGraceMS
}
