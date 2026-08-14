package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

const (
	defaultMaxEditFileBytes    = 8 << 20
	defaultMaxEditReplacements = 10_000
)

// Edit is the exact string replace tool.
type Edit struct {
	// Guard confines file access; if nil the tool denies all paths.
	Guard *PathGuard
	// MaxFileBytes bounds both the existing and resulting file size.
	MaxFileBytes int
	// MaxReplacements bounds replace_all match expansion.
	MaxReplacements int
}

// NewEdit returns an Edit tool.
func NewEdit(guard *PathGuard) *Edit {
	return &Edit{
		Guard:           guard,
		MaxFileBytes:    defaultMaxEditFileBytes,
		MaxReplacements: defaultMaxEditReplacements,
	}
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
		Description: "Replace exact text in a bounded file within allowed roots. Existing and resulting files are limited to 8 MiB by default.",
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
	ctx = nonNilContext(ctx)
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
	maxFileBytes := e.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = defaultMaxEditFileBytes
	}
	maxReplacements := e.MaxReplacements
	if maxReplacements <= 0 {
		maxReplacements = defaultMaxEditReplacements
	}
	if len(a.OldStr) > maxFileBytes {
		return tools.ErrorResult(fmt.Errorf("edit: old_str exceeds the maximum editable size of %d bytes", maxFileBytes)), nil
	}
	if len(a.NewStr) > maxFileBytes {
		return tools.ErrorResult(fmt.Errorf("edit: new_str exceeds the maximum editable size of %d bytes", maxFileBytes)), nil
	}

	// Always re-anchor the guard to the host at call time: the registry-built
	// guard captures the process cwd at registration, which is wrong when the
	// host CWD differs (SDK embedding, tests).
	guard := e.Guard
	if guard == nil && host != nil {
		guard = NewPathGuard(host.Roots(), host.CWD())
		defer guard.Close()
	}
	if guard == nil {
		return tools.ErrorResult(fmt.Errorf("edit: no path guard configured")), nil
	}

	rooted, err := guard.rooted(a.Path)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}

	file, info, err := openRootedRegular(rooted.root, rooted.name)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Errorf("edit: file %q does not exist", a.Path)), nil
		}
		return tools.ErrorResult(fmt.Errorf("edit: %q is not a regular file: %w", a.Path, err)), nil
	}
	defer file.Close()
	if info.Size() > int64(maxFileBytes) {
		return tools.ErrorResult(fmt.Errorf("edit: file %q is %d bytes; maximum editable size is %d bytes", a.Path, info.Size(), maxFileBytes)), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}
	emitProgress(host, "editing file", false, false)
	defer emitProgress(host, "edit finished", true, false)

	data, err := readUpTo(ctx, file, boundedEditReadLimit(maxFileBytes))
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}
	if len(data) > maxFileBytes {
		return tools.ErrorResult(fmt.Errorf("edit: file %q exceeds the maximum editable size of %d bytes", a.Path, maxFileBytes)), nil
	}

	content := string(data)
	count := strings.Count(content, a.OldStr)
	if count == 0 {
		return tools.ErrorResult(fmt.Errorf("edit: old_str not found in %q", a.Path)), nil
	}
	if count > 1 && !a.ReplaceAll {
		return tools.ErrorResult(fmt.Errorf("edit: old_str appears %d times in %q; use replace_all to replace all occurrences", count, a.Path)), nil
	}
	if a.OldStr == a.NewStr {
		return tools.ToolResult{
			Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("No changes needed for %s", a.Path))},
		}, nil
	}
	if a.ReplaceAll && count > maxReplacements {
		return tools.ErrorResult(fmt.Errorf("edit: replace_all matches %d occurrences; maximum is %d", count, maxReplacements)), nil
	}
	replacements := 1
	if a.ReplaceAll {
		replacements = count
	}
	if growth := len(a.NewStr) - len(a.OldStr); growth > 0 && replacements > (maxFileBytes-len(content))/growth {
		return tools.ErrorResult(fmt.Errorf("edit: replacement would exceed the maximum editable size of %d bytes", maxFileBytes)), nil
	}
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}

	var updated string
	if a.ReplaceAll {
		updated = strings.ReplaceAll(content, a.OldStr, a.NewStr)
	} else {
		updated = strings.Replace(content, a.OldStr, a.NewStr, 1)
	}

	diff := editDiff(content, updated, a.OldStr, a.NewStr, a.ReplaceAll)
	if err := ctx.Err(); err != nil {
		return tools.ErrorResult(err), nil
	}
	if err := atomicReplaceRooted(ctx, rooted, []byte(updated), info.Mode().Perm(), true); err != nil {
		return tools.ErrorResult(fmt.Errorf("edit: %w", err)), nil
	}

	return tools.ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("Replaced %d occurrence(s) in %s", count, a.Path))},
		Details: tools.DiffDetails{Path: a.Path, Diff: diff},
	}, nil
}

func boundedEditReadLimit(maxFileBytes int) int {
	maxInt := int(^uint(0) >> 1)
	if maxFileBytes < maxInt {
		return maxFileBytes + 1
	}
	return maxFileBytes
}
