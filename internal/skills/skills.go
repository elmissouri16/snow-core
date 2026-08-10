// Package skills implements the Agent Skills open format and its progressive
// disclosure lifecycle. Discovery loads metadata only; full instructions and
// bundled resources are read on demand by tools in this package.
package skills

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultMaxSkills    = 1000
	defaultMaxSkillFile = 2 << 20
	defaultCatalogBytes = 64 << 10
)

var strictNameRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

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

// Discover scans standard user and project paths. Higher-ranked project and
// Snow-native locations deterministically override cross-client locations.
func Discover(opts Options) *Registry {
	maxSkills := opts.MaxSkills
	if maxSkills <= 0 {
		maxSkills = defaultMaxSkills
	}
	maxFile := opts.MaxSkillFileSize
	if maxFile <= 0 {
		maxFile = defaultMaxSkillFile
	}
	r := &Registry{byName: make(map[string]Skill), allByName: make(map[string]Skill), maxFileSize: maxFile}

	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	snowHome := opts.SnowHome
	if snowHome == "" && home != "" {
		snowHome = filepath.Join(home, ".snow")
	}
	var dirs []sourceDir
	if opts.IncludeClaude && home != "" {
		dirs = append(dirs, sourceDir{filepath.Join(home, ".claude", "skills"), "user", "claude", 10})
	}
	if home != "" {
		dirs = append(dirs, sourceDir{filepath.Join(home, ".agents", "skills"), "user", "agents", 20})
	}
	if snowHome != "" {
		dirs = append(dirs, sourceDir{filepath.Join(snowHome, "skills"), "user", "snow", 30})
	}
	for _, dir := range opts.ExtraDirs {
		dirs = append(dirs, sourceDir{dir, "explicit", "configured", 100})
	}
	if opts.ProjectTrusted && opts.CWD != "" {
		if opts.IncludeClaude {
			dirs = append(dirs, sourceDir{filepath.Join(opts.CWD, ".claude", "skills"), "project", "claude", 210})
		}
		dirs = append(dirs,
			sourceDir{filepath.Join(opts.CWD, ".agents", "skills"), "project", "agents", 220},
			sourceDir{filepath.Join(opts.CWD, ".snow", "skills"), "project", "snow", 230},
		)
	} else if opts.CWD != "" {
		for _, path := range []string{filepath.Join(opts.CWD, ".agents", "skills"), filepath.Join(opts.CWD, ".snow", "skills")} {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: path, Level: "warning", Message: "project skills require an explicit trust allow"})
			}
		}
	}

	seenLocations := make(map[string]bool)
	candidateCount := 0
scanDirs:
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir.path)
		if err != nil || seenLocations[abs] {
			continue
		}
		seenLocations[abs] = true
		entries, err := os.ReadDir(abs)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: abs, Level: "warning", Message: err.Error()})
			}
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if candidateCount >= maxSkills {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: abs, Level: "warning", Message: fmt.Sprintf("skill discovery stopped at %d candidates", maxSkills)})
				break scanDirs
			}
			candidateCount++
			location := filepath.Join(abs, entry.Name(), "SKILL.md")
			skill, diagnostics, err := parse(location, maxFile)
			r.diagnostics = append(r.diagnostics, diagnostics...)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					r.diagnostics = append(r.diagnostics, Diagnostic{Path: location, Level: "error", Message: err.Error()})
				}
				continue
			}
			skill.Scope, skill.Source, skill.rank = dir.scope, dir.source, dir.rank
			if prior, exists := r.allByName[skill.Name]; exists {
				if prior.rank > skill.rank {
					r.diagnostics = append(r.diagnostics, Diagnostic{Path: location, Skill: skill.Name, Level: "warning", Message: "skill shadowed by higher-precedence " + prior.Location})
					continue
				}
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: prior.Location, Skill: skill.Name, Level: "warning", Message: "skill shadowed by higher-precedence " + location})
			}
			r.allByName[skill.Name] = skill
		}
	}
	for _, skill := range r.allByName {
		skill.Enabled = !opts.Disabled
		if !skill.Enabled {
			skill.DisabledBy = opts.DisabledReason
			if skill.DisabledBy == "" {
				skill.DisabledBy = "skills disabled by configuration"
			}
		}
		if enabled, ok := opts.Overrides[skill.Name]; ok {
			skill.Enabled = enabled
			skill.DisabledBy = ""
			if !enabled {
				skill.DisabledBy = opts.OverrideReasons[skill.Name]
				if skill.DisabledBy == "" {
					skill.DisabledBy = "disabled by named skill policy"
				}
			}
		}
		r.allByName[skill.Name] = skill
		r.allOrdered = append(r.allOrdered, skill)
		if skill.Enabled {
			r.byName[skill.Name] = skill
			r.ordered = append(r.ordered, skill)
		}
	}
	sort.Slice(r.ordered, func(i, j int) bool { return r.ordered[i].Name < r.ordered[j].Name })
	sort.Slice(r.allOrdered, func(i, j int) bool { return r.allOrdered[i].Name < r.allOrdered[j].Name })
	return r
}

