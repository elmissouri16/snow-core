package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/tools"
)

// Read is the file-reading tool.
type Read struct {
	// MaxOutputBytes caps the returned file content. Defaults to 262144.
	MaxOutputBytes int
	// Guard confines file access; if nil the tool denies all paths.
	Guard *PathGuard
}

// DefaultMaxOutputBytes is the default tool result cap.
const DefaultMaxOutputBytes = 262144

// truncationMarker is appended when output exceeds the cap.
const truncationMarker = "\n... [output truncated]"

// NewRead returns a Read tool with defaults.
func NewRead(guard *PathGuard) *Read {
	return &Read{MaxOutputBytes: DefaultMaxOutputBytes, Guard: guard}
}

// readArgs is the JSON schema payload for read.
type readArgs struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset"`
	Limit  *int   `json:"limit"`
}

// Schema implements tools.Tool.
func (r *Read) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "read",
		Description: "Read a UTF-8 text file within allowed roots.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": { "type": "string", "description": "Absolute or cwd-relative path of the file to read." },
    "offset": { "type": "integer", "description": "1-based start line. Only lines from this offset are returned." },
    "limit": { "type": "integer", "description": "Maximum number of lines to return." }
  }
}`),
	}
}

// Run implements tools.Tool.
func (r *Read) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("read: invalid arguments: %w", err)), nil
	}
	if a.Path == "" {
		return tools.ErrorResult(fmt.Errorf("read: path is required")), nil
	}

	guard := r.Guard
	if guard == nil && host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("read: no path guard configured")), nil
	}

	resolved, err := guard.Resolve(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read: %w", err)), nil
	}

	// Reject non-regular files (FIFOs, devices) so a blocking read cannot
	// hang the agent turn; also let cancellation abort early.
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Errorf("read: file %q does not exist", a.Path)), nil
		}
		return tools.ErrorResult(fmt.Errorf("read: %w", err)), nil
	}
	if !info.Mode().IsRegular() {
		return tools.ErrorResult(fmt.Errorf("read: %q is not a regular file", a.Path)), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read: %w", err)), nil
	}

	if isBinary(data) {
		return tools.ErrorResult(fmt.Errorf("read: %q appears to be a binary file", a.Path)), nil
	}

	content := string(data)
	if a.Offset != nil || a.Limit != nil {
		content = sliceLines(content, a.Offset, a.Limit)
	}

	cap := r.MaxOutputBytes
	if cap <= 0 {
		cap = DefaultMaxOutputBytes
	}
	if len(content) > cap {
		content = truncateRunes(content, cap) + truncationMarker
	}

	return tools.TextResult(content), nil
}

// truncateRunes truncates to the largest rune-boundary prefix of the given
// byte budget, so multi-byte UTF-8 runes are never split.
func truncateRunes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := []byte(s)[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// isBinary detects NUL bytes in the first 8KiB.
func isBinary(data []byte) bool {
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	return bytes.IndexByte(probe, 0) >= 0
}

// sliceLines applies 1-based offset and limit to a line-split view of content.
func sliceLines(content string, offset, limit *int) string {
	lines := strings.Split(content, "\n")
	start := 0
	if offset != nil && *offset > 1 {
		start = *offset - 1
		if start > len(lines) {
			start = len(lines)
		}
	}
	end := len(lines)
	if limit != nil && *limit >= 0 {
		if l := start + *limit; l < end {
			end = l
		}
	}
	if start > end {
		start = end
	}
	return strings.Join(lines[start:end], "\n")
}
