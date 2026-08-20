package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	if guard == nil && host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
		defer guard.Close()
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("write: no path guard configured")), nil
	}
	rooted, err := guard.rooted(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("write: %w", err)), nil
	}

	mode := os.FileMode(0o644)
	hasExisting := false
	var before string
	previewAvailable := false
	if file, info, openErr := openRootedRegular(rooted.root, rooted.name); openErr == nil {
		mode = info.Mode().Perm()
		hasExisting = true

		var unchanged bool
		var readErr error
		if info.Size() == int64(len(a.Content)) && (info.Size() > maxDiffInputBytes || len(a.Content) > maxDiffInputBytes) {
			unchanged, readErr = readerMatchesString(ctx, file, a.Content)
		} else {
			var data []byte
			data, readErr = readUpTo(ctx, file, maxDiffInputBytes+1)
			if len(data) <= maxDiffInputBytes {
				before = string(data)
				previewAvailable = true
				unchanged = before == a.Content
			}
		}
		closeErr := file.Close()
		if readErr != nil {
			return tools.ErrorResult(fmt.Errorf("write: read existing file: %w", readErr)), nil
		}
		if closeErr != nil {
			return tools.ErrorResult(fmt.Errorf("write: close existing file: %w", closeErr)), nil
		}
		if unchanged {
			return tools.ToolResult{
				Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("No changes needed for %s", a.Path))},
			}, nil
		}
	} else if !os.IsNotExist(openErr) {
		return tools.ErrorResult(fmt.Errorf("write: %q is not a regular file: %w", a.Path, openErr)), nil
	}

	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}
	emitProgress(host, "writing file", false, false)
	defer emitProgress(host, "write finished", true, false)
	if err := atomicReplaceRooted(ctx, rooted, []byte(a.Content), mode, hasExisting); err != nil {
		return tools.ErrorResult(fmt.Errorf("write: %w", err)), nil
	}
	result := tools.ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Wrote %d bytes to %s", len(a.Content), a.Path))},
	}
	if hasExisting && previewAvailable && len(a.Content) <= maxDiffInputBytes {
		result.Details = tools.DiffDetails{Path: a.Path, Diff: contentDiff(before, a.Content)}
	}
	return result, nil
}

func readerMatchesString(ctx context.Context, reader io.Reader, expected string) (bool, error) {
	const chunkSize = 32 * 1024
	buf := make([]byte, chunkSize)
	matched := 0
	for matched < len(expected) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		want := len(expected) - matched
		if want > len(buf) {
			want = len(buf)
		}
		n, err := reader.Read(buf[:want])
		if n > 0 {
			if string(buf[:n]) != expected[matched:matched+n] {
				return false, nil
			}
			matched += n
		}
		if err != nil {
			if err == io.EOF {
				return matched == len(expected), nil
			}
			return false, err
		}
		if n == 0 {
			return false, io.ErrNoProgress
		}
	}
	var extra [1]byte
	n, err := reader.Read(extra[:])
	if err != nil && err != io.EOF {
		return false, err
	}
	return n == 0, nil
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