func parse(path string, maxBytes int64) (Skill, []Diagnostic, error) {
	data, err := readBounded(path, maxBytes)
	if err != nil {
		return Skill{}, nil, err
	}
	meta, _, err := split(data)
	if err != nil {
		return Skill{}, nil, err
	}
	var fm frontmatter
	if err := yaml.Unmarshal(meta, &fm); err != nil {
		return Skill{}, nil, fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	fm.Name = strings.TrimSpace(fm.Name)
	fm.Description = strings.TrimSpace(fm.Description)
	if fm.Name == "" {
		return Skill{}, nil, errors.New("missing required name")
	}
	if fm.Description == "" {
		return Skill{}, nil, errors.New("missing required description")
	}
	dir := filepath.Dir(path)
	skill := Skill{Name: fm.Name, Description: fm.Description, License: strings.TrimSpace(fm.License), Compatibility: strings.TrimSpace(fm.Compatibility), Metadata: fm.Metadata, AllowedTools: strings.TrimSpace(fm.AllowedTools), Location: path, Directory: dir}
	var diagnostics []Diagnostic
	if len(skill.Name) > 64 || !strictNameRE.MatchString(skill.Name) || strings.Contains(skill.Name, "--") {
		diagnostics = append(diagnostics, Diagnostic{Path: path, Skill: skill.Name, Level: "warning", Message: "name does not satisfy the Agent Skills naming constraints; loaded leniently"})
	}
	if filepath.Base(dir) != skill.Name {
		diagnostics = append(diagnostics, Diagnostic{Path: path, Skill: skill.Name, Level: "warning", Message: "name does not match parent directory; loaded leniently"})
	}
	if len(skill.Description) > 1024 {
		diagnostics = append(diagnostics, Diagnostic{Path: path, Skill: skill.Name, Level: "warning", Message: "description exceeds 1024 characters; loaded leniently"})
	}
	if len(skill.Compatibility) > 500 {
		diagnostics = append(diagnostics, Diagnostic{Path: path, Skill: skill.Name, Level: "warning", Message: "compatibility exceeds 500 characters; loaded leniently"})
	}
	return skill, diagnostics, nil
}

func split(data []byte) (meta, body []byte, err error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, nil, errors.New("SKILL.md must start with YAML frontmatter")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 {
		if bytes.HasSuffix(data, []byte("\n---")) {
			end = len(data[4:]) - len("\n---")
			end += 4
			return data[4:end], nil, nil
		}
		return nil, nil, errors.New("SKILL.md has no closing frontmatter delimiter")
	}
	end += 4
	return data[4:end], bytes.TrimSpace(data[end+5:]), nil
}

func readBounded(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds %d-byte limit", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d-byte limit", maxBytes)
	}
	return data, nil
}

// List returns a defensive, name-sorted skill catalog.
func (r *Registry) List() []Skill {
	if r == nil {
		return nil
	}
	return append([]Skill(nil), r.ordered...)
}

// Inventory returns every discovered effective skill, including entries that
// policy suppresses from prompts and activation tools.
func (r *Registry) Inventory() []Skill {
	if r == nil {
		return nil
	}
	return append([]Skill(nil), r.allOrdered...)
}

// Lookup returns discovered metadata regardless of its enabled state.
func (r *Registry) Lookup(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	skill, ok := r.allByName[name]
	return skill, ok
}

// ResourceSummary returns the bounded resource inventory size for a discovered
// skill without loading any resource contents.
func (r *Registry) ResourceSummary(name string) (count int, truncated bool, err error) {
	skill, ok := r.Lookup(name)
	if !ok {
		return 0, false, fmt.Errorf("unknown skill %q", name)
	}
	resources, truncated, err := listResources(skill.Directory, 200)
	return len(resources), truncated, err
}

// Get returns a discovered skill by frontmatter name.
func (r *Registry) Get(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	s, ok := r.byName[name]
	return s, ok
}

// Diagnostics returns discovery warnings and errors.
func (r *Registry) Diagnostics() []Diagnostic {
	if r == nil {
		return nil
	}
	return append([]Diagnostic(nil), r.diagnostics...)
}

// CatalogPrompt returns tier-one progressive disclosure for the system prompt.
func (r *Registry) CatalogPrompt() string {
	if r == nil || len(r.ordered) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The following Agent Skills provide specialized instructions. When a task matches a description, or the user explicitly names a skill as $skill-name, call activate_skill with its name before proceeding. Use read_skill_resource for referenced files. Relative paths are rooted at the activated skill directory.\n<available_skills>\n")
	truncated := false
	for _, skill := range r.ordered {
		var entry strings.Builder
		entry.WriteString("  <skill><name>")
		_ = xml.EscapeText(&entry, []byte(skill.Name))
		entry.WriteString("</name><description>")
		description := skill.Description
		if len(description) > 1024 {
			description = strings.ToValidUTF8(description[:1024], "")
		}
		_ = xml.EscapeText(&entry, []byte(description))
		entry.WriteString("</description></skill>\n")
		if b.Len()+entry.Len()+len("  <truncated>true</truncated>\n</available_skills>") > defaultCatalogBytes {
			truncated = true
			break
		}
		b.WriteString(entry.String())
	}
	if truncated {
		b.WriteString("  <truncated>true</truncated>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func (r *Registry) load(name string) (Skill, []byte, error) {
	skill, ok := r.Get(name)
	if !ok {
		return Skill{}, nil, fmt.Errorf("unknown skill %q", name)
	}
	data, err := readBounded(skill.Location, r.maxFileSize)
	if err != nil {
		return Skill{}, nil, err
	}
	_, body, err := split(data)
	return skill, body, err
}

func listResources(root string, limit int) ([]string, bool, error) {
	var resources []string
	truncated := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if strings.Count(rel, string(filepath.Separator)) >= 5 || entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "SKILL.md" {
			return nil
		}
		if len(resources) >= limit {
			truncated = true
			return nil
		}
		resources = append(resources, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(resources)
	return resources, truncated, err
}
