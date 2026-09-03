// Package skills implements the Agent Skills open format and its progressive
// disclosure lifecycle. Discovery loads metadata only; full instructions and
// bundled resources are read on demand by tools in this package.
package skills

import (
	"errors"
	"io/fs"
)

const (
	defaultMaxSkills      = 1000
	defaultMaxSkillFile   = 2 << 20
	defaultCatalogBytes   = 64 << 10
	catalogDisabledReason = "excluded by Agent Skills catalog byte limit"
)

var allowedFrontmatterFields = map[string]struct{}{
	"name": {}, "description": {}, "license": {}, "compatibility": {}, "metadata": {}, "allowed-tools": {},
}

// Skill is metadata for a discovered Agent Skill. Body is deliberately not
// retained so edits are picked up at activation time.
type Skill struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
	Location      string            `json:"location"`
	Directory     string            `json:"directory"`
	Scope         string            `json:"scope"`
	Source        string            `json:"source"`
	Enabled       bool              `json:"enabled"`
	DisabledBy    string            `json:"disabled_by,omitempty"`
	rank          int
	identity      fs.FileInfo
}

// Diagnostic records malformed or shadowed skills without aborting startup.
type Diagnostic struct {
	Path    string `json:"path,omitempty"`
	Skill   string `json:"skill,omitempty"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Options controls bounded filesystem discovery.
type Options struct {
	CWD              string
	Home             string
	SnowHome         string
	ProjectTrusted   bool
	ExtraDirs        []string
	IncludeClaude    bool
	Disabled         bool
	DisabledReason   string
	Overrides        map[string]bool
	OverrideReasons  map[string]string
	MaxSkills        int
	MaxSkillFileSize int64
	MaxCatalogBytes  int
}

// Registry is the deterministic, collision-resolved skill catalog.
type Registry struct {
	byName      map[string]Skill
	ordered     []Skill
	allByName   map[string]Skill
	allOrdered  []Skill
	diagnostics []Diagnostic
	maxFileSize int64
}

type sourceDir struct {
	path   string
	scope  string
	source string
	rank   int
}

type frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	AllowedTools  string            `yaml:"allowed-tools"`
}

var errNonconformant = errors.New("Agent Skills frontmatter is nonconformant")
