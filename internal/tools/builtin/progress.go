package builtin

import "github.com/snow-core/snow/internal/tools"

// emitProgress is intentionally best-effort. Tools remain usable directly in
// tests and SDK integrations that do not provide a host progress sink.
func emitProgress(host tools.ToolHost, message string, done, isError bool) {
	if host == nil {
		return
	}
	host.EmitProgress(tools.ToolProgressEvent{
		Message: message,
		Done:    done,
		IsError: isError,
	})
}
