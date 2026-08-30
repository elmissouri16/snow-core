package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	artifactReadDefaultLines = 200
	artifactReadMaxLines     = 1000
	artifactReadMaxBytes     = 64 << 10
	artifactGrepMaxMatches   = 100
)

// ArtifactRead retrieves a bounded line window from the current session's
// private spill artifacts. It never accepts filesystem paths.
type ArtifactRead struct {
	Store   artifact.Store
	Current *SessionBinding
}

func NewArtifactRead(store artifact.Store, current *SessionBinding) *ArtifactRead {
	return &ArtifactRead{Store: store, Current: current}
}

func (t *ArtifactRead) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "artifact_read",
		Description: "Read a bounded line window from a private tool-result artifact in the current Snow session. Artifact IDs appear in pruned tool results; this tool never accepts filesystem paths.",
		Parameters:  json.RawMessage(`{"type":"object","required":["artifact_id"],"properties":{"artifact_id":{"type":"string","description":"Opaque artifact ID shown in a pruned tool result."},"offset":{"type":"integer","minimum":1,"default":1,"description":"1-based first line to return."},"limit":{"type":"integer","minimum":1,"maximum":1000,"default":200,"description":"Maximum lines to return; output is also capped at 64 KiB."}}}`),
		Discovery:   &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "artifacts", Keywords: []string{"artifact", "full tool result", "truncated output", "omitted output", "read artifact", "spill"}},
	}
}

func (t *ArtifactRead) Run(ctx context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.ErrorResult(fmt.Errorf("artifact_read: invalid arguments: %w", err)), nil
	}
	if args.Offset <= 0 {
		args.Offset = 1
	}
	if args.Limit <= 0 {
		args.Limit = artifactReadDefaultLines
	}
	if args.Limit > artifactReadMaxLines {
		return tools.ErrorResult(fmt.Errorf("artifact_read: limit exceeds %d", artifactReadMaxLines)), nil
	}
	reader, err := t.open(ctx, args.ArtifactID)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	defer reader.Close()
	out, start, end, total, err := artifactLineWindowReader(ctx, reader, args.Offset, args.Limit)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("artifact_read: scan: %w", err)), nil
	}
	if start >= total {
		return tools.TextResult(fmt.Sprintf("(offset %d beyond artifact; %d lines total)", args.Offset, total)), nil
	}
	if len(out) > artifactReadMaxBytes {
		out = artifactUTF8Prefix(out, artifactReadMaxBytes) + "\n[… read window truncated at 64 KiB …]"
	}
	return tools.TextResult(fmt.Sprintf("Artifact %s, lines %d-%d of %d:\n%s", args.ArtifactID, start+1, end, total, out)), nil
}

func artifactLineWindow(text string, offset, limit int) (string, int, int, int) {
	total := strings.Count(text, "\n") + 1
	startLine := offset - 1
	if startLine >= total {
		return "", startLine, startLine, total
	}
	startByte := 0
	for range startLine {
		next := strings.IndexByte(text[startByte:], '\n')
		startByte += next + 1
	}
	endLine := min(startLine+limit, total)
	endByte := startByte
	for line := startLine; line < endLine; line++ {
		next := strings.IndexByte(text[endByte:], '\n')
		if next < 0 {
			endByte = len(text)
			break
		}
		if line+1 == endLine {
			endByte += next
			break
		}
		endByte += next + 1
	}
	return text[startByte:endByte], startLine, endLine, total
}

func (t *ArtifactRead) open(ctx context.Context, id string) (io.ReadCloser, error) {
	if t == nil || t.Store == nil || t.Current == nil || t.Current.Current() == nil {
		return nil, errors.New("artifact: unavailable")
	}
	sessionID := t.Current.Current().ID()
	if opener, ok := t.Store.(artifact.TextOpener); ok {
		reader, _, err := opener.OpenText(ctx, sessionID, id)
		return reader, err
	}
	text, err := t.Store.ReadText(ctx, sessionID, id)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

func artifactLineWindowReader(ctx context.Context, reader io.Reader, offset, limit int) (string, int, int, int, error) {
	start := offset - 1
	line := 1
	buffered := bufio.NewReaderSize(reader, 64<<10)
	var out strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return "", start, start, 0, err
		}
		fragment, err := buffered.ReadSlice('\n')
		if line >= offset && line < offset+limit && out.Len() <= artifactReadMaxBytes {
			remaining := artifactReadMaxBytes + 1 - out.Len()
			if remaining > 0 {
				out.Write(fragment[:min(len(fragment), remaining)])
			}
		}
		if len(fragment) > 0 && fragment[len(fragment)-1] == '\n' {
			line++
		}
		switch {
		case err == nil, errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			value := strings.TrimSuffix(out.String(), "\n")
			total := line
			return value, start, min(start+limit, total), total, nil
		default:
			return "", start, start, 0, err
		}
	}
}

