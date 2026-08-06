package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snow-core/snow/internal/tools"
)

// Write is the file-creation/overwrite tool.
type Write struct {
	// Guard confines file access; if nil the tool denies all paths.
	Guard *PathGuard
}

// NewWrite returns a Write tool.
func NewWrite(guard *PathGuard) *Write {
	return &Write{Guard: guard}
}

// writeArgs is the JSON schema payload for write.
type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Schema implements tools.Tool.
func (w *Write) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "write",
		Description: "Create a new file or overwrite an existing one within allowed roots.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": { "type": "string", "description": "Absolute or cwd-relative path of the file to write." },
    "content": { "type": "string", "description": "Full file content." }
  }
}`),
	}
}

// Run implements tools.Tool.
func (w *Write) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: invalid arguments: %w", err)), nil
	}
	if a.Path == "" {
		return tools.ErrorResult(fmt.Errorf("write: path is required")), nil
	}

	guard := w.Guard
	if guard == nil && host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("write: no path guard configured")), nil
	}

	resolved, err := guard.Resolve(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("write: %w", err)), nil
	}

	if dir := filepath.Dir(resolved); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return tools.ErrorResult(fmt.Errorf("write: create parent dirs: %w", err)), nil
		}
	}

	if err := os.WriteFile(resolved, []byte(a.Content), 0o644); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: %w", err)), nil
	}

	return tools.TextResult(fmt.Sprintf("Wrote %d bytes to %s", len(a.Content), a.Path)), nil
}
