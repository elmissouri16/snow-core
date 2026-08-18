package mcp

import (
	"context"
	"errors"
	"fmt"
)

// safeRuntimeError intentionally does not wrap transport or server errors:
// SDK errors may include URL credentials, query values, headers, child output,
// or other server-controlled text. The operation and configured server ID are
// sufficient for status/tool diagnostics; context termination remains useful
// and contains no remote text.
func (rt *serverRuntime) safeRuntimeError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp %s %s: %w", rt.spec.ID, operation, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("mcp %s %s: %w", rt.spec.ID, operation, context.DeadlineExceeded)
	}
	return fmt.Errorf("mcp %s %s failed", rt.spec.ID, operation)
}