// ArtifactGrep searches one current-session artifact with RE2.
type ArtifactGrep struct {
	Store   artifact.Store
	Current *SessionBinding
}

func NewArtifactGrep(store artifact.Store, current *SessionBinding) *ArtifactGrep {
	return &ArtifactGrep{Store: store, Current: current}
}

func (t *ArtifactGrep) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "artifact_grep",
		Description: "Search a private tool-result artifact in the current Snow session with an RE2 regular expression. Returns bounded matching lines with line numbers.",
		Parameters:  json.RawMessage(`{"type":"object","required":["artifact_id","pattern"],"properties":{"artifact_id":{"type":"string","description":"Opaque artifact ID shown in a pruned tool result."},"pattern":{"type":"string","description":"Go RE2 expression to search for."},"ignore_case":{"type":"boolean","default":false,"description":"Perform case-insensitive matching."},"max_matches":{"type":"integer","minimum":1,"maximum":100,"default":50,"description":"Maximum matching lines to return."}}}`),
		Discovery:   &protocol.ToolDiscovery{Mode: protocol.ToolDiscoveryDeferred, Namespace: "artifacts", Keywords: []string{"artifact grep", "search artifact", "full tool result", "truncated output", "omitted output", "find in spill"}},
	}
}

func (t *ArtifactGrep) Run(ctx context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var args struct {
		ArtifactID string `json:"artifact_id"`
		Pattern    string `json:"pattern"`
		IgnoreCase bool   `json:"ignore_case"`
		MaxMatches int    `json:"max_matches"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.ErrorResult(fmt.Errorf("artifact_grep: invalid arguments: %w", err)), nil
	}
	if args.Pattern == "" || len(args.Pattern) > 4096 {
		return tools.ErrorResult(errors.New("artifact_grep: pattern must be 1..4096 bytes")), nil
	}
	if args.MaxMatches <= 0 {
		args.MaxMatches = 50
	}
	if args.MaxMatches > artifactGrepMaxMatches {
		return tools.ErrorResult(fmt.Errorf("artifact_grep: max_matches exceeds %d", artifactGrepMaxMatches)), nil
	}
	pattern := args.Pattern
	if args.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("artifact_grep: invalid RE2 pattern: %w", err)), nil
	}
	artifactReader := &ArtifactRead{Store: t.Store, Current: t.Current}
	reader, err := artifactReader.open(ctx, args.ArtifactID)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	defer reader.Close()
	var out strings.Builder
	buffered := bufio.NewReaderSize(reader, 64<<10)
	line, matches, skipped := 0, 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return tools.ErrorResult(err), nil
		}
		text, oversized, readErr := readBoundedSearchLine(ctx, buffered, maxSearchLineBytes)
		if len(text) > 0 || oversized {
			line++
			if oversized {
				skipped++
			} else {
				text = strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")
				if re.MatchString(text) {
					fmt.Fprintf(&out, "%d:%s\n", line, text)
					matches++
				}
			}
		}
		if matches >= args.MaxMatches || out.Len() >= artifactReadMaxBytes {
			break
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return tools.ErrorResult(fmt.Errorf("artifact_grep: scan: %w", readErr)), nil
		}
	}
	if skipped > 0 {
		fmt.Fprintf(&out, "[… %d line(s) larger than %d bytes skipped …]\n", skipped, maxSearchLineBytes)
	}
	if matches == 0 && skipped == 0 {
		return tools.TextResult("No matches."), nil
	}
	value := out.String()
	if len(value) > artifactReadMaxBytes {
		value = artifactUTF8Prefix(value, artifactReadMaxBytes) + "\n[… matches truncated …]"
	}
	return tools.TextResult(value), nil
}

func artifactUTF8Prefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	body := value[:limit]
	for len(body) > 0 && !utf8.ValidString(body) {
		body = body[:len(body)-1]
	}
	return body
}
