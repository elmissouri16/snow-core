package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	defaultToolSearchLimit = 5
	maxToolSearchLimit     = 5
	toolSearchCandidates   = 20
	maxToolSearchQuery     = 4096
)

// SearchTools is the small always-loaded recovery tool for deferred schemas.
type SearchTools struct {
	Router   tools.Router
	Registry tools.Registry
}

// NewSearchTools creates a metadata-only tool search capability.
func NewSearchTools(router tools.Router, registry tools.Registry) *SearchTools {
	return &SearchTools{Router: router, Registry: registry}
}

func (s *SearchTools) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "search_tools",
		Description: "Search metadata for currently deferred tools by capability or operation. Selected schemas become callable on the next model step; this does not execute the matched tools.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "Capability or operation to find."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 5, "default": 5}
  }
}`),
	}
}

func (s *SearchTools) Run(ctx context.Context, raw json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.ErrorResult(fmt.Errorf("search_tools: invalid arguments: %w", err)), nil
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return tools.ErrorResult(fmt.Errorf("search_tools: query is required")), nil
	}
	if len(args.Query) > maxToolSearchQuery {
		return tools.ErrorResult(fmt.Errorf("search_tools: query exceeds %d bytes", maxToolSearchQuery)), nil
	}
	if args.Limit == 0 {
		args.Limit = defaultToolSearchLimit
	}
	if args.Limit < 1 || args.Limit > maxToolSearchLimit {
		return tools.ErrorResult(fmt.Errorf("search_tools: limit must be between 1 and %d", maxToolSearchLimit)), nil
	}
	if s.Router == nil || s.Registry == nil {
		return tools.ErrorResult(fmt.Errorf("search_tools: router unavailable")), nil
	}

	started := time.Now()
	count := s.Router.DeferredCount()
	candidateLimit := min(count, max(toolSearchCandidates, args.Limit))
	var candidates, selected []tools.ToolMatch
	for candidateLimit > 0 {
		matches, err := s.Router.Search(ctx, args.Query, candidateLimit)
		if err != nil {
			return tools.ErrorResult(err), nil
		}
		candidates = matches
		selected = selected[:0]
		for _, match := range matches {
			desc, ok := tools.Metadata(s.Registry, match.ID)
			if !ok || !desc.Deferred {
				continue
			}
			if host != nil && !tools.CanExposeMetadata(host.Permission(), desc) {
				continue
			}
			match.Description = boundRunes(match.Description, 240)
			selected = append(selected, match)
			if len(selected) == args.Limit {
				break
			}
		}
		if len(selected) >= args.Limit || candidateLimit >= count {
			break
		}
		candidateLimit = min(count, candidateLimit*2)
	}
	latency := time.Since(started).Milliseconds()

	content := struct {
		Tools []tools.ToolMatch `json:"tools"`
	}{Tools: selected}
	encoded, err := json.Marshal(content)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("search_tools: encode results: %w", err)), nil
	}
	if len(selected) == 0 {
		encoded = []byte(`{"tools":[],"message":"No permitted deferred tools matched. Try a more specific capability or service name."}`)
	}
	return tools.ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock(string(encoded))},
		Details: tools.DiscoveryDetails{Matches: selected, CandidateCount: len(candidates), LatencyMS: latency},
	}, nil
}

func boundRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max-1]) + "…"
}
