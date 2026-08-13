package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/artifact"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
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
		Parameters:  json.RawMessage(`{"type":"object","required":["artifact_id"],"properties":{"artifact_id":{"type":"string"},"offset":{"type":"integer","minimum":1,"default":1},"limit":{"type":"integer","minimum":1,"maximum":1000,"default":200}}}`),
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
	text, err := t.read(ctx, args.ArtifactID)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	lines := strings.Split(text, "\n")
	start := args.Offset - 1
	if start >= len(lines) {
		return tools.TextResult(fmt.Sprintf("(offset %d beyond artifact; %d lines total)", args.Offset, len(lines))), nil
	}
	end := min(start+args.Limit, len(lines))
	out := strings.Join(lines[start:end], "\n")
	if len(out) > artifactReadMaxBytes {
		out = artifactUTF8Prefix(out, artifactReadMaxBytes) + "\n[… read window truncated at 64 KiB …]"
	}
	return tools.TextResult(fmt.Sprintf("Artifact %s, lines %d-%d of %d:\n%s", args.ArtifactID, start+1, end, len(lines), out)), nil
}

func (t *ArtifactRead) read(ctx context.Context, id string) (string, error) {
	if t == nil || t.Store == nil || t.Current == nil || t.Current.Current() == nil {
		return "", errors.New("artifact: unavailable")
	}
	return t.Store.ReadText(ctx, t.Current.Current().ID(), id)
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
		Parameters:  json.RawMessage(`{"type":"object","required":["artifact_id","pattern"],"properties":{"artifact_id":{"type":"string"},"pattern":{"type":"string"},"ignore_case":{"type":"boolean"},"max_matches":{"type":"integer","minimum":1,"maximum":100,"default":50}}}`),
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
	reader := &ArtifactRead{Store: t.Store, Current: t.Current}
	text, err := reader.read(ctx, args.ArtifactID)
	if err != nil {
		return tools.ErrorResult(err), nil
	}
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	line, matches := 0, 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return tools.ErrorResult(err), nil
		}
		line++
		if !re.MatchString(scanner.Text()) {
			continue
		}
		fmt.Fprintf(&out, "%d:%s\n", line, scanner.Text())
		matches++
		if matches >= args.MaxMatches || out.Len() >= artifactReadMaxBytes {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return tools.ErrorResult(fmt.Errorf("artifact_grep: scan: %w", err)), nil
	}
	if matches == 0 {
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
