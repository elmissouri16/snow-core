package web

import (
	"context"
	"regexp"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeProcessBackend is optional, live-only and session-CAS-bound on EVERY
// operation, including reads. It cannot activate a worker or launch a command.
type RuntimeProcessBackend interface {
	ProcessList(context.Context, string, string, protocol.RPCProcessControlListParams) (protocol.RPCProcessControlList, error)
	ProcessLogs(context.Context, string, string, protocol.RPCProcessControlLogsParams) (protocol.RPCProcessControlLogs, error)
	ProcessStop(context.Context, string, string, protocol.RPCProcessControlStopParams) (protocol.RPCProcessControlStop, error)
}

func (m *RuntimeManager) processRuntime(ctx context.Context, project, instance, session string) (*liveRuntime, error) {
	if !protocol.ValidProcessControlSession(session) {
		return nil, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, project, instance)
	if err != nil {
		return nil, err
	}
	if !r.processSessionCurrent(session) {
		r.control.Unlock()
		return nil, ErrRuntimeInvalid
	}
	return r, nil
}
func (r *liveRuntime) processSessionCurrent(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctx.Err() == nil && !r.transitioning && r.snapshot.SessionID == session && r.snapshot.Status != "failed" && r.snapshot.Status != "closing" && r.snapshot.Status != "opening"
}
func (m *RuntimeManager) ProcessList(ctx context.Context, project, instance string, p protocol.RPCProcessControlListParams) (protocol.RPCProcessControlList, error) {
	r, err := m.processRuntime(ctx, project, instance, p.SessionID)
	if err != nil {
		return protocol.RPCProcessControlList{}, err
	}
	defer r.control.Unlock()
	var result protocol.RPCProcessControlList
	if err := r.call(protocol.RPCRequest{Type: "process_control_list"}, p, &result); err != nil {
		return protocol.RPCProcessControlList{}, err
	}
	if !r.processSessionCurrent(p.SessionID) || result.SessionID != p.SessionID || !validProcessList(result) {
		return protocol.RPCProcessControlList{}, ErrRuntimeUnavailable
	}
	return result, nil
}
func (m *RuntimeManager) ProcessLogs(ctx context.Context, project, instance string, p protocol.RPCProcessControlLogsParams) (protocol.RPCProcessControlLogs, error) {
	if !p.Valid() {
		return protocol.RPCProcessControlLogs{}, ErrRuntimeInvalid
	}
	r, err := m.processRuntime(ctx, project, instance, p.SessionID)
	if err != nil {
		return protocol.RPCProcessControlLogs{}, err
	}
	defer r.control.Unlock()
	var result protocol.RPCProcessControlLogs
	if err := r.call(protocol.RPCRequest{Type: "process_control_logs"}, p, &result); err != nil {
		return protocol.RPCProcessControlLogs{}, err
	}
	if !r.processSessionCurrent(p.SessionID) || !validProcessLogs(result, p) {
		return protocol.RPCProcessControlLogs{}, ErrRuntimeUnavailable
	}
	result.Output = processPlainText(result.Output)
	return result, nil
}

// ProcessStop never retries or reopens a worker after a lost acknowledgment.
// call uses the admitted worker lifetime, not a disconnected browser lifetime.
func (m *RuntimeManager) ProcessStop(ctx context.Context, project, instance string, p protocol.RPCProcessControlStopParams) (protocol.RPCProcessControlStop, error) {
	if !p.Valid() {
		return protocol.RPCProcessControlStop{}, ErrRuntimeInvalid
	}
	r, err := m.processRuntime(ctx, project, instance, p.SessionID)
	if err != nil {
		return protocol.RPCProcessControlStop{}, err
	}
	defer r.control.Unlock()
	var result protocol.RPCProcessControlStop
	if err := r.call(protocol.RPCRequest{Type: "process_control_stop"}, p, &result); err != nil {
		return protocol.RPCProcessControlStop{}, err
	}
	if !r.processSessionCurrent(p.SessionID) || result.SessionID != p.SessionID || result.Process.ProcessID != p.ProcessID || !validProcessState(result.Process) {
		return protocol.RPCProcessControlStop{}, ErrRuntimeUnavailable
	}
	return result, nil
}
func validProcessState(p protocol.RPCManagedProcess) bool {
	return protocol.ValidManagedProcessID(p.ProcessID) && len(p.Name) <= 64 && p.Name == runtimeText(p.Name, 64) && processStatus(p.Status) && len(p.Signal) <= 64 && p.Signal == runtimeText(p.Signal, 64) && len(p.Reason) <= 64 && p.Reason == runtimeText(p.Reason, 64)
}
func processStatus(s string) bool { return s == "running" || s == "exited" || s == "stopped" }
func validProcessList(p protocol.RPCProcessControlList) bool {
	if !protocol.ValidProcessControlSession(p.SessionID) || len(p.Processes) > protocol.ProcessControlMaxRecords {
		return false
	}
	seen := make(map[string]bool, len(p.Processes))
	for _, item := range p.Processes {
		if !validProcessState(item) || seen[item.ProcessID] {
			return false
		}
		seen[item.ProcessID] = true
	}
	return true
}
func validProcessLogs(result protocol.RPCProcessControlLogs, p protocol.RPCProcessControlLogsParams) bool {
	limit := p.MaxBytes
	if limit == 0 {
		limit = protocol.ProcessControlMaxBytes
	}
	return result.SessionID == p.SessionID && result.ProcessID == p.ProcessID && processStatus(result.Status) && len(result.Output) <= limit && result.NextCursor >= 0 && result.Omitted >= 0 && (p.Cursor == nil || result.NextCursor >= *p.Cursor)
}

// Remove ANSI CSI/OSC before removing remaining control characters. Log cursor
// and omission counters remain byte offsets in the original managed output.
var processANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-_]`)

func processPlainText(text string) string {
	return runtimeText(processANSI.ReplaceAllString(text, ""), protocol.ProcessControlMaxBytes)
}
