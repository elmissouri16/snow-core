package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/snow-core/snow/internal/tools"
)

// Edit is the exact string replace tool.
type Edit struct {
	// Guard confines file access; if nil the tool denies all paths.
	Guard *PathGuard
}

// NewEdit returns an Edit tool.
func NewEdit(guard *PathGuard) *Edit {
	return &Edit{Guard: guard}
}

// editArgs is the JSON schema payload for edit.
type editArgs struct {
	Path       string `json:"path"`
	OldStr     string `json:"old_str"`
	NewStr     string `json:"new_str"`
	ReplaceAll bool   `json:"replace_all"`
}

// Schema implements tools.Tool.
func (e *Edit) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "edit",
		Description: "Replace an exact string occurrence in a file within allowed roots.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "required": ["path", "old_str", "new_str"],
  "properties": {
    "path": { "type": "string", "description": "Absolute or cwd-relative path of the file to edit." },
    "old_str": { "type": "string", "description": "Exact text to replace. Must appear exactly once unless replace_all is true." },
    "new_str": { "type": "string", "description": "Replacement text." },
    "replace_all": { "type": "boolean", "default": false, "description": "Replace every occurrence instead of failing on ambiguity." }
  }
}`),
	}
}

// Run implements tools.Tool.
func (e *Edit) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var a editArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: invalid arguments: %w", err)), nil
	}
	if a.Path == "" {
		return tools.ErrorResult(fmt.Errorf("edit: path is required")), nil
	}
	if a.OldStr == "" {
		return tools.ErrorResult(fmt.Errorf("edit: old_str is required")), nil
	}

	// Always re-anchor the guard to the host at call time: the registry-built
	// guard captures the process cwd at registration, which is wrong when the
	// host CWD differs (SDK embedding, tests).
	guard := e.Guard
	if host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("edit: no path guard configured")), nil
	}

	resolved, err := guard.Resolve(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}

	// Reject non-regular files (FIFOs, devices) and honor cancellation.
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Errorf("edit: file %q does not exist", a.Path)), nil
		}
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}
	if !info.Mode().IsRegular() {
		return tools.ErrorResult(fmt.Errorf("edit: %q is not a regular file", a.Path)), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}

	content := string(data)
	count := strings.Count(content, a.OldStr)
	if count == 0 {
		return tools.ErrorResult(fmt.Errorf("edit: old_str not found in %q", a.Path)), nil
	}
	if count > 1 && !a.ReplaceAll {
		return tools.ErrorResult(fmt.Errorf("edit: old_str appears %d times in %q; use replace_all to replace all occurrences", count, a.Path)), nil
	}

	var updated string
	if a.ReplaceAll {
		updated = strings.ReplaceAll(content, a.OldStr, a.NewStr)
	} else {
		updated = strings.Replace(content, a.OldStr, a.NewStr, 1)
	}

	if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}

	return tools.TextResult(fmt.Sprintf("Replaced %d occurrence(s) in %s", count, a.Path)), nil
}
