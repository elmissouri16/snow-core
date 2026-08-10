package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		Description: "Read a UTF-8 text file within allowed roots. Use offset and limit for large files.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": { "type": "string", "description": "Absolute or cwd-relative path of the file to read." },
    "offset": { "type": "integer", "description": "1-based start line. Defaults to the first line." },
    "limit": { "type": "integer", "description": "Maximum number of lines to return." }
  }
}`),
	}
}

// Run implements tools.Tool. It probes and streams the file instead of
// loading an entire large file into memory. Offset/limit reads stop as soon as
// the requested window or output budget is satisfied.
func (r *Read) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.ErrorResult(fmt.Errorf("read: invalid arguments: %w", err)), nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return tools.ErrorResult(fmt.Errorf("read: path is required")), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}

	// Re-anchor the guard to the host at call time. The registry guard is only
	// a fallback because an embedded SDK may have a different cwd and roots.
	guard := r.Guard
	if guard == nil && host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
		defer guard.Close()
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("read: no path guard configured")), nil
	}
	rooted, err := guard.rooted(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read: %w", err)), nil
	}

	file, _, err := openRootedRegular(rooted.root, rooted.name)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Errorf("read: file %q does not exist", a.Path)), nil
		}
		return tools.ErrorResult(fmt.Errorf("read: %q is not a regular file: %w", a.Path, err)), nil
	}
	defer file.Close()
	emitProgress(host, "reading file", false, false)
	defer emitProgress(host, "read finished", true, false)

	binary, err := binaryProbe(file, ctx)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read: %w", err)), nil
	}
	if binary {
		return tools.ErrorResult(fmt.Errorf("read: %q appears to be a binary file", a.Path)), nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return tools.ErrorResult(fmt.Errorf("read: reset file: %w", err)), nil
	}

	maxBytes := r.MaxOutputBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxOutputBytes
	}

	var content []byte
	var truncated bool
	if a.Offset == nil && a.Limit == nil {
		// Read only enough to determine whether content exceeds the cap. A few
		// extra bytes let truncateRunes back up from a split UTF-8 rune.
		readLimit := maxBytes + utf8.UTFMax
		if readLimit < maxBytes { // integer overflow guard
			readLimit = maxBytes
		}
		content, err = readUpTo(ctx, file, readLimit)
		truncated = len(content) > maxBytes
	} else {
		content, truncated, err = readLineWindow(ctx, file, a.Offset, a.Limit, maxBytes)
	}
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("read: %w", err)), nil
	}
	if truncated {
		content = append([]byte(truncateRunes(string(content), maxBytes)), truncationMarker...)
	}
	return tools.TextResult(string(content)), nil
}

// binaryProbe checks only the first 8 KiB and leaves the file at an
// unspecified offset; callers reset it before reading content.
func binaryProbe(file *os.File, ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	probe := make([]byte, 8192)
	n, err := file.Read(probe)
	if err != nil && err != io.EOF {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return bytes.IndexByte(probe[:n], 0) >= 0, nil
}

// readUpTo reads at most max bytes while checking cancellation between
// chunks. The caller can detect truncation by requesting max+UTFMax bytes.
func readUpTo(ctx context.Context, file *os.File, max int) ([]byte, error) {
	if max <= 0 {
		return nil, nil
	}
	chunkSize := 32 * 1024
	if max < chunkSize {
		chunkSize = max
	}
	data := make([]byte, 0, chunkSize)
	buf := make([]byte, chunkSize)
	for len(data) < max {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		want := max - len(data)
		if want < len(buf) {
			buf = buf[:want]
		}
		n, err := file.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if n == 0 {
			break
		}
	}
	return data, nil
}

// readLineWindow preserves the historical 1-based offset/limit behavior while
// avoiding allocation for lines outside the requested window. ReadSlice keeps
// even a single very long line bounded by the output budget.
func readLineWindow(ctx context.Context, file *os.File, offset, limit *int, maxBytes int) ([]byte, bool, error) {
	start := 1
	if offset != nil && *offset > 1 {
		start = *offset
	}
	lineLimit := -1
	if limit != nil && *limit >= 0 {
		lineLimit = *limit
	}
	if lineLimit == 0 {
		return nil, false, nil
	}

	reader := bufio.NewReaderSize(file, 32*1024)
	var out bytes.Buffer
	selected := 0
	lineNo := 0
	lineStarted := false
	lineSelected := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) == 0 && err == io.EOF && !lineStarted {
			break
		}
		if !lineStarted {
			lineStarted = true
			lineSelected = lineNo+1 >= start && (lineLimit < 0 || selected < lineLimit)
			if lineSelected && selected > 0 {
				if out.Len() >= maxBytes {
					return []byte(truncateRunes(out.String(), maxBytes)), true, nil
				}
				out.WriteByte('\n')
			}
		}

		if lineSelected && len(fragment) > 0 {
			body := fragment
			if err != bufio.ErrBufferFull && body[len(body)-1] == '\n' {
				body = body[:len(body)-1]
			}
			remaining := maxBytes - out.Len()
			if len(body) > remaining {
				if remaining > 0 {
					out.Write(body[:remaining])
				}
				return []byte(truncateRunes(out.String(), maxBytes)), true, nil
			}
			out.Write(body)
		}

		completeLine := err != bufio.ErrBufferFull
		if completeLine {
			if lineSelected {
				selected++
				if lineLimit >= 0 && selected >= lineLimit {
					return out.Bytes(), false, nil
				}
			}
			lineNo++
			lineStarted = false
			lineSelected = false
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			return nil, false, err
		}
	}
	return out.Bytes(), false, nil
}

// truncateRunes truncates to the largest rune-boundary prefix of the given
// byte budget, so multi-byte UTF-8 runes are never split.
func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		if maxBytes <= 0 {
			return ""
		}
		return s
	}
	b := []byte(s)[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// isBinary detects NUL bytes in the first 8KiB of an already-read buffer.
func isBinary(data []byte) bool {
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	return bytes.IndexByte(probe, 0) >= 0
}

// sliceLines is retained for package-level callers and tests that need the
// simple in-memory equivalent of readLineWindow.
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
