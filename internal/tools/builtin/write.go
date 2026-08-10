package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
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
		Description: "Create a new file or atomically overwrite an existing one within allowed roots.",
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

// Run implements tools.Tool. Writes are staged beside the destination and
// renamed into place, so a cancelled/failed write cannot leave a truncated
// destination behind. Existing permissions are preserved.
func (w *Write) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: invalid arguments: %w", err)), nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return tools.ErrorResult(fmt.Errorf("write: path is required")), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}

	// Re-anchor the guard to the host at call time. The registry guard is only
	// a fallback because an embedded SDK may have a different cwd and roots.
	guard := w.Guard
	if host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("write: no path guard configured")), nil
	}
	resolved, err := guard.Resolve(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("write: %w", err)), nil
	}

	mode := os.FileMode(0o644)
	hasExisting := false
	var before string
	if info, statErr := os.Stat(resolved); statErr == nil {
		if info.IsDir() {
			return tools.ErrorResult(fmt.Errorf("write: %q is a directory", a.Path)), nil
		}
		if !info.Mode().IsRegular() {
			return tools.ErrorResult(fmt.Errorf("write: %q is not a regular file", a.Path)), nil
		}
		mode = info.Mode().Perm()
		hasExisting = true
		data, readErr := os.ReadFile(resolved)
		if readErr != nil {
			return tools.ErrorResult(fmt.Errorf("write: read existing file: %w", readErr)), nil
		}
		before = string(data)
	} else if !os.IsNotExist(statErr) {
		return tools.ErrorResult(fmt.Errorf("write: %w", statErr)), nil
	}

	parent := filepath.Dir(resolved)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: create parent dirs: %w", err)), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}
	emitProgress(host, "writing file", false, false)
	defer emitProgress(host, "write finished", true, false)

	tmp, err := os.CreateTemp(parent, ".snow-write-*")
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("write: create temporary file: %w", err)), nil
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return tools.ErrorResult(fmt.Errorf("write: set permissions: %w", err)), nil
	}
	if err := writeAll(ctx, tmp, []byte(a.Content)); err != nil {
		_ = tmp.Close()
		return tools.ErrorResult(fmt.Errorf("write: %w", err)), nil
	}
	// Sync before rename: the destination is never replaced by data that has
	// not reached the filesystem, while the common path still uses one write.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return tools.ErrorResult(fmt.Errorf("write: sync: %w", err)), nil
	}
	if err := ctx.Err(); err != nil {
		_ = tmp.Close()
		return tools.ErrorResult(err), nil
	}
	if err := tmp.Close(); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: close: %w", err)), nil
	}
	if err := renameReplace(tmpName, resolved); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: replace %q: %w", a.Path, err)), nil
	}
	cleanup = false
	result := tools.ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Wrote %d bytes to %s", len(a.Content), a.Path))},
	}
	if hasExisting {
		result.Details = tools.DiffDetails{Path: a.Path, Diff: contentDiff(before, a.Content)}
	}
	return result, nil
}

func writeAll(ctx context.Context, dst io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := dst.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return ctx.Err()
}
