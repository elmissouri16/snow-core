package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// registerProcessControlRoutes is intentionally separate from runtimeAction:
// no generic RPC forwarding and no browser process-launch endpoint.
func (s *shell) registerProcessControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /projects/{project}/processes/{action}", s.processControlHTTP)
}
func (s *shell) processControlHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeFormLimit(w, r, 4096); !ok {
		return
	}
	backend, ok := s.runtimes.(RuntimeProcessBackend)
	if !ok || s.registry == nil {
		http.Error(w, "Managed process controls unavailable", http.StatusServiceUnavailable)
		return
	}
	instance, session := r.PostForm.Get("instance_id"), r.PostForm.Get("session_id")
	action := r.PathValue("action")
	allowed := map[string]bool{"csrf": true, "instance_id": true, "session_id": true}
	switch action {
	case "list":
	case "logs":
		allowed["process_id"], allowed["cursor"], allowed["max_bytes"] = true, true, true
	case "stop":
		allowed["process_id"], allowed["grace_ms"] = true, true
	default:
		http.NotFound(w, r)
		return
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "Invalid managed process request", http.StatusBadRequest)
			return
		}
	}
	if instance == "" || !runtimeOption(instance) || !protocol.ValidProcessControlSession(session) || len(r.URL.RawQuery) != 0 {
		http.Error(w, runtimePublicError(ErrRuntimeInvalid), http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	project, err := s.registry.Lookup(ctx, r.PathValue("project"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Unlike launch/edit, stopping an existing owned process remains useful when
	// its directory disappears. Registry ID and exact live session still bind it.
	if !s.processHTTPCurrent(project.ID, instance, session) {
		http.Error(w, runtimePublicError(ErrRuntimeInvalid), http.StatusConflict)
		return
	}
	var result any
	switch action {
	case "list":
		var value protocol.RPCProcessControlList
		value, err = backend.ProcessList(ctx, project.ID, instance, protocol.RPCProcessControlListParams{SessionID: session})
		if err == nil && (value.SessionID != session || !validProcessList(value)) {
			err = ErrRuntimeUnavailable
		}
		result = value
	case "logs":
		p := protocol.RPCProcessControlLogsParams{SessionID: session, ProcessID: r.PostForm.Get("process_id")}
		if r.PostForm.Has("cursor") {
			n, parseErr := processFormInt(r, "cursor")
			if parseErr != nil {
				err = parseErr
			} else {
				p.Cursor = new(n)
			}
		}
		if r.PostForm.Has("max_bytes") {
			n, parseErr := processFormInt(r, "max_bytes")
			if parseErr != nil || n > protocol.ProcessControlMaxBytes {
				err = ErrRuntimeInvalid
			} else {
				p.MaxBytes = int(n)
			}
		}
		if !p.Valid() {
			err = ErrRuntimeInvalid
		}
		if err == nil {
			var value protocol.RPCProcessControlLogs
			value, err = backend.ProcessLogs(ctx, project.ID, instance, p)
			if err == nil && !validProcessLogs(value, p) {
				err = ErrRuntimeUnavailable
			}
			value.Output = processPlainText(value.Output)
			result = value
		}
	case "stop":
		p := protocol.RPCProcessControlStopParams{SessionID: session, ProcessID: r.PostForm.Get("process_id")}
		if r.PostForm.Has("grace_ms") {
			n, parseErr := processFormInt(r, "grace_ms")
			if parseErr != nil || n > protocol.ProcessControlMaxGraceMS {
				err = ErrRuntimeInvalid
			} else {
				p.GraceMS = int(n)
			}
		}
		if !p.Valid() {
			err = ErrRuntimeInvalid
		}
		if err == nil {
			var value protocol.RPCProcessControlStop
			value, err = backend.ProcessStop(ctx, project.ID, instance, p)
			if err == nil && (value.SessionID != session || value.Process.ProcessID != p.ProcessID || !validProcessState(value.Process)) {
				err = ErrRuntimeUnavailable
			}
			result = value
		}
	}
	if err != nil || !s.processHTTPCurrent(project.ID, instance, session) {
		http.Error(w, runtimePublicError(ErrRuntimeInvalid), http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.runtimeJSON(w, struct {
		ProjectID  string `json:"project_id"`
		InstanceID string `json:"instance_id"`
		Result     any    `json:"result"`
	}{project.ID, instance, result})
}
func (s *shell) processHTTPCurrent(project, instance, session string) bool {
	snapshot, ok := s.runtimes.Snapshot(project)
	return ok && snapshot.ProjectID == project && snapshot.InstanceID == instance && snapshot.SessionID == session && snapshot.Status != "opening" && snapshot.Status != "closing" && snapshot.Status != "failed" && snapshot.Status != "switching"
}
func processFormInt(r *http.Request, key string) (int64, error) {
	text := r.PostForm.Get(key)
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != text {
		return 0, ErrRuntimeInvalid
	}
	return n, nil
}
