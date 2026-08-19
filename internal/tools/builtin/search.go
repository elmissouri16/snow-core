package builtin

import (
	"github.com/snow-core/snow/internal/config"
)

const (
	defaultSearchOutputBytes  = 256 * 1024
	defaultGrepMaxMatches     = 1000
	defaultGlobMaxResults     = 500
	searchLinePreviewBytes    = 300
	maxSearchLineBytes        = 1 << 20
	maxSearchPatternBytes     = 4096
	maxSearchGlobBytes        = 4096
	maxSearchDirectoryEntries = 100000
)

// Grep is a pure-Go content search tool (no external ripgrep dependency).
type Grep struct {
	guard *PathGuard
	// MaxOutputBytes caps combined result output.
	MaxOutputBytes int
	// MaxMatches bounds total matches returned.
	MaxMatches int
	Policy     config.EffectiveSearchPolicy
}

type grepArgs struct {
	Pattern        string   `json:"pattern"`
	Path           string   `json:"path"`
	Glob           string   `json:"glob"`
	IgnoreCase     bool     `json:"ignore_case"`
	MaxMatches     int      `json:"max_matches"`
	Hidden         *bool    `json:"hidden"`
	IncludeIgnored bool     `json:"include_ignored"`
	Exclude        []string `json:"exclude"`
}

// Glob is a bounded file path matching tool.
type Glob struct {
	guard *PathGuard
	// MaxOutputBytes caps the returned path list.
	MaxOutputBytes int
	// MaxResults bounds the number of returned paths.
	MaxResults int
	Policy     config.EffectiveSearchPolicy
}

type globArgs struct {
	Pattern        string   `json:"pattern"`
	Path           string   `json:"path"`
	MaxResults     int      `json:"max_results"`
	Hidden         *bool    `json:"hidden"`
	IncludeIgnored bool     `json:"include_ignored"`
	Exclude        []string `json:"exclude"`
}

// globMatcher is a compiled path matcher. Compiling once per tool call is
// important when a repository contains thousands of files.
type globMatcher struct {
	patternParts []string
	basename     bool
}
